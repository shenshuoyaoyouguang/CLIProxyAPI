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

func TestRedactProxyURL(t *testing.T) {
	t.Parallel()
	if got := redactProxyURL(""); got != "" {
		t.Fatalf("empty = %q", got)
	}
	if got := redactProxyURL("http://user:supersecretpass@proxy.example:8080"); got != "http://proxy.example:8080" {
		t.Fatalf("userinfo = %q, want host only", got)
	}
	if got := redactProxyURL("http://proxy.example:8080"); got != "http://proxy.example:8080" {
		t.Fatalf("plain = %q, want unchanged host URL", got)
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

func TestMarshalConfigForManagementJSON_RedactsProxyURLHeadersAndTLSKey(t *testing.T) {
	t.Parallel()
	const (
		proxyPassword = "supersecretpass"
		headerToken   = "hdr-secret-token-xyz"
		pemKey        = "-----BEGIN PRIVATE KEY-----\nMIIEsecretmaterial\n-----END PRIVATE KEY-----"
	)
	cfg := &config.Config{
		SDKConfig: config.SDKConfig{
			ProxyURL: "http://user:" + proxyPassword + "@global-proxy.example:3128",
			APIKeys:  []string{"client-api-key-abcdef"},
		},
		ClaudeKey: []config.ClaudeKey{
			{
				APIKey:   "sk-should-redact-12345678",
				ProxyURL: "http://user:" + proxyPassword + "@proxy.example:8080",
				Headers: map[string]string{
					"Authorization": "Bearer " + headerToken,
					"X-Custom":      "not-a-secret-header",
				},
			},
		},
		TLS: config.TLSConfig{
			Enable: true,
			Cert:   "-----BEGIN CERTIFICATE-----\nabc\n-----END CERTIFICATE-----",
			Key:    pemKey,
		},
	}
	raw, err := marshalConfigForManagementJSON(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	for _, secret := range []string{
		"sk-should-redact-12345678",
		proxyPassword,
		headerToken,
		"MIIEsecretmaterial",
		"client-api-key-abcdef",
	} {
		if strings.Contains(body, secret) {
			t.Fatalf("response still contains secret %q: %s", secret, body)
		}
	}
	// Operators should still see proxy host without credentials.
	if !strings.Contains(body, "proxy.example:8080") {
		t.Fatalf("expected redacted proxy host to remain visible: %s", body)
	}
	if !strings.Contains(body, "global-proxy.example:3128") {
		t.Fatalf("expected global proxy host to remain visible: %s", body)
	}
	// Non-sensitive custom headers stay readable.
	if !strings.Contains(body, "not-a-secret-header") {
		t.Fatalf("expected non-sensitive header value to remain: %s", body)
	}
	// Live config must remain unredacted.
	if cfg.ClaudeKey[0].ProxyURL != "http://user:"+proxyPassword+"@proxy.example:8080" {
		t.Fatalf("live proxy-url mutated: %q", cfg.ClaudeKey[0].ProxyURL)
	}
	if cfg.ClaudeKey[0].Headers["Authorization"] != "Bearer "+headerToken {
		t.Fatalf("live header mutated: %q", cfg.ClaudeKey[0].Headers["Authorization"])
	}
	if cfg.TLS.Key != pemKey {
		t.Fatalf("live tls key mutated")
	}
}
