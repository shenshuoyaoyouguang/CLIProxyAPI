package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestListAuthFiles_IncludesModelStatesSummary(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")

	nextRetry := time.Date(2026, 7, 14, 10, 30, 0, 0, time.UTC)
	manager := coreauth.NewManager(nil, nil, nil)
	record := &coreauth.Auth{
		ID:       "runtime-only-auth-model-states",
		Provider: "openai-compatibility",
		Attributes: map[string]string{
			"runtime_only": "true",
		},
		ModelStates: map[string]*coreauth.ModelState{
			"alias/upstream-a": {
				Status:         coreauth.StatusError,
				StatusMessage:  "rate limited",
				Unavailable:    true,
				NextRetryAfter: nextRetry,
				UpdatedAt:      nextRetry.Add(-time.Minute),
				LastError: &coreauth.Error{
					Code:    "rate_limit",
					Message: "429",
				},
			},
		},
	}
	if _, errRegister := manager.Register(context.Background(), record); errRegister != nil {
		t.Fatalf("failed to register auth record: %v", errRegister)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	h.tokenStore = &memoryAuthStore{}

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)

	h.ListAuthFiles(ginCtx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if errUnmarshal := json.Unmarshal(rec.Body.Bytes(), &payload); errUnmarshal != nil {
		t.Fatalf("failed to decode list payload: %v", errUnmarshal)
	}
	filesRaw := payload["files"].([]any)
	fileEntry := filesRaw[0].(map[string]any)
	statesRaw, ok := fileEntry["model_states"].([]any)
	if !ok {
		t.Fatalf("expected model_states array, got %#v", fileEntry["model_states"])
	}
	if len(statesRaw) != 1 {
		t.Fatalf("model_states count = %d, want 1", len(statesRaw))
	}
	state := statesRaw[0].(map[string]any)
	if state["model"] != "alias/upstream-a" {
		t.Fatalf("model = %#v, want alias/upstream-a", state["model"])
	}
	if state["unavailable"] != true {
		t.Fatalf("unavailable = %#v, want true", state["unavailable"])
	}
	if state["status_message"] != "rate limited" {
		t.Fatalf("status_message = %#v, want rate limited", state["status_message"])
	}
	if state["last_error_code"] != "rate_limit" {
		t.Fatalf("last_error_code = %#v, want rate_limit", state["last_error_code"])
	}
	if _, ok := state["next_retry_after"].(string); !ok {
		t.Fatalf("next_retry_after = %#v, want timestamp string", state["next_retry_after"])
	}
}
