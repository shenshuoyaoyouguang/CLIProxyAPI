package thinking

import (
	"fmt"
	"testing"

	"github.com/tidwall/gjson"
)

func TestEnsureMultiTurnReasoningPassback_NonDeepSeekModel(t *testing.T) {
	body := []byte(`{"model":"gpt-5","messages":[{"role":"assistant","content":"hello","tool_calls":[{"id":"call_1","type":"function","function":{"name":"test","arguments":"{}"}}]}]}`)
	original := string(body)
	result := EnsureMultiTurnReasoningPassback(body, "gpt-5")
	if string(result) != original {
		t.Errorf("non-DeepSeek model: body was modified")
	}
}

func TestEnsureMultiTurnReasoningPassback_V4WithToolCalls(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","reasoning_effort":"high","messages":[
		{"role":"user","content":"What's the weather?"},
		{"role":"assistant","content":"","reasoning_content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"call_1","content":"sunny"},
		{"role":"assistant","content":"","tool_calls":[{"id":"call_2","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"NYC\"}"}}]}
	]}`)

	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")

	if m1 := gjson.GetBytes(result, "messages.1.reasoning_content").String(); m1 != "" {
		t.Errorf("messages[1].reasoning_content = %q, want empty", m1)
	}
	if !gjson.GetBytes(result, "messages.3.reasoning_content").Exists() {
		t.Error("messages[3].reasoning_content missing")
	}
	if val := gjson.GetBytes(result, "messages.3.reasoning_content").String(); val != "" {
		t.Errorf("messages[3].reasoning_content = %q, want empty", val)
	}
}

func TestEnsureMultiTurnReasoningPassback_V4WithoutToolCalls(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","reasoning_effort":"high","messages":[
		{"role":"user","content":"Hello"},
		{"role":"assistant","content":"Hi there!"},
		{"role":"user","content":"What's 2+2?"},
		{"role":"assistant","content":"4"}
	]}`)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")
	for _, idx := range []int{1, 3} {
		path := gjson.GetBytes(result, fmt.Sprintf("messages.%d.reasoning_content", idx))
		if !path.Exists() {
			t.Errorf("messages[%d].reasoning_content missing", idx)
		}
	}
}

func TestEnsureMultiTurnReasoningPassback_Idempotent(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","thinking":{"type":"enabled"},"messages":[
		{"role":"user","content":"Hi"},
		{"role":"assistant","content":"Hello","reasoning_content":"thinking...","tool_calls":[{"id":"c1","type":"function","function":{"name":"test","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"result"},
		{"role":"assistant","content":"Done","reasoning_content":"all done"}
	]}`)
	original := string(body)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")
	if string(result) != original {
		t.Errorf("idempotent: body modified")
	}
}

func TestEnsureMultiTurnReasoningPassback_EmptyBody(t *testing.T) {
	if result := EnsureMultiTurnReasoningPassback(nil, "deepseek-v4-pro"); result != nil {
		t.Errorf("nil body: got %s", result)
	}
	invalid := []byte(`{not json}`)
	if string(EnsureMultiTurnReasoningPassback(invalid, "deepseek-v4-pro")) != string(invalid) {
		t.Error("invalid JSON modified")
	}
	noMsgs := []byte(`{"model":"deepseek-v4-pro","temperature":0.7}`)
	if string(EnsureMultiTurnReasoningPassback(noMsgs, "deepseek-v4-pro")) != string(noMsgs) {
		t.Error("no messages modified")
	}
}

func TestEnsureMultiTurnReasoningPassback_NonV4DeepSeek(t *testing.T) {
	body := []byte(`{"model":"deepseek-chat","thinking":{"type":"enabled"},"messages":[{"role":"assistant","content":"hello"}]}`)
	if string(EnsureMultiTurnReasoningPassback(body, "deepseek-chat")) != string(body) {
		t.Error("deepseek-chat must not be modified")
	}
}

func TestEnsureMultiTurnReasoningPassback_DeprecatedReasoner(t *testing.T) {
	// deepseek-reasoner is removed; must not receive passback.
	body := []byte(`{"model":"deepseek-reasoner","reasoning_effort":"high","messages":[
			{"role":"assistant","content":"thinking...","tool_calls":[{"id":"c1","type":"function","function":{"name":"test","arguments":"{}"}}]}
		]}`)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-reasoner")
	if gjson.GetBytes(result, "messages.0.reasoning_content").Exists() {
		t.Error("deepseek-reasoner must not receive passback after removal")
	}
}

func TestEnsureMultiTurnReasoningPassback_ModelSuffix(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","reasoning_effort":"high","messages":[{"role":"assistant","content":"hello"}]}`)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro(high)")
	if !gjson.GetBytes(result, "messages.0.reasoning_content").Exists() {
		t.Error("suffix model: not back-filled")
	}
}

func TestEnsureMultiTurnReasoningPassback_ThinkingDisabled_NoInjection(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","messages":[
		{"role":"user","content":"Hi"},
		{"role":"assistant","content":"Hello"}
	]}`)
	if string(EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")) != string(body) {
		t.Error("no flag + no history: should not inject")
	}
}

func TestEnsureMultiTurnReasoningPassback_ThinkingTypeDisabled_NoInjection(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","thinking":{"type":"disabled"},"messages":[
		{"role":"assistant","content":"hello","tool_calls":[{"id":"c1","type":"function","function":{"name":"test","arguments":"{}"}}]}
	]}`)
	if string(EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")) != string(body) {
		t.Error("thinking.type=disabled: should not inject")
	}
}

func TestEnsureMultiTurnReasoningPassback_ReasoningEffortNone_NoInjection(t *testing.T) {
	// P1-1: OpenAI applier writes "none" for ModeNone; must not activate gate.
	body := []byte(`{"model":"deepseek-v4-pro","reasoning_effort":"none","messages":[
		{"role":"assistant","content":"hello"}
	]}`)
	if string(EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")) != string(body) {
		t.Error(`reasoning_effort:"none" must not inject`)
	}
}

func TestEnsureMultiTurnReasoningPassback_ReasoningEffortNone_WithHistory_NoInjection(t *testing.T) {
	// P1: inactive effort is hard-off; history with liftable CoT must not re-open the gate.
	body := []byte(`{"model":"deepseek-v4-pro","reasoning_effort":"none","messages":[
		{"role":"assistant","content":"","thinking_blocks":[{"type":"thinking","thinking":"old cot"}],"tool_calls":[{"id":"c1","type":"function","function":{"name":"x","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"ok"},
		{"role":"assistant","content":"done"}
	]}`)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")
	if string(result) != string(body) {
		t.Error(`reasoning_effort:"none" with liftable history must not inject or rewrite`)
	}
}

func TestEnsureMultiTurnReasoningPassback_ThinkingEnabledWinsOverEffortNone(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","thinking":{"type":"enabled"},"reasoning_effort":"none","messages":[
		{"role":"assistant","content":"hello"}
	]}`)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")
	if !gjson.GetBytes(result, "messages.0.reasoning_content").Exists() {
		t.Error("thinking.type=enabled must still passback even when effort is none")
	}
}
func TestEnsureMultiTurnReasoningPassback_DisabledHardOffWithHistory(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","thinking":{"type":"disabled"},"messages":[
		{"role":"assistant","content":"","reasoning_content":"old cot","tool_calls":[{"id":"c1","type":"function","function":{"name":"x","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"ok"},
		{"role":"assistant","content":"done"}
	]}`)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")
	if gjson.GetBytes(result, "messages.2.reasoning_content").Exists() {
		t.Error("hard-off must not placeholder later turns")
	}
	if got := gjson.GetBytes(result, "messages.0.reasoning_content").String(); got != "old cot" {
		t.Errorf("existing rc mutated: %q", got)
	}
}

func TestEnsureMultiTurnReasoningPassback_HistorySignalTriggersInjection(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","messages":[
		{"role":"user","content":"What's the weather?"},
		{"role":"assistant","content":"","reasoning_content":"I should call get_weather.","tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"sunny"},
		{"role":"assistant","content":"","tool_calls":[{"id":"c2","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"NYC\"}"}}]}
	]}`)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")
	if m1 := gjson.GetBytes(result, "messages.1.reasoning_content").String(); m1 != "I should call get_weather." {
		t.Errorf("messages[1] = %q", m1)
	}
	if !gjson.GetBytes(result, "messages.3.reasoning_content").Exists() {
		t.Error("history gate should fill messages[3]")
	}
}

func TestEnsureMultiTurnReasoningPassback_DoesNotOverwriteNonEmpty(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","reasoning_effort":"high","messages":[
		{"role":"assistant","content":"","reasoning_content":"real chain-of-thought from round 1","tool_calls":[{"id":"c1","type":"function","function":{"name":"test","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"result"},
		{"role":"assistant","content":"done","reasoning_content":"second turn reasoning"}
	]}`)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")
	if rc := gjson.GetBytes(result, "messages.0.reasoning_content").String(); rc != "real chain-of-thought from round 1" {
		t.Errorf("messages[0] = %q", rc)
	}
	if rc := gjson.GetBytes(result, "messages.2.reasoning_content").String(); rc != "second turn reasoning" {
		t.Errorf("messages[2] = %q", rc)
	}
}

func TestEnsureMultiTurnReasoningPassback_LiftFromThinkingBlocks(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","thinking":{"type":"enabled"},"messages":[
		{"role":"user","content":"Weather?"},
		{"role":"assistant","content":"","thinking_blocks":[{"type":"thinking","thinking":"Call get_weather for Hangzhou."}],"tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]}
	]}`)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")
	if got := gjson.GetBytes(result, "messages.1.reasoning_content").String(); got != "Call get_weather for Hangzhou." {
		t.Errorf("lifted = %q", got)
	}
}

func TestEnsureMultiTurnReasoningPassback_LiftFromContentThinkingArray(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","reasoning_effort":"high","messages":[
		{"role":"assistant","content":[
			{"type":"thinking","thinking":"Plan A then tool."},
			{"type":"text","text":"checking"}
		],"tool_calls":[{"id":"c1","type":"function","function":{"name":"x","arguments":"{}"}}]}
	]}`)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")
	if got := gjson.GetBytes(result, "messages.0.reasoning_content").String(); got != "Plan A then tool." {
		t.Errorf("lifted = %q", got)
	}
}

func TestEnsureMultiTurnReasoningPassback_LiftOverEmptyReasoningContent(t *testing.T) {
	// P1-3: empty string must not block real CoT lift.
	body := []byte(`{"model":"deepseek-v4-pro","thinking":{"type":"enabled"},"messages":[
		{"role":"assistant","content":"","reasoning_content":"","thinking_blocks":[{"type":"thinking","thinking":"real CoT"}],"tool_calls":[{"id":"c1","type":"function","function":{"name":"x","arguments":"{}"}}]}
	]}`)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")
	if got := gjson.GetBytes(result, "messages.0.reasoning_content").String(); got != "real CoT" {
		t.Errorf("empty rc must be replaced by lift, got %q", got)
	}
}

func TestEnsureMultiTurnReasoningPassback_LiftActivatesGateWithoutRequestFlag(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","messages":[
		{"role":"assistant","content":"","thinking_blocks":[{"type":"thinking","thinking":"first CoT"}],"tool_calls":[{"id":"c1","type":"function","function":{"name":"x","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"ok"},
		{"role":"assistant","content":"done"}
	]}`)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")
	if got := gjson.GetBytes(result, "messages.0.reasoning_content").String(); got != "first CoT" {
		t.Errorf("messages[0] = %q", got)
	}
	if !gjson.GetBytes(result, "messages.2.reasoning_content").Exists() {
		t.Fatal("messages[2] should get placeholder after lift-activated gate")
	}
}

func TestEnsureMultiTurnReasoningPassback_V31Model(t *testing.T) {
	// deepseek-v3.1 is removed; must not receive passback.
	body := []byte(`{"model":"deepseek-v3.1","reasoning_effort":"high","messages":[
			{"role":"assistant","content":"hello"}
		]}`)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v3.1")
	if gjson.GetBytes(result, "messages.0.reasoning_content").Exists() {
		t.Error("deepseek-v3.1 must not receive passback after removal")
	}
}

func TestEnsureMultiTurnReasoningPassback_V31Lift(t *testing.T) {
	// deepseek-v3.1 is removed; must not receive passback.
	body := []byte(`{"model":"deepseek-v3.1","thinking":{"type":"enabled"},"messages":[
			{"role":"assistant","thinking_blocks":[{"type":"thinking","thinking":"v3.1 cot"}],"content":""}
		]}`)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v3.1")
	if gjson.GetBytes(result, "messages.0.reasoning_content").Exists() {
		t.Error("deepseek-v3.1 must not receive passback after removal")
	}
}

// TestEnsureMultiTurnReasoningPassback_InactiveEffortMultiToolPassback (issue #3)
// verifies that inactive reasoning_effort still injects the reasoning_content
// placeholder when history shows multi-tool reasoning continuity (an assistant
// turn already carried reasoning_content + tool calls).
func TestEnsureMultiTurnReasoningPassback_InactiveEffortMultiToolPassback(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","reasoning_effort":"none","messages":[
		{"role":"user","content":"What's the weather?"},
		{"role":"assistant","content":"","reasoning_content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"sunny"},
		{"role":"assistant","content":"","tool_calls":[{"id":"c2","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"NYC\"}"}}]}
	]}`)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")
	if !gjson.GetBytes(result, "messages.3.reasoning_content").Exists() {
		t.Error("issue #3: inactive effort + multi-tool history should still placeholder messages[3]")
	}
}

// TestEnsureMultiTurnReasoningPassback_EmptyRCWithToolCalls (issue #5) verifies
// that empty-string reasoning_content in history triggers the gate for
// subsequent tool-call turns even without an explicit reasoning_effort.
func TestEnsureMultiTurnReasoningPassback_EmptyRCWithToolCalls(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","messages":[
		{"role":"user","content":"hi"},
		{"role":"assistant","content":"","reasoning_content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"x","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"ok"},
		{"role":"assistant","content":"follow-up"}
	]}`)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")
	if !gjson.GetBytes(result, "messages.3.reasoning_content").Exists() {
		t.Error("issue #5: empty rc + tool calls should trigger gate for later turns")
	}
}

// TestEnsureMultiTurnReasoningPassback_LiftWrappedThinkingObject (issue #13)
// verifies that wrapped object thinking structures are lifted via GetThinkingText.
func TestEnsureMultiTurnReasoningPassback_LiftWrappedThinkingObject(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","thinking":{"type":"enabled"},"messages":[
		{"role":"assistant","content":"","thinking_blocks":[{"type":"thinking","thinking":{"text":"wrapped cot","cache_control":{"type":"ephemeral"}}}]}
	]}`)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")
	if got := gjson.GetBytes(result, "messages.0.reasoning_content").String(); got != "wrapped cot" {
		t.Errorf("issue #13: wrapped object lift = %q, want %q", got, "wrapped cot")
	}
}

// TestEnsureMultiTurnReasoningPassback_NullReasoningContentStandardized (issue #4)
// verifies that JSON null reasoning_content is standardized to "" instead of
// being left as null in the payload (which would yield HTTP 400 from DeepSeek).
func TestEnsureMultiTurnReasoningPassback_NullReasoningContentStandardized(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","reasoning_effort":"high","messages":[
		{"role":"assistant","content":"hello","reasoning_content":null},
		{"role":"assistant","content":"world","reasoning_content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"x","arguments":"{}"}}]}
	]}`)
	result := EnsureMultiTurnReasoningPassback(body, "deepseek-v4-pro")

	// Both null entries must be standardized to empty string, not left as null.
	rc0 := gjson.GetBytes(result, "messages.0.reasoning_content")
	if !rc0.Exists() {
		t.Error("issue #4: messages[0].reasoning_content missing after standardization")
	}
	if rc0.Type == gjson.Null {
		t.Error("issue #4: messages[0].reasoning_content still null, want empty string")
	}
	if rc0.String() != "" {
		t.Errorf("issue #4: messages[0].reasoning_content = %q, want empty string", rc0.String())
	}

	rc1 := gjson.GetBytes(result, "messages.1.reasoning_content")
	if !rc1.Exists() {
		t.Error("issue #4: messages[1].reasoning_content missing after standardization")
	}
	if rc1.Type == gjson.Null {
		t.Error("issue #4: messages[1].reasoning_content still null, want empty string")
	}
	if rc1.String() != "" {
		t.Errorf("issue #4: messages[1].reasoning_content = %q, want empty string", rc1.String())
	}
}
