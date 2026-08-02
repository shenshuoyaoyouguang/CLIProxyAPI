package registry

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestRebuildAndPublish_ConcurrentNoDeadlock exercises the singleflight rebuild
// path under concurrent readers (GetAvailableModels) and cache invalidations
// (RegisterClient). Run with -race to catch data races, and rely on the timeout
// to catch any waiter stranded on a rebuild token's done channel (the bug the
// hardened rebuildAndPublish fixes by always closing token.done).
func TestRebuildAndPublish_ConcurrentNoDeadlock(t *testing.T) {
	r := newTestModelRegistry()

	const handlerType = "gemini"
	baseModels := []*ModelInfo{
		{ID: "m1", Type: "gemini"},
		{ID: "m2", Type: "gemini"},
	}
	// Seed so readers always observe a non-nil snapshot.
	r.RegisterClient("client-seed", handlerType, baseModels)

	var wg sync.WaitGroup

	// Readers continuously fetch available models (mix of cache hits and misses).
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				models := r.GetAvailableModels(handlerType)
				if models == nil {
					t.Errorf("GetAvailableModels returned nil during concurrent rebuild")
					return
				}
			}
		}()
	}

	// Writers continuously invalidate the cache so rebuilds happen under load.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				r.RegisterClient(fmt.Sprintf("client-%d", id), handlerType, baseModels)
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("concurrent rebuild test timed out: possible deadlock on rebuild token")
	}
}

// TestRebuildAndPublish_SupersededTokenClosesOwnDone verifies the defensive branch
// of rebuildAndPublish: when a newer builder has superseded this token (cur !=
// token), the function must still close *its own* token's done so its waiters are
// released, and must NOT close the superseding token's done (which its owner will
// close). This prevents a future caller that breaks the singleflight invariant
// from permanently blocking waiters.
func TestRebuildAndPublish_SupersededTokenClosesOwnDone(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClient("client-1", "gemini", []*ModelInfo{{ID: "m1", Type: "gemini"}})

	// Simulate a superseded in-flight token: the map holds tokenA, but we call
	// rebuildAndPublish with a different tokenB (as a future buggy caller might).
	tokenA := &rebuildToken{done: make(chan struct{})}
	tokenB := &rebuildToken{done: make(chan struct{})}
	r.availableModelsRebuild["gemini"] = tokenA

	r.rebuildAndPublish("gemini", tokenB)

	// tokenB must be released so its waiters are not stranded.
	select {
	case <-tokenB.done:
	case <-time.After(time.Second):
		t.Fatal("tokenB.done was not closed; waiters would block forever")
	}

	// tokenA remains in the map (owned by its own builder) and must NOT be closed
	// here; closing it would steal another builder's waiters.
	select {
	case <-tokenA.done:
		t.Fatal("tokenA.done was closed by the wrong owner")
	default:
	}
	if _, ok := r.availableModelsRebuild["gemini"]; !ok {
		t.Fatal("tokenA should remain in the rebuild map")
	}
}

// TestRebuildAndPublish_OwnerTokenClosesAndRemoves verifies the normal path:
// when the token in the map matches the caller's token, it is removed from the
// map and its done channel is closed, releasing waiters.
func TestRebuildAndPublish_OwnerTokenClosesAndRemoves(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClient("client-1", "gemini", []*ModelInfo{{ID: "m1", Type: "gemini"}})

	token := &rebuildToken{done: make(chan struct{})}
	r.availableModelsRebuild["gemini"] = token

	r.rebuildAndPublish("gemini", token)

	if _, ok := r.availableModelsRebuild["gemini"]; ok {
		t.Fatal("owner token should have been removed from the rebuild map")
	}
	select {
	case <-token.done:
	case <-time.After(time.Second):
		t.Fatal("owner token.done was not closed")
	}
}

func TestPublishAvailableModelsSnapshotSkipsInvalidatedSnapshot(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClient("client-1", "openai", []*ModelInfo{{ID: "m1", OwnedBy: "team-a", DisplayName: "Model One"}})

	r.mutex.RLock()
	generation := r.availableModelsGeneration
	models, expiresAt := r.buildAvailableModelsLocked("openai", time.Now())
	r.mutex.RUnlock()

	r.RegisterClient("client-1", "openai", []*ModelInfo{{ID: "m1", OwnedBy: "team-a", DisplayName: "Model One Updated"}})

	token := &rebuildToken{done: make(chan struct{})}
	r.mutex.Lock()
	r.ensureAvailableModelsCacheLocked()
	r.availableModelsRebuild["openai"] = token
	r.mutex.Unlock()

	published, owner := r.publishAvailableModelsSnapshot("openai", token, generation, models, expiresAt)
	if published {
		t.Fatal("stale snapshot should not be published after cache invalidation")
	}
	if !owner {
		t.Fatal("matching owner token should be reported as owner")
	}
	select {
	case <-token.done:
	case <-time.After(time.Second):
		t.Fatal("owner token.done was not closed")
	}

	r.mutex.RLock()
	_, cached := r.availableModelsCache["openai"]
	r.mutex.RUnlock()
	if cached {
		t.Fatal("availableModelsCache should not contain the invalidated snapshot")
	}

	fresh := r.GetAvailableModels("openai")
	if len(fresh) != 1 {
		t.Fatalf("fresh available models = %d, want 1", len(fresh))
	}
	if got := fresh[0]["display_name"]; got != "Model One Updated" {
		t.Fatalf("fresh model display_name = %v, want Model One Updated", got)
	}
}
