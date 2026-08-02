package api

import (
	"net"
	"testing"
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
