package management

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// TestGetConfig_RendersConfigUnderLock verifies that GetConfig produces a
// stable JSON snapshot of the current config and that the response is taken
// while holding h.mu. The fix replaced a shallow struct copy (`cfgCopy := *cfg`)
// followed by lock-free JSON encoding with json.Marshal under the lock, which
// prevents concurrent Put* handlers from racing the encoder through shared
// slice/map backing arrays.
func TestGetConfig_RendersConfigUnderLock(t *testing.T) {
	cfg := &config.Config{
		Host: "127.0.0.1",
		Port: 8080,
		OpenAICompatibility: []config.OpenAICompatibility{
			{Name: "alpha", BaseURL: "https://alpha.example/v1"},
			{Name: "beta", BaseURL: "https://beta.example/v1"},
		},
		ClaudeKey: []config.ClaudeKey{
			{APIKey: "sk-alpha"},
			{APIKey: "sk-beta"},
		},
		OAuthExcludedModels: map[string][]string{
			"claude": {"claude-opus-4-5", "claude-haiku-4-5"},
		},
	}
	h := NewHandlerWithoutConfigFilePath(cfg, nil)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)
	h.GetConfig(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body struct {
		Host                string `json:"host"`
		Port                int    `json:"port"`
		OpenAICompatibility []struct {
			Name    string `json:"name"`
			BaseURL string `json:"base-url"`
		} `json:"openai-compatibility"`
		ClaudeKey []struct {
			APIKey string `json:"api-key"`
		} `json:"claude-api-key"`
		OAuthExcludedModels map[string][]string `json:"oauth-excluded-models,omitempty"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// Note: Host and Port use json:"-" so they are not serialized. Only verify
	// the visible slice/map fields to confirm the deep snapshot round-trips.
	// API keys are redacted in GetConfig so a stolen management session cannot
	// bulk-export provider credentials.
	if len(body.OpenAICompatibility) != 2 {
		t.Fatalf("openai-compatibility entries = %d, want 2", len(body.OpenAICompatibility))
	}
	if body.OpenAICompatibility[0].Name != "alpha" || body.OpenAICompatibility[1].Name != "beta" {
		t.Fatalf("openai-compatibility order/content mismatch: %+v", body.OpenAICompatibility)
	}
	if len(body.ClaudeKey) != 2 {
		t.Fatalf("claude-api-key entries = %d, want 2", len(body.ClaudeKey))
	}
	if body.ClaudeKey[0].APIKey == "sk-alpha" || body.ClaudeKey[1].APIKey == "sk-beta" {
		t.Fatalf("claude-api-key must be redacted, got %+v", body.ClaudeKey)
	}
	if body.ClaudeKey[0].APIKey != redactSecretValue("sk-alpha") || body.ClaudeKey[1].APIKey != redactSecretValue("sk-beta") {
		t.Fatalf("claude-api-key redaction mismatch: %+v", body.ClaudeKey)
	}
	if body.OAuthExcludedModels["claude"] == nil || len(body.OAuthExcludedModels["claude"]) != 2 {
		t.Fatalf("oauth-excluded-models mismatch: %+v", body.OAuthExcludedModels)
	}
}

// TestGetConfig_NilHandlerAndNilConfig exercises the degenerate paths so the
// lock-free returns remain panic-free.
func TestGetConfig_NilHandlerAndNilConfig(t *testing.T) {
	t.Run("nil handler", func(t *testing.T) {
		var h *Handler
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)
		h.GetConfig(ctx)
		if rec.Code != 200 {
			t.Fatalf("nil handler status = %d, want 200", rec.Code)
		}
	})
	t.Run("nil cfg", func(t *testing.T) {
		h := NewHandlerWithoutConfigFilePath(nil, nil)
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)
		h.GetConfig(ctx)
		if rec.Code != 200 {
			t.Fatalf("nil cfg status = %d, want 200", rec.Code)
		}
	})
}

func TestPutConfigYAMLTriggersConfigReloadHook(t *testing.T) {
	t.Parallel()

	h := &Handler{
		cfg:            &config.Config{Debug: false},
		configFilePath: writeTestConfigFile(t),
	}
	reloads, reloadDone := captureConfigReload(h)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/v0/management/config.yaml", strings.NewReader("debug: true\n"))
	ctx.Request.Header.Set("Content-Type", "application/yaml")

	h.PutConfigYAML(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	cfgSnapshot := waitForAsyncReload(t, reloads)
	waitForReloadDone(t, reloadDone)
	if cfgSnapshot == h.cfg {
		t.Fatalf("reload config = handler config %p, want independent snapshot", h.cfg)
	}
	if !cfgSnapshot.Debug {
		t.Fatal("reload snapshot Debug = false, want true")
	}
	if h.cfg == nil || !h.cfg.Debug {
		t.Fatal("handler config Debug = false, want true")
	}
}

// TestGetConfig_ConcurrentMutationDoesNotRace runs GetConfig in parallel with
// SetConfig (full pointer swap) and direct slice mutations to confirm the
// lock-protected json.Marshal path no longer exposes shared backing arrays to
// the encoder. Run with `-race` to surface data races.
func TestGetConfig_ConcurrentMutationDoesNotRace(t *testing.T) {
	cfg := &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{
			{Name: "alpha", BaseURL: "https://alpha.example/v1"},
		},
		ClaudeKey: []config.ClaudeKey{
			{APIKey: "sk-alpha"},
		},
		OAuthExcludedModels: map[string][]string{
			"claude": {"claude-opus-4-5"},
		},
	}
	h := NewHandlerWithoutConfigFilePath(cfg, nil)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writer 1: repeatedly swap the entire *config.Config via SetConfig.
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			i++
			replacement := &config.Config{
				OpenAICompatibility: []config.OpenAICompatibility{
					{Name: "alpha", BaseURL: "https://alpha.example/v1"},
					{Name: "beta", BaseURL: "https://beta.example/v1"},
				},
				ClaudeKey: []config.ClaudeKey{
					{APIKey: "sk-alpha"},
					{APIKey: "sk-beta"},
				},
				OAuthExcludedModels: map[string][]string{
					"claude": {"claude-opus-4-5", "claude-haiku-4-5"},
					"gemini": {"gemini-3-pro-preview"},
				},
			}
			_ = i
			h.SetConfig(replacement)
		}
	}()

	// Writer 2: mutate the originally-shared slice/map backing arrays. If
	// GetConfig still did a shallow copy, the JSON encoder would race these
	// writes. Under the lock-based marshal fix, GetConfig sees a stable
	// pointer-to-config and any swap via SetConfig is observed atomically.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			h.mu.Lock()
			cfg.OpenAICompatibility = append(cfg.OpenAICompatibility[:0],
				config.OpenAICompatibility{Name: "rotated", BaseURL: "https://rotated.example/v1"})
			cfg.OAuthExcludedModels["gemini"] = []string{"gemini-3-pro-preview"}
			h.mu.Unlock()
		}
	}()

	// Reader: hammer GetConfig.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)
			h.GetConfig(ctx)
			if rec.Code != http.StatusOK {
				t.Errorf("GetConfig status = %d, want %d", rec.Code, http.StatusOK)
				return
			}
			// Body must be valid JSON; we do not assert shape because writers
			// race in arbitrary configs. The test's job here is to surface
			// races under `-race`, not to pin content.
			var generic map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &generic); err != nil {
				t.Errorf("GetConfig returned invalid JSON: %v (body=%s)", err, rec.Body.String())
				return
			}
		}
	}()

	// Let writers run briefly.
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Roughly 50ms of concurrent mutation.
		for i := 0; i < 50; i++ {
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)
			h.GetConfig(ctx)
		}
		close(stop)
	}()

	wg.Wait()
}

// TestPutConfigYAML_RestoresRedactedSecrets is the end-to-end regression test
// for the bug where GET /v0/management/config redacts api-keys and a
// subsequent PUT /v0/management/config.yaml persists the redacted values to
// disk, breaking every provider auth. The test sets up a handler with real
// secrets, PUTs a YAML body containing only redacted placeholders, and
// verifies that the on-disk file ends up with the original secret values
// (not the placeholders).
func TestPutConfigYAML_RestoresRedactedSecrets(t *testing.T) {
	t.Parallel()

	const (
		claudeSecret = "sk-claude-secret-1234"
		openaiKey    = "sk-openai-compat-aaaa"
		clientAPIKey = "client-api-key-abcdef"
	)
	dir := t.TempDir()
	configPath := dir + string(os.PathSeparator) + "config.yaml"
	initialBody := strings.Join([]string{
		"claude-api-key:",
		"  - api-key: " + claudeSecret,
		"    base-url: https://claude.example.com",
		"openai-compatibility:",
		"  - name: alpha",
		"    base-url: https://alpha.example/v1",
		"    api-key-entries:",
		"      - api-key: " + openaiKey,
		"api-keys:",
		"  - " + clientAPIKey,
		"",
	}, "\n")
	if errWrite := os.WriteFile(configPath, []byte(initialBody), 0o600); errWrite != nil {
		t.Fatalf("seed config: %v", errWrite)
	}
	cfg, errLoad := config.LoadConfig(configPath)
	if errLoad != nil {
		t.Fatalf("load seed config: %v", errLoad)
	}
	h := &Handler{cfg: cfg, configFilePath: configPath}
	_, _ = captureConfigReload(h)

	// PUT body has every secret replaced by its redaction (exactly what the
	// management UI would send back after a GET).
	putBody := strings.Join([]string{
		"claude-api-key:",
		"  - api-key: " + redactSecretValue(claudeSecret),
		"    base-url: https://claude.example.com",
		"openai-compatibility:",
		"  - name: alpha",
		"    base-url: https://alpha.example/v1",
		"    api-key-entries:",
		"      - api-key: " + redactSecretValue(openaiKey),
		"api-keys:",
		"  - " + redactSecretValue(clientAPIKey),
		"",
	}, "\n")
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/v0/management/config.yaml", strings.NewReader(putBody))
	ctx.Request.Header.Set("Content-Type", "application/yaml")
	h.PutConfigYAML(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("PutConfigYAML status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// Verify the on-disk file contains the REAL secrets, not the redacted
	// placeholders.
	persisted, errRead := os.ReadFile(configPath)
	if errRead != nil {
		t.Fatalf("read persisted config: %v", errRead)
	}
	persistedStr := string(persisted)
	for _, secret := range []string{claudeSecret, openaiKey, clientAPIKey} {
		if !strings.Contains(persistedStr, secret) {
			t.Fatalf("persisted config missing secret %q:\n%s", secret, persistedStr)
		}
		if strings.Contains(persistedStr, redactSecretValue(secret)) {
			t.Fatalf("persisted config still has redacted placeholder for %q:\n%s", secret, persistedStr)
		}
	}
	// Verify the in-memory handler config has the real secrets.
	if got := h.cfg.ClaudeKey[0].APIKey; got != claudeSecret {
		t.Fatalf("in-memory ClaudeKey[0].APIKey = %q, want %q", got, claudeSecret)
	}
	if got := h.cfg.OpenAICompatibility[0].APIKeyEntries[0].APIKey; got != openaiKey {
		t.Fatalf("in-memory OpenAICompatibility[0].APIKeyEntries[0].APIKey = %q, want %q", got, openaiKey)
	}
	if got := h.cfg.APIKeys[0]; got != clientAPIKey {
		t.Fatalf("in-memory APIKeys[0] = %q, want %q", got, clientAPIKey)
	}
}

// TestPutConfigYAML_PreservesNewSecretsInPutBody verifies that when the
// operator PUTs a body with brand-new (non-redacted) secrets, the restoration
// logic does NOT clobber them with old values from the live config.
func TestPutConfigYAML_PreservesNewSecretsInPutBody(t *testing.T) {
	t.Parallel()

	const (
		oldSecret = "sk-old-secret-value-1234"
		newSecret = "sk-brand-new-real-key-9876"
	)
	dir := t.TempDir()
	configPath := dir + string(os.PathSeparator) + "config.yaml"
	initialBody := fmt.Sprintf("claude-api-key:\n  - api-key: %s\n", oldSecret)
	if errWrite := os.WriteFile(configPath, []byte(initialBody), 0o600); errWrite != nil {
		t.Fatalf("seed config: %v", errWrite)
	}
	cfg, errLoad := config.LoadConfig(configPath)
	if errLoad != nil {
		t.Fatalf("load seed config: %v", errLoad)
	}
	h := &Handler{cfg: cfg, configFilePath: configPath}
	_, _ = captureConfigReload(h)

	putBody := fmt.Sprintf("claude-api-key:\n  - api-key: %s\n", newSecret)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/v0/management/config.yaml", strings.NewReader(putBody))
	ctx.Request.Header.Set("Content-Type", "application/yaml")
	h.PutConfigYAML(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("PutConfigYAML status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	persisted, errRead := os.ReadFile(configPath)
	if errRead != nil {
		t.Fatalf("read persisted config: %v", errRead)
	}
	persistedStr := string(persisted)
	if !strings.Contains(persistedStr, newSecret) {
		t.Fatalf("new secret should be persisted:\n%s", persistedStr)
	}
	if strings.Contains(persistedStr, oldSecret) {
		t.Fatalf("old secret should NOT replace the new one:\n%s", persistedStr)
	}
}
