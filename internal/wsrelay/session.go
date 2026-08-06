package wsrelay

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	readTimeout          = 60 * time.Second
	writeTimeout         = 10 * time.Second
	maxInboundMessageLen = 64 << 20 // 64 MiB
	heartbeatInterval    = 30 * time.Second
	// dispatchTimeout bounds how long a slow consumer gets to accept a message
	// before its request is failed. Silently dropping messages truncated streams
	// while still reporting success downstream. Delivery happens in a
	// per-request worker goroutine, so this budget never blocks the session
	// read loop.
	dispatchTimeout = 30 * time.Second
	// dispatchPollInterval is how often a blocked delivery retries. Sends are
	// attempted under the request lock, so a blocking channel send is not an
	// option: it would race with close and panic.
	dispatchPollInterval = 5 * time.Millisecond
	// maxStalledMessages caps how many messages may queue up for a consumer that
	// cannot keep up before the request is failed outright. It bounds the memory
	// a stalled request can hold while the delivery worker waits out
	// dispatchTimeout.
	maxStalledMessages = 512
)

var errClosed = errors.New("websocket session closed")

type pendingRequest struct {
	mu     sync.Mutex
	ch     chan Message
	closed bool
	// done is closed when the request lifecycle ends (terminal message
	// delivered, ctx cancelled, or session closed). The context-watchdog
	// goroutine selects on it so it does not leak until ctx/session close.
	done chan struct{}

	// stalled holds messages that could not be handed to the consumer
	// immediately (buffer full). A single worker goroutine drains it in FIFO
	// order so a slow consumer never reorders a stream, while the session read
	// loop keeps draining the socket. Both fields are guarded by mu.
	stalled       []Message
	stalledWorker bool
}

// deliver hands msg to the consumer, waiting up to timeout when the buffer is
// full. It reports whether the message was accepted.
func (pr *pendingRequest) deliver(msg Message, sessionClosed <-chan struct{}, timeout time.Duration) bool {
	if pr == nil {
		return false
	}
	deadline := time.Now().Add(timeout)
	var ticker *time.Ticker
	for {
		pr.mu.Lock()
		if pr.closed {
			pr.mu.Unlock()
			return false
		}
		select {
		case pr.ch <- msg:
			pr.mu.Unlock()
			return true
		default:
		}
		pr.mu.Unlock()
		if !time.Now().Before(deadline) {
			return false
		}
		// Reuse a single ticker across retries instead of allocating a fresh
		// timer per poll, which would build up thousands of timers for a message
		// stuck until dispatchTimeout. The ticker is only created once a delivery
		// actually stalls, keeping the fast path allocation-free.
		if ticker == nil {
			ticker = time.NewTicker(dispatchPollInterval)
			defer ticker.Stop()
		}
		select {
		case <-sessionClosed:
			return false
		case <-ticker.C:
		}
	}
}

// forceDeliver guarantees that a terminal message reaches the consumer, evicting
// buffered chunks when necessary. A truncated stream must still surface its
// terminal status instead of closing as if it had completed.
func (pr *pendingRequest) forceDeliver(msg Message) {
	if pr == nil {
		return
	}
	pr.mu.Lock()
	defer pr.mu.Unlock()
	if pr.closed {
		return
	}
	for i := 0; i <= cap(pr.ch); i++ {
		select {
		case pr.ch <- msg:
			return
		default:
		}
		select {
		case <-pr.ch:
		default:
		}
	}
}

func (pr *pendingRequest) close() {
	if pr == nil {
		return
	}
	pr.mu.Lock()
	defer pr.mu.Unlock()
	if pr.closed {
		return
	}
	pr.closed = true
	close(pr.ch)
	close(pr.done)
}

type session struct {
	conn       *websocket.Conn
	manager    *Manager
	provider   string
	id         string
	closed     chan struct{}
	closeOnce  sync.Once
	writeMutex sync.Mutex
	pending    sync.Map // map[string]*pendingRequest
}

func newSession(conn *websocket.Conn, mgr *Manager, id string) *session {
	s := &session{
		conn:     conn,
		manager:  mgr,
		provider: "",
		id:       id,
		closed:   make(chan struct{}),
	}
	conn.SetReadLimit(maxInboundMessageLen)
	if errReadDeadline := conn.SetReadDeadline(time.Now().Add(readTimeout)); errReadDeadline != nil {
		if mgr != nil {
			mgr.logWarnf("wsrelay: set initial read deadline: %v", errReadDeadline)
		}
	}
	conn.SetPongHandler(func(string) error {
		if errReadDeadline := conn.SetReadDeadline(time.Now().Add(readTimeout)); errReadDeadline != nil {
			if mgr != nil {
				mgr.logWarnf("wsrelay: pong handler set read deadline: %v", errReadDeadline)
			}
		}
		return nil
	})
	s.startHeartbeat()
	return s
}

func (s *session) startHeartbeat() {
	if s == nil || s.conn == nil {
		return
	}
	ticker := time.NewTicker(heartbeatInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-s.closed:
				return
			case <-ticker.C:
				s.writeMutex.Lock()
				err := s.conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(writeTimeout))
				s.writeMutex.Unlock()
				if err != nil {
					s.cleanup(err)
					return
				}
			}
		}
	}()
}

func (s *session) run() {
	defer s.cleanup(errClosed)
	for {
		// Re-arm the read deadline before every read. Only refreshing it in the
		// pong handler tore down active data-only clients that do not emit pongs
		// once the initial deadline expired.
		if err := s.conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			s.cleanup(err)
			return
		}
		var msg Message
		if err := s.conn.ReadJSON(&msg); err != nil {
			s.cleanup(err)
			return
		}
		s.dispatch(msg)
	}
}

func isTerminalMessageType(msgType string) bool {
	return msgType == MessageTypeHTTPResp || msgType == MessageTypeError || msgType == MessageTypeStreamEnd
}

func (s *session) closePending(id string) {
	if actual, loaded := s.pending.LoadAndDelete(id); loaded {
		actual.(*pendingRequest).close()
	}
}

func (s *session) dispatch(msg Message) {
	if msg.Type == MessageTypePing {
		_ = s.conn.SetReadDeadline(time.Now().Add(readTimeout))
		_ = s.send(context.Background(), Message{ID: msg.ID, Type: MessageTypePong})
		return
	}
	if value, ok := s.pending.Load(msg.ID); ok {
		req := value.(*pendingRequest)
		if isTerminalMessageType(msg.Type) {
			req.forceDeliver(msg)
			s.closePending(msg.ID)
			return
		}
		s.route(msg, req)
		return
	}
	if isTerminalMessageType(msg.Type) {
		s.manager.logDebugf("wsrelay: received terminal message for unknown id %s (provider=%s)", msg.ID, s.provider)
	}
}

// route hands a non-terminal message to the consumer. On the fast path the
// message is delivered directly; when the consumer buffer is full it is queued
// for the per-request delivery worker, so a slow consumer never blocks the
// session read loop. A request whose consumer falls too far behind is failed
// explicitly rather than stalling the read loop.
func (s *session) route(msg Message, req *pendingRequest) {
	req.mu.Lock()
	if req.closed {
		req.mu.Unlock()
		return
	}
	if req.stalledWorker {
		if len(req.stalled) >= maxStalledMessages {
			req.mu.Unlock()
			s.failStalled(msg.ID, req)
			return
		}
		req.stalled = append(req.stalled, msg)
		req.mu.Unlock()
		return
	}
	select {
	case req.ch <- msg:
		req.mu.Unlock()
		return
	default:
	}
	// The consumer buffer is full and no delivery worker is running: queue the
	// message and start the serialized worker so the read loop keeps draining
	// the socket.
	if len(req.stalled) >= maxStalledMessages {
		req.mu.Unlock()
		s.failStalled(msg.ID, req)
		return
	}
	req.stalled = append(req.stalled, msg)
	req.stalledWorker = true
	req.mu.Unlock()
	go s.stalledDeliveryWorker(msg.ID, req)
}

// stalledDeliveryWorker serially drains req.stalled for a consumer that could
// not keep up, preserving message order. The consumer has one cumulative
// dispatchTimeout budget from the first stall; when it is exhausted the request
// is failed explicitly instead of blocking the session read loop.
func (s *session) stalledDeliveryWorker(id string, req *pendingRequest) {
	deadline := time.Now().Add(dispatchTimeout)
	for {
		req.mu.Lock()
		if req.closed {
			req.mu.Unlock()
			return
		}
		if len(req.stalled) == 0 {
			// The consumer caught up; the read loop resumes direct delivery.
			req.stalledWorker = false
			req.mu.Unlock()
			return
		}
		msg := req.stalled[0]
		req.stalled = req.stalled[1:]
		req.mu.Unlock()

		if req.deliver(msg, s.closed, time.Until(deadline)) {
			continue
		}
		// The message was not accepted: the stall budget expired, the request was
		// closed, or the session is tearing down.
		select {
		case <-s.closed:
			return
		default:
		}
		req.mu.Lock()
		alreadyClosed := req.closed
		req.mu.Unlock()
		if alreadyClosed {
			return
		}
		s.failStalled(id, req)
		return
	}
}

// failStalled fails a request whose consumer could not keep up: it forces a
// terminal error to the consumer and closes the pending request.
func (s *session) failStalled(id string, req *pendingRequest) {
	if s.manager != nil {
		s.manager.logDebugf("wsrelay: consumer stalled for id %s (provider=%s), failing request", id, s.provider)
	}
	req.forceDeliver(Message{
		ID:      id,
		Type:    MessageTypeError,
		Payload: map[string]any{"error": "wsrelay: consumer stalled, stream truncated"},
	})
	s.closePending(id)
}

func (s *session) send(ctx context.Context, msg Message) error {
	select {
	case <-s.closed:
		return errClosed
	default:
	}
	s.writeMutex.Lock()
	defer s.writeMutex.Unlock()
	if err := s.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}
	if err := s.conn.WriteJSON(msg); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	return nil
}

func (s *session) request(ctx context.Context, msg Message) (<-chan Message, error) {
	if msg.ID == "" {
		return nil, fmt.Errorf("wsrelay: message id is required")
	}
	if _, loaded := s.pending.LoadOrStore(msg.ID, &pendingRequest{ch: make(chan Message, 8), done: make(chan struct{})}); loaded {
		return nil, fmt.Errorf("wsrelay: duplicate message id %s", msg.ID)
	}
	value, _ := s.pending.Load(msg.ID)
	req := value.(*pendingRequest)
	if err := s.send(ctx, msg); err != nil {
		if actual, loaded := s.pending.LoadAndDelete(msg.ID); loaded {
			req := actual.(*pendingRequest)
			req.close()
		}
		return nil, err
	}
	go func() {
		select {
		case <-ctx.Done():
			if actual, loaded := s.pending.LoadAndDelete(msg.ID); loaded {
				actual.(*pendingRequest).close()
			}
		case <-s.closed:
		case <-req.done:
		}
	}()
	return req.ch, nil
}

func (s *session) cleanup(cause error) {
	s.closeOnce.Do(func() {
		close(s.closed)
		s.pending.Range(func(key, value any) bool {
			req := value.(*pendingRequest)
			msg := Message{ID: key.(string), Type: MessageTypeError, Payload: map[string]any{"error": cause.Error()}}
			req.forceDeliver(msg)
			req.close()
			s.pending.Delete(key)
			return true
		})
		_ = s.conn.Close()
		if s.manager != nil {
			s.manager.handleSessionClosed(s, cause)
		}
	})
}
