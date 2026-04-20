package claude

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"golang.org/x/net/proxy"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNewClaudeAuthWithProxyURL_OverrideDirectTakesPrecedence(t *testing.T) {
	cfg := &config.Config{SDKConfig: config.SDKConfig{ProxyURL: "socks5://proxy.example.com:1080"}}
	auth := NewClaudeAuthWithProxyURL(cfg, "direct")

	transport, ok := auth.httpClient.Transport.(*utlsRoundTripper)
	if !ok || transport == nil {
		t.Fatalf("expected utlsRoundTripper, got %T", auth.httpClient.Transport)
	}
	if transport.dialer != proxy.Direct {
		t.Fatalf("expected proxy.Direct, got %T", transport.dialer)
	}
}

func TestNewClaudeAuthWithProxyURL_OverrideProxyAppliedWithoutConfig(t *testing.T) {
	auth := NewClaudeAuthWithProxyURL(nil, "socks5://proxy.example.com:1080")

	transport, ok := auth.httpClient.Transport.(*utlsRoundTripper)
	if !ok || transport == nil {
		t.Fatalf("expected utlsRoundTripper, got %T", auth.httpClient.Transport)
	}
	if transport.dialer == proxy.Direct {
		t.Fatalf("expected proxy dialer, got %T", transport.dialer)
	}
}

func TestGenerateAuthURLWithRedirect_UsesProvidedRedirect(t *testing.T) {
	auth := NewClaudeAuthWithProxyURL(&config.Config{}, "")

	authURL, returnedState, err := auth.GenerateAuthURLWithRedirect("state-1", "http://localhost:2468/callback", &PKCECodes{
		CodeVerifier:  "verifier",
		CodeChallenge: "challenge",
	})
	if err != nil {
		t.Fatalf("GenerateAuthURLWithRedirect() error = %v", err)
	}
	if returnedState != "state-1" {
		t.Fatalf("returnedState = %q, want %q", returnedState, "state-1")
	}

	parsedURL, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("url.Parse(authURL) error = %v", err)
	}
	if got := parsedURL.Query().Get("redirect_uri"); got != "http://localhost:2468/callback" {
		t.Fatalf("redirect_uri = %q, want %q", got, "http://localhost:2468/callback")
	}
}

func TestExchangeCodeForTokensWithRedirect_UsesProvidedRedirect(t *testing.T) {
	var requestBody []byte
	auth := &ClaudeAuth{
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				var errRead error
				requestBody, errRead = io.ReadAll(req.Body)
				if errRead != nil {
					t.Fatalf("io.ReadAll(req.Body) error = %v", errRead)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(
						`{"access_token":"tok-123","refresh_token":"ref-123","token_type":"Bearer","expires_in":3600,"organization":{"uuid":"org-1","name":"Org"},"account":{"uuid":"acct-1","email_address":"claude@example.com"}}`,
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
		"state-1",
		"http://localhost:2468/callback",
		&PKCECodes{
			CodeVerifier:  "verifier",
			CodeChallenge: "challenge",
		},
	)
	if err != nil {
		t.Fatalf("ExchangeCodeForTokensWithRedirect() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(requestBody, &payload); err != nil {
		t.Fatalf("json.Unmarshal(requestBody) error = %v", err)
	}
	if got, _ := payload["redirect_uri"].(string); got != "http://localhost:2468/callback" {
		t.Fatalf("redirect_uri = %q, want %q", got, "http://localhost:2468/callback")
	}
}
