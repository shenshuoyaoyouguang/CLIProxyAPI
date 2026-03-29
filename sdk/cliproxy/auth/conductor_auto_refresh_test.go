package auth

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

type autoRefreshTestExecutor struct {
	provider string
	started  chan string
	release  <-chan struct{}
	done     *sync.WaitGroup
}

func (e *autoRefreshTestExecutor) Identifier() string { return e.provider }

func (e *autoRefreshTestExecutor) Execute(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (e *autoRefreshTestExecutor) ExecuteStream(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, nil
}

func (e *autoRefreshTestExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	if e.done != nil {
		defer e.done.Done()
	}
	if e.started != nil {
		e.started <- auth.ID
	}
	if e.release != nil {
		<-e.release
	}
	return auth, nil
}

func (e *autoRefreshTestExecutor) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (e *autoRefreshTestExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestManagerStartAutoRefreshRunsInitialRefreshImmediately(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)

	started := make(chan string, 1)
	release := make(chan struct{})
	exec := &autoRefreshTestExecutor{
		provider: "gemini",
		started:  started,
		release:  release,
	}
	manager.RegisterExecutor(exec)

	auth := &Auth{
		ID:       "expired-on-startup",
		Provider: "gemini",
		Metadata: map[string]any{
			"email":                    "startup@example.com",
			"expires_at":               time.Now().Add(-time.Minute).Format(time.RFC3339),
			"refresh_interval_seconds": 300,
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer close(release)

	manager.StartAutoRefresh(ctx, time.Hour)
	defer manager.StopAutoRefresh()

	select {
	case gotID := <-started:
		if gotID != auth.ID {
			t.Fatalf("Refresh() auth ID = %q, want %q", gotID, auth.ID)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("expected expired auth to refresh immediately on startup")
	}
}

func TestManagerCheckRefreshesStartsMoreThanThreeDueAuthsPromptly(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)

	started := make(chan string, 4)
	release := make(chan struct{})
	var done sync.WaitGroup
	done.Add(4)

	exec := &autoRefreshTestExecutor{
		provider: "gemini",
		started:  started,
		release:  release,
		done:     &done,
	}
	manager.RegisterExecutor(exec)

	for i := 0; i < 4; i++ {
		auth := &Auth{
			ID:       "due-auth-" + strconv.Itoa(i),
			Provider: "gemini",
			Metadata: map[string]any{
				"email":                    "user-" + strconv.Itoa(i) + "@example.com",
				"expires_at":               time.Now().Add(-time.Minute).Format(time.RFC3339),
				"refresh_interval_seconds": 300,
			},
		}
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatalf("register auth %d: %v", i, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager.checkRefreshes(ctx)

	seen := make(map[string]struct{}, 4)
	timeout := time.After(250 * time.Millisecond)
	for len(seen) < 4 {
		select {
		case authID := <-started:
			seen[authID] = struct{}{}
		case <-timeout:
			t.Fatalf("expected 4 refreshes to start promptly, got %d", len(seen))
		}
	}

	close(release)
	done.Wait()
}
