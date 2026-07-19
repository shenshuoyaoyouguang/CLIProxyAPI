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

// runRestoreRedactedSecrets is a tiny helper that calls restoreRedactedSecrets
// and fails the test on error, returning the restored body for assertions.
func runRestoreRedactedSecrets(t *testing.T, body []byte, current *config.Config) []byte {
	t.Helper()
	out, err := restoreRedactedSecrets(body, current)
	if err != nil {
		t.Fatalf("restoreRedactedSecrets error = %v", err)
	}
	return out
}

func TestRestoreRedactedSecrets_RestoresAllProviderAPIKeys(t *testing.T) {
	t.Parallel()
	const (
		claudeSecret = "sk-claude-secret-1234"
		codexSecret  = "sk-codex-secret-5678"
		geminiSecret = "sk-gemini-secret-abcd"
		xaiSecret    = "sk-xai-secret-wxyz-99"
		vertexSecret = "sk-vertex-secret-pppp"
	)
	current := &config.Config{
		ClaudeKey: []config.ClaudeKey{{APIKey: claudeSecret, BaseURL: "https://claude.example.com"}},
		CodexKey:  []config.CodexKey{{APIKey: codexSecret, BaseURL: "https://codex.example.com"}},
		GeminiKey: []config.GeminiKey{{APIKey: geminiSecret, BaseURL: "https://gemini.example.com"}},
		XAIKey:    []config.XAIKey{{APIKey: xaiSecret, BaseURL: "https://xai.example.com"}},
		VertexCompatAPIKey: []config.VertexCompatKey{
			{APIKey: vertexSecret, BaseURL: "https://vertex.example.com"},
		},
	}
	// Body uses redacted forms exactly as the management UI would send back.
	body := []byte(strings.Join([]string{
		"claude-api-key:",
		"  - api-key: " + redactSecretValue(claudeSecret),
		"    base-url: https://claude.example.com",
		"codex-api-key:",
		"  - api-key: " + redactSecretValue(codexSecret),
		"    base-url: https://codex.example.com",
		"gemini-api-key:",
		"  - api-key: " + redactSecretValue(geminiSecret),
		"    base-url: https://gemini.example.com",
		"xai-api-key:",
		"  - api-key: " + redactSecretValue(xaiSecret),
		"    base-url: https://xai.example.com",
		"vertex-api-key:",
		"  - api-key: " + redactSecretValue(vertexSecret),
		"    base-url: https://vertex.example.com",
		"",
	}, "\n"))

	out := runRestoreRedactedSecrets(t, body, current)
	outStr := string(out)
	for _, secret := range []string{claudeSecret, codexSecret, geminiSecret, xaiSecret, vertexSecret} {
		if !strings.Contains(outStr, secret) {
			t.Fatalf("restored body missing secret %q:\n%s", secret, outStr)
		}
		if strings.Contains(outStr, redactSecretValue(secret)) {
			t.Fatalf("restored body still has redacted placeholder for %q:\n%s", secret, outStr)
		}
	}
}

func TestRestoreRedactedSecrets_RestoresTopLevelAPIKeysAndProxyURL(t *testing.T) {
	t.Parallel()
	const (
		clientKey    = "client-api-key-abcdef"
		proxyWithCred = "http://user:supersecretpass@global-proxy.example:3128"
	)
	current := &config.Config{
		SDKConfig: config.SDKConfig{
			APIKeys:  []string{clientKey},
			ProxyURL: proxyWithCred,
		},
	}
	body := []byte(strings.Join([]string{
		"api-keys:",
		"  - " + redactSecretValue(clientKey),
		"proxy-url: " + redactProxyURL(proxyWithCred),
		"",
	}, "\n"))

	out := runRestoreRedactedSecrets(t, body, current)
	outStr := string(out)
	if !strings.Contains(outStr, clientKey) {
		t.Fatalf("api-keys not restored: %s", outStr)
	}
	if !strings.Contains(outStr, proxyWithCred) {
		t.Fatalf("proxy-url not restored (expected credentials): %s", outStr)
	}
	if strings.Contains(outStr, redactSecretValue(clientKey)) {
		t.Fatalf("api-keys still redacted: %s", outStr)
	}
}

func TestRestoreRedactedSecrets_RestoresSensitiveHeaders(t *testing.T) {
	t.Parallel()
	const (
		authToken = "Bearer bearer-token-secret-xyz"
		customTok = "x-custom-token-value-1234"
	)
	current := &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{
			{
				Name: "alpha",
				Headers: map[string]string{
					"Authorization": authToken,
					"X-Api-Key":     customTok,
					"X-Not-Secret":  "not-a-secret-value",
				},
			},
		},
	}
	body := []byte(strings.Join([]string{
		"openai-compatibility:",
		"  - name: alpha",
		"    headers:",
		"      Authorization: " + redactSecretValue(authToken),
		"      X-Api-Key: " + redactSecretValue(customTok),
		"      X-Not-Secret: not-a-secret-value",
		"",
	}, "\n"))

	out := runRestoreRedactedSecrets(t, body, current)
	outStr := string(out)
	if !strings.Contains(outStr, authToken) {
		t.Fatalf("Authorization not restored: %s", outStr)
	}
	if !strings.Contains(outStr, customTok) {
		t.Fatalf("X-Api-Key not restored: %s", outStr)
	}
	if !strings.Contains(outStr, "not-a-secret-value") {
		t.Fatalf("non-sensitive header should remain visible: %s", outStr)
	}
}

func TestRestoreRedactedSecrets_RestoresTLSPrivateKey(t *testing.T) {
	t.Parallel()
	const pemKey = "-----BEGIN PRIVATE KEY-----\nMIIEsecretmaterial\n-----END PRIVATE KEY-----"
	current := &config.Config{
		TLS: config.TLSConfig{
			Enable: true,
			Cert:   "-----BEGIN CERTIFICATE-----\nabc\n-----END CERTIFICATE-----",
			Key:    pemKey,
		},
	}
	body := []byte(strings.Join([]string{
		"tls:",
		"  enable: true",
		"  cert: |",
		"    -----BEGIN CERTIFICATE-----",
		"    abc",
		"    -----END CERTIFICATE-----",
		"  key: " + redactSecretValue(pemKey),
		"",
	}, "\n"))

	out := runRestoreRedactedSecrets(t, body, current)
	outStr := string(out)
	// The PEM content must be present (YAML may re-emit it as a literal
	// block with indentation, so check the key markers separately).
	if !strings.Contains(outStr, "-----BEGIN PRIVATE KEY-----") ||
		!strings.Contains(outStr, "MIIEsecretmaterial") ||
		!strings.Contains(outStr, "-----END PRIVATE KEY-----") {
		t.Fatalf("TLS private key not restored:\n%s", outStr)
	}
	if strings.Contains(outStr, redactSecretValue(pemKey)) {
		t.Fatalf("TLS key still redacted:\n%s", outStr)
	}
}

func TestRestoreRedactedSecrets_PreservesNewAPIKeys(t *testing.T) {
	t.Parallel()
	// Live config has the old key; the operator PUTs a brand-new full key
	// (not a redacted placeholder). Restoration must NOT overwrite it.
	const (
		oldKey = "sk-old-key-value-1234"
		newKey = "sk-brand-new-real-key-9876"
	)
	current := &config.Config{
		ClaudeKey: []config.ClaudeKey{{APIKey: oldKey}},
	}
	body := []byte(strings.Join([]string{
		"claude-api-key:",
		"  - api-key: " + newKey,
		"",
	}, "\n"))

	out := runRestoreRedactedSecrets(t, body, current)
	outStr := string(out)
	if !strings.Contains(outStr, newKey) {
		t.Fatalf("new key should be preserved: %s", outStr)
	}
	if strings.Contains(outStr, oldKey) {
		t.Fatalf("old key should NOT replace the new key: %s", outStr)
	}
}

func TestRestoreRedactedSecrets_OpenAICompatibilityReorderByname(t *testing.T) {
	t.Parallel()
	const (
		alphaKey = "sk-alpha-secret-aaaa"
		betaKey  = "sk-beta-secret-bbbb"
	)
	current := &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{
			{
				Name: "alpha",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: alphaKey},
				},
			},
			{
				Name: "beta",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: betaKey},
				},
			},
		},
	}
	// Body has beta FIRST (reordered) and uses redacted forms.
	body := []byte(strings.Join([]string{
		"openai-compatibility:",
		"  - name: beta",
		"    api-key-entries:",
		"      - api-key: " + redactSecretValue(betaKey),
		"  - name: alpha",
		"    api-key-entries:",
		"      - api-key: " + redactSecretValue(alphaKey),
		"",
	}, "\n"))

	out := runRestoreRedactedSecrets(t, body, current)
	outStr := string(out)
	// Both secrets must be restored despite the reorder.
	if !strings.Contains(outStr, betaKey) {
		t.Fatalf("beta key not restored: %s", outStr)
	}
	if !strings.Contains(outStr, alphaKey) {
		t.Fatalf("alpha key not restored: %s", outStr)
	}
	// Order should be preserved as beta-then-alpha (restoration must not
	// rewrite structure).
	betaIdx := strings.Index(outStr, "name: beta")
	alphaIdx := strings.Index(outStr, "name: alpha")
	if betaIdx < 0 || alphaIdx < 0 || betaIdx > alphaIdx {
		t.Fatalf("expected beta before alpha, got beta=%d alpha=%d:\n%s", betaIdx, alphaIdx, outStr)
	}
}

func TestRestoreRedactedSecrets_PreservesCommentsAndOrder(t *testing.T) {
	t.Parallel()
	const secret = "sk-claude-secret-1234"
	current := &config.Config{
		ClaudeKey: []config.ClaudeKey{{APIKey: secret}},
	}
	body := []byte(strings.Join([]string{
		"# top-level comment",
		"debug: true  # inline comment",
		"",
		"# provider section",
		"claude-api-key:",
		"  - api-key: " + redactSecretValue(secret) + "  # masked",
		"",
	}, "\n"))

	out := runRestoreRedactedSecrets(t, body, current)
	outStr := string(out)
	if !strings.Contains(outStr, secret) {
		t.Fatalf("secret not restored:\n%s", outStr)
	}
	if !strings.Contains(outStr, "# top-level comment") {
		t.Fatalf("top-level comment lost:\n%s", outStr)
	}
	if !strings.Contains(outStr, "# provider section") {
		t.Fatalf("section comment lost:\n%s", outStr)
	}
	if !strings.Contains(outStr, "# inline comment") {
		t.Fatalf("inline comment lost:\n%s", outStr)
	}
	if !strings.Contains(outStr, "# masked") {
		t.Fatalf("masked comment lost:\n%s", outStr)
	}
}

func TestRestoreRedactedSecrets_AmbiguousRedactionLeftAlone(t *testing.T) {
	t.Parallel()
	// Two DIFFERENT secrets share the same redacted prefix/suffix. The
	// fallback scan finds multiple matches and cannot disambiguate, so the
	// redacted value must be left in place rather than risk restoring the
	// wrong credential.
	const (
		secretA = "sk-alpha-middle-one-aaaa"
		secretB = "sk-alpha-middle-two-aaaa"
	)
	if redactSecretValue(secretA) != redactSecretValue(secretB) {
		t.Fatalf("test setup: secrets must share redaction, got %q vs %q",
			redactSecretValue(secretA), redactSecretValue(secretB))
	}
	current := &config.Config{
		ClaudeKey: []config.ClaudeKey{
			{APIKey: secretA, BaseURL: "https://a.example.com"},
			{APIKey: secretB, BaseURL: "https://b.example.com"},
		},
	}
	// Body swaps order so the index-aligned candidate is the wrong one;
	// since redactions are identical the fallback scan also can't
	// disambiguate. The redacted placeholder must remain.
	redacted := redactSecretValue(secretA)
	body := []byte(strings.Join([]string{
		"claude-api-key:",
		"  - base-url: https://b.example.com",
		"    api-key: " + redacted,
		"  - base-url: https://a.example.com",
		"    api-key: " + redacted,
		"",
	}, "\n"))

	out := runRestoreRedactedSecrets(t, body, current)
	outStr := string(out)
	if !strings.Contains(outStr, redacted) {
		t.Fatalf("ambiguous redaction should be left in place, got:\n%s", outStr)
	}
	if strings.Contains(outStr, secretA) || strings.Contains(outStr, secretB) {
		t.Fatalf("ambiguous redaction should NOT be restored to either secret:\n%s", outStr)
	}
}

func TestRestoreRedactedSecrets_NilCurrentReturnsBodyUnchanged(t *testing.T) {
	t.Parallel()
	body := []byte("claude-api-key:\n  - api-key: sk-a...1234\n")
	out, err := restoreRedactedSecrets(body, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(body) {
		t.Fatalf("nil current should return body unchanged; got:\n%s", string(out))
	}
}

func TestRestoreRedactedSecrets_InvalidYAMLReturnsError(t *testing.T) {
	t.Parallel()
	body := []byte("claude-api-key: [unclosed\n")
	_, err := restoreRedactedSecrets(body, &config.Config{})
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestRestoreRedactedSecrets_RestoresProviderProxyURLWithUserinfo(t *testing.T) {
	t.Parallel()
	const proxyWithCred = "http://user:supersecretpass@proxy.example:8080"
	current := &config.Config{
		ClaudeKey: []config.ClaudeKey{
			{APIKey: "sk-key", ProxyURL: proxyWithCred},
		},
	}
	body := []byte(strings.Join([]string{
		"claude-api-key:",
		"  - api-key: sk-key",
		"    proxy-url: " + redactProxyURL(proxyWithCred),
		"",
	}, "\n"))

	out := runRestoreRedactedSecrets(t, body, current)
	outStr := string(out)
	if !strings.Contains(outStr, proxyWithCred) {
		t.Fatalf("provider proxy-url credentials not restored:\n%s", outStr)
	}
}

func TestRestoreRedactedSecrets_OpenAICompatibilityAPIKeyEntries(t *testing.T) {
	t.Parallel()
	const (
		entry1Key = "sk-entry-one-secret-aa"
		entry2Key = "sk-entry-two-secret-bb"
	)
	current := &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{
			{
				Name: "alpha",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: entry1Key, ProxyURL: "http://u:p@proxy1.example:3128"},
					{APIKey: entry2Key, ProxyURL: ""},
				},
			},
		},
	}
	body := []byte(strings.Join([]string{
		"openai-compatibility:",
		"  - name: alpha",
		"    api-key-entries:",
		"      - api-key: " + redactSecretValue(entry1Key),
		"        proxy-url: " + redactProxyURL("http://u:p@proxy1.example:3128"),
		"      - api-key: " + redactSecretValue(entry2Key),
		"",
	}, "\n"))

	out := runRestoreRedactedSecrets(t, body, current)
	outStr := string(out)
	if !strings.Contains(outStr, entry1Key) {
		t.Fatalf("entry1 key not restored:\n%s", outStr)
	}
	if !strings.Contains(outStr, entry2Key) {
		t.Fatalf("entry2 key not restored:\n%s", outStr)
	}
	if !strings.Contains(outStr, "http://u:p@proxy1.example:3128") {
		t.Fatalf("entry1 proxy credentials not restored:\n%s", outStr)
	}
}

func TestRestoreRedactedSecrets_RoundTripsViaMarshalConfigForManagementJSON(t *testing.T) {
	t.Parallel()
	// This is the canonical regression: marshal redacts, then restore must
	// undo it. Compose the YAML body manually from the same redaction
	// helpers so the test stays focused on the contract.
	const secret = "sk-claude-secret-1234"
	current := &config.Config{
		ClaudeKey: []config.ClaudeKey{{APIKey: secret}},
	}
	redacted, errMarshal := marshalConfigForManagementJSON(current)
	if errMarshal != nil {
		t.Fatalf("marshal: %v", errMarshal)
	}
	var doc map[string]any
	if errDecode := json.Unmarshal(redacted, &doc); errDecode != nil {
		t.Fatalf("decode: %v", errDecode)
	}
	redactedKey := doc["claude-api-key"].([]any)[0].(map[string]any)["api-key"].(string)
	if redactedKey == secret {
		t.Fatal("marshal did not redact the api key")
	}
	body := []byte("claude-api-key:\n  - api-key: " + redactedKey + "\n")
	out := runRestoreRedactedSecrets(t, body, current)
	if !strings.Contains(string(out), secret) {
		t.Fatalf("round-trip restore failed; redacted=%s, body=\n%s", redactedKey, string(out))
	}
}
