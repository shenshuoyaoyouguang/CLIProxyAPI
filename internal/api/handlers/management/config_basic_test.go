package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	if len(body.OpenAICompatibility) != 2 {
		t.Fatalf("openai-compatibility entries = %d, want 2", len(body.OpenAICompatibility))
	}
	if body.OpenAICompatibility[0].Name != "alpha" || body.OpenAICompatibility[1].Name != "beta" {
		t.Fatalf("openai-compatibility order/content mismatch: %+v", body.OpenAICompatibility)
	}
	if len(body.ClaudeKey) != 2 || body.ClaudeKey[0].APIKey != "sk-alpha" || body.ClaudeKey[1].APIKey != "sk-beta" {
		t.Fatalf("claude-api-key mismatch: %+v", body.ClaudeKey)
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
