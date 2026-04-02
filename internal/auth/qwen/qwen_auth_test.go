package qwen

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestPollForTokenUsesInjectedHTTPClient(t *testing.T) {
	var captured *http.Request
	qa := &QwenAuth{
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				captured = req.Clone(req.Context())
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{"access_token":"access-token","refresh_token":"refresh-token","token_type":"Bearer","resource_url":"resource.qwen.ai","expires_in":3600}`)),
				}, nil
			}),
		},
	}

	tokenData, err := qa.PollForToken("device-code", "code-verifier")
	if err != nil {
		t.Fatalf("PollForToken error: %v", err)
	}
	if captured == nil {
		t.Fatal("expected injected http client to capture request")
	}
	if captured.URL.String() != QwenOAuthTokenEndpoint {
		t.Fatalf("request URL = %q, want %q", captured.URL.String(), QwenOAuthTokenEndpoint)
	}
	if got := captured.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type = %q, want %q", got, "application/x-www-form-urlencoded")
	}
	if got := captured.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("Accept = %q, want %q", got, "application/json")
	}
	if got := captured.Header.Get("User-Agent"); got != qwenUserAgent() {
		t.Fatalf("User-Agent = %q, want %q", got, qwenUserAgent())
	}
	if err := captured.ParseForm(); err != nil {
		t.Fatalf("ParseForm error: %v", err)
	}
	if got := captured.PostForm.Get("grant_type"); got != QwenOAuthGrantType {
		t.Fatalf("grant_type = %q, want %q", got, QwenOAuthGrantType)
	}
	if got := captured.PostForm.Get("client_id"); got != QwenOAuthClientID {
		t.Fatalf("client_id = %q, want %q", got, QwenOAuthClientID)
	}
	if got := captured.PostForm.Get("device_code"); got != "device-code" {
		t.Fatalf("device_code = %q, want %q", got, "device-code")
	}
	if got := captured.PostForm.Get("code_verifier"); got != "code-verifier" {
		t.Fatalf("code_verifier = %q, want %q", got, "code-verifier")
	}
	if tokenData.AccessToken != "access-token" {
		t.Fatalf("AccessToken = %q, want %q", tokenData.AccessToken, "access-token")
	}
	if tokenData.RefreshToken != "refresh-token" {
		t.Fatalf("RefreshToken = %q, want %q", tokenData.RefreshToken, "refresh-token")
	}
	if tokenData.TokenType != "Bearer" {
		t.Fatalf("TokenType = %q, want %q", tokenData.TokenType, "Bearer")
	}
	if tokenData.ResourceURL != "resource.qwen.ai" {
		t.Fatalf("ResourceURL = %q, want %q", tokenData.ResourceURL, "resource.qwen.ai")
	}
	if tokenData.Expire == "" {
		t.Fatal("expected Expire to be populated")
	}
}
