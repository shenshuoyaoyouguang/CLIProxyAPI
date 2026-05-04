package management

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/redisqueue"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func TestExportUsage_ReturnsEmptyWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prevQueueEnabled := redisqueue.Enabled()
	prevUsageEnabled := redisqueue.UsageStatisticsEnabled()
	redisqueue.SetEnabled(true)
	redisqueue.SetUsageStatisticsEnabled(false)
	t.Cleanup(func() {
		redisqueue.SetEnabled(prevQueueEnabled)
		redisqueue.SetUsageStatisticsEnabled(prevUsageEnabled)
	})

	h := NewHandlerWithoutConfigFilePath(nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/v0/management/usage/export", nil)
	w := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(w)
	ginCtx.Request = req

	h.ExportUsage(ginCtx)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var payload usage.ExportPayload
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if payload.Version != 1 {
		t.Errorf("expected version 1, got %d", payload.Version)
	}

	if payload.ExportedAt == "" {
		t.Error("expected exported_at to be set")
	}

	if len(payload.Usage.APIs) != 0 {
		t.Errorf("expected empty APIs, got %d", len(payload.Usage.APIs))
	}
}

func TestImportUsage_AcceptsExportPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewHandlerWithoutConfigFilePath(nil, nil)

	exportPayload := usage.ExportPayload{
		Version:    1,
		ExportedAt: "2024-01-01T00:00:00Z",
		Usage: usage.UsageSnapshot{
			APIs: map[string]usage.APIEntry{
				"POST /v1/chat/completions": {
					TotalRequests: 100,
					SuccessCount:  90,
					FailureCount:  10,
					TotalTokens:   5000,
					Models:        map[string]usage.ModelEntry{},
				},
			},
		},
	}

	body, _ := json.Marshal(exportPayload)
	req := httptest.NewRequest(http.MethodPost, "/v0/management/usage/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(w)
	ginCtx.Request = req

	h.ImportUsage(ginCtx)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response usage.ImportResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response.TotalRequests != 100 {
		t.Errorf("expected total_requests 100, got %d", response.TotalRequests)
	}

	if response.FailedRequests != 10 {
		t.Errorf("expected failed_requests 10, got %d", response.FailedRequests)
	}
}

func TestImportUsage_AcceptsUsageSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewHandlerWithoutConfigFilePath(nil, nil)

	snapshot := usage.UsageSnapshot{
		APIs: map[string]usage.APIEntry{
			"POST /v1/chat/completions": {
				TotalRequests: 50,
				SuccessCount:  45,
				FailureCount:  5,
				TotalTokens:   2500,
				Models:        map[string]usage.ModelEntry{},
			},
		},
	}

	body, _ := json.Marshal(snapshot)
	req := httptest.NewRequest(http.MethodPost, "/v0/management/usage/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(w)
	ginCtx.Request = req

	h.ImportUsage(ginCtx)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response usage.ImportResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response.TotalRequests != 50 {
		t.Errorf("expected total_requests 50, got %d", response.TotalRequests)
	}
}

func TestImportUsage_RejectsInvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewHandlerWithoutConfigFilePath(nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/v0/management/usage/import", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(w)
	ginCtx.Request = req

	h.ImportUsage(ginCtx)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestImportUsage_HandlerNil(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var h *Handler

	req := httptest.NewRequest(http.MethodPost, "/v0/management/usage/import", nil)
	w := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(w)
	ginCtx.Request = req

	h.ImportUsage(ginCtx)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestExportUsage_HandlerNil(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var h *Handler

	req := httptest.NewRequest(http.MethodGet, "/v0/management/usage/export", nil)
	w := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(w)
	ginCtx.Request = req

	h.ExportUsage(ginCtx)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}
