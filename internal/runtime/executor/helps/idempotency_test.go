package helps

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestGenerateIdempotencyKey_NewKey(t *testing.T) {
	ctx := context.Background()
	key := GenerateIdempotencyKey(ctx)
	if key == "" {
		t.Fatal("key should not be empty")
	}
	if !strings.HasPrefix(string(key), "cpa-") {
		t.Fatalf("key should start with 'cpa-', got %q", key)
	}
}

func TestGenerateIdempotencyKey_ReusesFromContext(t *testing.T) {
	existing := IdempotencyKey("cpa-existing-key")
	ctx := ContextWithIdempotencyKey(context.Background(), existing)
	key := GenerateIdempotencyKey(ctx)
	if key != existing {
		t.Fatalf("should reuse existing key, got %q want %q", key, existing)
	}
}

func TestGenerateIdempotencyKey_UniqueKeys(t *testing.T) {
	ctx := context.Background()
	key1 := GenerateIdempotencyKey(ctx)
	key2 := GenerateIdempotencyKey(ctx)
	if key1 == key2 {
		t.Fatal("two generated keys should be different")
	}
}

func TestApplyIdempotencyKey(t *testing.T) {
	req, _ := http.NewRequest("POST", "http://example.com", nil)
	key := IdempotencyKey("cpa-test-key")
	ApplyIdempotencyKey(req, key)
	if req.Header.Get("X-Idempotency-Key") != "cpa-test-key" {
		t.Fatalf("header not set correctly, got %q", req.Header.Get("X-Idempotency-Key"))
	}
}

func TestIdempotencyKeyFromRequest(t *testing.T) {
	req, _ := http.NewRequest("POST", "http://example.com", nil)
	req.Header.Set("X-Idempotency-Key", "cpa-from-header")
	key := IdempotencyKeyFromRequest(req)
	if key != "cpa-from-header" {
		t.Fatalf("got %q, want %q", key, "cpa-from-header")
	}
}

func TestIdempotencyKeyRoundTrip(t *testing.T) {
	ctx := context.Background()
	key := GenerateIdempotencyKey(ctx)
	ctx = ContextWithIdempotencyKey(ctx, key)

	req, _ := http.NewRequest("POST", "http://example.com", nil)
	ApplyIdempotencyKey(req, IdempotencyKeyFromContext(ctx))

	extracted := IdempotencyKeyFromRequest(req)
	if extracted != key {
		t.Fatalf("round-trip failed: got %q, want %q", extracted, key)
	}
}
