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
