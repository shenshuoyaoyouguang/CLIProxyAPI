package helps

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestPrepareOpenAICompatChatBody_SkipPassback(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro","thinking":{"type":"enabled"},"messages":[{"role":"assistant","content":"hi"}]}`)
	out, err := PrepareOpenAICompatChatBody(OpenAICompatChatPrepareInput{
		Body:          body,
		ModelForApply: "deepseek-v4-pro",
		BaseModel:     "deepseek-v4-pro",
		FromFormat:    "openai",
		WireToFormat:  "openai",
		ProviderKey:   "opencode",
		SkipPassback:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(out, "messages.0.reasoning_content").Exists() {
		t.Error("SkipPassback=true must not inject reasoning_content")
	}
}

func TestPrepareOpenAICompatChatBody_MiddleBeforePassback(t *testing.T) {
	// D7 order: Middle stamps a marker; passback then fills empty RC.
	body := []byte(`{"model":"deepseek-v4-pro","thinking":{"type":"enabled"},"messages":[{"role":"assistant","content":"hi"}]}`)
	out, err := PrepareOpenAICompatChatBody(OpenAICompatChatPrepareInput{
		Body:          body,
		ModelForApply: "deepseek-v4-pro",
		BaseModel:     "deepseek-v4-pro",
		FromFormat:    "openai",
		WireToFormat:  "openai",
		ProviderKey:   "opencode",
		SkipPassback:  false,
		Middle: func(b []byte) []byte {
			updated, errSet := sjson.SetBytes(b, "x_middle_ran", true)
			if errSet != nil {
				t.Fatalf("middle set: %v", errSet)
			}
			return updated
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !gjson.GetBytes(out, "x_middle_ran").Bool() {
		t.Error("Middle did not run")
	}
	if !gjson.GetBytes(out, "messages.0.reasoning_content").Exists() {
		t.Error("passback should run after Middle and inject reasoning_content")
	}
}

func TestPrepareOpenAICompatChatBody_NonDeepSeekNoPassback(t *testing.T) {
	body := []byte(`{"model":"gpt-5","messages":[{"role":"assistant","content":"hi"}]}`)
	out, err := PrepareOpenAICompatChatBody(OpenAICompatChatPrepareInput{
		Body:          body,
		ModelForApply: "gpt-5",
		BaseModel:     "gpt-5",
		FromFormat:    "openai",
		WireToFormat:  "openai",
		ProviderKey:   "openrouter",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(out, "messages.0.reasoning_content").Exists() {
		t.Error("non-DeepSeek must not inject reasoning_content")
	}
}

func TestOpenAICompatThinkingRoute_DeepSeek(t *testing.T) {
	to, provider := openAICompatThinkingRoute("deepseek-v4-flash", "openai", "opencode")
	if to != "deepseek" || provider != "deepseek" {
		t.Errorf("route = %q/%q, want deepseek/deepseek", to, provider)
	}
	to, provider = openAICompatThinkingRoute("gpt-5", "openai", "opencode")
	if to != "openai" || provider != "opencode" {
		t.Errorf("route = %q/%q, want openai/opencode", to, provider)
	}
}

func TestCountOpenAIChatTokens_IncludesReasoningContent(t *testing.T) {
	enc, err := TokenizerForModel("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	base := []byte(`{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"ok"}]}`)
	withRC := []byte(`{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"ok","reasoning_content":"` + strings.Repeat("chain-of-thought ", 40) + `"}]}`)
	emptyRC := []byte(`{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"ok","reasoning_content":""}]}`)

	nBase, err := CountOpenAIChatTokens(enc, base)
	if err != nil {
		t.Fatal(err)
	}
	nRC, err := CountOpenAIChatTokens(enc, withRC)
	if err != nil {
		t.Fatal(err)
	}
	nEmpty, err := CountOpenAIChatTokens(enc, emptyRC)
	if err != nil {
		t.Fatal(err)
	}
	if nRC <= nBase {
		t.Errorf("long RC must increase count: base=%d withRC=%d", nBase, nRC)
	}
	if nEmpty != nBase {
		t.Errorf("empty RC must not change count: base=%d empty=%d", nBase, nEmpty)
	}
}

// AC10: Prepare lifts assistant thinking_blocks → reasoning_content for DeepSeek
// multi-turn (user then assistant). Locate assistant by role, not messages index.
func TestPrepareOpenAICompatChatBody_AC10_DeepSeekLiftFromThinkingBlocks(t *testing.T) {
	body := []byte(`{
		"model": "deepseek-v4-flash",
		"thinking": {"type": "enabled"},
		"messages": [
			{"role": "user", "content": "Weather?"},
			{"role": "assistant", "content": "", "thinking_blocks": [{"type": "thinking", "thinking": "Call get_weather for Hangzhou."}], "tool_calls": [{"id": "c1", "type": "function", "function": {"name": "get_weather", "arguments": "{}"}}]}
		]
	}`)
	out, err := PrepareOpenAICompatChatBody(OpenAICompatChatPrepareInput{
		Body:          body,
		ModelForApply: "deepseek-v4-flash",
		BaseModel:     "deepseek-v4-flash",
		FromFormat:    "openai",
		WireToFormat:  "openai",
		ProviderKey:   "opencode",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got string
	found := false
	for _, m := range gjson.GetBytes(out, "messages").Array() {
		if m.Get("role").String() != "assistant" {
			continue
		}
		found = true
		got = m.Get("reasoning_content").String()
		break
	}
	if !found {
		t.Fatal("no assistant message in prepared body")
	}
	if got != "Call get_weather for Hangzhou." {
		t.Fatalf("assistant reasoning_content = %q, want lifted CoT", got)
	}
}

// AC10: existing non-empty reasoning_content is unchanged through Prepare.
func TestPrepareOpenAICompatChatBody_AC10_NonEmptyReasoningContentStable(t *testing.T) {
	const want = "real chain-of-thought from round 1"
	body := []byte(`{
		"model": "deepseek-v4-flash",
		"thinking": {"type": "enabled"},
		"messages": [
			{"role": "user", "content": "Weather?"},
			{"role": "assistant", "content": "", "reasoning_content": "real chain-of-thought from round 1", "tool_calls": [{"id": "c1", "type": "function", "function": {"name": "get_weather", "arguments": "{}"}}]},
			{"role": "tool", "tool_call_id": "c1", "content": "sunny"}
		]
	}`)
	out, err := PrepareOpenAICompatChatBody(OpenAICompatChatPrepareInput{
		Body:          body,
		ModelForApply: "deepseek-v4-flash",
		BaseModel:     "deepseek-v4-flash",
		FromFormat:    "openai",
		WireToFormat:  "openai",
		ProviderKey:   "opencode",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for _, m := range gjson.GetBytes(out, "messages").Array() {
		if m.Get("role").String() == "assistant" {
			got = m.Get("reasoning_content").String()
			break
		}
	}
	if got != want {
		t.Fatalf("assistant reasoning_content = %q, want stable %q", got, want)
	}
}

// AC10: non-DeepSeek openai-compat model — messages[] shape byte-stable
// (no reasoning_content injection; no role/content rewrite).
func TestPrepareOpenAICompatChatBody_AC10_NonDeepSeekMessagesStable(t *testing.T) {
	body := []byte(`{
		"model": "gpt-5",
		"messages": [
			{"role": "user", "content": "hi"},
			{"role": "assistant", "content": "hello", "tool_calls": [{"id": "c1", "type": "function", "function": {"name": "x", "arguments": "{}"}}]},
			{"role": "tool", "tool_call_id": "c1", "content": "ok"}
		]
	}`)
	wantMsgs := gjson.GetBytes(body, "messages").Raw
	out, err := PrepareOpenAICompatChatBody(OpenAICompatChatPrepareInput{
		Body:          body,
		ModelForApply: "gpt-5",
		BaseModel:     "gpt-5",
		FromFormat:    "openai",
		WireToFormat:  "openai",
		ProviderKey:   "openrouter",
	})
	if err != nil {
		t.Fatal(err)
	}
	gotMsgs := gjson.GetBytes(out, "messages").Raw
	if gotMsgs != wantMsgs {
		t.Fatalf("messages not stable for non-DeepSeek:\nwant %s\ngot  %s", wantMsgs, gotMsgs)
	}
	for _, m := range gjson.GetBytes(out, "messages").Array() {
		if m.Get("reasoning_content").Exists() {
			t.Fatalf("non-DeepSeek must not inject reasoning_content; message=%s", m.Raw)
		}
	}
}
