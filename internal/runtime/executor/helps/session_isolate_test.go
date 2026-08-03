package helps

import (
	"context"
	"encoding/hex"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

// setGinTestModeOnce ensures gin's global mode is set exactly once: the helpers
// below run under t.Parallel() and concurrent gin.SetMode calls race on gin's
// package-level mode variable.
var setGinTestModeOnce sync.Once

func setGinTestMode() {
	setGinTestModeOnce.Do(func() { gin.SetMode(gin.TestMode) })
}

func testContextWithUserAPIKey(apiKey string) context.Context {
	setGinTestMode()
	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	if apiKey != "" {
		ginCtx.Set("userApiKey", apiKey)
	}
	return context.WithValue(context.Background(), "gin", ginCtx)
}

func testContextWithClientIP(apiKey, clientIP string) context.Context {
	setGinTestMode()
	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	if apiKey != "" {
		ginCtx.Set("userApiKey", apiKey)
	}
	if clientIP != "" {
		req := httptest.NewRequest("GET", "http://local/", nil)
		req.RemoteAddr = clientIP + ":12345"
		ginCtx.Request = req
	}
	return context.WithValue(context.Background(), "gin", ginCtx)
}

// TestIsolateClientControlledSessionKey_AnonymousDistinctIPsIsolated verifies
// that anonymous callers (no userApiKey) from different client IPs do not share
// the degraded namespace: identical client-controlled session keys must yield
// distinct isolated keys (review S1).
func TestIsolateClientControlledSessionKey_AnonymousDistinctIPsIsolated(t *testing.T) {
	t.Parallel()

	keyA := IsolateClientControlledSessionKey(testContextWithClientIP("", "203.0.113.10"), "session:shared")
	keyB := IsolateClientControlledSessionKey(testContextWithClientIP("", "203.0.113.20"), "session:shared")
	if keyA == "" || keyB == "" {
		t.Fatalf("keys must be non-empty: A=%q B=%q", keyA, keyB)
	}
	if keyA == keyB {
		t.Fatalf("anonymous callers from distinct IPs must not share key: both %q", keyA)
	}

	// Same IP + same session key must stay deterministic so in-process
	// continuity holds within one client.
	if keyA != IsolateClientControlledSessionKey(testContextWithClientIP("", "203.0.113.10"), "session:shared") {
		t.Fatal("degraded key must be deterministic for the same client IP")
	}

	// Different session keys from the same IP must remain distinct.
	if keyC := IsolateClientControlledSessionKey(testContextWithClientIP("", "203.0.113.10"), "session:other"); keyC == keyA {
		t.Fatal("distinct session keys from the same IP must not collide")
	}

	// Caller with an API key is unaffected by the IP entropy.
	keyD := IsolateClientControlledSessionKey(testContextWithClientIP("api-key-a", "203.0.113.10"), "session:shared")
	if keyD == keyA {
		t.Fatal("caller-isolated key must differ from degraded key")
	}
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

// TestIsolateClientControlledSessionKey_IgnoresXForwardedFor verifies that an
// anonymous caller cannot join another client's degraded namespace by forging
// an X-Forwarded-For header. The degraded IP split must be keyed by the
// unspoofable peer address (RemoteAddr), not gin's ClientIP() which trusts
// client-supplied X-Forwarded-For by default (review S1).
func TestIsolateClientControlledSessionKey_IgnoresXForwardedFor(t *testing.T) {
	t.Parallel()
	setGinTestMode()

	// Anonymous caller whose real peer is 203.0.113.10 forges X-Forwarded-For
	// claiming to be the victim 203.0.113.99.
	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest("GET", "http://local/", nil)
	req.RemoteAddr = "203.0.113.10:54321"
	req.Header.Set("X-Forwarded-For", "203.0.113.99")
	ginCtx.Request = req
	ctx := context.WithValue(context.Background(), "gin", ginCtx)

	forgedKey := IsolateClientControlledSessionKey(ctx, "session:shared")
	victimKey := IsolateClientControlledSessionKey(testContextWithClientIP("", "203.0.113.99"), "session:shared")
	if forgedKey == victimKey {
		t.Fatalf("forged X-Forwarded-For must not join the victim's degraded namespace: both %q", forgedKey)
	}
	// The forged caller must land in its own peer-IP namespace instead.
	peerKey := IsolateClientControlledSessionKey(testContextWithClientIP("", "203.0.113.10"), "session:shared")
	if forgedKey != peerKey {
		t.Fatalf("forged caller should be keyed by peer RemoteAddr IP: got %q want %q", forgedKey, peerKey)
	}
}
