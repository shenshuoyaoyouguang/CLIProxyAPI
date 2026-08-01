package management

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// TestPatchZAIKeyUpdatesExecutionFields verifies the zai-api-key PATCH endpoint
// (zai is a first-class provider and must expose the same management surface
// as xai/codex).
func TestPatchZAIKeyUpdatesExecutionFields(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{ZAIKey: []config.ZAIKey{{
			APIKey:         "zai-key",
			Priority:       1,
			BaseURL:        "https://api.z.ai/api/paas/v4",
			Websockets:     true,
			DisableCooling: false,
		}}},
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/zai-api-key", strings.NewReader(`{
		"index": 0,
		"value": {
			"priority": 7,
			"websockets": false,
			"disable-cooling": true
		}
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.PatchZAIKey(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	entry := h.cfg.ZAIKey[0]
	if entry.Priority != 7 {
		t.Fatalf("priority = %d, want 7", entry.Priority)
	}
	if entry.Websockets {
		t.Fatal("websockets = true, want false")
	}
	if !entry.DisableCooling {
		t.Fatal("disable-cooling = false, want true")
	}
}

// TestDeleteZAIKeyByAPIKey verifies DELETE with the api-key query parameter.
func TestDeleteZAIKeyByAPIKey(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{ZAIKey: []config.ZAIKey{
			{APIKey: "keep", BaseURL: "https://api.z.ai/api/paas/v4"},
			{APIKey: "drop", BaseURL: "https://api.z.ai/api/paas/v4"},
		}},
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/v0/management/zai-api-key?api-key=drop", nil)

	h.DeleteZAIKey(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(h.cfg.ZAIKey) != 1 || h.cfg.ZAIKey[0].APIKey != "keep" {
		t.Fatalf("remaining keys = %+v, want only keep", h.cfg.ZAIKey)
	}
}

// TestGetZAIKeysIncludesAuthIndex verifies GET returns the zai-api-key list.
func TestGetZAIKeysIncludesAuthIndex(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{ZAIKey: []config.ZAIKey{{
			APIKey:  "zai-key",
			BaseURL: "https://api.z.ai/api/paas/v4",
		}}},
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/zai-api-key", nil)

	h.GetZAIKeys(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "zai-api-key") {
		t.Fatalf("body missing zai-api-key list: %s", rec.Body.String())
	}
}
