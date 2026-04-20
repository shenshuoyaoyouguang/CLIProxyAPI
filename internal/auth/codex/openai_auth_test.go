package codex

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRefreshTokensWithRetry_NonRetryableOnlyAttemptsOnce(t *testing.T) {
	var calls int32
	auth := &CodexAuth{
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				atomic.AddInt32(&calls, 1)
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_grant","code":"refresh_token_reused"}`)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}),
		},
	}

	_, err := auth.RefreshTokensWithRetry(context.Background(), "dummy_refresh_token", 3)
	if err == nil {
		t.Fatalf("expected error for non-retryable refresh failure")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "refresh_token_reused") {
		t.Fatalf("expected refresh_token_reused in error, got: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 refresh attempt, got %d", got)
	}
}

func TestNewCodexAuthWithProxyURL_OverrideDirectDisablesProxy(t *testing.T) {
	cfg := &config.Config{SDKConfig: config.SDKConfig{ProxyURL: "http://proxy.example.com:8080"}}
	auth := NewCodexAuthWithProxyURL(cfg, "direct")

	transport, ok := auth.httpClient.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("expected http.Transport, got %T", auth.httpClient.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("expected direct transport to disable proxy function")
	}
}

func TestNewCodexAuthWithProxyURL_OverrideProxyTakesPrecedence(t *testing.T) {
	cfg := &config.Config{SDKConfig: config.SDKConfig{ProxyURL: "http://global.example.com:8080"}}
	auth := NewCodexAuthWithProxyURL(cfg, "http://override.example.com:8081")

	transport, ok := auth.httpClient.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("expected http.Transport, got %T", auth.httpClient.Transport)
	}
	req, errReq := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if errReq != nil {
		t.Fatalf("new request: %v", errReq)
	}
	proxyURL, errProxy := transport.Proxy(req)
	if errProxy != nil {
		t.Fatalf("proxy func: %v", errProxy)
	}
	if proxyURL == nil || proxyURL.String() != "http://override.example.com:8081" {
		t.Fatalf("proxy URL = %v, want http://override.example.com:8081", proxyURL)
	}
}

func TestGenerateAuthURLWithRedirect_UsesProvidedRedirect(t *testing.T) {
	auth := NewCodexAuthWithProxyURL(&config.Config{}, "")

	authURL, err := auth.GenerateAuthURLWithRedirect("state-1", "http://localhost:2468/auth/callback", &PKCECodes{
		CodeVerifier:  "verifier",
		CodeChallenge: "challenge",
	})
	if err != nil {
		t.Fatalf("GenerateAuthURLWithRedirect() error = %v", err)
	}

	parsedURL, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("Parse(authURL) error = %v", err)
	}
	if got := parsedURL.Query().Get("redirect_uri"); got != "http://localhost:2468/auth/callback" {
		t.Fatalf("redirect_uri = %q, want %q", got, "http://localhost:2468/auth/callback")
	}
}

func TestExchangeCodeForTokensWithRedirect_UsesProvidedRedirect(t *testing.T) {
	var requestBody string
	auth := &CodexAuth{
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body, errRead := io.ReadAll(req.Body)
				if errRead != nil {
					t.Fatalf("ReadAll(req.Body) error = %v", errRead)
				}
				requestBody = string(body)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(
						`{"access_token":"tok-123","refresh_token":"ref-123","id_token":"","token_type":"Bearer","expires_in":3600}`,
					)),
					Header:  make(http.Header),
					Request: req,
				}, nil
			}),
		},
	}

	_, err := auth.ExchangeCodeForTokensWithRedirect(
		context.Background(),
		"code-123",
		"http://localhost:2468/auth/callback",
		&PKCECodes{
			CodeVerifier:  "verifier",
			CodeChallenge: "challenge",
		},
	)
	if err != nil {
		t.Fatalf("ExchangeCodeForTokensWithRedirect() error = %v", err)
	}

	values, err := url.ParseQuery(requestBody)
	if err != nil {
		t.Fatalf("ParseQuery(requestBody) error = %v", err)
	}
	if got := values.Get("redirect_uri"); got != "http://localhost:2468/auth/callback" {
		t.Fatalf("redirect_uri = %q, want %q", got, "http://localhost:2468/auth/callback")
	}
}
