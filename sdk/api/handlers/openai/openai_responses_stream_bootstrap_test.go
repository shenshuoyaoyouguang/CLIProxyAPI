package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

type immediateResponsesStreamErrorExecutor struct{}

func (e *immediateResponsesStreamErrorExecutor) Identifier() string { return "codex" }

func (e *immediateResponsesStreamErrorExecutor) Execute(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{Code: "not_implemented", Message: "Execute not implemented"}
}

func (e *immediateResponsesStreamErrorExecutor) ExecuteStream(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	ch := make(chan coreexecutor.StreamChunk, 1)
	ch <- coreexecutor.StreamChunk{
		Err: &coreauth.Error{
			Code:       "upstream_failed",
			Message:    "upstream failed before first byte",
			HTTPStatus: http.StatusBadGateway,
		},
	}
	close(ch)
	return &coreexecutor.StreamResult{Chunks: ch}, nil
}

func (e *immediateResponsesStreamErrorExecutor) Refresh(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *immediateResponsesStreamErrorExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{Code: "not_implemented", Message: "CountTokens not implemented"}
}

func (e *immediateResponsesStreamErrorExecutor) HttpRequest(ctx context.Context, auth *coreauth.Auth, req *http.Request) (*http.Response, error) {
	return nil, &coreauth.Error{
		Code:       "not_implemented",
		Message:    "HttpRequest not implemented",
		HTTPStatus: http.StatusNotImplemented,
	}
}

func TestHandleStreamingResponseUsesSSEErrorBeforeFirstChunk(t *testing.T) {
	gin.SetMode(gin.TestMode)

	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(&immediateResponsesStreamErrorExecutor{})

	auth := &coreauth.Auth{
		ID:       "auth1",
		Provider: "codex",
		Status:   coreauth.StatusActive,
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("manager.Register(auth): %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "test-model"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	base := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager)
	h := NewOpenAIResponsesAPIHandler(base)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	h.handleStreamingResponse(c, []byte(`{"model":"test-model","stream":true}`))

	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("expected SSE content type, got %q", got)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Fatalf("expected SSE error event, got %q", body)
	}
	if strings.Contains(body, `{"error":{`) {
		t.Fatalf("expected streaming error chunk instead of HTTP JSON error body, got %q", body)
	}
}
