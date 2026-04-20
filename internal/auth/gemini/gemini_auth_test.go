package gemini

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func freeTCPPort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	return listener.Addr().(*net.TCPAddr).Port
}

func waitForTCPPort(t *testing.T, port int) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	address := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for callback server on %s", address)
}

func testOAuthConfig(callbackPort int) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     ClientID,
		ClientSecret: ClientSecret,
		RedirectURL:  fmt.Sprintf("http://localhost:%d/oauth2callback", callbackPort),
		Scopes:       Scopes,
		Endpoint:     google.Endpoint,
	}
}

func TestBuildGeminiAuthURL_UsesRandomStatePerLogin(t *testing.T) {
	config := testOAuthConfig(DefaultCallbackPort)

	stateA, authURLA, err := buildGeminiAuthURL(config)
	if err != nil {
		t.Fatalf("buildGeminiAuthURL() error = %v", err)
	}
	stateB, authURLB, err := buildGeminiAuthURL(config)
	if err != nil {
		t.Fatalf("buildGeminiAuthURL() second call error = %v", err)
	}
	if stateA == "" || stateB == "" {
		t.Fatal("expected non-empty oauth states")
	}
	if stateA == "state-token" || stateB == "state-token" {
		t.Fatal("expected dynamically generated oauth state, got fixed state-token")
	}
	if stateA == stateB {
		t.Fatalf("expected different oauth states, got duplicate %q", stateA)
	}

	parsedA, err := url.Parse(authURLA)
	if err != nil {
		t.Fatalf("url.Parse(authURLA) error = %v", err)
	}
	parsedB, err := url.Parse(authURLB)
	if err != nil {
		t.Fatalf("url.Parse(authURLB) error = %v", err)
	}
	if got := parsedA.Query().Get("state"); got != stateA {
		t.Fatalf("authURLA state = %q, want %q", got, stateA)
	}
	if got := parsedB.Query().Get("state"); got != stateB {
		t.Fatalf("authURLB state = %q, want %q", got, stateB)
	}
}

func TestGetTokenFromWeb_RejectsMismatchedState(t *testing.T) {
	g := NewGeminiAuth()
	callbackPort := freeTCPPort(t)
	conf := testOAuthConfig(callbackPort)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := g.getTokenFromWeb(ctx, conf, &WebLoginOptions{
			NoBrowser:    true,
			CallbackPort: callbackPort,
		})
		done <- err
	}()

	waitForTCPPort(t, callbackPort)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/oauth2callback?code=test-code&state=wrong-state", callbackPort))
	if err != nil {
		t.Fatalf("http.Get(callback) error = %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("callback status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "state mismatch") {
			t.Fatalf("getTokenFromWeb() error = %v, want state mismatch", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for getTokenFromWeb to return after state mismatch")
	}
}

func TestGetTokenFromWeb_HonorsCancellation(t *testing.T) {
	g := NewGeminiAuth()
	callbackPort := freeTCPPort(t)
	conf := testOAuthConfig(callbackPort)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := g.getTokenFromWeb(ctx, conf, &WebLoginOptions{
			NoBrowser:    true,
			CallbackPort: callbackPort,
		})
		done <- err
	}()

	waitForTCPPort(t, callbackPort)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("getTokenFromWeb() error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for getTokenFromWeb to stop after cancellation")
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", callbackPort))
	if err != nil {
		t.Fatalf("expected callback port to be released, got %v", err)
	}
	_ = listener.Close()
}
