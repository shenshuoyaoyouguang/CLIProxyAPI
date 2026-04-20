package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestClaudeLogin_UsesCustomCallbackPort(t *testing.T) {
	callbackPort := freeTCPPort(t)
	authenticator := NewClaudeAuthenticator()
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
			return fmt.Errorf("timeout waiting for claude login to stop after cancellation")
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Login() error = %v, want context.Canceled", err)
	}

	wantRedirect := fmt.Sprintf(
		"redirect_uri=http%%3A%%2F%%2Flocalhost%%3A%d%%2Fcallback",
		callbackPort,
	)
	if !strings.Contains(output, wantRedirect) {
		t.Fatalf("login output did not contain custom redirect URI %q\noutput:\n%s", wantRedirect, output)
	}
}
