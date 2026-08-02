package ratelimit

import (
	"context"
	"net/http"
	"testing"
)

func TestUserIDResolverHeaderStrategy(t *testing.T) {
	t.Parallel()
	r := NewUserIDResolver(StrategyHeader, "X-DeepSeek-User-ID", "", "cliproxy")
	h := make(http.Header)
	h.Set("X-DeepSeek-User-ID", "tenant-42")
	got := r.Resolve(context.Background(), nil, h, "cred", "key")
	if got != "tenant-42" {
		t.Fatalf("got %q, want tenant-42", got)
	}
}

func TestUserIDResolverPerCredentialStable(t *testing.T) {
	t.Parallel()
	r := NewUserIDResolver(StrategyPerCredential, "", "", "cliproxy")
	a := r.Resolve(context.Background(), nil, nil, "auth-a", "key-a")
	b := r.Resolve(context.Background(), nil, nil, "auth-a", "key-a")
	c := r.Resolve(context.Background(), nil, nil, "auth-b", "key-b")
	if a == "" || a != b {
		t.Fatalf("stable id mismatch: %q vs %q", a, b)
	}
	if a == c {
		t.Fatalf("different credentials must differ: %q", a)
	}
}

func TestParseUserIDStrategy(t *testing.T) {
	t.Parallel()
	if ParseUserIDStrategy("header") != StrategyHeader {
		t.Fatal("header")
	}
	if ParseUserIDStrategy("unknown") != StrategyPerCredential {
		t.Fatal("default")
	}
}
