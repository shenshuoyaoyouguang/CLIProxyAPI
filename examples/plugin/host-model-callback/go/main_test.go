package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
)

// decodeEnvelopeHMC is a test helper that unmarshals an envelope.
func decodeEnvelopeHMC(t *testing.T, raw []byte) (bool, json.RawMessage, *envelopeError) {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	return env.OK, env.Result, env.Error
}

// TestHandleMethod_Register verifies registration returns management_api capability and correct schema.
func TestHandleMethod_Register(t *testing.T) {
	for _, method := range []string{pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure} {
		t.Run(method, func(t *testing.T) {
			raw, err := handleMethod(method, nil)
			if err != nil {
				t.Fatalf("handleMethod(%q) error = %v", method, err)
			}
			ok, result, _ := decodeEnvelopeHMC(t, raw)
			if !ok {
				t.Fatalf("handleMethod(%q) ok = false, want true", method)
			}
			var reg registration
			if err := json.Unmarshal(result, &reg); err != nil {
				t.Fatalf("unmarshal registration: %v", err)
			}
			if !reg.Capabilities.ManagementAPI {
				t.Errorf("ManagementAPI = false, want true")
			}
			if reg.SchemaVersion != pluginabi.SchemaVersion {
				t.Errorf("SchemaVersion = %d, want %d", reg.SchemaVersion, pluginabi.SchemaVersion)
			}
			if reg.Metadata.Name != pluginName {
				t.Errorf("Name = %q, want %q", reg.Metadata.Name, pluginName)
			}
		})
	}
}

// TestHandleMethod_ManagementRegister verifies the resources registration uses the correct path and menu.
func TestHandleMethod_ManagementRegister(t *testing.T) {
	raw, err := handleMethod(pluginabi.MethodManagementRegister, nil)
	if err != nil {
		t.Fatalf("handleMethod(management.register) error = %v", err)
	}
	ok, result, _ := decodeEnvelopeHMC(t, raw)
	if !ok {
		t.Fatalf("handleMethod(management.register) ok = false, want true")
	}
	var reg managementRegistration
	if err := json.Unmarshal(result, &reg); err != nil {
		t.Fatalf("unmarshal management registration: %v", err)
	}
	if len(reg.Resources) != 1 {
		t.Fatalf("Resources len = %d, want 1", len(reg.Resources))
	}
	if reg.Resources[0].Path != resourcePath {
		t.Errorf("Path = %q, want %q", reg.Resources[0].Path, resourcePath)
	}
	if reg.Resources[0].Menu != "Host Model Callback" {
		t.Errorf("Menu = %q, want %q", reg.Resources[0].Menu, "Host Model Callback")
	}
	if reg.Resources[0].Description == "" {
		t.Errorf("Description is empty")
	}
}

// TestHandleMethod_UnknownMethod verifies unknown methods return an error envelope.
func TestHandleMethod_UnknownMethod(t *testing.T) {
	raw, err := handleMethod("unknown.method", nil)
	if err != nil {
		t.Fatalf("handleMethod(unknown) returned error = %v, want nil", err)
	}
	ok, _, errInfo := decodeEnvelopeHMC(t, raw)
	if ok {
		t.Fatalf("handleMethod(unknown) ok = true, want false")
	}
	if errInfo == nil || errInfo.Code != "unknown_method" {
		t.Errorf("error code = %v, want %q", errInfo, "unknown_method")
	}
}

// TestApplyMode covers all recognized mode strings.
func TestApplyMode(t *testing.T) {
	tests := []struct {
		mode       string
		wantStream bool
	}{
		{"stream", true},
		{"streaming", true},
		{"STREAM", true},
		{"Streaming", true},
		{"non-stream", false},
		{"non_stream", false},
		{"nonstream", false},
		{"sync", false},
		{"unknown", false}, // unknown mode leaves stream unchanged
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			opts := &runOptions{Stream: false}
			applyMode(opts, tt.mode)
			if opts.Stream != tt.wantStream {
				t.Errorf("applyMode(%q): Stream = %v, want %v", tt.mode, opts.Stream, tt.wantStream)
			}
		})
	}
}

// TestApplyMode_DoesNotChangeUnknown verifies unknown mode does not flip stream from true.
func TestApplyMode_DoesNotChangeUnknown(t *testing.T) {
	opts := &runOptions{Stream: true}
	applyMode(opts, "nonsense")
	if !opts.Stream {
		t.Errorf("applyMode(nonsense) changed Stream from true to false, should be unchanged")
	}
}

// TestApplyBoolQuery covers valid true/false values and invalid input.
func TestApplyBoolQuery(t *testing.T) {
	tests := []struct {
		value   string
		want    bool
		wantErr bool
	}{
		{"true", true, false},
		{"True", true, false},
		{"TRUE", true, false},
		{"1", true, false},
		{"false", false, false},
		{"False", false, false},
		{"0", false, false},
		{"", false, false}, // empty: no-op
		{"notabool", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			q := url.Values{}
			if tt.value != "" {
				q.Set("flag", tt.value)
			}
			target := false
			err := applyBoolQuery(q, "flag", &target)
			if (err != nil) != tt.wantErr {
				t.Fatalf("applyBoolQuery(%q) error = %v, wantErr = %v", tt.value, err, tt.wantErr)
			}
			if !tt.wantErr && target != tt.want {
				t.Errorf("applyBoolQuery(%q) target = %v, want %v", tt.value, target, tt.want)
			}
		})
	}
}

// TestApplyQueryOptions_Model verifies model override via query.
func TestApplyQueryOptions_Model(t *testing.T) {
	opts := &runOptions{Model: defaultModel}
	q := url.Values{"model": []string{"custom-model"}}
	if err := applyQueryOptions(opts, q); err != nil {
		t.Fatalf("applyQueryOptions error = %v", err)
	}
	if opts.Model != "custom-model" {
		t.Errorf("Model = %q, want %q", opts.Model, "custom-model")
	}
}

// TestApplyQueryOptions_EntryExitProtocol verifies protocol overrides via query.
func TestApplyQueryOptions_EntryExitProtocol(t *testing.T) {
	opts := &runOptions{EntryProtocol: "openai", ExitProtocol: "openai"}
	q := url.Values{
		"entry_protocol": []string{"gemini"},
		"exit_protocol":  []string{"claude"},
	}
	if err := applyQueryOptions(opts, q); err != nil {
		t.Fatalf("applyQueryOptions error = %v", err)
	}
	if opts.EntryProtocol != "gemini" {
		t.Errorf("EntryProtocol = %q, want %q", opts.EntryProtocol, "gemini")
	}
	if opts.ExitProtocol != "claude" {
		t.Errorf("ExitProtocol = %q, want %q", opts.ExitProtocol, "claude")
	}
}

// TestApplyQueryOptions_NilQuery verifies nil query is a no-op.
func TestApplyQueryOptions_NilQuery(t *testing.T) {
	opts := &runOptions{Model: "original"}
	if err := applyQueryOptions(opts, nil); err != nil {
		t.Fatalf("applyQueryOptions(nil) error = %v", err)
	}
	if opts.Model != "original" {
		t.Errorf("Model = %q, want %q (nil query should be no-op)", opts.Model, "original")
	}
}

// TestApplyQueryOptions_InvalidBody verifies that invalid JSON in body query returns an error.
func TestApplyQueryOptions_InvalidBody(t *testing.T) {
	opts := &runOptions{}
	q := url.Values{"body": []string{"not-json"}}
	err := applyQueryOptions(opts, q)
	if err == nil {
		t.Fatal("applyQueryOptions with invalid JSON body should return error")
	}
}

// TestApplyQueryOptions_ValidJSONBody verifies valid JSON body is accepted.
func TestApplyQueryOptions_ValidJSONBody(t *testing.T) {
	opts := &runOptions{}
	q := url.Values{"body": []string{`{"model":"x","stream":false}`}}
	if err := applyQueryOptions(opts, q); err != nil {
		t.Fatalf("applyQueryOptions error = %v", err)
	}
	if len(opts.Body) == 0 {
		t.Errorf("Body is empty after valid JSON body query")
	}
}

// TestApplyQueryOptions_StreamAndImplicitClose verifies bool query parsing for stream and implicit_close.
func TestApplyQueryOptions_StreamAndImplicitClose(t *testing.T) {
	opts := &runOptions{}
	q := url.Values{
		"stream":         []string{"true"},
		"implicit_close": []string{"true"},
	}
	if err := applyQueryOptions(opts, q); err != nil {
		t.Fatalf("applyQueryOptions error = %v", err)
	}
	if !opts.Stream {
		t.Errorf("Stream = false, want true")
	}
	if !opts.ImplicitClose {
		t.Errorf("ImplicitClose = false, want true")
	}
}

// TestApplyBodyOptions_AllFields verifies all fields in managementBodyOptions are applied.
func TestApplyBodyOptions_AllFields(t *testing.T) {
	streamTrue := true
	implicitCloseTrue := true
	bodyOpts := managementBodyOptions{
		Model:         "test-model",
		EntryProtocol: "claude",
		ExitProtocol:  "openai",
		Prompt:        "test prompt",
		Stream:        &streamTrue,
		Alt:           "test-alt",
		ImplicitClose: &implicitCloseTrue,
	}
	raw, _ := json.Marshal(bodyOpts)
	opts := &runOptions{
		Model:         defaultModel,
		EntryProtocol: "openai",
		ExitProtocol:  "openai",
		Prompt:        defaultPrompt,
	}
	if err := applyBodyOptions(opts, raw); err != nil {
		t.Fatalf("applyBodyOptions error = %v", err)
	}
	if opts.Model != "test-model" {
		t.Errorf("Model = %q, want %q", opts.Model, "test-model")
	}
	if opts.EntryProtocol != "claude" {
		t.Errorf("EntryProtocol = %q, want %q", opts.EntryProtocol, "claude")
	}
	if opts.ExitProtocol != "openai" {
		t.Errorf("ExitProtocol = %q, want %q", opts.ExitProtocol, "openai")
	}
	if opts.Prompt != "test prompt" {
		t.Errorf("Prompt = %q, want %q", opts.Prompt, "test prompt")
	}
	if !opts.Stream {
		t.Errorf("Stream = false, want true")
	}
	if opts.Alt != "test-alt" {
		t.Errorf("Alt = %q, want %q", opts.Alt, "test-alt")
	}
	if !opts.ImplicitClose {
		t.Errorf("ImplicitClose = false, want true")
	}
}

// TestApplyBodyOptions_InvalidJSON verifies that invalid JSON body returns an error.
func TestApplyBodyOptions_InvalidJSON(t *testing.T) {
	opts := &runOptions{}
	if err := applyBodyOptions(opts, []byte("not-json")); err == nil {
		t.Fatal("applyBodyOptions with invalid JSON should return error")
	}
}

// TestApplyBodyOptions_InvalidBodyField verifies that invalid JSON in the body field returns an error.
func TestApplyBodyOptions_InvalidBodyField(t *testing.T) {
	raw := []byte(`{"body": "not-json"}`) // body is a json.RawMessage but invalid JSON in string form
	opts := &runOptions{}
	// The "body" field is json.RawMessage so this will be stored as the string "not-json"
	// but applyBodyOptions checks json.Valid and should return error
	if err := applyBodyOptions(opts, raw); err == nil {
		t.Fatal("applyBodyOptions with invalid body field should return error")
	}
}

// TestOptionsFromManagementRequest_Defaults verifies default values are set correctly.
func TestOptionsFromManagementRequest_Defaults(t *testing.T) {
	opts, err := optionsFromManagementRequest(managementRequest{})
	if err != nil {
		t.Fatalf("optionsFromManagementRequest error = %v", err)
	}
	if opts.Model != defaultModel {
		t.Errorf("Model = %q, want %q", opts.Model, defaultModel)
	}
	if opts.Mode != "non-stream" {
		t.Errorf("Mode = %q, want %q", opts.Mode, "non-stream")
	}
	if opts.EntryProtocol != "openai" {
		t.Errorf("EntryProtocol = %q, want %q", opts.EntryProtocol, "openai")
	}
	if opts.ExitProtocol != "openai" {
		t.Errorf("ExitProtocol = %q, want %q", opts.ExitProtocol, "openai")
	}
	if opts.Prompt != defaultPrompt {
		t.Errorf("Prompt = %q, want %q", opts.Prompt, defaultPrompt)
	}
	if opts.Stream {
		t.Errorf("Stream = true, want false")
	}
}

// TestOptionsFromManagementRequest_HostCallbackID verifies host callback ID is trimmed and propagated.
func TestOptionsFromManagementRequest_HostCallbackID(t *testing.T) {
	req := managementRequest{HostCallbackID: "  cb-123  "}
	opts, err := optionsFromManagementRequest(req)
	if err != nil {
		t.Fatalf("optionsFromManagementRequest error = %v", err)
	}
	if opts.HostCallbackID != "cb-123" {
		t.Errorf("HostCallbackID = %q, want %q", opts.HostCallbackID, "cb-123")
	}
}

// TestOptionsFromManagementRequest_StreamModeFromQuery verifies mode=stream sets Stream=true and Mode="stream".
func TestOptionsFromManagementRequest_StreamModeFromQuery(t *testing.T) {
	req := managementRequest{
		Query: url.Values{"stream": []string{"true"}},
	}
	opts, err := optionsFromManagementRequest(req)
	if err != nil {
		t.Fatalf("optionsFromManagementRequest error = %v", err)
	}
	if !opts.Stream {
		t.Errorf("Stream = false, want true")
	}
	if opts.Mode != "stream" {
		t.Errorf("Mode = %q, want %q", opts.Mode, "stream")
	}
}

// TestModelRequestBody_Generated verifies the auto-generated request body uses opts values.
func TestModelRequestBody_Generated(t *testing.T) {
	opts := runOptions{
		Model:  "gpt-5.5",
		Stream: false,
		Prompt: "Hello world",
	}
	raw, err := modelRequestBody(opts)
	if err != nil {
		t.Fatalf("modelRequestBody error = %v", err)
	}
	var req chatCompletionRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("unmarshal chat request: %v", err)
	}
	if req.Model != "gpt-5.5" {
		t.Errorf("Model = %q, want %q", req.Model, "gpt-5.5")
	}
	if req.Stream {
		t.Errorf("Stream = true, want false")
	}
	if len(req.Messages) != 1 {
		t.Fatalf("Messages len = %d, want 1", len(req.Messages))
	}
	if req.Messages[0].Role != "user" {
		t.Errorf("Role = %q, want %q", req.Messages[0].Role, "user")
	}
	if req.Messages[0].Content != "Hello world" {
		t.Errorf("Content = %q, want %q", req.Messages[0].Content, "Hello world")
	}
}

// TestModelRequestBody_CustomBody verifies that when opts.Body is set it is returned as-is.
func TestModelRequestBody_CustomBody(t *testing.T) {
	custom := []byte(`{"custom":"body"}`)
	opts := runOptions{Body: custom, Model: "unused"}
	raw, err := modelRequestBody(opts)
	if err != nil {
		t.Fatalf("modelRequestBody error = %v", err)
	}
	if !bytes.Equal(raw, custom) {
		t.Errorf("modelRequestBody returned %q, want %q", raw, custom)
	}
	// Ensure the returned slice is a copy (not the same underlying array).
	raw[0] = 'x'
	if opts.Body[0] == 'x' {
		t.Errorf("modelRequestBody shares memory with opts.Body, want independent copy")
	}
}

// TestRenderPage_ContainsModel verifies that the rendered page includes the model name.
func TestRenderPage_ContainsModel(t *testing.T) {
	opts := runOptions{
		Model:         "test-model-xyz",
		Mode:          "non-stream",
		EntryProtocol: "openai",
		ExitProtocol:  "openai",
	}
	page := renderPage(opts, 200, nil, nil, nil, "", "", "")
	if !bytes.Contains(page, []byte("test-model-xyz")) {
		t.Errorf("rendered page does not contain model name %q", "test-model-xyz")
	}
}

// TestRenderPage_ErrorSection verifies that an error message appears in the error section.
func TestRenderPage_ErrorSection(t *testing.T) {
	opts := runOptions{Model: defaultModel, Mode: "non-stream", EntryProtocol: "openai", ExitProtocol: "openai"}
	page := renderPage(opts, 0, nil, nil, nil, "something went wrong", "", "")
	pageStr := string(page)
	if !strings.Contains(pageStr, "something went wrong") {
		t.Errorf("rendered page does not contain error text")
	}
	if !strings.Contains(pageStr, "Error") {
		t.Errorf("rendered page does not contain 'Error' heading")
	}
}

// TestRenderPage_CloseErrorSection verifies that a close error appears in the close error section.
func TestRenderPage_CloseErrorSection(t *testing.T) {
	opts := runOptions{Model: defaultModel, Mode: "stream", EntryProtocol: "openai", ExitProtocol: "openai"}
	page := renderPage(opts, 200, nil, nil, nil, "", "explicit close mode", "close failed")
	pageStr := string(page)
	if !strings.Contains(pageStr, "close failed") {
		t.Errorf("rendered page does not contain close error text")
	}
}

// TestRenderPage_StreamChunks verifies that stream chunks appear in the page.
func TestRenderPage_StreamChunks(t *testing.T) {
	opts := runOptions{Model: defaultModel, Mode: "stream", EntryProtocol: "openai", ExitProtocol: "openai", Stream: true}
	chunks := []string{"chunk1", "chunk2"}
	page := renderPage(opts, 200, nil, nil, chunks, "", "", "")
	pageStr := string(page)
	if !strings.Contains(pageStr, "chunk1") || !strings.Contains(pageStr, "chunk2") {
		t.Errorf("rendered page does not contain stream chunks")
	}
}

// TestRenderPage_HTMLEscaping verifies that HTML-unsafe characters in values are escaped.
func TestRenderPage_HTMLEscaping(t *testing.T) {
	opts := runOptions{
		Model:         "<script>alert(1)</script>",
		Mode:          "non-stream",
		EntryProtocol: "openai",
		ExitProtocol:  "openai",
	}
	page := renderPage(opts, 200, nil, nil, nil, "", "", "")
	pageStr := string(page)
	if strings.Contains(pageStr, "<script>") {
		t.Errorf("rendered page contains unescaped <script> tag")
	}
	if !strings.Contains(pageStr, "&lt;script&gt;") {
		t.Errorf("rendered page does not contain escaped script tag")
	}
}

// TestRenderPage_BodySection verifies that the response body appears in the page.
func TestRenderPage_BodySection(t *testing.T) {
	opts := runOptions{Model: defaultModel, Mode: "non-stream", EntryProtocol: "openai", ExitProtocol: "openai"}
	body := []byte(`{"key":"value"}`)
	page := renderPage(opts, 200, nil, body, nil, "", "", "")
	pageStr := string(page)
	if !strings.Contains(pageStr, "Body") {
		t.Errorf("rendered page does not contain 'Body' heading")
	}
	if !strings.Contains(pageStr, "value") {
		t.Errorf("rendered page does not contain body content")
	}
}

// TestRenderPage_HeadersSection verifies that response headers appear in the page.
func TestRenderPage_HeadersSection(t *testing.T) {
	opts := runOptions{Model: defaultModel, Mode: "non-stream", EntryProtocol: "openai", ExitProtocol: "openai"}
	headers := http.Header{"content-type": []string{"application/json"}}
	page := renderPage(opts, 200, headers, nil, nil, "", "", "")
	pageStr := string(page)
	if !strings.Contains(pageStr, "Headers") {
		t.Errorf("rendered page does not contain 'Headers' heading")
	}
	if !strings.Contains(pageStr, "application/json") {
		t.Errorf("rendered page does not contain header value")
	}
}

// TestPrettyBody_ValidJSON verifies that valid JSON is indented.
func TestPrettyBody_ValidJSON(t *testing.T) {
	raw := []byte(`{"a":1}`)
	result := prettyBody(raw)
	if !strings.Contains(result, "\n") {
		t.Errorf("prettyBody(%q) did not indent JSON, got %q", raw, result)
	}
}

// TestPrettyBody_InvalidJSON verifies that invalid JSON is returned as-is.
func TestPrettyBody_InvalidJSON(t *testing.T) {
	raw := []byte("not json")
	result := prettyBody(raw)
	if result != "not json" {
		t.Errorf("prettyBody(%q) = %q, want %q", raw, result, "not json")
	}
}

// TestPrettyJSON_Map verifies that a map is marshaled with indentation.
func TestPrettyJSON_Map(t *testing.T) {
	m := map[string]string{"key": "val"}
	result := prettyJSON(m)
	if !strings.Contains(result, "\n") {
		t.Errorf("prettyJSON(map) did not indent JSON, got %q", result)
	}
	if !strings.Contains(result, "key") {
		t.Errorf("prettyJSON(map) does not contain key, got %q", result)
	}
}

// TestCloneHeader_Nil verifies nil input returns nil.
func TestCloneHeader_Nil(t *testing.T) {
	if cloneHeader(nil) != nil {
		t.Errorf("cloneHeader(nil) = non-nil, want nil")
	}
}

// TestCloneHeader_Independence verifies that mutations to the clone do not affect the original.
func TestCloneHeader_Independence(t *testing.T) {
	orig := http.Header{"Content-Type": []string{"application/json"}}
	cloned := cloneHeader(orig)
	cloned["Content-Type"][0] = "text/html"
	if orig["Content-Type"][0] != "application/json" {
		t.Errorf("cloneHeader: mutation of clone affected original header")
	}
}

// TestCloneValues_Nil verifies nil input returns nil.
func TestCloneValues_Nil(t *testing.T) {
	if cloneValues(nil) != nil {
		t.Errorf("cloneValues(nil) = non-nil, want nil")
	}
}

// TestCloneValues_Independence verifies that mutations to the clone do not affect the original.
func TestCloneValues_Independence(t *testing.T) {
	orig := url.Values{"key": []string{"a", "b"}}
	cloned := cloneValues(orig)
	cloned["key"][0] = "x"
	if orig["key"][0] != "a" {
		t.Errorf("cloneValues: mutation of clone affected original values")
	}
}

// TestHTMLResponse verifies the response has the correct content-type, status, and body.
func TestHTMLResponse(t *testing.T) {
	body := []byte("<h1>Hello</h1>")
	resp := htmlResponse(200, body)
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	ctValues := resp.Headers.Values("content-type")
	if len(ctValues) == 0 || ctValues[0] != resourceContentType {
		t.Errorf("content-type = %v, want %q", ctValues, resourceContentType)
	}
	if !bytes.Equal(resp.Body, body) {
		t.Errorf("Body = %q, want %q", resp.Body, body)
	}
}

// TestPluginRegistration_ManagementAPI verifies the plugin registration has the management_api capability.
func TestPluginRegistration_ManagementAPI(t *testing.T) {
	reg := pluginRegistration()
	if !reg.Capabilities.ManagementAPI {
		t.Errorf("ManagementAPI = false, want true")
	}
	if reg.Metadata.Name != pluginName {
		t.Errorf("Name = %q, want %q", reg.Metadata.Name, pluginName)
	}
	if reg.Metadata.ConfigFields == nil {
		t.Errorf("ConfigFields is nil, want empty slice")
	}
}

// TestOkEnvelope_RoundTrip verifies that okEnvelope produces a parseable success envelope.
func TestOkEnvelope_RoundTrip(t *testing.T) {
	type payload struct {
		X int `json:"x"`
	}
	raw, err := okEnvelope(payload{X: 42})
	if err != nil {
		t.Fatalf("okEnvelope error = %v", err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.OK {
		t.Errorf("OK = false, want true")
	}
	var p payload
	if err := json.Unmarshal(env.Result, &p); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if p.X != 42 {
		t.Errorf("X = %d, want 42", p.X)
	}
}

// TestErrorEnvelope_Fields verifies that errorEnvelope produces a failed envelope with correct fields.
func TestErrorEnvelope_Fields(t *testing.T) {
	raw := errorEnvelope("err_code", "err message")
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.OK {
		t.Errorf("OK = true, want false")
	}
	if env.Error == nil {
		t.Fatal("Error is nil")
	}
	if env.Error.Code != "err_code" {
		t.Errorf("Code = %q, want %q", env.Error.Code, "err_code")
	}
	if env.Error.Message != "err message" {
		t.Errorf("Message = %q, want %q", env.Error.Message, "err message")
	}
}