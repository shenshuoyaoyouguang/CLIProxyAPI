// Package store provides test helpers for store implementations.
package store

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

// TestConfig returns a minimal valid config YAML for testing.
func TestConfig() []byte {
	return []byte(`port: 3456
host: 127.0.0.1
debug: true
`)
}

// TestAuth creates a test Auth with the given ID, provider, and disabled state.
func TestAuth(id, provider string, disabled bool) *cliproxyauth.Auth {
	now := time.Now().UTC()
	metadata := map[string]any{
		"access_token":  "sk-test-" + randomString(8),
		"refresh_token": "rt-test-" + randomString(8),
		"type":          provider,
	}
	if disabled {
		metadata["disabled"] = true
	}
	return &cliproxyauth.Auth{
		ID:        id,
		Provider:  provider,
		FileName:  id + ".json",
		Disabled:  disabled,
		Status:    cliproxyauth.StatusActive,
		Metadata:  metadata,
		CreatedAt: now,
		UpdatedAt: now,
		Attributes: map[string]string{
			cliproxyauth.AttributeSourceBackend: cliproxyauth.AuthSourceGit,
		},
	}
}

// WriteTestAuthFiles writes auth entries to JSON files in the given directory.
func WriteTestAuthFiles(t *testing.T, dir string, auths map[string]*cliproxyauth.Auth) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create auth dir: %v", err)
	}
	for filename, auth := range auths {
		path := filepath.Join(dir, filename)
		data, err := json.MarshalIndent(auth, "", "  ")
		if err != nil {
			t.Fatalf("marshal auth %s: %v", filename, err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("write auth %s: %v", filename, err)
		}
	}
}

// RunConcurrent runs fn concurrently n times and reports any errors via t.
func RunConcurrent(t *testing.T, n int, fn func() error) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// randomString generates a random alphanumeric string of length n.
func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var sb strings.Builder
	sb.Grow(n)
	for i := 0; i < n; i++ {
		sb.WriteByte(letters[rand.Intn(len(letters))])
	}
	return sb.String()
}
