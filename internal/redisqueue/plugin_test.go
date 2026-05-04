package redisqueue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	internallogging "github.com/router-for-me/CLIProxyAPI/v6/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage/record"
)

func TestUsageQueuePluginPayloadIncludesStableFieldsAndSuccess(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithRequestID(context.Background(), "ctx-request-id")
		ctx = internallogging.WithEndpoint(ctx, "POST /v1/chat/completions")
		ctx = internallogging.WithResponseStatusHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusOK)

		plugin := &usageQueuePlugin{}
		plugin.HandleUsage(ctx, record.Record{
			Provider:    "openai",
			Model:       "gpt-5.4",
			APIKey:      "test-key",
			AuthIndex:   "0",
			AuthType:    "apikey",
			Source:      "user@example.com",
			RequestedAt: time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
			Latency:     1500 * time.Millisecond,
			Detail: record.Detail{
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			},
		})

		payload := popSinglePayload(t)
		requireStringField(t, payload, "provider", "openai")
		requireStringField(t, payload, "model", "gpt-5.4")
		requireStringField(t, payload, "endpoint", "POST /v1/chat/completions")
		requireStringField(t, payload, "auth_type", "apikey")
		requireStringField(t, payload, "request_id", "ctx-request-id")
		requireBoolField(t, payload, "failed", false)
	})
}

func TestUsageQueuePluginPayloadIncludesStableFieldsAndFailureAndGinRequestID(t *testing.T) {
	withEnabledQueue(t, func() {
		ctx := internallogging.WithRequestID(context.Background(), "gin-request-id")
		ctx = internallogging.WithEndpoint(ctx, "GET /v1/responses")
		ctx = internallogging.WithResponseStatusHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusInternalServerError)

		plugin := &usageQueuePlugin{}
		plugin.HandleUsage(ctx, record.Record{
			Provider:    "openai",
			Model:       "gpt-5.4-mini",
			APIKey:      "test-key",
			AuthIndex:   "0",
			AuthType:    "apikey",
			Source:      "user@example.com",
			RequestedAt: time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
			Latency:     2500 * time.Millisecond,
			Detail: record.Detail{
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			},
		})

		payload := popSinglePayload(t)
		requireStringField(t, payload, "provider", "openai")
		requireStringField(t, payload, "model", "gpt-5.4-mini")
		requireStringField(t, payload, "endpoint", "GET /v1/responses")
		requireStringField(t, payload, "auth_type", "apikey")
		requireStringField(t, payload, "request_id", "gin-request-id")
		requireBoolField(t, payload, "failed", true)
	})
}

func TestUsageQueuePluginAsyncIgnoresRecycledGinContext(t *testing.T) {
	withEnabledQueue(t, func() {
		ginCtx := newTestGinContext(t, http.MethodPost, "/v1/chat/completions", http.StatusOK)
		ctx := context.WithValue(context.Background(), "gin", ginCtx)
		ctx = internallogging.WithRequestID(ctx, "ctx-request-id")
		ctx = internallogging.WithEndpoint(ctx, "POST /v1/chat/completions")
		ctx = internallogging.WithResponseStatusHolder(ctx)
		internallogging.SetResponseStatus(ctx, http.StatusInternalServerError)

		mgr := newLocalManager()

		mgr.Register(pluginFunc(func(_ context.Context, _ record.Record) {
			ginCtx.Request = httptest.NewRequest(http.MethodGet, "http://example.com/v1/responses", nil)
			ginCtx.Status(http.StatusOK)
		}))
		mgr.Register(&usageQueuePlugin{})

		mgr.Publish(ctx, record.Record{
			Provider:    "openai",
			Model:       "gpt-5.4",
			APIKey:      "test-key",
			AuthIndex:   "0",
			AuthType:    "apikey",
			Source:      "user@example.com",
			RequestedAt: time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
			Latency:     1500 * time.Millisecond,
			Detail: record.Detail{
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			},
		})

		payload := waitForSinglePayload(t, 2*time.Second)
		requireStringField(t, payload, "endpoint", "POST /v1/chat/completions")
		requireStringField(t, payload, "request_id", "ctx-request-id")
		requireBoolField(t, payload, "failed", true)
	})
}

func withEnabledQueue(t *testing.T, fn func()) {
	t.Helper()

	prevQueueEnabled := Enabled()
	prevUsageEnabled := UsageStatisticsEnabled()

	SetEnabled(false)
	SetEnabled(true)
	SetUsageStatisticsEnabled(true)

	defer func() {
		SetEnabled(false)
		SetEnabled(prevQueueEnabled)
		SetUsageStatisticsEnabled(prevUsageEnabled)
	}()

	fn()
}

func newTestGinContext(t *testing.T, method, path string, status int) *gin.Context {
	t.Helper()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(method, "http://example.com"+path, nil)
	if status != 0 {
		ginCtx.Status(status)
	}
	return ginCtx
}

func popSinglePayload(t *testing.T) map[string]json.RawMessage {
	t.Helper()

	items := PopOldest(10)
	if len(items) != 1 {
		t.Fatalf("PopOldest() items = %d, want 1", len(items))
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(items[0], &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return payload
}

func waitForSinglePayload(t *testing.T, timeout time.Duration) map[string]json.RawMessage {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		items := PopOldest(10)
		if len(items) == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if len(items) != 1 {
			t.Fatalf("PopOldest() items = %d, want 1", len(items))
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(items[0], &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		return payload
	}
	t.Fatalf("timeout waiting for queued payload")
	return nil
}

func requireStringField(t *testing.T, payload map[string]json.RawMessage, key, want string) {
	t.Helper()

	raw, ok := payload[key]
	if !ok {
		t.Fatalf("payload missing %q", key)
	}
	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal %q: %v", key, err)
	}
	if got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}

type pluginFunc func(context.Context, record.Record)

func (fn pluginFunc) HandleUsage(ctx context.Context, r record.Record) {
	fn(ctx, r)
}

type localManager struct {
	mu      sync.Mutex
	plugins []record.Plugin
}

func newLocalManager() *localManager { return &localManager{} }

func (m *localManager) Register(p record.Plugin) {
	m.mu.Lock()
	m.plugins = append(m.plugins, p)
	m.mu.Unlock()
}

func (m *localManager) Publish(ctx context.Context, r record.Record) {
	m.mu.Lock()
	plugins := m.plugins
	m.mu.Unlock()
	for _, p := range plugins {
		p.HandleUsage(ctx, r)
	}
}

func requireBoolField(t *testing.T, payload map[string]json.RawMessage, key string, want bool) {
	t.Helper()

	raw, ok := payload[key]
	if !ok {
		t.Fatalf("payload missing %q", key)
	}
	var got bool
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal %q: %v", key, err)
	}
	if got != want {
		t.Fatalf("%s = %t, want %t", key, got, want)
	}
}
