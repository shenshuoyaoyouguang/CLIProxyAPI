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
	// dispatchTimeout bounds how long the session read loop waits for a slow
	// consumer before failing that request. Silently dropping messages truncated
	// streams while still reporting success downstream.
	dispatchTimeout = 30 * time.Second
	// dispatchPollInterval is how often a blocked delivery retries. Sends are
	// attempted under the request lock, so a blocking channel send is not an
	// option: it would race with close and panic.
	dispatchPollInterval = 5 * time.Millisecond
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
}

// deliver hands msg to the consumer, waiting up to timeout when the buffer is
// full. It reports whether the message was accepted.
func (pr *pendingRequest) deliver(msg Message, sessionClosed <-chan struct{}, timeout time.Duration) bool {
	if pr == nil {
		return false
	}
	deadline := time.Now().Add(timeout)
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
		select {
		case <-sessionClosed:
			return false
		case <-time.After(dispatchPollInterval):
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
	conn.SetReadDeadline(time.Now().Add(readTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(readTimeout))
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
		if req.deliver(msg, s.closed, dispatchTimeout) {
			return
		}
		// The consumer stalled past the dispatch timeout. Fail this request
		// explicitly; dropping the message would hand the caller a truncated
		// stream with no error attached.
		if s.manager != nil {
			s.manager.logDebugf("wsrelay: consumer stalled for id %s (provider=%s), failing request", msg.ID, s.provider)
		}
		req.forceDeliver(Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Payload: map[string]any{"error": "wsrelay: consumer stalled, stream truncated"},
		})
		s.closePending(msg.ID)
		return
	}
	if isTerminalMessageType(msg.Type) {
		s.manager.logDebugf("wsrelay: received terminal message for unknown id %s (provider=%s)", msg.ID, s.provider)
	}
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
