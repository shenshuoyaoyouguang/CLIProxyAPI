package executor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	kimiauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/kimi"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	_ "github.com/router-for-me/CLIProxyAPI/v6/internal/translator"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestNormalizeKimiToolMessageLinks_UsesCallIDFallback(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"list_directory:1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]},
			{"role":"tool","call_id":"list_directory:1","content":"[]"}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	got := gjson.GetBytes(out, "messages.1.tool_call_id").String()
	if got != "list_directory:1" {
		t.Fatalf("messages.1.tool_call_id = %q, want %q", got, "list_directory:1")
	}
}

func TestNormalizeKimiToolMessageLinks_InferSinglePendingID(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"call_123","type":"function","function":{"name":"read_file","arguments":"{}"}}]},
			{"role":"tool","content":"file-content"}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	got := gjson.GetBytes(out, "messages.1.tool_call_id").String()
	if got != "call_123" {
		t.Fatalf("messages.1.tool_call_id = %q, want %q", got, "call_123")
	}
}

func TestNormalizeKimiToolMessageLinks_AmbiguousMissingIDIsNotInferred(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[
				{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}},
				{"id":"call_2","type":"function","function":{"name":"read_file","arguments":"{}"}}
			]},
			{"role":"tool","content":"result-without-id"}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	if gjson.GetBytes(out, "messages.1.tool_call_id").Exists() {
		t.Fatalf("messages.1.tool_call_id should be absent for ambiguous case, got %q", gjson.GetBytes(out, "messages.1.tool_call_id").String())
	}
}

func TestNormalizeKimiToolMessageLinks_PreservesExistingToolCallID(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","call_id":"different-id","content":"result"}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	got := gjson.GetBytes(out, "messages.1.tool_call_id").String()
	if got != "call_1" {
		t.Fatalf("messages.1.tool_call_id = %q, want %q", got, "call_1")
	}
}

func TestNormalizeKimiToolMessageLinks_InheritsPreviousReasoningForAssistantToolCalls(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":"plan","reasoning_content":"previous reasoning"},
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	got := gjson.GetBytes(out, "messages.1.reasoning_content").String()
	if got != "previous reasoning" {
		t.Fatalf("messages.1.reasoning_content = %q, want %q", got, "previous reasoning")
	}
}

func TestNormalizeKimiToolMessageLinks_InsertsFallbackReasoningWhenMissing(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	reasoning := gjson.GetBytes(out, "messages.0.reasoning_content")
	if !reasoning.Exists() {
		t.Fatalf("messages.0.reasoning_content should exist")
	}
	if reasoning.String() != "[reasoning unavailable]" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", reasoning.String(), "[reasoning unavailable]")
	}
}

func TestNormalizeKimiToolMessageLinks_UsesContentAsReasoningFallback(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":[{"type":"text","text":"first line"},{"type":"text","text":"second line"}],"tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	got := gjson.GetBytes(out, "messages.0.reasoning_content").String()
	if got != "first line\nsecond line" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "first line\nsecond line")
	}
}

func TestNormalizeKimiToolMessageLinks_ReplacesEmptyReasoningContent(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":"assistant summary","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}],"reasoning_content":""}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	got := gjson.GetBytes(out, "messages.0.reasoning_content").String()
	if got != "assistant summary" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "assistant summary")
	}
}

func TestNormalizeKimiToolMessageLinks_PreservesExistingAssistantReasoning(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}],"reasoning_content":"keep me"}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	got := gjson.GetBytes(out, "messages.0.reasoning_content").String()
	if got != "keep me" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "keep me")
	}
}

func TestNormalizeKimiToolMessageLinks_RepairsIDsAndReasoningTogether(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}],"reasoning_content":"r1"},
			{"role":"tool","call_id":"call_1","content":"[]"},
			{"role":"assistant","tool_calls":[{"id":"call_2","type":"function","function":{"name":"read_file","arguments":"{}"}}]},
			{"role":"tool","call_id":"call_2","content":"file"}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	if got := gjson.GetBytes(out, "messages.1.tool_call_id").String(); got != "call_1" {
		t.Fatalf("messages.1.tool_call_id = %q, want %q", got, "call_1")
	}
	if got := gjson.GetBytes(out, "messages.3.tool_call_id").String(); got != "call_2" {
		t.Fatalf("messages.3.tool_call_id = %q, want %q", got, "call_2")
	}
	if got := gjson.GetBytes(out, "messages.2.reasoning_content").String(); got != "r1" {
		t.Fatalf("messages.2.reasoning_content = %q, want %q", got, "r1")
	}
}

func TestApplyKimiHeadersWithAuth_PrefersAuthDeviceID(t *testing.T) {
	auth := &cliproxyauth.Auth{
		ID:       "kimi-auth",
		Provider: "kimi",
		Metadata: map[string]any{"device_id": "auth-device"},
		Storage: &kimiauth.KimiTokenStorage{
			DeviceID: "storage-device",
			Type:     "kimi",
		},
	}

	req := httptest.NewRequest("POST", "https://example.com", nil)
	applyKimiHeadersWithAuth(req, "token", false, auth)

	if got := req.Header.Get("X-Msh-Device-Id"); got != "auth-device" {
		t.Fatalf("X-Msh-Device-Id = %q, want %q", got, "auth-device")
	}
}

func TestApplyKimiHeadersWithAuth_BackfillsStorageDeviceID(t *testing.T) {
	auth := &cliproxyauth.Auth{
		ID:       "kimi-auth",
		Provider: "kimi",
		Metadata: map[string]any{"device_id": "auth-device"},
		Storage: &kimiauth.KimiTokenStorage{
			Type: "kimi",
		},
	}

	req := httptest.NewRequest("POST", "https://example.com", nil)
	applyKimiHeadersWithAuth(req, "token", false, auth)

	storage, ok := auth.Storage.(*kimiauth.KimiTokenStorage)
	if !ok || storage == nil {
		t.Fatalf("expected KimiTokenStorage to be present")
	}
	if storage.DeviceID != "auth-device" {
		t.Fatalf("storage.DeviceID = %q, want %q", storage.DeviceID, "auth-device")
	}
}

func TestResolveKimiDeviceID_FallsBackToLocalDeviceIDAndBackfillsMetadata(t *testing.T) {
	tempDir := t.TempDir()
	setKimiDeviceEnv(t, tempDir)
	if err := os.MkdirAll(kimiDeviceDir(tempDir), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(kimiDeviceDir(tempDir), "device_id"), []byte("local-device\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	auth := &cliproxyauth.Auth{ID: "kimi-auth", Provider: "kimi"}

	got := resolveKimiDeviceID(auth)
	if got != "local-device" {
		t.Fatalf("resolveKimiDeviceID() = %q, want %q", got, "local-device")
	}
	if deviceID, _ := auth.Metadata["device_id"].(string); deviceID != "local-device" {
		t.Fatalf("auth.Metadata[device_id] = %q, want %q", deviceID, "local-device")
	}
}

func TestKimiExecute_ClaudeRequestsUseOpenAICompatPath(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotAnthropicBeta string
	var gotThinkingType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAnthropicBeta = r.Header.Get("Anthropic-Beta")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}

		gotThinkingType = gjson.GetBytes(body, "thinking.type").String()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 1,
			"model":   "kimi-k2.5",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "ok",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     1,
				"completion_tokens": 1,
				"total_tokens":      2,
			},
		})
	}))
	defer server.Close()

	exec := NewKimiExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		ID:       "kimi-auth",
		Provider: "kimi",
		Attributes: map[string]string{
			"base_url": server.URL,
		},
		Metadata: map[string]any{
			"access_token": "test-token",
			"device_id":    "device-1",
		},
	}

	req := cliproxyexecutor.Request{
		Model: "kimi-k2.5",
		Payload: []byte(`{
			"model":"kimi-k2.5",
			"max_tokens":1024,
			"messages":[{"role":"user","content":"hi"}],
			"thinking":{"type":"enabled","budget_tokens":31999}
		}`),
	}
	opts := cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("claude"),
	}

	resp, err := exec.Execute(context.Background(), auth, req, opts)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(resp.Payload) == 0 {
		t.Fatalf("expected translated response payload")
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("upstream path = %q, want %q", gotPath, "/v1/chat/completions")
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization = %q, want %q", gotAuth, "Bearer test-token")
	}
	if gotAnthropicBeta != "" {
		t.Fatalf("Anthropic-Beta should be absent, got %q", gotAnthropicBeta)
	}
	if gotThinkingType != "" {
		t.Fatalf("thinking.type should be cleared for Kimi OpenAI payload, got %q", gotThinkingType)
	}
}

func TestKimiCountTokens_ClaudeRequestsUseLocalOpenAICompatCounting(t *testing.T) {
	exec := NewKimiExecutor(&config.Config{})
	req := cliproxyexecutor.Request{
		Model: "kimi-k2.5",
		Payload: []byte(`{
			"model":"kimi-k2.5",
			"messages":[{"role":"user","content":"hello"}],
			"thinking":{"type":"enabled","budget_tokens":31999}
		}`),
	}
	opts := cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("claude"),
	}

	resp, err := exec.CountTokens(context.Background(), nil, req, opts)
	if err != nil {
		t.Fatalf("CountTokens() error = %v", err)
	}
	if !gjson.ValidBytes(resp.Payload) {
		t.Fatalf("expected JSON payload, got %q", string(resp.Payload))
	}
	if !gjson.GetBytes(resp.Payload, "input_tokens").Exists() && !gjson.GetBytes(resp.Payload, "usage.input_tokens").Exists() && !gjson.GetBytes(resp.Payload, "usage.prompt_tokens").Exists() {
		t.Fatalf("expected token count fields in response, got %s", string(resp.Payload))
	}
}

func TestKimiPrepareRequest_InjectsDeviceHeaders(t *testing.T) {
	exec := NewKimiExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{
		ID:       "kimi-auth",
		Provider: "kimi",
		Metadata: map[string]any{
			"access_token": "token",
			"device_id":    "device-1",
		},
	}
	req := httptest.NewRequest("GET", "https://example.com", nil)

	if err := exec.PrepareRequest(req, auth); err != nil {
		t.Fatalf("PrepareRequest() error = %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer token" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer token")
	}
	if got := req.Header.Get("X-Msh-Device-Id"); got != "device-1" {
		t.Fatalf("X-Msh-Device-Id = %q, want %q", got, "device-1")
	}
	if got := req.Header.Get("User-Agent"); got != "KimiCLI/1.10.6" {
		t.Fatalf("User-Agent = %q, want %q", got, "KimiCLI/1.10.6")
	}
}

func kimiDeviceDir(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "kimi")
	case "windows":
		return filepath.Join(home, "AppData", "Roaming", "kimi")
	default:
		return filepath.Join(home, ".local", "share", "kimi")
	}
}

func setKimiDeviceEnv(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	}
}
