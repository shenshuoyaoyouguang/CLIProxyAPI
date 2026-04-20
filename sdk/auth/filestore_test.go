package auth

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractAccessToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata map[string]any
		expected string
	}{
		{
			"antigravity top-level access_token",
			map[string]any{"access_token": "tok-abc"},
			"tok-abc",
		},
		{
			"gemini nested token.access_token",
			map[string]any{
				"token": map[string]any{"access_token": "tok-nested"},
			},
			"tok-nested",
		},
		{
			"top-level takes precedence over nested",
			map[string]any{
				"access_token": "tok-top",
				"token":        map[string]any{"access_token": "tok-nested"},
			},
			"tok-top",
		},
		{
			"empty metadata",
			map[string]any{},
			"",
		},
		{
			"whitespace-only access_token",
			map[string]any{"access_token": "   "},
			"",
		},
		{
			"wrong type access_token",
			map[string]any{"access_token": 12345},
			"",
		},
		{
			"token is not a map",
			map[string]any{"token": "not-a-map"},
			"",
		},
		{
			"nested whitespace-only",
			map[string]any{
				"token": map[string]any{"access_token": "  "},
			},
			"",
		},
		{
			"fallback to nested when top-level empty",
			map[string]any{
				"access_token": "",
				"token":        map[string]any{"access_token": "tok-fallback"},
			},
			"tok-fallback",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractAccessToken(tt.metadata)
			if got != tt.expected {
				t.Errorf("extractAccessToken() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFileTokenStoreList_RestoresPersistedAccountHealth(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	filePath := filepath.Join(baseDir, "claude.json")
	payload := map[string]any{
		"type": "claude",
		"account_health": map[string]any{
			"degraded":         true,
			"degraded_reason":  "429_rate_limited",
			"degraded_message": "429 rate limited",
			"cooldown_until":   time.Now().Add(30 * time.Minute).UnixMilli(),
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal auth payload: %v", err)
	}
	if err := os.WriteFile(filePath, data, 0o600); err != nil {
		t.Fatalf("failed to seed auth file: %v", err)
	}

	store := NewFileTokenStore()
	store.SetBaseDir(baseDir)

	auths, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("len(auths) = %d, want 1", len(auths))
	}
	if !auths[0].Unavailable {
		t.Fatalf("expected auth to restore unavailable=true from persisted health")
	}
	if _, ok := auths[0].AccountHealth(); !ok {
		t.Fatalf("expected account health metadata to be restored")
	}
}
