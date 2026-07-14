package auth

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type fixedSelector struct {
	authID string
}

func (s fixedSelector) Pick(_ context.Context, _, _ string, _ cliproxyexecutor.Options, auths []*Auth) (*Auth, error) {
	for _, auth := range auths {
		if auth.ID == s.authID {
			return auth, nil
		}
	}
	return nil, nil
}

// --- Fix 1: Cache key canonicalization (model suffix handling) ---

func TestSessionAffinitySelector_ModelSuffixSharesBinding(t *testing.T) {
	t.Parallel()

	fallback := &RoundRobinSelector{}
	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: fallback,
		TTL:      time.Minute,
	})
	defer selector.Stop()

	auths := []*Auth{
		{ID: "auth-a"},
		{ID: "auth-b"},
		{ID: "auth-c"},
	}

	headers := make(http.Header)
	headers.Set("X-Session-ID", "suffix-test-session")
	opts := cliproxyexecutor.Options{Headers: headers}

	// First pick with base model
	first, err := selector.Pick(context.Background(), "mixed", "grok-4.5", opts, auths)
	if err != nil {
		t.Fatalf("Pick(grok-4.5) error = %v", err)
	}

	// Same session with thinking suffix should hit the same binding
	got, err := selector.Pick(context.Background(), "mixed", "grok-4.5:thinking", opts, auths)
	if err != nil {
		t.Fatalf("Pick(grok-4.5:thinking) error = %v", err)
	}
	if got.ID != first.ID {
		t.Errorf("model suffix variant picked different auth: got %q, want %q", got.ID, first.ID)
	}

	// Extended thinking suffix variant
	got2, err := selector.Pick(context.Background(), "mixed", "grok-4.5:thinking:16k", opts, auths)
	if err != nil {
		t.Fatalf("Pick(grok-4.5:thinking:16k) error = %v", err)
	}
	if got2.ID != first.ID {
		t.Errorf("extended suffix variant picked different auth: got %q, want %q", got2.ID, first.ID)
	}
}

func TestSessionAffinitySelector_ModelSuffixCacheHitLogged(t *testing.T) {
	t.Parallel()

	fallback := &RoundRobinSelector{}
	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: fallback,
		TTL:      time.Minute,
	})
	defer selector.Stop()

	auths := []*Auth{{ID: "auth-x"}, {ID: "auth-y"}}

	headers := make(http.Header)
	headers.Set("X-Session-ID", "canonical-cache-hit")
	opts := cliproxyexecutor.Options{Headers: headers}

	// Establish binding via base model
	first, _ := selector.Pick(context.Background(), "provider", "claude-sonnet-4-20250514", opts, auths)

	// Suffixed variant should cache-hit, not miss
	second, _ := selector.Pick(context.Background(), "provider", "claude-sonnet-4-20250514:thinking", opts, auths)
	if second.ID != first.ID {
		t.Errorf("suffixed model should cache-hit same auth: got %q, want %q", second.ID, first.ID)
	}

	// Reverse: establish with suffix, access without
	headers2 := make(http.Header)
	headers2.Set("X-Session-ID", "canonical-reverse")
	opts2 := cliproxyexecutor.Options{Headers: headers2}

	firstSuffix, _ := selector.Pick(context.Background(), "provider", "model-x:thinking", opts2, auths)
	baseAccess, _ := selector.Pick(context.Background(), "provider", "model-x", opts2, auths)
	if baseAccess.ID != firstSuffix.ID {
		t.Errorf("base model should hit cache from suffixed binding: got %q, want %q", baseAccess.ID, firstSuffix.ID)
	}
}

func TestSessionAffinitySelector_MessageHashFallbackUsesCanonicalModelKey(t *testing.T) {
	t.Parallel()

	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: fixedSelector{authID: "auth-b"},
		TTL:      time.Minute,
	})
	defer selector.Stop()

	auths := []*Auth{{ID: "auth-a"}, {ID: "auth-b"}}
	multiTurn := []byte(`{"messages":[{"role":"user","content":"Hello"},{"role":"assistant","content":"Hi"},{"role":"user","content":"Continue"}]}`)
	primaryID, fallbackID := extractSessionIDs(nil, multiTurn, nil)
	if primaryID == "" || fallbackID == "" || primaryID == fallbackID {
		t.Fatalf("unexpected message-hash ids primary=%q fallback=%q", primaryID, fallbackID)
	}

	selector.cache.Set("mixed::"+fallbackID+"::"+canonicalModelKey("deepseek-chat"), "auth-a")
	got, err := selector.Pick(context.Background(), "mixed", "deepseek-chat:thinking", cliproxyexecutor.Options{OriginalRequest: multiTurn}, auths)
	if err != nil {
		t.Fatalf("fallback Pick() error = %v", err)
	}
	if got.ID != "auth-a" {
		t.Fatalf("message-hash fallback picked %q, want auth-a from canonical model binding", got.ID)
	}
}

func TestSessionAffinitySelector_DifferentModelsStillSeparate(t *testing.T) {
	t.Parallel()

	fallback := &RoundRobinSelector{}
	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: fallback,
		TTL:      time.Minute,
	})
	defer selector.Stop()

	authA := &Auth{ID: "auth-a"}
	authB := &Auth{ID: "auth-b"}

	headers := make(http.Header)
	headers.Set("X-Session-ID", "model-isolation-test")
	opts := cliproxyexecutor.Options{Headers: headers}

	// model-a -> only auth-a available
	pickedA, _ := selector.Pick(context.Background(), "provider", "model-a", opts, []*Auth{authA})
	if pickedA.ID != "auth-a" {
		t.Fatalf("expected auth-a for model-a, got %q", pickedA.ID)
	}

	// model-b -> only auth-b available (different actual model, not just suffix)
	pickedB, _ := selector.Pick(context.Background(), "provider", "model-b", opts, []*Auth{authB})
	if pickedB.ID != "auth-b" {
		t.Fatalf("expected auth-b for model-b, got %q", pickedB.ID)
	}

	// Confirm they don't interfere
	pickedA2, _ := selector.Pick(context.Background(), "provider", "model-a", opts, []*Auth{authA})
	if pickedA2.ID != "auth-a" {
		t.Errorf("model-a binding corrupted: got %q", pickedA2.ID)
	}
}

// --- Fix 2: Selective invalidation (only on 429) ---

func TestManager_MarkResult_NonRateLimitDoesNotInvalidateSessionAffinity(t *testing.T) {
	t.Parallel()

	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: &RoundRobinSelector{},
		TTL:      time.Minute,
	})
	defer selector.Stop()

	m := NewManager(nil, nil, nil)
	m.selector = selector

	auth := &Auth{ID: "auth-1", Provider: "grok"}
	if _, err := m.Register(context.Background(), auth); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Establish a session binding
	headers := make(http.Header)
	headers.Set("X-Session-ID", "persist-on-502")
	opts := cliproxyexecutor.Options{Headers: headers}
	picked, err := selector.Pick(context.Background(), "mixed", "grok-4.5", opts, []*Auth{{ID: "auth-1"}})
	if err != nil || picked.ID != "auth-1" {
		t.Fatalf("initial pick failed: %v / %v", picked, err)
	}

	// Simulate a 502 failure (NOT 429)
	m.MarkResult(context.Background(), Result{
		AuthID:   "auth-1",
		Provider: "grok",
		Model:    "grok-4.5",
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusBadGateway, Message: "upstream timeout"},
	})

	// Session binding should still be intact
	cacheKey := "mixed::header:persist-on-502::" + canonicalModelKey("grok-4.5")
	if cachedID, ok := selector.cache.Get(cacheKey); !ok || cachedID != "auth-1" {
		t.Errorf("502 failure should NOT invalidate session binding: cached=%q ok=%v", cachedID, ok)
	}
}

func TestManager_MarkResult_RateLimitInvalidatesSessionAffinity(t *testing.T) {
	t.Parallel()

	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: &RoundRobinSelector{},
		TTL:      time.Minute,
	})
	defer selector.Stop()

	m := NewManager(nil, nil, nil)
	m.selector = selector

	auth := &Auth{ID: "auth-rl", Provider: "claude"}
	if _, err := m.Register(context.Background(), auth); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Establish session binding
	headers := make(http.Header)
	headers.Set("X-Session-ID", "invalidate-on-429")
	opts := cliproxyexecutor.Options{Headers: headers}
	picked, _ := selector.Pick(context.Background(), "mixed", "claude-3", opts, []*Auth{{ID: "auth-rl"}})
	if picked.ID != "auth-rl" {
		t.Fatalf("initial pick: got %q, want auth-rl", picked.ID)
	}

	// Simulate a 429 rate-limit failure
	m.MarkResult(context.Background(), Result{
		AuthID:   "auth-rl",
		Provider: "claude",
		Model:    "claude-3",
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusTooManyRequests, Message: "rate limited"},
	})

	// Session binding should be invalidated
	cacheKey := "mixed::header:invalidate-on-429::" + canonicalModelKey("claude-3")
	if _, ok := selector.cache.Get(cacheKey); ok {
		t.Error("429 failure should invalidate session binding, but cache still has entry")
	}
}

func TestManager_MarkResult_SuccessDoesNotInvalidateSessionAffinity(t *testing.T) {
	t.Parallel()

	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: &RoundRobinSelector{},
		TTL:      time.Minute,
	})
	defer selector.Stop()

	m := NewManager(nil, nil, nil)
	m.selector = selector

	auth := &Auth{ID: "auth-ok", Provider: "gemini"}
	if _, err := m.Register(context.Background(), auth); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Establish session binding
	headers := make(http.Header)
	headers.Set("X-Session-ID", "success-no-invalidate")
	opts := cliproxyexecutor.Options{Headers: headers}
	selector.Pick(context.Background(), "mixed", "gemini-pro", opts, []*Auth{{ID: "auth-ok"}})

	// Successful result should not touch bindings
	m.MarkResult(context.Background(), Result{
		AuthID:   "auth-ok",
		Provider: "gemini",
		Model:    "gemini-pro",
		Success:  true,
	})

	cacheKey := "mixed::header:success-no-invalidate::" + canonicalModelKey("gemini-pro")
	if cachedID, ok := selector.cache.Get(cacheKey); !ok || cachedID != "auth-ok" {
		t.Errorf("success should NOT invalidate session binding: cached=%q ok=%v", cachedID, ok)
	}
}

func TestManager_MarkResult_NilErrorDoesNotInvalidateSessionAffinity(t *testing.T) {
	t.Parallel()

	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: &RoundRobinSelector{},
		TTL:      time.Minute,
	})
	defer selector.Stop()

	m := NewManager(nil, nil, nil)
	m.selector = selector

	auth := &Auth{ID: "auth-nil-err", Provider: "openai"}
	if _, err := m.Register(context.Background(), auth); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Establish binding
	headers := make(http.Header)
	headers.Set("X-Session-ID", "nil-error-test")
	opts := cliproxyexecutor.Options{Headers: headers}
	selector.Pick(context.Background(), "mixed", "gpt-4", opts, []*Auth{{ID: "auth-nil-err"}})

	// Failure with nil Error field (edge case)
	m.MarkResult(context.Background(), Result{
		AuthID:   "auth-nil-err",
		Provider: "openai",
		Model:    "gpt-4",
		Success:  false,
		Error:    nil,
	})

	cacheKey := "mixed::header:nil-error-test::" + canonicalModelKey("gpt-4")
	if cachedID, ok := selector.cache.Get(cacheKey); !ok || cachedID != "auth-nil-err" {
		t.Errorf("nil-error failure should NOT invalidate: cached=%q ok=%v", cachedID, ok)
	}
}

// --- Integrated scenario: single-auth retry loop no longer thrashes cache ---

func TestSessionAffinitySelector_SingleAuthRetryNoThrash(t *testing.T) {
	t.Parallel()

	fallback := &RoundRobinSelector{}
	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: fallback,
		TTL:      time.Minute,
	})
	defer selector.Stop()

	m := NewManager(nil, nil, nil)
	m.selector = selector

	auth := &Auth{ID: "sole-auth", Provider: "xai"}
	if _, err := m.Register(context.Background(), auth); err != nil {
		t.Fatalf("register: %v", err)
	}

	headers := make(http.Header)
	headers.Set("X-Session-ID", "single-auth-retry")
	opts := cliproxyexecutor.Options{Headers: headers}
	auths := []*Auth{{ID: "sole-auth"}}

	// Simulate the exact log scenario: repeated picks + 502 failures
	for i := 0; i < 5; i++ {
		picked, err := selector.Pick(context.Background(), "mixed", "grok-4.5", opts, auths)
		if err != nil {
			t.Fatalf("Pick() #%d error = %v", i, err)
		}
		if picked.ID != "sole-auth" {
			t.Fatalf("Pick() #%d unexpected auth %q", i, picked.ID)
		}

		// Mark as 502 failure (not 429)
		m.MarkResult(context.Background(), Result{
			AuthID:   "sole-auth",
			Provider: "xai",
			Model:    "grok-4.5",
			Success:  false,
			Error:    &Error{HTTPStatus: http.StatusBadGateway, Message: "upstream timeout"},
		})
	}

	// After 5 failures, binding should STILL be in cache (no thrashing)
	cacheKey := "mixed::header:single-auth-retry::" + canonicalModelKey("grok-4.5")
	if cachedID, ok := selector.cache.Get(cacheKey); !ok || cachedID != "sole-auth" {
		t.Errorf("single-auth retry should retain binding after 502s: cached=%q ok=%v", cachedID, ok)
	}
}

func TestSessionAffinitySelector_SingleAuthRateLimitedDoesInvalidate(t *testing.T) {
	t.Parallel()

	fallback := &RoundRobinSelector{}
	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: fallback,
		TTL:      time.Minute,
	})
	defer selector.Stop()

	m := NewManager(nil, nil, nil)
	m.selector = selector

	auth := &Auth{ID: "sole-auth-rl", Provider: "xai"}
	if _, err := m.Register(context.Background(), auth); err != nil {
		t.Fatalf("register: %v", err)
	}

	headers := make(http.Header)
	headers.Set("X-Session-ID", "single-auth-ratelimit")
	opts := cliproxyexecutor.Options{Headers: headers}
	auths := []*Auth{{ID: "sole-auth-rl"}}

	// Establish binding
	picked, _ := selector.Pick(context.Background(), "mixed", "grok-4.5", opts, auths)
	if picked.ID != "sole-auth-rl" {
		t.Fatalf("initial pick: got %q", picked.ID)
	}

	// 429 should still invalidate (so other sessions don't hit this auth)
	m.MarkResult(context.Background(), Result{
		AuthID:   "sole-auth-rl",
		Provider: "xai",
		Model:    "grok-4.5",
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusTooManyRequests, Message: "rate limited"},
	})

	cacheKey := "mixed::header:single-auth-ratelimit::" + canonicalModelKey("grok-4.5")
	if _, ok := selector.cache.Get(cacheKey); ok {
		t.Error("429 should still invalidate even for single auth")
	}
}

// --- Concurrency under the new behavior ---

func TestSessionAffinitySelector_ConcurrentPicksWithFailures(t *testing.T) {
	t.Parallel()

	fallback := &RoundRobinSelector{}
	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: fallback,
		TTL:      time.Minute,
	})
	defer selector.Stop()

	m := NewManager(nil, nil, nil)
	m.selector = selector

	auth := &Auth{ID: "concurrent-auth", Provider: "mixed"}
	if _, err := m.Register(context.Background(), auth); err != nil {
		t.Fatalf("register: %v", err)
	}

	headers := make(http.Header)
	headers.Set("X-Session-ID", "concurrent-failure-test")
	opts := cliproxyexecutor.Options{Headers: headers}
	auths := []*Auth{{ID: "concurrent-auth"}}

	// Establish initial binding
	selector.Pick(context.Background(), "mixed", "model", opts, auths)

	var wg sync.WaitGroup
	start := make(chan struct{})

	// Multiple goroutines doing picks and marking 502 failures concurrently
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 20; j++ {
				picked, err := selector.Pick(context.Background(), "mixed", "model", opts, auths)
				if err != nil {
					t.Errorf("concurrent Pick() error = %v", err)
					return
				}
				if picked.ID != "concurrent-auth" {
					t.Errorf("concurrent Pick() got %q, want concurrent-auth", picked.ID)
					return
				}
				// Simulate non-429 failure
				m.MarkResult(context.Background(), Result{
					AuthID:   "concurrent-auth",
					Provider: "mixed",
					Model:    "model",
					Success:  false,
					Error:    &Error{HTTPStatus: http.StatusBadGateway, Message: "timeout"},
				})
			}
		}()
	}

	close(start)
	wg.Wait()

	// Binding should still be intact after all concurrent 502s
	cacheKey := "mixed::header:concurrent-failure-test::" + canonicalModelKey("model")
	if cachedID, ok := selector.cache.Get(cacheKey); !ok || cachedID != "concurrent-auth" {
		t.Errorf("binding should survive concurrent 502 failures: cached=%q ok=%v", cachedID, ok)
	}
}

// --- Edge cases for the HTTP status boundary ---

func TestManager_MarkResult_VariousStatusCodesInvalidation(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		httpStatus       int
		shouldInvalidate bool
	}{
		{"400_BadRequest", http.StatusBadRequest, false},
		{"401_Unauthorized", http.StatusUnauthorized, false},
		{"403_Forbidden", http.StatusForbidden, false},
		{"404_NotFound", http.StatusNotFound, false},
		{"429_TooManyRequests", http.StatusTooManyRequests, true},
		{"500_InternalServerError", http.StatusInternalServerError, false},
		{"502_BadGateway", http.StatusBadGateway, false},
		{"503_ServiceUnavailable", http.StatusServiceUnavailable, false},
		{"504_GatewayTimeout", http.StatusGatewayTimeout, false},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
				Fallback: &RoundRobinSelector{},
				TTL:      time.Minute,
			})
			defer selector.Stop()

			m := NewManager(nil, nil, nil)
			m.selector = selector

			authID := "auth-" + tc.name
			auth := &Auth{ID: authID, Provider: "test"}
			if _, err := m.Register(context.Background(), auth); err != nil {
				t.Fatalf("register: %v", err)
			}

			sessionID := "status-test-" + tc.name
			headers := make(http.Header)
			headers.Set("X-Session-ID", sessionID)
			opts := cliproxyexecutor.Options{Headers: headers}
			selector.Pick(context.Background(), "mixed", "model", opts, []*Auth{{ID: authID}})

			m.MarkResult(context.Background(), Result{
				AuthID:   authID,
				Provider: "test",
				Model:    "model",
				Success:  false,
				Error:    &Error{HTTPStatus: tc.httpStatus, Message: "error"},
			})

			cacheKey := "mixed::header:" + sessionID + "::" + canonicalModelKey("model")
			_, ok := selector.cache.Get(cacheKey)

			if tc.shouldInvalidate && ok {
				t.Errorf("status %d should invalidate, but binding still in cache", tc.httpStatus)
			}
			if !tc.shouldInvalidate && !ok {
				t.Errorf("status %d should NOT invalidate, but binding was removed", tc.httpStatus)
			}
		})
	}
}
