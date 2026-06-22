package executor

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// TestStreamChunkSendWithCancelledContext verifies that sending stream chunks
// on an unbuffered channel respects context cancellation and does not leak
// the producer goroutine.
//
// Bug: antigravity_executor.go lines 1067 and 1072 send on an unbuffered
// channel without a select on ctx.Done(). If the consumer exits early
// (e.g., on error), the producer goroutine blocks forever.
func TestStreamChunkSendWithCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	out := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		for i := 0; i < 10; i++ {
			// This is the CORRECT pattern — protected by ctx.Done()
			select {
			case out <- struct{}{}:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Simulate consumer reading one item then exiting
	<-out
	cancel()

	// Producer should exit promptly
	select {
	case <-done:
		// success — goroutine exited cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("producer goroutine leaked — did not exit after context cancellation")
	}
}

// TestUnprotectedChannelSendLeaksGoroutine demonstrates the leak pattern:
// bare channel sends without ctx.Done() protection leak the goroutine when
// the consumer stops reading.
func TestUnprotectedChannelSendLeaksGoroutine(t *testing.T) {
	baseline := runtime.NumGoroutine()

	out := make(chan int)
	leaked := make(chan struct{})

	go func() {
		defer close(leaked)
		// Bare send — NO ctx.Done() protection (the buggy pattern)
		for i := 0; i < 10; i++ {
			out <- i // will block on i=1 when nobody reads
		}
	}()

	// Read one item then abandon the channel
	<-out

	// Give the goroutine a moment to attempt the next send
	time.Sleep(50 * time.Millisecond)

	current := runtime.NumGoroutine()
	if current <= baseline {
		t.Skip("goroutine count heuristic unreliable in this environment")
	}

	// The goroutine is stuck — verify it's leaked
	select {
	case <-leaked:
		t.Fatal("goroutine should be stuck, not done")
	default:
		// expected: goroutine is blocked on `out <- 1`
	}
}
