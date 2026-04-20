package auth

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestAntigravityLogin_HonorsCancellation(t *testing.T) {
	callbackPort := freeTCPPort(t)
	authenticator := NewAntigravityAuthenticator()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := captureStdout(t, func() error {
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
			return fmt.Errorf("timeout waiting for antigravity login to stop after cancellation")
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Login() error = %v, want context.Canceled", err)
	}
}
