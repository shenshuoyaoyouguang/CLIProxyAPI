package executor

import (
	"errors"
	"io"
	"testing"
)

func TestIsRetryableStreamDisconnect_EOF_BeforeDone(t *testing.T) {
	// Upstream closed connection (unexpected EOF) before sending [DONE].
	// This is retryable — no data was committed to the client.
	if !isRetryableStreamDisconnect(io.ErrUnexpectedEOF, false) {
		t.Fatal("expected io.ErrUnexpectedEOF without [DONE] to be retryable")
	}
}

func TestIsRetryableStreamDisconnect_EOF_AfterDone(t *testing.T) {
	// Upstream closed connection after [DONE] — this is normal stream end.
	if isRetryableStreamDisconnect(io.ErrUnexpectedEOF, true) {
		t.Fatal("expected io.ErrUnexpectedEOF with [DONE] to NOT be retryable")
	}
}

func TestIsRetryableStreamDisconnect_NormalEOF_BeforeDone(t *testing.T) {
	// Normal io.EOF without [DONE] — upstream closed gracefully.
	// Should not retry (this is not a disconnect, just missing marker).
	if isRetryableStreamDisconnect(io.EOF, false) {
		t.Fatal("expected io.EOF to NOT be retryable")
	}
}

func TestIsRetryableStreamDisconnect_NilError(t *testing.T) {
	// No error — not retryable.
	if isRetryableStreamDisconnect(nil, false) {
		t.Fatal("expected nil error to NOT be retryable")
	}
}

func TestIsRetryableStreamDisconnect_UnrelatedError(t *testing.T) {
	// Unrelated error — not retryable.
	if isRetryableStreamDisconnect(errors.New("something else"), false) {
		t.Fatal("expected unrelated error to NOT be retryable")
	}
}
