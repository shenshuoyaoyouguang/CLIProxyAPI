package auth

import (
	"os"
	"path/filepath"
	"testing"

	kimiauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/kimi"
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

func TestReadAuthFile_HydratesKimiStorage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "kimi.json")
	raw := `{
		"type":"kimi",
		"access_token":"access-1",
		"refresh_token":"refresh-1",
		"token_type":"Bearer",
		"scope":"user",
		"device_id":"device-1",
		"expired":"2026-03-10T10:00:00Z"
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	store := NewFileTokenStore()
	auth, err := store.readAuthFile(path, dir)
	if err != nil {
		t.Fatalf("readAuthFile() error = %v", err)
	}
	if auth == nil {
		t.Fatalf("readAuthFile() returned nil auth")
	}

	storage, ok := auth.Storage.(*kimiauth.KimiTokenStorage)
	if !ok || storage == nil {
		t.Fatalf("expected KimiTokenStorage, got %T", auth.Storage)
	}
	if storage.AccessToken != "access-1" {
		t.Fatalf("storage.AccessToken = %q, want %q", storage.AccessToken, "access-1")
	}
	if storage.RefreshToken != "refresh-1" {
		t.Fatalf("storage.RefreshToken = %q, want %q", storage.RefreshToken, "refresh-1")
	}
	if storage.DeviceID != "device-1" {
		t.Fatalf("storage.DeviceID = %q, want %q", storage.DeviceID, "device-1")
	}
	if storage.Type != "kimi" {
		t.Fatalf("storage.Type = %q, want %q", storage.Type, "kimi")
	}
}
