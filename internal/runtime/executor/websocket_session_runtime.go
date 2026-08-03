package executor

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// websocketSessionRuntime is the shared execution-session lifecycle used by the
// xAI and Codex websocket executors. The two providers differ only in how they
// dial upstream, how they log, how they configure a freshly dialed connection,
// and the per-message read deadline; the connection reuse, reader pump,
// invalidation and session-close flows are identical.
type websocketSessionRuntime struct {
	store *codexWebsocketSessionStore
	// defaultStore is used when store is nil, mirroring the pre-refactor
	// fallback to the provider global session store.
	defaultStore *codexWebsocketSessionStore

	dial func(ctx context.Context, auth *cliproxyauth.Auth, wsURL string, headers http.Header) (*websocket.Conn, *websocketConnectionCloser, *http.Response, error)

	logConnected    func(sessionID string, authID string, wsURL string)
	logDisconnected func(sessionID string, authID string, wsURL string, reason string, err error)

	// onSessionRemoved is invoked after a session is removed from the store by
	// CloseExecutionSession, before the session is closed. The xAI executor uses
	// it to drop its per-session ID-state; other executors leave it nil.
	onSessionRemoved func(sessionID string)

	// pongWriteTimeout is the deadline for pong replies. Zero means no deadline.
	pongWriteTimeout time.Duration

	// readDeadline is applied before every upstream ReadMessage by the reader
	// pump and the no-session read path. Zero disables the deadline.
	readDeadline time.Duration

	// executorName prefixes error messages, e.g. "xai websockets executor".
	executorName string
}

func (r *websocketSessionRuntime) sessionStore() *codexWebsocketSessionStore {
	if r == nil {
		return nil
	}
	if r.store != nil {
		return r.store
	}
	return r.defaultStore
}

func (r *websocketSessionRuntime) getOrCreateSession(sessionID string) *codexWebsocketSession {
	sessionID = strings.TrimSpace(sessionID)
	if r == nil || sessionID == "" {
		return nil
	}
	store := r.sessionStore()
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.sessions == nil {
		store.sessions = make(map[string]*codexWebsocketSession)
	}
	if sess, ok := store.sessions[sessionID]; ok && sess != nil {
		return sess
	}
	sess := &codexWebsocketSession{
		sessionID:            sessionID,
		upstreamDisconnectCh: make(chan error, 1),
	}
	store.sessions[sessionID] = sess
	return sess
}

func (r *websocketSessionRuntime) UpstreamDisconnectChan(sessionID string) <-chan error {
	sess := r.getOrCreateSession(sessionID)
	if sess == nil {
		return nil
	}
	return sess.upstreamDisconnectCh
}

func (r *websocketSessionRuntime) ensureUpstreamConn(ctx context.Context, auth *cliproxyauth.Auth, sess *codexWebsocketSession, authID string, wsURL string, headers http.Header) (*websocket.Conn, *websocketConnectionCloser, *http.Response, error) {
	if sess == nil {
		return r.dial(ctx, auth, wsURL, headers)
	}

	if staleConn, staleCloser, staleAuthID, staleWSURL, staleLifecycle := detachMismatchedWebsocketSessionConn(sess, authID, wsURL); staleConn != nil {
		r.logDisconnected(sess.sessionID, staleAuthID, staleWSURL, "target_changed", nil)
		if staleCloser != nil {
			if errClose := staleCloser.Close(); errClose != nil {
				log.Errorf("%s: close stale websocket error: %v", r.executorName, errClose)
			}
		}
		if staleLifecycle != nil {
			staleLifecycle.End("target_changed")
		}
	}

	sess.connMu.Lock()
	conn := sess.conn
	closer := sess.connCloser
	readerConn := sess.readerConn
	sess.connMu.Unlock()
	if conn != nil {
		if readerConn != conn {
			sess.connMu.Lock()
			sess.readerConn = conn
			sess.connMu.Unlock()
			r.configureConn(sess, conn)
			go r.readUpstreamLoop(sess, conn)
		}
		return conn, closer, nil, nil
	}

	conn, closer, resp, errDial := r.dial(ctx, auth, wsURL, headers)
	if errDial != nil {
		return nil, closer, resp, errDial
	}

	sess.connMu.Lock()
	if sess.conn != nil {
		previous := sess.conn
		previousCloser := sess.connCloser
		sess.connMu.Unlock()
		if errClose := closer.Close(); errClose != nil {
			log.Errorf("%s: close websocket error: %v", r.executorName, errClose)
		}
		return previous, previousCloser, nil, nil
	}
	sess.conn = conn
	sess.connCloser = closer
	sess.wsURL = wsURL
	sess.authID = authID
	sess.readerConn = conn
	sess.connMu.Unlock()

	r.configureConn(sess, conn)
	go r.readUpstreamLoop(sess, conn)
	r.logConnected(sess.sessionID, authID, wsURL)
	return conn, closer, resp, nil
}

func (r *websocketSessionRuntime) configureConn(sess *codexWebsocketSession, conn *websocket.Conn) {
	if sess == nil || conn == nil {
		return
	}
	sess.resetUpstreamDisconnectError(conn)
	conn.SetPingHandler(func(appData string) error {
		sess.writeMu.Lock()
		defer sess.writeMu.Unlock()
		// Reply pongs from the same write lock to avoid concurrent writes.
		deadline := time.Time{}
		if r.pongWriteTimeout > 0 {
			deadline = time.Now().Add(r.pongWriteTimeout)
		}
		return conn.WriteControl(websocket.PongMessage, []byte(appData), deadline)
	})
	defaultCloseHandler := conn.CloseHandler()
	conn.SetCloseHandler(func(code int, text string) error {
		sess.setUpstreamDisconnectError(conn, &websocket.CloseError{Code: code, Text: text})
		return defaultCloseHandler(code, text)
	})
}

func (r *websocketSessionRuntime) readUpstreamLoop(sess *codexWebsocketSession, conn *websocket.Conn) {
	if r == nil || sess == nil || conn == nil {
		return
	}
	for {
		if r.readDeadline > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(r.readDeadline))
		}
		token := sess.nextReadToken(conn)
		msgType, payload, errRead := conn.ReadMessage()
		if errRead != nil {
			invalidate := func() {
				r.invalidateUpstreamConn(sess, conn, "upstream_disconnected", errRead)
			}
			invalidated := false
			ch, done := sess.activeForConn(conn)
			if ch != nil {
				invalidated = sendTerminalWebsocketRead(ch, done, codexWebsocketRead{conn: conn, err: errRead, token: token}, invalidate)
				if sess.clearActive(conn, ch) {
					close(ch)
				}
			}
			if !invalidated {
				invalidate()
			}
			return
		}

		if msgType != websocket.TextMessage {
			if msgType == websocket.BinaryMessage {
				errBinary := fmt.Errorf("%s: unexpected binary message", r.executorName)
				invalidate := func() {
					r.invalidateUpstreamConn(sess, conn, "unexpected_binary", errBinary)
				}
				invalidated := false
				ch, done := sess.activeForConn(conn)
				if ch != nil {
					invalidated = sendTerminalWebsocketRead(ch, done, codexWebsocketRead{conn: conn, err: errBinary, token: token}, invalidate)
					if sess.clearActive(conn, ch) {
						close(ch)
					}
				}
				if !invalidated {
					invalidate()
				}
				return
			}
			continue
		}

		ch, done := sess.activeForConn(conn)
		if ch == nil {
			continue
		}
		select {
		case ch <- codexWebsocketRead{conn: conn, msgType: msgType, payload: payload, token: token}:
		case <-done:
		}
	}
}

func (r *websocketSessionRuntime) invalidateUpstreamConn(sess *codexWebsocketSession, conn *websocket.Conn, reason string, err error) {
	r.invalidateUpstreamConnWithNotify(sess, conn, reason, err, true)
}

func (r *websocketSessionRuntime) invalidateUpstreamConnWithoutDisconnectNotify(sess *codexWebsocketSession, conn *websocket.Conn, reason string, err error) {
	r.invalidateUpstreamConnWithNotify(sess, conn, reason, err, false)
}

func (r *websocketSessionRuntime) invalidateUpstreamConnWithNotify(sess *codexWebsocketSession, conn *websocket.Conn, reason string, err error, notify bool) {
	if sess == nil || conn == nil {
		return
	}

	sess.connMu.Lock()
	current := sess.conn
	authID := sess.authID
	wsURL := sess.wsURL
	sessionID := sess.sessionID
	if current == nil || current != conn {
		sess.connMu.Unlock()
		return
	}
	lifecycle := sess.lifecycle
	closer := sess.connCloser
	sess.lifecycle = nil
	sess.lifecycleModel = ""
	sess.conn = nil
	sess.connCloser = nil
	if sess.readerConn == conn {
		sess.readerConn = nil
	}
	sess.connMu.Unlock()

	sess.removeReadToken(conn)

	r.logDisconnected(sessionID, authID, wsURL, reason, err)
	if notify {
		sess.notifyUpstreamDisconnect(err)
	}
	if closer != nil {
		if errClose := closer.Close(); errClose != nil {
			log.Errorf("%s: close websocket error: %v", r.executorName, errClose)
		}
	}
	if lifecycle != nil {
		lifecycle.End(reason)
	}
}

func (r *websocketSessionRuntime) CloseExecutionSession(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if r == nil || sessionID == "" {
		return
	}
	if sessionID == cliproxyauth.CloseAllExecutionSessionsID {
		r.closeAllExecutionSessions("executor_shutdown")
		return
	}

	store := r.sessionStore()
	store.mu.Lock()
	sess := store.sessions[sessionID]
	delete(store.sessions, sessionID)
	store.mu.Unlock()
	if r.onSessionRemoved != nil {
		r.onSessionRemoved(sessionID)
	}

	r.closeExecutionSession(sess, "session_closed")
}

func (r *websocketSessionRuntime) closeAllExecutionSessions(reason string) {
	if r == nil {
		return
	}

	store := r.sessionStore()
	store.mu.Lock()
	sessionIDs := make([]string, 0, len(store.sessions))
	sessions := make([]*codexWebsocketSession, 0, len(store.sessions))
	for sessionID, sess := range store.sessions {
		sessionIDs = append(sessionIDs, sessionID)
		delete(store.sessions, sessionID)
		if sess != nil {
			sessions = append(sessions, sess)
		}
	}
	store.mu.Unlock()

	// Invoke the provider callback outside the store lock, mirroring the
	// single-session path: a callback must not run under the global session
	// lock, which would couple provider-state locking to the session store's.
	if r.onSessionRemoved != nil {
		for i := range sessionIDs {
			r.onSessionRemoved(sessionIDs[i])
		}
	}

	for i := range sessions {
		r.closeExecutionSession(sessions[i], reason)
	}
}

func (r *websocketSessionRuntime) closeExecutionSession(sess *codexWebsocketSession, reason string) {
	closeWebsocketSession(sess, reason, r.logDisconnected, r.executorName)
}

// closeWebsocketSession closes a session's upstream connection and ends its
// bound execution lifecycle, logging the disconnect through the provider
// callback.
func closeWebsocketSession(sess *codexWebsocketSession, reason string, logDisconnected func(sessionID string, authID string, wsURL string, reason string, err error), executorName string) {
	if sess == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "session_closed"
	}

	sess.connMu.Lock()
	conn := sess.conn
	authID := sess.authID
	wsURL := sess.wsURL
	lifecycle := sess.lifecycle
	closer := sess.connCloser
	sess.lifecycle = nil
	sess.lifecycleModel = ""
	sess.conn = nil
	sess.connCloser = nil
	if sess.readerConn == conn {
		sess.readerConn = nil
	}
	sessionID := sess.sessionID
	sess.connMu.Unlock()

	sess.removeReadToken(conn)

	if conn != nil {
		logDisconnected(sessionID, authID, wsURL, reason, nil)
		if closer != nil {
			if errClose := closer.Close(); errClose != nil {
				log.Errorf("%s: close websocket error: %v", executorName, errClose)
			}
		}
	}
	if lifecycle != nil {
		lifecycle.End(reason)
	}
}

// readMessage reads the next upstream websocket message. For a session-less
// connection it reads directly, applying the runtime read deadline when set.
//
// minToken is the value the caller captured from currentReadToken before writing
// its request. Events stamped with a token < minToken were read by the shared
// pump before this request was written and belong to the previous turn on the
// reused connection (for example a trailing response.done after
// response.completed); they are dropped. Terminal error events are never
// dropped: they apply to the connection itself, and dropping one would leave the
// caller blocked instead of surfacing the disconnect.
//
// The comparison is intentionally strict: the pump advances the counter before
// every ReadMessage, so the first event of the current turn can be stamped
// exactly minToken when the pump began its read before the snapshot (a fresh
// connection's first event always is); an inclusive bound would drop that
// legitimate event. The residual race where the pump starts reading a stale
// event after the snapshot but before the write stamps it > minToken and leaks
// it through — closing that window would require coordination between the pump
// and the consumer beyond the counter, so the strict comparison is the safe
// choice for the common case.
func (r *websocketSessionRuntime) readMessage(ctx context.Context, sess *codexWebsocketSession, conn *websocket.Conn, readCh chan codexWebsocketRead, minToken uint64) (int, []byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if sess == nil {
		if conn == nil {
			return 0, nil, fmt.Errorf("%s: websocket conn is nil", r.executorName)
		}
		if r.readDeadline > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(r.readDeadline))
		}
		msgType, payload, errRead := conn.ReadMessage()
		return msgType, payload, errRead
	}
	if conn == nil {
		return 0, nil, fmt.Errorf("%s: websocket conn is nil", r.executorName)
	}
	if readCh == nil {
		return 0, nil, fmt.Errorf("%s: session read channel is nil", r.executorName)
	}
	for {
		select {
		case <-ctx.Done():
			return 0, nil, ctx.Err()
		case ev, ok := <-readCh:
			if !ok {
				return 0, nil, fmt.Errorf("%s: session read channel closed", r.executorName)
			}
			if ev.conn != conn {
				continue
			}
			if ev.err == nil && ev.token != 0 && ev.token < minToken {
				continue
			}
			if ev.err != nil {
				return 0, nil, ev.err
			}
			return ev.msgType, ev.payload, nil
		}
	}
}
