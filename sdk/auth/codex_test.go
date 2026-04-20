package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	codexinternal "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/codex"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}

	os.Stdout = writer
	outputCh := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		outputCh <- string(data)
	}()

	runErr := fn()

	_ = writer.Close()
	os.Stdout = oldStdout

	return <-outputCh, runErr
}

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

func TestBuildAuthRecordIncludesOAuthMetadata(t *testing.T) {
	authenticator := NewCodexAuthenticator()
	authSvc := codexinternal.NewCodexAuth(&config.Config{})

	record, err := authenticator.buildAuthRecord(authSvc, &codexinternal.CodexAuthBundle{
		TokenData: codexinternal.CodexTokenData{
			AccessToken:  "access-123",
			RefreshToken: "refresh-123",
			AccountID:    "account-123",
			Email:        "codex@example.com",
			Expire:       "2026-04-21T00:00:00Z",
		},
		LastRefresh: "2026-04-20T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("buildAuthRecord() error = %v", err)
	}

	if got, _ := record.Metadata["access_token"].(string); got != "access-123" {
		t.Fatalf("metadata.access_token = %q, want %q", got, "access-123")
	}
	if got, _ := record.Metadata["account_id"].(string); got != "account-123" {
		t.Fatalf("metadata.account_id = %q, want %q", got, "account-123")
	}
	if got, _ := record.Metadata["email"].(string); got != "codex@example.com" {
		t.Fatalf("metadata.email = %q, want %q", got, "codex@example.com")
	}
}

func TestCodexLogin_UsesCustomCallbackPortAndHonorsCancellation(t *testing.T) {
	callbackPort := freeTCPPort(t)
	authenticator := NewCodexAuthenticator()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	output, err := captureStdout(t, func() error {
		done := make(chan error, 1)
		go func() {
			_, loginErr := authenticator.Login(ctx, &config.Config{}, &LoginOptions{
				NoBrowser:    true,
				CallbackPort: callbackPort,
			})
			done <- loginErr
		}()

		time.Sleep(200 * time.Millisecond)
		cancel()

		select {
		case loginErr := <-done:
			return loginErr
		case <-time.After(3 * time.Second):
			return fmt.Errorf("timeout waiting for login to stop after cancellation")
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Login() error = %v, want context.Canceled", err)
	}

	wantRedirect := fmt.Sprintf(
		"redirect_uri=http%%3A%%2F%%2Flocalhost%%3A%d%%2Fauth%%2Fcallback",
		callbackPort,
	)
	if !strings.Contains(output, wantRedirect) {
		t.Fatalf("login output did not contain custom redirect URI %q\noutput:\n%s", wantRedirect, output)
	}
}
