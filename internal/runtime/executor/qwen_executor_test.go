package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
)

var (
	qwenPassthroughUsagePluginOnce sync.Once
	qwenPassthroughUsagePlugin     = &authScopedUsagePlugin{
		authID:  "qwen-passthrough-no-usage",
		records: make(chan usage.Record, 8),
	}
)

func TestQwenExecutorParseSuffix(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		wantBase  string
		wantLevel string
	}{
		{"no suffix", "qwen-max", "qwen-max", ""},
		{"with level suffix", "qwen-max(high)", "qwen-max", "high"},
		{"with budget suffix", "qwen-max(16384)", "qwen-max", "16384"},
		{"complex model name", "qwen-plus-latest(medium)", "qwen-plus-latest", "medium"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := thinking.ParseSuffix(tt.model)
			if result.ModelName != tt.wantBase {
				t.Errorf("ParseSuffix(%q).ModelName = %q, want %q", tt.model, result.ModelName, tt.wantBase)
			}
		})
	}
}

func expectedQwenUserAgent() string {
	platform := runtime.GOOS
	switch platform {
	case "windows":
		platform = "win32"
	}
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x64"
	case "386":
		arch = "x86"
	}
	return "QwenCode/0.14.0 (" + platform + "; " + arch + ")"
}

func TestApplyQwenHeadersNonStreamUsesMinimalOfficialHeaderSet(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://example.com", nil)
	applyQwenHeaders(req, "token-123", false)

	wantUA := expectedQwenUserAgent()
	if got := req.Header.Get("Authorization"); got != "Bearer token-123" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer token-123")
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", got, "application/json")
	}
	if got := req.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("Accept = %q, want %q", got, "application/json")
	}
	if got := req.Header.Get("User-Agent"); got != wantUA {
		t.Fatalf("User-Agent = %q, want %q", got, wantUA)
	}
	if got := req.Header.Get("X-DashScope-UserAgent"); got != wantUA {
		t.Fatalf("X-DashScope-UserAgent = %q, want %q", got, wantUA)
	}
	if got := req.Header.Get("X-DashScope-CacheControl"); got != "enable" {
		t.Fatalf("X-DashScope-CacheControl = %q, want %q", got, "enable")
	}
	if got := req.Header.Get("X-DashScope-AuthType"); got != "qwen-oauth" {
		t.Fatalf("X-DashScope-AuthType = %q, want %q", got, "qwen-oauth")
	}
	for _, header := range []string{
		"X-Stainless-Runtime-Version",
		"X-Stainless-Lang",
		"X-Stainless-Arch",
		"X-Stainless-Package-Version",
		"X-Stainless-Retry-Count",
		"X-Stainless-Os",
		"X-Stainless-Runtime",
		"Sec-Fetch-Mode",
	} {
		if got := req.Header.Get(header); got != "" {
			t.Fatalf("expected %s to be absent, got %q", header, got)
		}
	}
}

func TestApplyQwenHeadersStreamUsesMinimalOfficialHeaderSet(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://example.com", nil)
	applyQwenHeaders(req, "token-123", true)

	if got := req.Header.Get("Accept"); got != "text/event-stream" {
		t.Fatalf("Accept = %q, want %q", got, "text/event-stream")
	}
	if got := req.Header.Get("User-Agent"); got != expectedQwenUserAgent() {
		t.Fatalf("User-Agent = %q, want %q", got, expectedQwenUserAgent())
	}
	if got := req.Header.Get("X-DashScope-UserAgent"); got != expectedQwenUserAgent() {
		t.Fatalf("X-DashScope-UserAgent = %q, want %q", got, expectedQwenUserAgent())
	}
}

func TestQwenExecutorExecuteStream_PublishesFallbackUsageWithoutUsageChunk(t *testing.T) {
	qwenPassthroughUsagePluginOnce.Do(func() {
		usage.RegisterPlugin(qwenPassthroughUsagePlugin)
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	executor := NewQwenExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		ID: "qwen-passthrough-no-usage",
		Attributes: map[string]string{
			"api_key":  "token-123",
			"base_url": server.URL,
		},
	}

	result, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "qwen-max",
		Payload: []byte(`{"model":"qwen-max","messages":[{"role":"user","content":"hi"}]}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Stream:       true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}

	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream chunk error: %v", chunk.Err)
		}
	}

	record := waitForUsageRecord(t, qwenPassthroughUsagePlugin.records)
	if record.AuthID != auth.ID {
		t.Fatalf("usage record auth_id = %q, want %q", record.AuthID, auth.ID)
	}
	if record.Provider != "qwen" {
		t.Fatalf("usage record provider = %q, want %q", record.Provider, "qwen")
	}
	if record.Failed {
		t.Fatal("usage fallback should mark request as successful")
	}
	if record.Detail != (usage.Detail{}) {
		t.Fatalf("usage fallback detail = %+v, want zero-value detail", record.Detail)
	}
}
