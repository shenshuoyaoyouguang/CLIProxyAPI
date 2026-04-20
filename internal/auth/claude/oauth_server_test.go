package claude

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitForCallbackContext_HonorsCancellation(t *testing.T) {
	server := NewOAuthServer(54545)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := server.WaitForCallbackContext(ctx, time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitForCallbackContext() error = %v, want context.Canceled", err)
	}
}
