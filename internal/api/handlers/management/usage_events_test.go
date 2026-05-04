package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/redisqueue"
)

func TestGetUsageEvents_ReturnsEmptyWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prevQueueEnabled := redisqueue.Enabled()
	prevUsageEnabled := redisqueue.UsageStatisticsEnabled()
	redisqueue.SetEnabled(false)
	redisqueue.SetUsageStatisticsEnabled(false)
	t.Cleanup(func() {
		redisqueue.SetEnabled(prevQueueEnabled)
		redisqueue.SetUsageStatisticsEnabled(prevUsageEnabled)
	})

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/v0/management/usage-events", nil)
	ginCtx.Request = req
	h.GetUsageEvents(ginCtx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload UsageEventsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Events) != 0 {
		t.Fatalf("events len = %d, want 0", len(payload.Events))
	}
}

func TestGetUsageEvents_ReturnsEventsFromQueue(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prevQueueEnabled := redisqueue.Enabled()
	prevUsageEnabled := redisqueue.UsageStatisticsEnabled()
	redisqueue.SetEnabled(true)
	redisqueue.SetUsageStatisticsEnabled(true)
	t.Cleanup(func() {
		redisqueue.SetEnabled(prevQueueEnabled)
		redisqueue.SetUsageStatisticsEnabled(prevUsageEnabled)
	})

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)

	eventJSON := `{
		"timestamp": "2026-05-04T12:00:00Z",
		"provider": "openai",
		"model": "gpt-5",
		"source": "user@example.com",
		"auth_index": "abc123",
		"auth_type": "apikey",
		"endpoint": "POST /v1/chat/completions",
		"request_id": "req-001",
		"latency_ms": 1500,
		"failed": false,
		"tokens": {
			"input_tokens": 100,
			"output_tokens": 200,
			"reasoning_tokens": 50,
			"cached_tokens": 30,
			"total_tokens": 350
		}
	}`
	redisqueue.Enqueue([]byte(eventJSON))

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/v0/management/usage-events", nil)
	ginCtx.Request = req
	h.GetUsageEvents(ginCtx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload UsageEventsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Events) != 1 {
		t.Fatalf("events len = %d, want 1", len(payload.Events))
	}

	event := payload.Events[0]
	if event.Provider != "openai" {
		t.Fatalf("provider = %q, want %q", event.Provider, "openai")
	}
	if event.Model != "gpt-5" {
		t.Fatalf("model = %q, want %q", event.Model, "gpt-5")
	}
	if event.InputTokens != 100 {
		t.Fatalf("input_tokens = %d, want %d", event.InputTokens, 100)
	}
	if event.OutputTokens != 200 {
		t.Fatalf("output_tokens = %d, want %d", event.OutputTokens, 200)
	}
	if event.ReasoningTokens != 50 {
		t.Fatalf("reasoning_tokens = %d, want %d", event.ReasoningTokens, 50)
	}
	if event.CachedTokens != 30 {
		t.Fatalf("cached_tokens = %d, want %d", event.CachedTokens, 30)
	}
	if event.TotalTokens != 350 {
		t.Fatalf("total_tokens = %d, want %d", event.TotalTokens, 350)
	}
	if event.Failed {
		t.Fatalf("failed = %v, want %v", event.Failed, false)
	}
}

func TestGetUsageEvents_RespectsLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prevQueueEnabled := redisqueue.Enabled()
	prevUsageEnabled := redisqueue.UsageStatisticsEnabled()
	redisqueue.SetEnabled(true)
	redisqueue.SetUsageStatisticsEnabled(true)
	t.Cleanup(func() {
		redisqueue.SetEnabled(prevQueueEnabled)
		redisqueue.SetUsageStatisticsEnabled(prevUsageEnabled)
	})

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)

	for i := 0; i < 5; i++ {
		eventJSON := `{
			"timestamp": "2026-05-04T12:00:00Z",
			"provider": "openai",
			"model": "gpt-5",
			"tokens": {"input_tokens": 100, "output_tokens": 200, "total_tokens": 300}
		}`
		redisqueue.Enqueue([]byte(eventJSON))
	}

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/v0/management/usage-events?limit=2", nil)
	ginCtx.Request = req
	h.GetUsageEvents(ginCtx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload UsageEventsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Events) != 2 {
		t.Fatalf("events len = %d, want 2", len(payload.Events))
	}
}

func TestGetUsageEvents_HandlerNil(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var h *Handler

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/v0/management/usage-events", nil)
	ginCtx.Request = req
	h.GetUsageEvents(ginCtx)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestGetUsageEvents_DoesNotConsumeEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prevQueueEnabled := redisqueue.Enabled()
	prevUsageEnabled := redisqueue.UsageStatisticsEnabled()
	redisqueue.SetEnabled(true)
	redisqueue.SetUsageStatisticsEnabled(true)
	t.Cleanup(func() {
		redisqueue.SetEnabled(prevQueueEnabled)
		redisqueue.SetUsageStatisticsEnabled(prevUsageEnabled)
	})

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)

	eventJSON := `{
		"timestamp": "2026-05-04T12:00:00Z",
		"provider": "openai",
		"model": "gpt-5",
		"tokens": {"input_tokens": 100, "output_tokens": 200, "total_tokens": 300}
	}`
	redisqueue.Enqueue([]byte(eventJSON))

	rec1 := httptest.NewRecorder()
	ginCtx1, _ := gin.CreateTestContext(rec1)
	req1 := httptest.NewRequest(http.MethodGet, "/v0/management/usage-events", nil)
	ginCtx1.Request = req1
	h.GetUsageEvents(ginCtx1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("first call status = %d, want %d", rec1.Code, http.StatusOK)
	}

	var payload1 UsageEventsResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &payload1); err != nil {
		t.Fatalf("decode first payload: %v", err)
	}
	if len(payload1.Events) != 1 {
		t.Fatalf("first call events len = %d, want 1", len(payload1.Events))
	}

	rec2 := httptest.NewRecorder()
	ginCtx2, _ := gin.CreateTestContext(rec2)
	req2 := httptest.NewRequest(http.MethodGet, "/v0/management/usage-events", nil)
	ginCtx2.Request = req2
	h.GetUsageEvents(ginCtx2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("second call status = %d, want %d", rec2.Code, http.StatusOK)
	}

	var payload2 UsageEventsResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &payload2); err != nil {
		t.Fatalf("decode second payload: %v", err)
	}
	if len(payload2.Events) != 1 {
		t.Fatalf("second call events len = %d, want 1 (event should still be available)", len(payload2.Events))
	}
}
