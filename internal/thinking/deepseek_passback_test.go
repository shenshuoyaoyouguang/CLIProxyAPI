package thinking

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestNormalizeDeepSeekModelID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"deepseek-v4-pro", "deepseek-v4-pro"},
		{"DeepSeek-V4-Pro(high)", "deepseek-v4-pro"},
		{"deepseek/deepseek-v3.1", "deepseek-v3.1"},
		{"  deepseek-reasoner  ", "deepseek-reasoner"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := NormalizeDeepSeekModelID(tc.in); got != tc.want {
			t.Errorf("NormalizeDeepSeekModelID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRequiresDeepSeekReasoningPassback(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"deepseek-v4-pro", true},
		{"deepseek-v4-flash", true},
		{"deepseek-v4-pro(high)", true},
		{"deepseek/deepseek-v4-flash", true},
		{"DeepSeek-V4-Pro", true},
		{"deepseek-v3.1", true},
		{"deepseek/deepseek-v3.1", true},
		{"deepseek-reasoner", true},
		{"deepseek-chat", false},
		{"gpt-5", false},
		{"claude-3-opus", false},
		{"", false},
		// Issue #12: strings.Contains would mismatch these; prefix match must not.
		{"not-deepseek-v4", false},
		{"mydeepseek-v4-pro", false},
		{"deepseek-v4", true}, // prefix match catches bare v4 alias
	}
	for _, tc := range cases {
		if got := RequiresDeepSeekReasoningPassback(tc.model); got != tc.want {
			t.Errorf("RequiresDeepSeekReasoningPassback(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}

func TestIsInactiveReasoningEffort(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"none", true},
		{"NONE", true},
		{"off", true},
		{"disabled", true},
		{"high", false},
		{"auto", false},
		{"max", false},
	}
	for _, tc := range cases {
		if got := IsInactiveReasoningEffort(tc.in); got != tc.want {
			t.Errorf("IsInactiveReasoningEffort(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestDeepSeekThinkingActive(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "effort none inactive",
			body: `{"reasoning_effort":"none","messages":[{"role":"assistant","content":"hi"}]}`,
			want: false,
		},
		{
			name: "thinking disabled hard off with history",
			body: `{"thinking":{"type":"disabled"},"messages":[{"role":"assistant","reasoning_content":"old cot","content":"hi"}]}`,
			want: false,
		},
		{
			name: "thinking enabled",
			body: `{"thinking":{"type":"enabled"},"messages":[{"role":"assistant","content":"hi"}]}`,
			want: true,
		},
		{
			name: "effort high",
			body: `{"reasoning_effort":"high","messages":[{"role":"assistant","content":"hi"}]}`,
			want: true,
		},
		{
			name: "history nonempty reasoning",
			body: `{"messages":[{"role":"assistant","reasoning_content":"plan","content":""}]}`,
			want: true,
		},
		{
			name: "history empty reasoning only",
			body: `{"messages":[{"role":"assistant","reasoning_content":"","content":"hi"}]}`,
			want: false,
		},
		{
			name: "liftable thinking_blocks",
			body: `{"messages":[{"role":"assistant","thinking_blocks":[{"type":"thinking","thinking":"cot"}],"content":""}]}`,
			want: true,
		},
		{
			// P1: inactive effort must hard-off even when history still
			// carries liftable thinking from a prior thinking turn.
			name: "effort none hard off despite liftable history",
			body: `{"reasoning_effort":"none","messages":[{"role":"assistant","thinking_blocks":[{"type":"thinking","thinking":"old cot"}],"content":"hi"}]}`,
			want: false,
		},
		{
			name: "effort off hard off despite nonempty reasoning_content history",
			body: `{"reasoning_effort":"off","messages":[{"role":"assistant","reasoning_content":"old plan","content":"hi"}]}`,
			want: false,
		},
		{
			// Explicit thinking.enabled wins over inactive effort.
			name: "thinking enabled wins over effort none",
			body: `{"thinking":{"type":"enabled"},"reasoning_effort":"none","messages":[{"role":"assistant","content":"hi"}]}`,
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(tc.body)
			msgs := gjson.GetBytes(body, "messages")
			if got := DeepSeekThinkingActive(body, msgs); got != tc.want {
				t.Errorf("DeepSeekThinkingActive = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestShouldReplaceDeepSeekReasoningContent(t *testing.T) {
	missing := gjson.Get(`{}`, "reasoning_content")
	if !ShouldReplaceDeepSeekReasoningContent(missing) {
		t.Error("missing should replace")
	}
	empty := gjson.Get(`{"reasoning_content":""}`, "reasoning_content")
	if !ShouldReplaceDeepSeekReasoningContent(empty) {
		t.Error("empty should replace")
	}
	ws := gjson.Get(`{"reasoning_content":"  "}`, "reasoning_content")
	if !ShouldReplaceDeepSeekReasoningContent(ws) {
		t.Error("whitespace should replace")
	}
	keep := gjson.Get(`{"reasoning_content":"real"}`, "reasoning_content")
	if ShouldReplaceDeepSeekReasoningContent(keep) {
		t.Error("non-empty must not replace")
	}
}

func TestLiftDeepSeekReasoningText(t *testing.T) {
	msg := gjson.Parse(`{"thinking_blocks":[{"type":"thinking","thinking":"from blocks"}],"content":"x"}`)
	got, ok := LiftDeepSeekReasoningText(msg)
	if !ok || got != "from blocks" {
		t.Errorf("thinking_blocks lift = %q ok=%v", got, ok)
	}

	msg2 := gjson.Parse(`{"content":[{"type":"thinking","thinking":"from array"},{"type":"text","text":"hi"}]}`)
	got2, ok2 := LiftDeepSeekReasoningText(msg2)
	if !ok2 || got2 != "from array" {
		t.Errorf("content array lift = %q ok=%v", got2, ok2)
	}

	msg3 := gjson.Parse(`{"content":"plain"}`)
	if _, ok3 := LiftDeepSeekReasoningText(msg3); ok3 {
		t.Error("plain content should not lift")
	}
}

// TestLiftDeepSeekReasoningText_WrappedObject (issue #13) verifies that
// thinking blocks carrying a wrapped object ({"thinking":{"text":"..."}}) are
// parsed via GetThinkingText rather than returning empty from direct .String().
func TestLiftDeepSeekReasoningText_WrappedObject(t *testing.T) {
	msg := gjson.Parse(`{"thinking_blocks":[{"type":"thinking","thinking":{"text":"wrapped cot","cache_control":{"type":"ephemeral"}}}]}`)
	got, ok := LiftDeepSeekReasoningText(msg)
	if !ok || got != "wrapped cot" {
		t.Errorf("wrapped object lift = %q ok=%v, want %q ok=true", got, ok, "wrapped cot")
	}

	// Also verify the content-array variant.
	msg2 := gjson.Parse(`{"content":[{"type":"thinking","thinking":{"text":"array wrapped"}}]}`)
	got2, ok2 := LiftDeepSeekReasoningText(msg2)
	if !ok2 || got2 != "array wrapped" {
		t.Errorf("content array wrapped lift = %q ok=%v, want %q ok=true", got2, ok2, "array wrapped")
	}
}

// TestDeepSeekThinkingActive_InactiveEffortWithMultiToolHistory (issue #3)
// verifies that inactive reasoning_effort still activates the L4 gate when the
// history shows multi-tool reasoning continuity (reasoning_content field present
// + tool calls), so tool-call chains keep their passback.
func TestDeepSeekThinkingActive_InactiveEffortWithMultiToolHistory(t *testing.T) {
	body := `{"reasoning_effort":"none","messages":[
		{"role":"assistant","content":"","reasoning_content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"x","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"ok"},
		{"role":"assistant","content":"done"}
	]}`
	msgs := gjson.Get(body, "messages")
	if !DeepSeekThinkingActive([]byte(body), msgs) {
		t.Error("inactive effort + multi-tool reasoning history should activate gate (issue #3)")
	}
}

// TestDeepSeekThinkingActive_InactiveEffortWithLiftableOnlyNoRC (issue #3 regression guard)
// verifies that inactive effort + liftable thinking_blocks (but NO reasoning_content
// field) does NOT activate the gate — this is the chat-mode pollution guard.
func TestDeepSeekThinkingActive_InactiveEffortWithLiftableOnlyNoRC(t *testing.T) {
	body := `{"reasoning_effort":"none","messages":[
		{"role":"assistant","thinking_blocks":[{"type":"thinking","thinking":"old cot"}],"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"x","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"ok"}
	]}`
	msgs := gjson.Get(body, "messages")
	if DeepSeekThinkingActive([]byte(body), msgs) {
		t.Error("inactive effort + liftable-only history (no rc field) must not activate gate")
	}
}

// TestDeepSeekThinkingActive_EmptyRCWithToolCalls (issue #5) verifies that
// empty-string reasoning_content in history triggers the gate when tool calls
// follow, even without an explicit reasoning_effort in the request.
func TestDeepSeekThinkingActive_EmptyRCWithToolCalls(t *testing.T) {
	body := `{"messages":[
		{"role":"user","content":"hi"},
		{"role":"assistant","content":"","reasoning_content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"x","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"ok"},
		{"role":"assistant","content":"follow-up"}
	]}`
	msgs := gjson.Get(body, "messages")
	if !DeepSeekThinkingActive([]byte(body), msgs) {
		t.Error("empty rc + tool calls should activate gate (issue #5)")
	}
}

// TestDeepSeekThinkingActive_EmptyRCWithoutToolCalls verifies that empty-string
// reasoning_content alone (no tool calls) does NOT activate the gate — this
// preserves the existing chat-mode behavior.
func TestDeepSeekThinkingActive_EmptyRCWithoutToolCalls(t *testing.T) {
	body := `{"messages":[{"role":"assistant","content":"hi","reasoning_content":""}]}`
	msgs := gjson.Get(body, "messages")
	if DeepSeekThinkingActive([]byte(body), msgs) {
		t.Error("empty rc without tool calls must not activate gate")
	}
}
