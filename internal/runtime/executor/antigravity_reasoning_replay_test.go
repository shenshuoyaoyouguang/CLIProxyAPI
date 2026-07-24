package executor

import (
	"context"
	"strings"
	"testing"

	internalcache "github.com/router-for-me/CLIProxyAPI/v7/internal/cache"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/tidwall/gjson"
)

func antigravityReplayTestScope(modelName, sessionID, apiKey string) antigravityReasoningReplayScope {
	return antigravityReasoningReplayScope{
		modelName:  modelName,
		sessionKey: helps.IsolateClientControlledSessionKey(testContextWithAPIKey(apiKey), "session:"+sessionID),
	}
}

func TestAntigravityReasoningReplayAccumulatorMultiToolSSEChunks(t *testing.T) {
	internalcache.ClearAntigravityReasoningReplayCache()
	t.Cleanup(internalcache.ClearAntigravityReasoningReplayCache)

	requestPayload := []byte(`{"sessionId":"sess-1","request":{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}}`)
	scope := antigravityReplayTestScope("gemini-3-flash-agent", "sess-1", "ag-caller-1")
	acc := newAntigravityReasoningReplayAccumulator(scope, requestPayload)
	if acc == nil {
		t.Fatal("accumulator is nil")
	}
	if acc.contentIndex != 1 || acc.nextPartIndex != 0 {
		t.Fatalf("pending model slot = %d/%d, want 1/0", acc.contentIndex, acc.nextPartIndex)
	}

	line1 := []byte(`data: {"response":{"candidates":[{"content":{"parts":[{"thoughtSignature":"sig-first","functionCall":{"name":"Read","args":{"file_path":"/a"},"id":"id1"}}]}}]}}`)
	line2 := []byte(`data: {"response":{"candidates":[{"content":{"parts":[{"functionCall":{"name":"Read","args":{"file_path":"/b"},"id":"id2"}}]}}]}}`)
	acc.ObserveSSELine(line1)
	acc.ObserveSSELine(line2)
	acc.Flush(context.Background())

	items, ok := internalcache.GetAntigravityReasoningReplayItems(scope.modelName, scope.sessionKey)
	if !ok || len(items) != 2 {
		t.Fatalf("cached items = %v ok=%v, want 2 items", len(items), ok)
	}
	pi0 := int(gjson.GetBytes(items[0], "partIndex").Int())
	pi1 := int(gjson.GetBytes(items[1], "partIndex").Int())
	if pi0 != 0 || pi1 != 1 {
		t.Fatalf("partIndex = %d,%d, want 0,1", pi0, pi1)
	}
	if got := gjson.GetBytes(items[0], "thoughtSignature").String(); got != "sig-first" {
		t.Fatalf("first sig = %q", got)
	}
}

func TestPrepareAntigravityGeminiReasoningReplayPayloadInjectsCachedToolPart(t *testing.T) {
	internalcache.ClearAntigravityReasoningReplayCache()
	t.Cleanup(internalcache.ClearAntigravityReasoningReplayCache)

	scopeSeed := antigravityReplayTestScope("gemini-3-flash-agent", "sess-2", "ag-caller-2")
	item := []byte(`{"type":"function_call_part","contentIndex":1,"partIndex":0,"name":"Read","call_id":"id1","args":{"file_path":"/a"},"thoughtSignature":"sig-first"}`)
	if !internalcache.CacheAntigravityReasoningReplayItems(scopeSeed.modelName, scopeSeed.sessionKey, [][]byte{item}) {
		t.Fatal("cache write failed")
	}

	req := cliproxyexecutor.Request{}
	opts := cliproxyexecutor.Options{}
	payload := []byte(`{"sessionId":"sess-2","request":{"contents":[{"role":"user","parts":[{"text":"hi"}]},{"role":"user","parts":[{"functionResponse":{"id":"id1","name":"Read","response":{"result":"ok"}}}]}]}}`)
	out, scope, err := prepareAntigravityGeminiReasoningReplayPayload(testContextWithAPIKey("ag-caller-2"), "gemini-3-flash-agent", req, opts, payload)
	if err != nil {
		t.Fatalf("prepare error: %v", err)
	}
	if !scope.valid() {
		t.Fatal("scope invalid")
	}
	if scope.sessionKey != scopeSeed.sessionKey {
		t.Fatalf("scope.sessionKey = %q, want isolated %q", scope.sessionKey, scopeSeed.sessionKey)
	}
	if gjson.GetBytes(out, "request.contents.1.role").String() != "model" {
		t.Fatalf("functionCall replay must be model role at [1], got %s", string(out))
	}
	if got := gjson.GetBytes(out, "request.contents.1.parts.0.thoughtSignature").String(); got != "sig-first" {
		t.Fatalf("thoughtSignature = %q, want sig-first", got)
	}
	if !gjson.GetBytes(out, "request.contents.1.parts.0.functionCall").Exists() {
		t.Fatalf("functionCall not injected: %s", string(out))
	}
	if !gjson.GetBytes(out, "request.contents.2.parts.0.functionResponse").Exists() {
		t.Fatalf("functionResponse should follow model functionCall at [2]: %s", string(out))
	}
}

func TestPrepareAntigravityGeminiReasoningReplayInsertsBeforeModelFunctionResponse(t *testing.T) {
	internalcache.ClearAntigravityReasoningReplayCache()
	t.Cleanup(internalcache.ClearAntigravityReasoningReplayCache)

	scopeSeed := antigravityReplayTestScope("gemini-3-flash-agent", "sess-3", "ag-caller-3")
	item := []byte(`{"type":"function_call_part","contentIndex":1,"partIndex":0,"name":"Read","call_id":"id1","args":{"file_path":"/a"},"thoughtSignature":"sig-first"}`)
	internalcache.CacheAntigravityReasoningReplayItems(scopeSeed.modelName, scopeSeed.sessionKey, [][]byte{item})

	payload := []byte(`{"sessionId":"sess-3","request":{"contents":[{"role":"user","parts":[{"text":"hi"}]},{"role":"model","parts":[{"functionResponse":{"id":"id1","name":"Read","response":{"result":"ok"}}}]}]}}`)
	out, _, err := prepareAntigravityGeminiReasoningReplayPayload(testContextWithAPIKey("ag-caller-3"), "gemini-3-flash-agent", cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, payload)
	if err != nil {
		t.Fatal(err)
	}
	if !gjson.GetBytes(out, "request.contents.1.parts.0.functionCall").Exists() || gjson.GetBytes(out, "request.contents.1.role").String() != "model" {
		t.Fatalf("want model functionCall at [1]: %s", string(out))
	}
	if !gjson.GetBytes(out, "request.contents.2.parts.0.functionResponse").Exists() {
		t.Fatalf("functionResponse should be at [2]: %s", string(out))
	}
}

func TestMergeAntigravityFunctionCallPartReplayMergesSignatureIntoExistingFunctionCall(t *testing.T) {
	internalcache.ClearAntigravityReasoningReplayCache()
	t.Cleanup(internalcache.ClearAntigravityReasoningReplayCache)

	scopeSeed := antigravityReplayTestScope("gemini-3-flash-agent", "sess-merge", "ag-caller-merge")
	item := []byte(`{"type":"function_call_part","contentIndex":1,"partIndex":0,"name":"Read","call_id":"id1","args":{"file_path":"/a"},"thoughtSignature":"sig-first"}`)
	internalcache.CacheAntigravityReasoningReplayItems(scopeSeed.modelName, scopeSeed.sessionKey, [][]byte{item})

	payload := []byte(`{"sessionId":"sess-merge","request":{"contents":[{"role":"user","parts":[{"text":"hi"}]},{"role":"model","parts":[{"functionCall":{"id":"id1","name":"Read","args":{"file_path":"/a"}}}]},{"role":"user","parts":[{"functionResponse":{"id":"id1","name":"Read","response":{"result":"ok"}}}]}]}}`)
	out, _, err := prepareAntigravityGeminiReasoningReplayPayload(testContextWithAPIKey("ag-caller-merge"), "gemini-3-flash-agent", cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, payload)
	if err != nil {
		t.Fatal(err)
	}
	if got := gjson.GetBytes(out, "request.contents.1.parts.0.thoughtSignature").String(); got != "sig-first" {
		t.Fatalf("thoughtSignature = %q, want sig-first; body=%s", got, out)
	}
}

func TestPrepareAntigravityGeminiReasoningReplayPayloadAppendsStaleThoughtSignatureWithoutNullParts(t *testing.T) {
	internalcache.ClearAntigravityReasoningReplayCache()
	t.Cleanup(internalcache.ClearAntigravityReasoningReplayCache)

	scopeSeed := antigravityReplayTestScope("gemini-3-flash-agent", "sess-stale-text", "ag-caller-stale")
	item := []byte(`{"type":"thought_signature","contentIndex":8,"partIndex":3,"thoughtSignature":"stale-thought-sig-ok12"}`)
	internalcache.CacheAntigravityReasoningReplayItems(scopeSeed.modelName, scopeSeed.sessionKey, [][]byte{item})

	payload := []byte(`{"sessionId":"sess-stale-text","request":{"contents":[{"role":"user","parts":[{"text":"hi"}]},{"role":"model","parts":[{"text":"visible answer"}]},{"role":"user","parts":[{"text":"next"}]}]}}`)
	out, _, err := prepareAntigravityGeminiReasoningReplayPayload(testContextWithAPIKey("ag-caller-stale"), "gemini-3-flash-agent", cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, payload)
	if err != nil {
		t.Fatal(err)
	}

	parts := gjson.GetBytes(out, "request.contents.1.parts").Array()
	if len(parts) != 2 {
		t.Fatalf("parts length = %d, want 2; body=%s", len(parts), out)
	}
	for i, part := range parts {
		if part.Type == gjson.Null {
			t.Fatalf("parts.%d is null; body=%s", i, out)
		}
	}
	if got := parts[0].Get("text").String(); got != "visible answer" {
		t.Fatalf("text part = %q, want visible answer; body=%s", got, out)
	}
	if got := parts[1].Get("thoughtSignature").String(); got != "stale-thought-sig-ok12" {
		t.Fatalf("thoughtSignature = %q, want stale-thought-sig-ok12; body=%s", got, out)
	}
}

func TestAntigravityReasoningReplayScopeDisablesContentHashWithoutSessionId(t *testing.T) {
	payload := []byte(`{"request":{"contents":[{"role":"user","parts":[{"text":"stable-user-text"}]}]}}`)
	// antigravityReasoningReplayScopeFromPayload itself must NOT do content-hash
	// fallback (the fallback lives in antigravityReasoningReplayScopeFromContentHash
	// so it can be namespaced by caller).
	scope := antigravityReasoningReplayScopeFromPayload("gemini-3-flash-agent", payload)
	if scope.valid() {
		t.Fatalf("payload-only resolver must not do content-hash fallback; got scope %+v", scope)
	}

	// Explicit sessionId without caller API key no longer disables replay
	// (issue #8): the key is now namespaced by a process-level degraded salt
	// so distinct processes still isolate, and the sessionKey itself provides
	// per-session isolation within the process.
	withSession := []byte(`{"sessionId":"shared-session","request":{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}}`)
	noAPIKeyScope := antigravityReasoningReplayScopeFromRequest(context.Background(), "gemini-3-flash-agent", cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, withSession)
	if !noAPIKeyScope.valid() {
		t.Fatalf("sessionId without caller API key must still produce a degraded scope (issue #8): %+v", noAPIKeyScope)
	}
	if !strings.HasPrefix(noAPIKeyScope.sessionKey, "degraded:") || !strings.Contains(noAPIKeyScope.sessionKey, "session:shared-session") {
		t.Fatalf("degraded sessionKey = %q, want degraded:*:session:shared-session", noAPIKeyScope.sessionKey)
	}

	scopeA := antigravityReasoningReplayScopeFromRequest(testContextWithAPIKey("api-a"), "gemini-3-flash-agent", cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, withSession)
	scopeB := antigravityReasoningReplayScopeFromRequest(testContextWithAPIKey("api-b"), "gemini-3-flash-agent", cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, withSession)
	if !scopeA.valid() || !scopeB.valid() {
		t.Fatalf("scopes with API keys must be valid: A=%+v B=%+v", scopeA, scopeB)
	}
	if scopeA.sessionKey == scopeB.sessionKey {
		t.Fatalf("callers must not share session keys: both %q", scopeA.sessionKey)
	}
	if !strings.HasPrefix(scopeA.sessionKey, "caller:") || !strings.Contains(scopeA.sessionKey, "session:shared-session") {
		t.Fatalf("sessionKey A = %q, want caller-isolated session:shared-session", scopeA.sessionKey)
	}

	// Trusted execution scopes remain usable without a caller API key.
	execScope := antigravityReasoningReplayScopeFromRequest(context.Background(), "gemini-3-flash-agent", cliproxyexecutor.Request{}, cliproxyexecutor.Options{
		Metadata: map[string]any{cliproxyexecutor.ExecutionSessionMetadataKey: "trusted-ag"},
	}, []byte(`{"request":{"contents":[]}}`))
	if !execScope.valid() || execScope.sessionKey != "execution:trusted-ag" {
		t.Fatalf("execution scope = %+v, want execution:trusted-ag", execScope)
	}
}

// TestAntigravityReasoningReplayScopeContentHashFallback covers issue #7:
// without an explicit sessionId, derive a session key from the first user
// message and namespace it via IsolateClientControlledSessionKey so tenants
// with the same opening prompt cannot share thought_signature state.
func TestAntigravityReasoningReplayScopeContentHashFallback(t *testing.T) {
	payload := []byte(`{"request":{"contents":[{"role":"user","parts":[{"text":"stable-user-text"}]}]}}`)

	// 1. No apiKey still yields a valid scope (issue #8 degraded namespace).
	scopeNoKey := antigravityReasoningReplayScopeFromRequest(context.Background(), "gemini-3-flash-agent", cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, payload)
	if !scopeNoKey.valid() {
		t.Fatalf("content-hash fallback without apiKey must produce valid scope (issue #7 + #8): %+v", scopeNoKey)
	}
	if !strings.HasPrefix(scopeNoKey.sessionKey, "degraded:") || !strings.Contains(scopeNoKey.sessionKey, "content:") {
		t.Fatalf("degraded content-hash sessionKey = %q, want degraded:*:content:*", scopeNoKey.sessionKey)
	}

	// 2. Distinct apiKeys with the same first user text must not share keys.
	scopeA := antigravityReasoningReplayScopeFromRequest(testContextWithAPIKey("api-a"), "gemini-3-flash-agent", cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, payload)
	scopeB := antigravityReasoningReplayScopeFromRequest(testContextWithAPIKey("api-b"), "gemini-3-flash-agent", cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, payload)
	if !scopeA.valid() || !scopeB.valid() {
		t.Fatalf("content-hash fallback scopes must be valid: A=%+v B=%+v", scopeA, scopeB)
	}
	if scopeA.sessionKey == scopeB.sessionKey {
		t.Fatalf("tenants with same first prompt must not share content-hash sessionKey: both %q", scopeA.sessionKey)
	}

	// 3. Same apiKey + same first user text must be deterministic.
	scopeA2 := antigravityReasoningReplayScopeFromRequest(testContextWithAPIKey("api-a"), "gemini-3-flash-agent", cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, payload)
	if scopeA.sessionKey != scopeA2.sessionKey {
		t.Fatalf("content-hash fallback must be deterministic for same caller: %q vs %q", scopeA.sessionKey, scopeA2.sessionKey)
	}

	// 4. Distinct first user texts under the same apiKey must differ.
	payloadOther := []byte(`{"request":{"contents":[{"role":"user","parts":[{"text":"different-text"}]}]}}`)
	scopeOther := antigravityReasoningReplayScopeFromRequest(testContextWithAPIKey("api-a"), "gemini-3-flash-agent", cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, payloadOther)
	if !scopeOther.valid() {
		t.Fatalf("other payload scope invalid: %+v", scopeOther)
	}
	if scopeA.sessionKey == scopeOther.sessionKey {
		t.Fatalf("distinct first prompts must produce distinct content-hash keys: both %q", scopeA.sessionKey)
	}
}

// TestAntigravityReasoningReplayScopeExecutionMetadataPriority covers issue #9:
// trusted execution metadata sessionId wins over client payload sessionId.
func TestAntigravityReasoningReplayScopeExecutionMetadataPriority(t *testing.T) {
	// Both payload and execution metadata present -> execution must win.
	payload := []byte(`{"sessionId":"client-supplied","request":{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}}`)
	opts := cliproxyexecutor.Options{
		Metadata: map[string]any{cliproxyexecutor.ExecutionSessionMetadataKey: "server-issued"},
	}

	scope := antigravityReasoningReplayScopeFromRequest(testContextWithAPIKey("api-a"), "gemini-3-flash-agent", cliproxyexecutor.Request{}, opts, payload)
	if !scope.valid() {
		t.Fatalf("scope must be valid: %+v", scope)
	}
	// execution: keys are not rewritten by IsolateClientControlledSessionKey.
	if scope.sessionKey != "execution:server-issued" {
		t.Fatalf("sessionKey = %q, want execution:server-issued (execution metadata must win, issue #9)", scope.sessionKey)
	}

	// Same priority applies to req.Metadata.
	reqWithMetadata := cliproxyexecutor.Request{
		Metadata: map[string]any{cliproxyexecutor.ExecutionSessionMetadataKey: "server-issued-2"},
	}
	scope2 := antigravityReasoningReplayScopeFromRequest(testContextWithAPIKey("api-a"), "gemini-3-flash-agent", reqWithMetadata, cliproxyexecutor.Options{}, payload)
	if !scope2.valid() || scope2.sessionKey != "execution:server-issued-2" {
		t.Fatalf("scope2 = %+v, want execution:server-issued-2 (req.Metadata priority, issue #9)", scope2)
	}
}

func TestAntigravityReplayToolCallKeysUsesNativeFunctionCallID(t *testing.T) {
	fc := gjson.Parse(`{"name":"Read","args":{"file_path":"/a"},"id":"id-native"}`)
	keys := antigravityReplayToolCallKeysFromPart(fc)
	if len(keys) != 1 {
		t.Fatalf("keys = %v", keys)
	}
	fc2 := gjson.Parse(`{"name":"Read","args":{"file_path":"/a"},"id":"id-native-2"}`)
	keys2 := antigravityReplayToolCallKeysFromPart(fc2)
	if keys[0] == keys2[0] {
		t.Fatalf("parallel tool calls should not share replay key: %v vs %v", keys, keys2)
	}
}

func TestAntigravityRequestHasMatchingFunctionResponseWhitespaceCallID(t *testing.T) {
	item := gjson.Parse(`{"call_id":" "}`)
	if !antigravityRequestHasMatchingFunctionResponse(nil, item) {
		t.Fatal("whitespace-only call_id should be treated as empty => true")
	}
}
