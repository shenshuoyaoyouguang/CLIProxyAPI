package api

import (
	"net"
	"sync"
)

type muxListener struct {
	addr    net.Addr
	connCh  chan net.Conn
	closeCh chan struct{}
	once    sync.Once
	// mu guards closed. Put sets it after enqueueing to detect connections
	// that escaped Close's drain; Close sets it before draining so the flag is
	// already visible to any Put racing the drain.
	mu     sync.Mutex
	closed bool
}

func newMuxListener(addr net.Addr, buffer int) *muxListener {
	if buffer <= 0 {
		buffer = 1
	}
	return &muxListener{
		addr:    addr,
		connCh:  make(chan net.Conn, buffer),
		closeCh: make(chan struct{}),
	}
}

func (l *muxListener) Put(conn net.Conn) error {
	if conn == nil {
		return nil
	}
	select {
	case <-l.closeCh:
		return net.ErrClosed
	case l.connCh <- conn:
	}
	// Close may have drained connCh before this enqueue landed (a Put blocked
	// in a TLS handshake or peek when Close ran). Re-check the closed flag and
	// close the connection ourselves so an enqueue that escaped the drain does
	// not leak its file descriptor for the process lifetime. Closing an
	// already-closed connection is idempotent, so a conn drained by Close and
	// re-checked here is safe.
	l.mu.Lock()
	closed := l.closed
	l.mu.Unlock()
	if closed {
		_ = conn.Close()
		return net.ErrClosed
	}
	return nil
}

func (l *muxListener) Accept() (net.Conn, error) {
	select {
	case <-l.closeCh:
		return nil, net.ErrClosed
	case conn := <-l.connCh:
		if conn == nil {
			return nil, net.ErrClosed
		}
		return conn, nil
	}
}

func (l *muxListener) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		close(l.closeCh)
		// Mark the listener closed before draining: any Put that enqueues after
		// the drain finishes (a routeMuxConnection goroutine still in its
		// handshake or peek) observes the flag in its post-send check and
		// closes its own connection, so no queued connection leaks.
		l.mu.Lock()
		l.closed = true
		l.mu.Unlock()
		// Drain connections already queued in connCh so their file descriptors
		// do not leak for the process lifetime.
		for {
			select {
			case conn := <-l.connCh:
				if conn != nil {
					_ = conn.Close()
				}
			default:
				return
			}
		}
	})
	return nil
}

func (l *muxListener) Addr() net.Addr {
	if l == nil {
		return &net.TCPAddr{}
	}
	if l.addr == nil {
		return &net.TCPAddr{}
	}
	return l.addr
}
