package cliproxy

import (
	"net/http"
	"strings"
	"sync"

	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/proxyutil"
	log "github.com/sirupsen/logrus"
)

// directTransportCacheLimit caps how many per-credential direct transports are retained.
// When exceeded the whole direct cache is dropped so idle pools can be GC'd.
const directTransportCacheLimit = 1024

// defaultRoundTripperProvider returns a per-auth HTTP RoundTripper.
// Proxy URLs are cached by the proxy string. Direct / empty-proxy credentials
// get an isolated transport keyed by auth identity so concurrent credentials
// for the same upstream host do not share one connection pool / HTTP/2 worker.
type defaultRoundTripperProvider struct {
	mu          sync.RWMutex
	proxyCache  map[string]http.RoundTripper
	directCache map[string]http.RoundTripper
}

func newDefaultRoundTripperProvider() *defaultRoundTripperProvider {
	return &defaultRoundTripperProvider{
		proxyCache:  make(map[string]http.RoundTripper),
		directCache: make(map[string]http.RoundTripper),
	}
}

// RoundTripperFor implements coreauth.RoundTripperProvider.
func (p *defaultRoundTripperProvider) RoundTripperFor(auth *coreauth.Auth) http.RoundTripper {
	if p == nil || auth == nil {
		return nil
	}
	proxyStr := strings.TrimSpace(auth.ProxyURL)
	if proxyStr == "" || isDirectProxySetting(proxyStr) {
		return p.roundTripperForDirect(auth, proxyStr)
	}

	p.mu.RLock()
	rt := p.proxyCache[proxyStr]
	p.mu.RUnlock()
	if rt != nil {
		return rt
	}

	transport, _, errBuild := proxyutil.BuildHTTPTransport(proxyStr)
	if errBuild != nil {
		log.Errorf("%v", errBuild)
		return nil
	}
	if transport == nil {
		return nil
	}

	p.mu.Lock()
	if existing := p.proxyCache[proxyStr]; existing != nil {
		p.mu.Unlock()
		return existing
	}
	if p.proxyCache == nil {
		p.proxyCache = make(map[string]http.RoundTripper)
	}
	p.proxyCache[proxyStr] = transport
	p.mu.Unlock()
	return transport
}

// InvalidateAuth drops any cached direct transport for the given auth so idle
// connections can be closed after the credential is removed.
func (p *defaultRoundTripperProvider) InvalidateAuth(auth *coreauth.Auth) {
	if p == nil || auth == nil {
		return
	}
	key := directTransportCacheKey(auth)
	if key == "" {
		return
	}
	p.mu.Lock()
	rt := p.directCache[key]
	delete(p.directCache, key)
	p.mu.Unlock()
	closeIdleConnections(rt)
}

func (p *defaultRoundTripperProvider) roundTripperForDirect(auth *coreauth.Auth, proxyStr string) http.RoundTripper {
	key := directTransportCacheKey(auth)
	if key == "" {
		return newDirectTransport(proxyStr)
	}

	p.mu.RLock()
	rt := p.directCache[key]
	p.mu.RUnlock()
	if rt != nil {
		return rt
	}

	transport := newDirectTransport(proxyStr)
	p.mu.Lock()
	if existing := p.directCache[key]; existing != nil {
		p.mu.Unlock()
		return existing
	}
	if p.directCache == nil {
		p.directCache = make(map[string]http.RoundTripper)
	}
	if len(p.directCache) >= directTransportCacheLimit {
		for _, old := range p.directCache {
			closeIdleConnections(old)
		}
		p.directCache = make(map[string]http.RoundTripper)
	}
	p.directCache[key] = transport
	p.mu.Unlock()
	return transport
}

func newDirectTransport(proxyStr string) *http.Transport {
	if isDirectProxySetting(proxyStr) {
		return proxyutil.NewDirectTransport()
	}
	// Empty proxy inherits DefaultTransport settings, including env-based proxy.
	if transport, ok := http.DefaultTransport.(*http.Transport); ok && transport != nil {
		return transport.Clone()
	}
	return &http.Transport{}
}

func isDirectProxySetting(proxyStr string) bool {
	return strings.EqualFold(proxyStr, "direct") || strings.EqualFold(proxyStr, "none")
}

func directTransportCacheKey(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	// EnsureIndex() returns "" exactly when auth has no ID and no other identity
	// seed (see Auth.indexSeed), so an additional auth.ID fallback here would be
	// unreachable.
	if idx := strings.TrimSpace(auth.EnsureIndex()); idx != "" {
		return "direct:" + idx
	}
	return ""
}

func closeIdleConnections(rt http.RoundTripper) {
	type idleCloser interface {
		CloseIdleConnections()
	}
	if closer, ok := rt.(idleCloser); ok {
		closer.CloseIdleConnections()
	}
}
