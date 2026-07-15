package store

import (
	"context"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

// StoreFactory provides a factory for creating store instances in conformance tests.
type StoreFactory interface {
	NewStore(t interface {
		Helper()
		Fatalf(format string, args ...interface{})
		Skipf(format string, args ...interface{})
		TempDir() string
	}) (Store, func())
	Scheme() string
}

// Store is the common interface that all storage backends implement.
type Store interface {
	SetBaseDir(dir string)
	Bootstrap(ctx context.Context, exampleConfigPath string) error
	Save(ctx context.Context, auth *cliproxyauth.Auth) (string, error)
	List(ctx context.Context) ([]*cliproxyauth.Auth, error)
	Delete(ctx context.Context, id string) error
	PersistConfig(ctx context.Context) error
	PersistAuthFiles(ctx context.Context, message string, paths ...string) error
	Close() error
	ConfigPath() string
	AuthDir() string
}

// ConformanceTests runs the standard conformance test suite against a store factory.
func ConformanceTests(t *testing.T, factory StoreFactory) {
	t.Helper()

	t.Run("SaveAndList", func(t *testing.T) {
		store, cleanup := factory.NewStore(t)
		defer cleanup()

		// Save an auth
		auth := testAuth("conformance-test", "openai", false)
		path, err := store.Save(context.Background(), auth)
		if err != nil {
			t.Fatalf("Save: %v", err)
		}
		if path == "" {
			t.Fatal("Save returned empty path")
		}

		// List should include it
		list, err := store.List(context.Background())
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		found := false
		for _, a := range list {
			if a.ID == "conformance-test.json" {
				found = true
				if a.Provider != "openai" {
					t.Errorf("provider = %v, want openai", a.Provider)
				}
				break
			}
		}
		if !found {
			t.Error("saved auth not found in list")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		store, cleanup := factory.NewStore(t)
		defer cleanup()

		auth := testAuth("delete-test", "claude", false)
		if _, err := store.Save(context.Background(), auth); err != nil {
			t.Fatalf("Save: %v", err)
		}

		if err := store.Delete(context.Background(), "delete-test"); err != nil {
			t.Fatalf("Delete: %v", err)
		}

		list, err := store.List(context.Background())
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, a := range list {
			if a.ID == "delete-test.json" {
				t.Error("deleted auth still in list")
			}
		}
	})

	t.Run("Update", func(t *testing.T) {
		store, cleanup := factory.NewStore(t)
		defer cleanup()

		auth := testAuth("update-test", "gemini", false)
		auth.Metadata["access_token"] = "sk-initial"
		if _, err := store.Save(context.Background(), auth); err != nil {
			t.Fatalf("Save initial: %v", err)
		}

		auth.Metadata["access_token"] = "sk-updated"
		if _, err := store.Save(context.Background(), auth); err != nil {
			t.Fatalf("Save update: %v", err)
		}

		list, err := store.List(context.Background())
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, a := range list {
			if a.ID == "update-test.json" {
				if a.Metadata["access_token"] != "sk-updated" {
					t.Errorf("access_token = %v, want sk-updated", a.Metadata["access_token"])
				}
				return
			}
		}
		t.Error("updated auth not found in list")
	})

	t.Run("DisabledAuth", func(t *testing.T) {
		store, cleanup := factory.NewStore(t)
		defer cleanup()

		auth := testAuth("disabled-test", "openai", true)
		auth.Disabled = true
		if _, err := store.Save(context.Background(), auth); err != nil {
			t.Fatalf("Save disabled: %v", err)
		}

		list, err := store.List(context.Background())
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, a := range list {
			if a.ID == "disabled-test.json" && !a.Disabled {
				t.Error("disabled auth should have Disabled=true")
			}
		}
	})

	t.Run("ConfigRoundtrip", func(t *testing.T) {
		store1, cleanup1 := factory.NewStore(t)
		defer cleanup1()

		wantConfig := []byte("server:\n  port: 18080\n")
		configPath1 := store1.ConfigPath()
		if configPath1 == "" {
			t.Skip("store does not support config persistence")
		}
		if err := os.WriteFile(configPath1, wantConfig, 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if err := store1.PersistConfig(context.Background()); err != nil {
			t.Fatalf("PersistConfig: %v", err)
		}

		store2, cleanup2 := factory.NewStore(t)
		defer cleanup2()

		configPath := store2.ConfigPath()
		if configPath == "" {
			t.Skip("store does not support config persistence")
		}
		gotConfig, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		if normalizeLineEndings(string(gotConfig)) != normalizeLineEndings(string(wantConfig)) {
			t.Fatalf("config roundtrip mismatch\ngot:\n%s\nwant:\n%s", string(gotConfig), string(wantConfig))
		}
	})

	t.Run("Close", func(t *testing.T) {
		store, cleanup := factory.NewStore(t)
		defer cleanup()

		if err := store.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
}

// testAuth creates a test Auth for conformance tests.
func testAuth(id, provider string, disabled bool) *cliproxyauth.Auth {
	now := time.Now().UTC()
	return &cliproxyauth.Auth{
		ID:       id,
		Provider: provider,
		FileName: id + ".json",
		Disabled: disabled,
		Status:   cliproxyauth.StatusActive,
		Metadata: map[string]any{
			"access_token": "sk-conformance-" + randomString(8),
			"type":         provider,
		},
		CreatedAt: now,
		UpdatedAt: now,
		Attributes: map[string]string{
			cliproxyauth.AttributeSourceBackend: cliproxyauth.AuthSourceGit,
		},
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

// randomBool returns a random boolean.
func randomBool() bool {
	return rand.Intn(2) == 0
}

// contains reports whether substr is within s.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
