package helps

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// antigravityGroundingURLTimeout bounds the HEAD redirect probe so a hanging
// Vertex Search endpoint cannot stall a streaming response. This is a bounded
// background probe (like the credits-hint probe), not the main upstream stream.
const antigravityGroundingURLTimeout = 5 * time.Second

// AntigravityGroundingURLCache memoizes resolved redirect URLs for one request.
// Streaming chunks repeatedly carry the same grounding chunks; resolving each
// URI once per request avoids a blocking HEAD per chunk.
type AntigravityGroundingURLCache struct {
	mu       sync.Mutex
	resolved map[string]string
}

// NewAntigravityGroundingURLCache creates an empty per-request cache.
func NewAntigravityGroundingURLCache() *AntigravityGroundingURLCache {
	return &AntigravityGroundingURLCache{resolved: map[string]string{}}
}

func isAntigravityVertexSearchRedirect(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.Scheme == "https" &&
		parsed.Host == "vertexaisearch.cloud.google.com" &&
		strings.HasPrefix(parsed.Path, "/grounding-api-redirect/")
}

func resolveAntigravityGroundingURL(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, rawURL string) string {
	if !isAntigravityVertexSearchRedirect(rawURL) {
		return rawURL
	}
	client := NewProxyAwareHTTPClient(ctx, cfg, auth, antigravityGroundingURLTimeout)
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	req, errReq := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if errReq != nil {
		log.WithError(errReq).Debug("antigravity grounding url: create redirect request failed")
		return rawURL
	}
	resp, errDo := client.Do(req)
	if errDo != nil {
		log.WithError(errDo).Debug("antigravity grounding url: resolve redirect failed")
		return rawURL
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.WithError(errClose).Debug("antigravity grounding url: close redirect response failed")
		}
	}()

	if resp.StatusCode < http.StatusMultipleChoices || resp.StatusCode >= http.StatusBadRequest {
		return rawURL
	}
	location := strings.TrimSpace(resp.Header.Get("Location"))
	if location == "" {
		return rawURL
	}
	parsed, errParse := url.Parse(location)
	if errParse != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return rawURL
	}
	return location
}

// ResolveAntigravityGroundingURLs replaces Vertex Search redirect URLs in grounding chunks with their target URLs.
// cache memoizes resolutions for the lifetime of one request; pass nil to use a
// per-call map (correct for single-shot callers, wasteful per streaming chunk).
func ResolveAntigravityGroundingURLs(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, payload []byte, cache *AntigravityGroundingURLCache) []byte {
	if len(payload) == 0 {
		return payload
	}

	basePath := "response.candidates.0.groundingMetadata.groundingChunks"
	chunks := gjson.GetBytes(payload, basePath)
	if !chunks.IsArray() {
		basePath = "candidates.0.groundingMetadata.groundingChunks"
		chunks = gjson.GetBytes(payload, basePath)
	}
	if !chunks.IsArray() {
		return payload
	}

	output := payload
	for i, chunk := range chunks.Array() {
		uri := strings.TrimSpace(chunk.Get("web.uri").String())
		if uri == "" {
			continue
		}
		resolvedURI := resolveAntigravityGroundingURLCached(ctx, cfg, auth, uri, cache)
		if resolvedURI == uri {
			continue
		}
		updated, errSet := sjson.SetBytes(output, fmt.Sprintf("%s.%d.web.uri", basePath, i), resolvedURI)
		if errSet != nil {
			log.WithError(errSet).Debug("antigravity grounding url: set resolved url failed")
			continue
		}
		output = updated
	}
	return output
}

func resolveAntigravityGroundingURLCached(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, uri string, cache *AntigravityGroundingURLCache) string {
	if cache == nil {
		return resolveAntigravityGroundingURL(ctx, cfg, auth, uri)
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if resolved, ok := cache.resolved[uri]; ok {
		return resolved
	}
	resolved := resolveAntigravityGroundingURL(ctx, cfg, auth, uri)
	cache.resolved[uri] = resolved
	return resolved
}
