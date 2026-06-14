package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// headerToAnyMap tests

func TestHeaderToAnyMapNilInput(t *testing.T) {
	result := headerToAnyMap(nil)
	if result == nil {
		t.Fatal("headerToAnyMap(nil) = nil, want empty map")
	}
	if len(result) != 0 {
		t.Fatalf("headerToAnyMap(nil) len = %d, want 0", len(result))
	}
}

func TestHeaderToAnyMapSingleValueHeader(t *testing.T) {
	h := http.Header{"Content-Type": []string{"application/json"}}
	result := headerToAnyMap(h)
	v, ok := result["Content-Type"]
	if !ok {
		t.Fatal("headerToAnyMap() missing Content-Type key")
	}
	if v != "application/json" {
		t.Fatalf("headerToAnyMap() Content-Type = %v, want application/json", v)
	}
}

func TestHeaderToAnyMapMultiValueHeaderReturnsSlice(t *testing.T) {
	h := http.Header{"Accept": []string{"text/html", "application/json"}}
	result := headerToAnyMap(h)
	v, ok := result["Accept"]
	if !ok {
		t.Fatal("headerToAnyMap() missing Accept key")
	}
	values, okSlice := v.([]string)
	if !okSlice {
		t.Fatalf("headerToAnyMap() Accept type = %T, want []string", v)
	}
	if len(values) != 2 {
		t.Fatalf("headerToAnyMap() Accept len = %d, want 2", len(values))
	}
}

func TestHeaderToAnyMapSkipsEmptyValueHeaders(t *testing.T) {
	h := http.Header{"X-Empty": []string{}}
	result := headerToAnyMap(h)
	if _, ok := result["X-Empty"]; ok {
		t.Fatal("headerToAnyMap() included header with empty values, want skipped")
	}
}

// dedupeStrings tests

func TestDedupeStringsEmptyInput(t *testing.T) {
	result := dedupeStrings(nil)
	if result != nil {
		t.Fatalf("dedupeStrings(nil) = %v, want nil", result)
	}
}

func TestDedupeStringsRemovesDuplicates(t *testing.T) {
	result := dedupeStrings([]string{"X-Foo", "X-Bar", "X-Foo"})
	if len(result) != 2 {
		t.Fatalf("dedupeStrings() len = %d, want 2", len(result))
	}
}

func TestDedupeStringsIsCaseInsensitive(t *testing.T) {
	result := dedupeStrings([]string{"x-foo", "X-Foo", "X-FOO"})
	if len(result) != 1 {
		t.Fatalf("dedupeStrings() len = %d, want 1 (case insensitive)", len(result))
	}
}

func TestDedupeStringsPreservesCanonicalForm(t *testing.T) {
	result := dedupeStrings([]string{"content-type"})
	if len(result) != 1 {
		t.Fatalf("dedupeStrings() len = %d, want 1", len(result))
	}
	if result[0] != "Content-Type" {
		t.Fatalf("dedupeStrings() = %q, want canonical Content-Type", result[0])
	}
}

// stringSliceFromAny tests

func TestStringSliceFromAnyWithStringSlice(t *testing.T) {
	input := []string{"a", "b", "c"}
	result, ok := stringSliceFromAny(input)
	if !ok {
		t.Fatal("stringSliceFromAny([]string) ok = false")
	}
	if len(result) != 3 || result[0] != "a" {
		t.Fatalf("stringSliceFromAny([]string) = %v, want [a b c]", result)
	}
}

func TestStringSliceFromAnyWithAnySlice(t *testing.T) {
	input := []any{"x", "y"}
	result, ok := stringSliceFromAny(input)
	if !ok {
		t.Fatal("stringSliceFromAny([]any strings) ok = false")
	}
	if len(result) != 2 || result[0] != "x" {
		t.Fatalf("stringSliceFromAny([]any) = %v", result)
	}
}

func TestStringSliceFromAnyWithMixedAnySliceFails(t *testing.T) {
	input := []any{"x", 42}
	_, ok := stringSliceFromAny(input)
	if ok {
		t.Fatal("stringSliceFromAny(mixed types) ok = true, want false")
	}
}

func TestStringSliceFromAnyWithNonSliceFails(t *testing.T) {
	_, ok := stringSliceFromAny("not a slice")
	if ok {
		t.Fatal("stringSliceFromAny(string) ok = true, want false")
	}
}

func TestStringSliceFromAnyReturnsCopy(t *testing.T) {
	input := []string{"a", "b"}
	result, _ := stringSliceFromAny(input)
	result[0] = "modified"
	if input[0] != "a" {
		t.Fatal("stringSliceFromAny() did not return a copy")
	}
}

// updateHeaderFromAny tests

func TestUpdateHeaderFromAnySetsStringValue(t *testing.T) {
	h := http.Header{}
	updateHeaderFromAny(h, map[string]any{"X-Custom": "value1"})
	if got := h.Get("X-Custom"); got != "value1" {
		t.Fatalf("updateHeaderFromAny() X-Custom = %q, want value1", got)
	}
}

func TestUpdateHeaderFromAnyDeletesHeaderOnNilValue(t *testing.T) {
	h := http.Header{"X-Delete-Me": []string{"old"}}
	cleared := updateHeaderFromAny(h, map[string]any{"X-Delete-Me": nil})
	if got := h.Get("X-Delete-Me"); got != "" {
		t.Fatalf("updateHeaderFromAny(nil) X-Delete-Me = %q, want deleted", got)
	}
	if len(cleared) == 0 {
		t.Fatal("updateHeaderFromAny(nil) cleared list is empty, want entry")
	}
}

func TestUpdateHeaderFromAnyDeletesHeaderOnEmptySlice(t *testing.T) {
	h := http.Header{"X-Delete-Me": []string{"old"}}
	cleared := updateHeaderFromAny(h, map[string]any{"X-Delete-Me": []string{}})
	if got := h.Get("X-Delete-Me"); got != "" {
		t.Fatalf("updateHeaderFromAny([]) X-Delete-Me = %q, want deleted", got)
	}
	if len(cleared) == 0 {
		t.Fatal("updateHeaderFromAny([]) cleared list is empty, want entry")
	}
}

func TestUpdateHeaderFromAnyIgnoresNonMapInput(t *testing.T) {
	h := http.Header{"X-Existing": []string{"keep"}}
	cleared := updateHeaderFromAny(h, "not a map")
	if len(cleared) != 0 {
		t.Fatalf("updateHeaderFromAny(non-map) cleared = %v, want empty", cleared)
	}
	if h.Get("X-Existing") != "keep" {
		t.Fatal("updateHeaderFromAny(non-map) modified headers, want no-op")
	}
}

func TestUpdateHeaderFromAnyIgnoresNilHeader(t *testing.T) {
	// Should not panic on nil header.
	cleared := updateHeaderFromAny(nil, map[string]any{"X-Foo": "bar"})
	if len(cleared) != 0 {
		t.Fatalf("updateHeaderFromAny(nil header) cleared = %v, want empty", cleared)
	}
}

// cloneHeader tests

func TestCloneHeaderIsIndependent(t *testing.T) {
	orig := http.Header{"X-Foo": []string{"a", "b"}}
	cloned := cloneHeader(orig)
	cloned["X-Foo"][0] = "modified"
	if orig.Get("X-Foo") != "a" {
		t.Fatal("cloneHeader() is not independent: original was modified through clone")
	}
}

func TestCloneHeaderCopiesAllKeys(t *testing.T) {
	orig := http.Header{"X-A": []string{"1"}, "X-B": []string{"2"}}
	cloned := cloneHeader(orig)
	if len(cloned) != 2 {
		t.Fatalf("cloneHeader() len = %d, want 2", len(cloned))
	}
}

func TestCloneHeaderEmptyInput(t *testing.T) {
	cloned := cloneHeader(http.Header{})
	if len(cloned) != 0 {
		t.Fatalf("cloneHeader(empty) = %v, want empty", cloned)
	}
}

// applyJSRequestHook: hook not found falls back to original body

func TestApplyJSRequestHookFunctionNotFoundReturnsOriginalBody(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "noop.js")
	if errWrite := os.WriteFile(scriptPath, []byte(`// no functions defined`), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}
	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	original := []byte(`{"original":true}`)
	processed, _, err := plugin.applyJSRequestHook(scriptPath, "on_before_request", original, "gpt-test", "openai", "", http.Header{}, "")
	if err != nil {
		t.Fatalf("applyJSRequestHook(noop) error = %v", err)
	}
	if string(processed) != string(original) {
		t.Fatalf("applyJSRequestHook(noop) body = %q, want original %q", processed, original)
	}
}

// applyJSRequestHook: JS returning a plain string replaces body

func TestApplyJSRequestHookReturningStringReplacesBody(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "replace.js")
	script := `function on_before_request(ctx) { return "replaced_body"; }`
	if errWrite := os.WriteFile(scriptPath, []byte(script), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}
	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	processed, _, err := plugin.applyJSRequestHook(scriptPath, "on_before_request", []byte("original"), "gpt-test", "openai", "", http.Header{}, "")
	if err != nil {
		t.Fatalf("applyJSRequestHook() error = %v", err)
	}
	if string(processed) != "replaced_body" {
		t.Fatalf("applyJSRequestHook() body = %q, want replaced_body", processed)
	}
}

// applyJSRequestHook: JS returning null falls back to original body

func TestApplyJSRequestHookReturningNullKeepsOriginalBody(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "nullreturn.js")
	script := `function on_before_request(ctx) { return null; }`
	if errWrite := os.WriteFile(scriptPath, []byte(script), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}
	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	original := []byte(`{"keep":true}`)
	processed, _, err := plugin.applyJSRequestHook(scriptPath, "on_before_request", original, "gpt-test", "openai", "", http.Header{}, "")
	if err != nil {
		t.Fatalf("applyJSRequestHook(null return) error = %v", err)
	}
	if string(processed) != string(original) {
		t.Fatalf("applyJSRequestHook(null) body = %q, want original", processed)
	}
}

// applyJSAfterResponse: hook not found returns unchanged

func TestApplyJSAfterResponseHookNotFoundReturnsFalseChanged(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "noop_resp.js")
	if errWrite := os.WriteFile(scriptPath, []byte(`// no functions`), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}
	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	body, _, changed, err := plugin.applyJSAfterResponse(scriptPath, "gpt-test", "openai", nil, nil, `{"original":true}`, nil, http.Header{}, false, nil, "")
	if err != nil {
		t.Fatalf("applyJSAfterResponse(no hook) error = %v", err)
	}
	if changed {
		t.Fatal("applyJSAfterResponse(no hook) changed = true, want false")
	}
	if body != `{"original":true}` {
		t.Fatalf("applyJSAfterResponse(no hook) body = %q, want original", body)
	}
}

// applyJSAfterResponse: JS modifying response headers

func TestApplyJSAfterResponseSetsResponseHeader(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "setheader.js")
	script := `
function on_after_nonstream_response(ctx) {
    ctx.headers["X-Response"] = "injected";
    return ctx;
}
`
	if errWrite := os.WriteFile(scriptPath, []byte(script), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}
	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	headers := http.Header{}
	_, ph, _, err := plugin.applyJSAfterResponse(scriptPath, "gpt-test", "openai", nil, nil, `{}`, nil, headers, false, nil, "")
	if err != nil {
		t.Fatalf("applyJSAfterResponse() error = %v", err)
	}
	if ph == nil {
		t.Fatal("applyJSAfterResponse() processedHeaders = nil, want non-nil")
	}
	if got := ph.headers.Get("X-Response"); got != "injected" {
		t.Fatalf("applyJSAfterResponse() X-Response = %q, want injected", got)
	}
}

// interceptRequest: no script paths returns passthrough

func TestInterceptRequestNoScriptsPassthrough(t *testing.T) {
	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig(), pluginDir: ""}
	// With no script paths configured and no builtins, it should pass through.
	// We set pluginDir to a non-existent path so builtinScriptPaths returns nil.
	plugin.pluginDir = t.TempDir() // exists but has no scripts/ dir

	ctx := context.Background()
	resp, err := plugin.InterceptRequestBeforeAuth(ctx, pluginapi.RequestInterceptRequest{
		Body: []byte(`{"model":"gpt-test"}`),
	})
	if err != nil {
		t.Fatalf("InterceptRequestBeforeAuth() error = %v", err)
	}
	if len(resp.Body) != 0 {
		t.Fatalf("InterceptRequestBeforeAuth() body = %q, want empty passthrough", resp.Body)
	}
}

func TestApplyJSBeforeRequestUsesReturnedCtxBody(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "before.js")
	script := `

    var req = JSON.parse(ctx.body);
    req.messages[0].content = req.messages[0].content.replace("sensitive_word", "safe_word");
    ctx.body = JSON.stringify(req);
    ctx.headers["X-Plugin"] = "updated";
    return ctx;
}
`
	if errWrite := os.WriteFile(scriptPath, []byte(script), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}

	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	headers := http.Header{"X-Plugin": []string{"original"}}
	processed, _, errApply := plugin.applyJSRequestHook(
		scriptPath,
		"on_before_request",
		[]byte(`{"messages":[{"role":"user","content":"contains sensitive_word"}]}`),
		"gpt-test",
		"openai",
		"",
		headers,
		"",
	)
	if errApply != nil {
		t.Fatalf("applyJSRequestHook() error = %v", errApply)
	}
	if body := string(processed); !strings.Contains(body, "safe_word") || strings.Contains(body, "sensitive_word") {
		t.Fatalf("processed body = %q, want sensitive word rewritten", body)
	}
	if got := headers.Get("X-Plugin"); got != "updated" {
		t.Fatalf("header X-Plugin = %q, want updated", got)
	}
}

func TestApplyJSAfterAuthRequestReceivesFormats(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "after_auth.js")
	script := `
function on_after_auth_request(ctx) {
    if (ctx.source_format !== "openai" || ctx.to_format !== "codex") {
        throw new Error("unexpected formats: " + ctx.source_format + " -> " + ctx.to_format);
    }
    if (ctx.sourceFormat !== "openai" || ctx.toFormat !== "codex") {
        throw new Error("unexpected camel formats: " + ctx.sourceFormat + " -> " + ctx.toFormat);
    }
    var req = JSON.parse(ctx.body);
    req.after_auth = ctx.source_format + "_to_" + ctx.to_format;
    ctx.headers["X-Protocol"] = req.after_auth;
    ctx.body = JSON.stringify(req);
    return ctx;
}
`
	if errWrite := os.WriteFile(scriptPath, []byte(script), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}

	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	headers := http.Header{}
	processed, _, errApply := plugin.applyJSRequestHook(
		scriptPath,
		"on_after_auth_request",
		[]byte(`{"model":"gpt-test"}`),
		"gpt-test",
		"openai",
		"codex",
		headers,
		"",
	)
	if errApply != nil {
		t.Fatalf("applyJSRequestHook() error = %v", errApply)
	}
	if body := string(processed); !strings.Contains(body, `"after_auth":"openai_to_codex"`) {
		t.Fatalf("processed body = %q, want after_auth marker", body)
	}
	if got := headers.Get("X-Protocol"); got != "openai_to_codex" {
		t.Fatalf("header X-Protocol = %q, want openai_to_codex", got)
	}
}

func TestApplyJSAfterResponseUsesFrozenNativeHistoryChunks(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "stream.js")
	script := `
function on_after_stream_response(ctx) {
    if (!Object.isFrozen(ctx.history_chunks)) {
        throw new Error("history_chunks is not frozen");
    }
    var original = ctx.history_chunks[0];
    try {
        ctx.history_chunks[0] = "changed";
    } catch (e) {
    }
    if (ctx.history_chunks[0] !== original) {
        throw new Error("history_chunks item was changed");
    }
    try {
        ctx.history_chunks = ["changed"];
    } catch (e) {
    }
    if (ctx.history_chunks[0] !== original) {
        throw new Error("history_chunks property was replaced");
    }
    return { chunk: ctx.chunk + "|ok" };
}
`
	if errWrite := os.WriteFile(scriptPath, []byte(script), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}

	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	chunk := `data: {"choices":[{"delta":{},"finish_reason":null}]}`
	processedBody, _, changed, errApply := plugin.applyJSAfterResponse(
		scriptPath,
		"gpt-test",
		"openai",
		nil,
		nil,
		"",
		&chunk,
		http.Header{},
		true,
		[]string{`data: {"choices":[{"delta":{"tool_calls":[{"index":0}]}}]}`},
		"",
	)
	if errApply != nil {
		t.Fatalf("applyJSAfterResponse() error = %v", errApply)
	}
	if !changed {
		t.Fatal("applyJSAfterResponse() changed = false, want true")
	}
	if processedBody != chunk+"|ok" {
		t.Fatalf("applyJSAfterResponse() body = %q, want %q", processedBody, chunk+"|ok")
	}
}

func TestApplyJSAfterResponseDispatchesNonStreamHook(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "nonstream.js")
	script := `
function on_after_stream_response(ctx) {
    throw new Error("stream hook should not run");
}
function on_after_nonstream_response(ctx) {
    return { body: ctx.body + "|nonstream" };
}
`
	if errWrite := os.WriteFile(scriptPath, []byte(script), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}

	plugin := &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	processedBody, _, changed, errApply := plugin.applyJSAfterResponse(
		scriptPath,
		"gpt-test",
		"openai",
		nil,
		nil,
		`{"ok":true}`,
		nil,
		http.Header{},
		false,
		nil,
		"",
	)
	if errApply != nil {
		t.Fatalf("applyJSAfterResponse() error = %v", errApply)
	}
	if !changed {
		t.Fatal("applyJSAfterResponse() changed = false, want true")
	}
	if processedBody != `{"ok":true}|nonstream` {
		t.Fatalf("applyJSAfterResponse() body = %q", processedBody)
	}
}
