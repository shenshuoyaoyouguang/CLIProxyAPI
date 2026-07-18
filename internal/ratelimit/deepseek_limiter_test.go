package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSetMaxConcurrencyWakesWaiters(t *testing.T) {
	t.Parallel()
	mgr := NewDeepSeekLimiterManager(DeepSeekLimiterConfig{
		GlobalMaxConcurrency:    0,
		PerUserIDMaxConcurrency: 1,
	})
	release1, err := mgr.Acquire(context.Background(), "u1")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	var secondOK atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		rel, errAcq := mgr.Acquire(ctx, "u1")
		if errAcq != nil {
			t.Errorf("second acquire: %v", errAcq)
			return
		}
		secondOK.Store(true)
		rel()
	}()

	// Give waiter time to block.
	time.Sleep(50 * time.Millisecond)
	if !mgr.SetShardConcurrency("u1", 2) {
		t.Fatal("shard not found")
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter was not woken by SetMaxConcurrency")
	}
	if !secondOK.Load() {
		t.Fatal("second acquire did not succeed after tune")
	}
	release1()
}

func TestAcquireRespectsPerUserCap(t *testing.T) {
	t.Parallel()
	mgr := NewDeepSeekLimiterManager(DeepSeekLimiterConfig{
		PerUserIDMaxConcurrency: 1,
	})
	rel, err := mgr.Acquire(context.Background(), "cap")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer rel()

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, err = mgr.Acquire(ctx, "cap")
	if err == nil {
		t.Fatal("expected second acquire to block until timeout")
	}
}
