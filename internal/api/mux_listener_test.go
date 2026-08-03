package api

import (
	"errors"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

// TestMuxListenerCloseDrainsQueuedConns verifies Close() closes connections
// already queued in connCh so their file descriptors do not leak (H24h).
func TestMuxListenerCloseDrainsQueuedConns(t *testing.T) {
	ln := newMuxListener(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}, 4)
	serverSide, clientSide := net.Pipe()
	defer func() {
		_ = clientSide.Close()
	}()

	if err := ln.Put(serverSide); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// The queued conn must be closed by Close(): a read on the peer errors.
	buf := make([]byte, 1)
	if _, err := clientSide.Read(buf); err == nil {
		t.Fatal("queued conn not closed after listener Close")
	}

	// Close is idempotent.
	if err := ln.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

// TestMuxListenerPutRacingCloseClosesConn verifies that a Put racing Close —
// e.g. a routeMuxConnection goroutine finishing its handshake/peek while the
// listener shuts down — never leaks the connection: either Close's drain picks
// it up, or the post-send closed-flag check in Put closes it.
func TestMuxListenerPutRacingCloseClosesConn(t *testing.T) {
	ln := newMuxListener(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}, 4)

	const puts = 64
	var wg sync.WaitGroup
	clientSides := make([]net.Conn, 0, puts)
	var clientMu sync.Mutex
	start := make(chan struct{})

	for i := 0; i < puts; i++ {
		serverSide, clientSide := net.Pipe()
		clientMu.Lock()
		clientSides = append(clientSides, clientSide)
		clientMu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			// Mirror routeMuxConnection: on Put error the caller closes the conn.
			if err := ln.Put(serverSide); err != nil {
				_ = serverSide.Close()
			}
		}()
	}

	close(start)
	_ = ln.Close()
	wg.Wait()

	// Every conn must end up closed, whether by the drain or by Put itself.
	for i := 0; i < puts; i++ {
		clientMu.Lock()
		clientSide := clientSides[i]
		clientMu.Unlock()
		buf := make([]byte, 1)
		_ = clientSide.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, err := clientSide.Read(buf)
		if err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("conn %d read error = %v, want closed conn (not a hang)", i, err)
		}
		_ = clientSide.Close()
	}
}
