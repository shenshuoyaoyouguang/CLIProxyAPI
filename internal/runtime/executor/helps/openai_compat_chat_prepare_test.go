package helps

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// normalizedJSON unmarshals JSON into a shape-comparable value (order-independent
// for object keys), so fixture equality is robust to whitespace and key ordering.
func normalizedJSON(t *testing.T, data string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		t.Fatalf("invalid JSON fixture: %v\n%s", err, data)
	}
	return v
}

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

// AC10: DeepSeek lift path (thinking_blocks -> reasoning_content) produces a
// byte-shaped output equal to a known expected-output fixture, not just a
// single-field assertion. This locks the full messages[] transform so a future
// change to lift/placeholder ordering or extra field mutation is caught.
func TestPrepareOpenAICompatChatBody_AC10_DeepSeekLiftFixtureEquality(t *testing.T) {
	body := []byte(`{
		"model": "deepseek-v4-flash",
		"thinking": {"type": "enabled"},
		"messages": [
			{"role": "user", "content": "Weather?"},
			{"role": "assistant", "content": "", "thinking_blocks": [{"type": "thinking", "thinking": "Call get_weather for Hangzhou."}], "tool_calls": [{"id": "c1", "type": "function", "function": {"name": "get_weather", "arguments": "{}"}}]},
			{"role": "tool", "tool_call_id": "c1", "content": "sunny"}
		]
	}`)
	// Expected messages[] after Prepare: user unchanged; assistant keeps its
	// thinking_blocks/tool_calls and gains reasoning_content lifted from the
	// thinking block; tool message unchanged.
	wantMessages := `[
		{"role": "user", "content": "Weather?"},
		{"role": "assistant", "content": "", "thinking_blocks": [{"type": "thinking", "thinking": "Call get_weather for Hangzhou."}], "tool_calls": [{"id": "c1", "type": "function", "function": {"name": "get_weather", "arguments": "{}"}}], "reasoning_content": "Call get_weather for Hangzhou."},
		{"role": "tool", "tool_call_id": "c1", "content": "sunny"}
	]`
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
	got := gjson.GetBytes(out, "messages").Raw
	if !reflect.DeepEqual(normalizedJSON(t, got), normalizedJSON(t, wantMessages)) {
		t.Fatalf("DeepSeek lift messages[] mismatch:\nwant %s\ngot  %s", wantMessages, got)
	}
}

// CountTokens shares the thinking+passback prepare with Execute but passes
// Middle=nil, so PayloadConfig side effects (e.g. an override rule) must NOT
// appear in the CountTokens body. This is the documented residual: CountTokens
// does not count exactly what Execute sends. The test drives the real
// ApplyPayloadConfigWithRequest to prove the divergence is Middle-gated.
func TestPrepareOpenAICompatChatBody_CountTokensOmitsPayloadConfig(t *testing.T) {
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Override: []config.PayloadRule{
				{
					Models: []config.PayloadModelRule{{Name: "deepseek-v4-flash"}},
					Params: map[string]any{"top_p": 0.5},
				},
			},
		},
	}
	body := []byte(`{"model":"deepseek-v4-flash","thinking":{"type":"enabled"},"messages":[{"role":"assistant","content":"hi"}]}`)
	base := OpenAICompatChatPrepareInput{
		Body:          body,
		ModelForApply: "deepseek-v4-flash",
		BaseModel:     "deepseek-v4-flash",
		FromFormat:    "openai",
		WireToFormat:  "openai",
		ProviderKey:   "opencode",
	}

	// Execute-like: PayloadConfig runs as Middle.
	executeInput := base
	executeInput.Middle = func(b []byte) []byte {
		return ApplyPayloadConfigWithRequest(cfg, "deepseek-v4-flash", "openai", "openai", "", b, body, "deepseek-v4-flash", "", nil)
	}
	executeOut, err := PrepareOpenAICompatChatBody(executeInput)
	if err != nil {
		t.Fatal(err)
	}

	// CountTokens-like: Middle omitted.
	countInput := base
	countInput.Middle = nil
	countOut, err := PrepareOpenAICompatChatBody(countInput)
	if err != nil {
		t.Fatal(err)
	}

	// The override must reach the Execute body but not the CountTokens body.
	if got := gjson.GetBytes(executeOut, "top_p"); !got.Exists() {
		t.Fatalf("Execute body must carry PayloadConfig override top_p; body=%s", executeOut)
	} else if got.Num != 0.5 {
		t.Fatalf("Execute body top_p = %v, want 0.5", got.Num)
	}
	if gjson.GetBytes(countOut, "top_p").Exists() {
		t.Fatalf("CountTokens body must NOT carry PayloadConfig override top_p; body=%s", countOut)
	}

	// Both paths must still share the thinking+passback result (reasoning_content
	// back-fill), so the divergence is strictly PayloadConfig and nothing else.
	if !gjson.GetBytes(executeOut, "messages.0.reasoning_content").Exists() {
		t.Error("Execute body missing passback reasoning_content")
	}
	if !gjson.GetBytes(countOut, "messages.0.reasoning_content").Exists() {
		t.Error("CountTokens body missing passback reasoning_content")
	}
}
