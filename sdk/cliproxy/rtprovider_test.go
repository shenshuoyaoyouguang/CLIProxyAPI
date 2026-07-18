package cliproxy

import (
	"net/http"
	"testing"

	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestRoundTripperForDirectBypassesProxy(t *testing.T) {
	t.Parallel()

	provider := newDefaultRoundTripperProvider()
	rt := provider.RoundTripperFor(&coreauth.Auth{ProxyURL: "direct"})
	transport, ok := rt.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", rt)
	}
	if transport.Proxy != nil {
		t.Fatal("expected direct transport to disable proxy function")
	}
}

func TestRoundTripperForEmptyProxyIsolatesByAuth(t *testing.T) {
	t.Parallel()

	provider := newDefaultRoundTripperProvider()
	authA := &coreauth.Auth{
		ID: "auth-a",
		Attributes: map[string]string{
			"api_key":  "key-a",
			"base_url": "https://api.deepseek.com",
		},
	}
	authB := &coreauth.Auth{
		ID: "auth-b",
		Attributes: map[string]string{
			"api_key":  "key-b",
			"base_url": "https://api.deepseek.com",
		},
	}

	rtA1 := provider.RoundTripperFor(authA)
	rtA2 := provider.RoundTripperFor(authA)
	rtB := provider.RoundTripperFor(authB)

	if rtA1 == nil || rtB == nil {
		t.Fatalf("expected non-nil transports, got A=%v B=%v", rtA1, rtB)
	}
	if rtA1 != rtA2 {
		t.Fatal("same auth must reuse the cached direct transport")
	}
	if rtA1 == rtB {
		t.Fatal("different credentials must not share a direct transport")
	}
}

func TestRoundTripperForProxySharedByProxyURL(t *testing.T) {
	t.Parallel()

	provider := newDefaultRoundTripperProvider()
	proxyURL := "http://proxy.example.com:8080"
	authA := &coreauth.Auth{ID: "auth-a", ProxyURL: proxyURL, Attributes: map[string]string{"api_key": "key-a"}}
	authB := &coreauth.Auth{ID: "auth-b", ProxyURL: proxyURL, Attributes: map[string]string{"api_key": "key-b"}}

	rtA := provider.RoundTripperFor(authA)
	rtB := provider.RoundTripperFor(authB)
	if rtA == nil || rtB == nil {
		t.Fatalf("expected non-nil proxy transports, got A=%v B=%v", rtA, rtB)
	}
	if rtA != rtB {
		t.Fatal("same proxy URL must share one transport")
	}
}

func TestRoundTripperInvalidateAuthDropsDirectCache(t *testing.T) {
	t.Parallel()

	provider := newDefaultRoundTripperProvider()
	auth := &coreauth.Auth{
		ID: "auth-invalidate",
		Attributes: map[string]string{
			"api_key":  "key-invalidate",
			"base_url": "https://api.deepseek.com",
		},
	}

	first := provider.RoundTripperFor(auth)
	if first == nil {
		t.Fatal("expected non-nil transport")
	}
	provider.InvalidateAuth(auth)
	second := provider.RoundTripperFor(auth)
	if second == nil {
		t.Fatal("expected rebuilt transport after invalidate")
	}
	if first == second {
		t.Fatal("invalidate must drop the cached direct transport")
	}
}
