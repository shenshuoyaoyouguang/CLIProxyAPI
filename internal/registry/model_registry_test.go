package registry

import (
	"sync"
	"testing"
	"time"
)

// newTestRegistry builds a minimal ModelRegistry for unit tests without touching
// the global singleton. Fields are accessed directly because the test shares the
// package.
func newTestRegistry() *ModelRegistry {
	return &ModelRegistry{
		models:                 make(map[string]*ModelRegistration),
		clientModels:           make(map[string][]string),
		clientModelInfos:       make(map[string]map[string]*ModelInfo),
		clientProviders:        make(map[string]string),
		availableModelsCache:   make(map[string]availableModelsCacheEntry),
		availableModelsRebuild: make(map[string]*rebuildToken),
		knownHandlerTypes:      make(map[string]struct{}),
		mutex:                  &sync.RWMutex{},
	}
}

func TestApplyProviderChangeCountsLocked_NoProviderChange(t *testing.T) {
	r := newTestRegistry()
	r.models["m1"] = &ModelRegistration{Providers: map[string]int{"gemini": 2}}

	oldCounts := map[string]int{"m1": 2}
	newCounts := map[string]int{"m1": 2} // same provider

	// Same provider -> must be a no-op.
	r.applyProviderChangeCountsLocked("gemini", "gemini", oldCounts, newCounts)
	if got := r.models["m1"].Providers["gemini"]; got != 2 {
		t.Fatalf("m1 gemini count = %d, want 2 (no provider change should be a no-op)", got)
	}
}

func TestApplyProviderChangeCountsLocked_RemovesOverlap(t *testing.T) {
	r := newTestRegistry()
	r.models["m1"] = &ModelRegistration{
		Providers:      map[string]int{"gemini": 2},
		InfoByProvider: map[string]*ModelInfo{"gemini": {}},
	}
	r.models["m2"] = &ModelRegistration{Providers: map[string]int{"gemini": 1}}

	oldCounts := map[string]int{"m1": 2, "m2": 1}
	newCounts := map[string]int{"m1": 2, "m2": 1}

	// Provider change gemini -> claude removes the entire overlap.
	r.applyProviderChangeCountsLocked("gemini", "claude", oldCounts, newCounts)
	if _, ok := r.models["m1"].Providers["gemini"]; ok {
		t.Fatalf("m1 gemini should have been removed on provider change")
	}
	if _, ok := r.models["m1"].InfoByProvider["gemini"]; ok {
		t.Fatalf("m1 gemini InfoByProvider should have been removed on provider change")
	}
	if _, ok := r.models["m2"].Providers["gemini"]; ok {
		t.Fatalf("m2 gemini should have been removed on provider change")
	}
}

func TestApplyProviderChangeCountsLocked_PartialOverlap(t *testing.T) {
	r := newTestRegistry()
	r.models["m3"] = &ModelRegistration{Providers: map[string]int{"gemini": 3}}

	// New provider count (1) is less than old (3): only 1 of the old shares are
	// re-attributed, leaving 2 under the old provider.
	r.applyProviderChangeCountsLocked("gemini", "claude", map[string]int{"m3": 3}, map[string]int{"m3": 1})
	if got := r.models["m3"].Providers["gemini"]; got != 2 {
		t.Fatalf("m3 gemini count = %d, want 2 (3 - 1 overlap)", got)
	}
}

func TestGetAvailableModels_CacheThenHit(t *testing.T) {
	r := newTestRegistry()
	r.models["m1"] = &ModelRegistration{
		Info:  &ModelInfo{ID: "m1", OwnedBy: "tester"},
		Count: 1,
	}

	got := r.GetAvailableModels("openai")
	if len(got) != 1 {
		t.Fatalf("openai available models = %d, want 1", len(got))
	}
	if got[0]["id"] != "m1" {
		t.Fatalf("expected model id m1, got %v", got[0]["id"])
	}

	// Second call must hit the cache and still return the model.
	got2 := r.GetAvailableModels("openai")
	if len(got2) != 1 {
		t.Fatalf("cached openai available models = %d, want 1", len(got2))
	}
}

func TestGetAvailableModels_InvalidatePrefetch(t *testing.T) {
	r := newTestRegistry()
	r.models["m1"] = &ModelRegistration{
		Info:  &ModelInfo{ID: "m1", OwnedBy: "tester"},
		Count: 1,
	}

	if got := r.GetAvailableModels("openai"); len(got) != 1 {
		t.Fatalf("pre-invalidate available = %d, want 1", len(got))
	}

	// Invalidate (triggers a background prefetch for the known handlerType).
	r.mutex.Lock()
	r.invalidateAvailableModelsCacheLocked()
	r.mutex.Unlock()

	// The background prefetch should repopulate the cache within a short deadline.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if got := r.GetAvailableModels("openai"); len(got) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("model did not reappear after invalidation prefetch")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestGetAvailableModels_ConcurrentNoRace(t *testing.T) {
	r := newTestRegistry()
	r.models["m1"] = &ModelRegistration{
		Info:  &ModelInfo{ID: "m1", OwnedBy: "tester"},
		Count: 1,
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.GetAvailableModels("openai")
		}()
	}
	wg.Wait()

	// Drain any in-flight rebuild tokens so the next read completes cleanly.
	for i := 0; i < 50; i++ {
		_ = r.GetAvailableModels("openai")
	}
}

func TestApplyProviderChangeCountsLocked_EmptyOldProvider(t *testing.T) {
	r := newTestRegistry()
	r.models["m4"] = &ModelRegistration{Providers: map[string]int{"gemini": 1}}

	// Empty oldProvider -> must be a no-op so existing registrations survive.
	r.applyProviderChangeCountsLocked("", "claude", map[string]int{}, map[string]int{"m4": 1})
	if _, ok := r.models["m4"].Providers["gemini"]; !ok {
		t.Fatalf("m4 gemini should remain when oldProvider is empty")
	}
}

func TestComputeAddRemoveLocked(t *testing.T) {
	r := newTestRegistry()
	oldModels := []string{"a", "b", "b", "c"}
	oldCounts := map[string]int{"a": 1, "b": 2, "c": 1}
	newCounts := map[string]int{"a": 1, "b": 1, "d": 1}
	unique := []string{"a", "b", "d"}

	added, removed := r.computeAddRemoveLocked(unique, oldCounts, newCounts)

	// "d" is new -> added; "c" dropped from newCounts -> removed; "a"/"b" overlap.
	wantAdded := map[string]bool{"d": true}
	wantRemoved := map[string]bool{"c": true}
	if len(added) != len(wantAdded) {
		t.Fatalf("added = %v, want %v", added, wantRemoved)
	}
	for _, id := range added {
		if !wantAdded[id] {
			t.Fatalf("unexpected added model %q", id)
		}
	}
	if len(removed) != len(wantRemoved) {
		t.Fatalf("removed = %v, want %v", removed, wantRemoved)
	}
	for _, id := range removed {
		if !wantRemoved[id] {
			t.Fatalf("unexpected removed model %q", id)
		}
	}
	_ = oldModels
}
