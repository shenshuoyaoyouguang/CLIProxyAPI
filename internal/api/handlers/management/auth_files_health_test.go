package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestPatchAuthFileStatus_PersistsPermanentDegradedState(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	store := &memoryAuthStore{}
	manager := coreauth.NewManager(store, nil, nil)
	record := &coreauth.Auth{
		ID:       "claude.json",
		FileName: "claude.json",
		Provider: "claude",
		Attributes: map[string]string{
			"path": "/tmp/claude.json",
		},
		Metadata: map[string]any{"type": "claude"},
	}
	if _, err := manager.Register(context.Background(), record); err != nil {
		t.Fatalf("failed to register auth: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	body := `{"name":"claude.json","degraded":true,"degradedReason":"401_unauthorized","degradedMessage":"401 unauthorized","cooldownUntil":null}`

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/auth-files/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req

	h.PatchAuthFileStatus(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	updated, ok := manager.GetByID("claude.json")
	if !ok || updated == nil {
		t.Fatalf("expected auth to exist after patch")
	}
	state, ok := updated.AccountHealth()
	if !ok || state == nil {
		t.Fatalf("expected account health state to be persisted")
	}
	if !state.Degraded {
		t.Fatalf("expected degraded=true")
	}
	if state.CooldownUntil != nil {
		t.Fatalf("expected permanent degraded state with nil cooldown")
	}
	if !updated.Unavailable {
		t.Fatalf("expected auth to be unavailable after permanent degradation")
	}
}

func TestAuthFileHealth_PutGetRecover(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	store := &memoryAuthStore{}
	manager := coreauth.NewManager(store, nil, nil)
	record := &coreauth.Auth{
		ID:       "gemini.json",
		FileName: "gemini.json",
		Provider: "gemini",
		Attributes: map[string]string{
			"path": "/tmp/gemini.json",
		},
		Metadata: map[string]any{"type": "gemini"},
	}
	if _, err := manager.Register(context.Background(), record); err != nil {
		t.Fatalf("failed to register auth: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)

	putBody := `{"gemini.json":{"degraded":true,"degraded_reason":"429_rate_limited","degraded_message":"429 rate limited","consecutive_failures":3,"failure_statuses":[429,429,429],"cooldown_until":4102444800000}}`
	putRec := httptest.NewRecorder()
	putCtx, _ := gin.CreateTestContext(putRec)
	putReq := httptest.NewRequest(http.MethodPut, "/v0/management/auth-files/health", strings.NewReader(putBody))
	putReq.Header.Set("Content-Type", "application/json")
	putCtx.Request = putReq
	h.PutAuthFileHealth(putCtx)

	if putRec.Code != http.StatusOK {
		t.Fatalf("expected PUT status %d, got %d with body %s", http.StatusOK, putRec.Code, putRec.Body.String())
	}

	getRec := httptest.NewRecorder()
	getCtx, _ := gin.CreateTestContext(getRec)
	getReq := httptest.NewRequest(http.MethodGet, "/v0/management/auth-files/health", nil)
	getCtx.Request = getReq
	h.GetAuthFileHealth(getCtx)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected GET status %d, got %d with body %s", http.StatusOK, getRec.Code, getRec.Body.String())
	}

	var healthPayload map[string]map[string]any
	if err := json.Unmarshal(getRec.Body.Bytes(), &healthPayload); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}
	if _, ok := healthPayload["gemini.json"]; !ok {
		t.Fatalf("expected gemini.json health entry in response")
	}

	recoverRec := httptest.NewRecorder()
	recoverCtx, _ := gin.CreateTestContext(recoverRec)
	recoverReq := httptest.NewRequest(http.MethodPost, "/v0/management/auth-files/health/gemini.json/recover", nil)
	recoverCtx.Params = gin.Params{{Key: "name", Value: "gemini.json"}}
	recoverCtx.Request = recoverReq
	h.RecoverAuthFileHealth(recoverCtx)

	if recoverRec.Code != http.StatusOK {
		t.Fatalf("expected recover status %d, got %d with body %s", http.StatusOK, recoverRec.Code, recoverRec.Body.String())
	}

	updated, ok := manager.GetByID("gemini.json")
	if !ok || updated == nil {
		t.Fatalf("expected auth to exist after recover")
	}
	if _, ok := updated.AccountHealth(); ok {
		t.Fatalf("expected account health to be cleared after recover")
	}
	if updated.Unavailable {
		t.Fatalf("expected recovered auth to be available")
	}
}
