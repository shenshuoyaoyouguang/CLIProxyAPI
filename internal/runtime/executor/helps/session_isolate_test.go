package helps

import (
	"context"
	"encoding/hex"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func testContextWithUserAPIKey(apiKey string) context.Context {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	if apiKey != "" {
		ginCtx.Set("userApiKey", apiKey)
	}
	return context.WithValue(context.Background(), "gin", ginCtx)
}

func TestIsolateClientControlledSessionKey(t *testing.T) {
	t.Parallel()

	if got := IsolateClientControlledSessionKey(context.Background(), ""); got != "" {
		t.Fatalf("empty key = %q, want empty", got)
	}
	if got := IsolateClientControlledSessionKey(context.Background(), "execution:trusted"); got != "execution:trusted" {
		t.Fatalf("trusted execution key = %q, want unchanged", got)
	}

	// Issue #8: client-controlled sessions without userApiKey are no longer fully
	// disabled; they fall back to a process-level degraded namespace.
	got := IsolateClientControlledSessionKey(context.Background(), "session:shared")
	if got == "" {
		t.Fatalf("client key without API key = empty, want degraded namespace")
	}
	if !strings.HasPrefix(got, "degraded:") || !strings.HasSuffix(got, ":session:shared") {
		t.Fatalf("degraded key = %q, want degraded:<salt>:session:shared", got)
	}
	// Degraded salt must be stable within a process so in-process continuity holds.
	if IsolateClientControlledSessionKey(context.Background(), "session:shared") != got {
		t.Fatal("degraded namespace must be deterministic within a process")
	}

	keyA := IsolateClientControlledSessionKey(testContextWithUserAPIKey("api-key-a"), "session:shared")
	keyB := IsolateClientControlledSessionKey(testContextWithUserAPIKey("api-key-b"), "session:shared")
	if keyA == "" || keyB == "" {
		t.Fatalf("keys with API key must be non-empty: A=%q B=%q", keyA, keyB)
	}
	if keyA == keyB {
		t.Fatalf("different callers must not share key: both %q", keyA)
	}
	if !strings.HasPrefix(keyA, "caller:") || !strings.HasSuffix(keyA, ":session:shared") {
		t.Fatalf("key A = %q, want caller-isolated session:shared", keyA)
	}
	if IsolateClientControlledSessionKey(testContextWithUserAPIKey("api-key-a"), "session:shared") != keyA {
		t.Fatal("isolation must be deterministic for the same caller")
	}
}

// TestIsolateClientControlledSessionKeyNamespaceWidth checks that the caller
// namespace uses at least 128 bits (32 hex chars) to avoid birthday collisions
// (issue #11).
func TestIsolateClientControlledSessionKeyNamespaceWidth(t *testing.T) {
	t.Parallel()

	key := IsolateClientControlledSessionKey(testContextWithUserAPIKey("api-key-a"), "session:shared")
	// Shape: caller:<32+ hex chars>:session:shared
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 {
		t.Fatalf("key = %q, want 3 colon-separated parts", key)
	}
	if parts[0] != "caller" {
		t.Fatalf("prefix = %q, want caller", parts[0])
	}
	ns := parts[1]
	// 32 hex chars = 128 bits; require >= 32 for issue #11.
	if len(ns) < 32 {
		t.Fatalf("caller namespace hash = %d hex chars, want >= 32 (128 bits) to avoid birthday collisions", len(ns))
	}
	if _, err := hex.DecodeString(ns); err != nil {
		t.Fatalf("caller namespace %q is not valid hex: %v", ns, err)
	}
}
