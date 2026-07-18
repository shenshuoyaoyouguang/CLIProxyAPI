package management

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestRedactSecretValue(t *testing.T) {
	t.Parallel()
	if got := redactSecretValue(""); got != "" {
		t.Fatalf("empty = %q, want empty", got)
	}
	if got := redactSecretValue("short"); got != "***" {
		t.Fatalf("short = %q, want ***", got)
	}
	if got := redactSecretValue("sk-abcdefghijklmnop"); got != "sk-a...mnop" {
		t.Fatalf("long = %q, want sk-a...mnop", got)
	}
}

func TestMarshalConfigForManagementJSON_RedactsAPIKeys(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		ClaudeKey: []config.ClaudeKey{
			{APIKey: "sk-claude-secret-value"},
		},
		GeminiKey: []config.GeminiKey{
			{APIKey: "proxy-client-secret-key"},
		},
		OpenAICompatibility: []config.OpenAICompatibility{
			{
				Name: "alpha",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: "compat-secret-key-1234"},
				},
			},
		},
	}
	raw, err := marshalConfigForManagementJSON(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	for _, secret := range []string{
		"sk-claude-secret-value",
		"proxy-client-secret-key",
		"compat-secret-key-1234",
	} {
		if strings.Contains(body, secret) {
			t.Fatalf("response still contains secret %q: %s", secret, body)
		}
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Live config must remain unredacted.
	if cfg.ClaudeKey[0].APIKey != "sk-claude-secret-value" {
		t.Fatalf("live config was mutated: %q", cfg.ClaudeKey[0].APIKey)
	}
}
