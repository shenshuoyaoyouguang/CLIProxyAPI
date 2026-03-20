package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig_ClampsLogsMaxTotalSizeMB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("logs-max-total-size-mb: 1048577\n"), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.LogsMaxTotalSizeMB != MaxLogsMaxTotalSizeMB {
		t.Fatalf("expected logs-max-total-size-mb to be clamped to %d, got %d", MaxLogsMaxTotalSizeMB, cfg.LogsMaxTotalSizeMB)
	}
}

func TestSaveConfigPreserveComments_PrunesRemovedAmpUpstreamAPIKeysWithoutDroppingUnknownAmpKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := []byte("ampcode:\n  upstream-url: https://example.com\n  upstream-api-keys:\n    - upstream-api-key: old\n      api-keys:\n        - key\n  custom-extra: keep-me\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	cfg := &Config{
		AmpCode: AmpCode{
			UpstreamURL:     "https://example.com",
			UpstreamAPIKeys: nil,
		},
	}
	if err := SaveConfigPreserveComments(path, cfg); err != nil {
		t.Fatalf("SaveConfigPreserveComments returned error: %v", err)
	}

	persisted, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted config: %v", err)
	}
	persistedText := string(persisted)
	if strings.Contains(persistedText, "upstream-api-keys") {
		t.Fatalf("expected upstream-api-keys to be pruned, got %s", persistedText)
	}
	if !strings.Contains(persistedText, "custom-extra: keep-me") {
		t.Fatalf("expected unknown ampcode key to be preserved, got %s", persistedText)
	}
}
