package openai

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	"github.com/tidwall/gjson"
)

// TestConvertCompletionsRequestToChatCompletions_PromptArray 覆盖 OpenAI Completions API
// 中 prompt 为字符串数组的场景：早期实现仅取 String()，导致数组被丢弃并回落到
// "Complete this:" 兜底提示，旧客户端因此拿到无关响应。
func TestConvertCompletionsRequestToChatCompletions_PromptArray(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "string prompt",
			body: `{"model":"gpt-3.5-turbo-instruct","prompt":"Tell me a joke"}`,
			want: "Tell me a joke",
		},
		{
			name: "single-element array prompt",
			body: `{"model":"gpt-3.5-turbo-instruct","prompt":["Summarize this"]}`,
			want: "Summarize this",
		},
		{
			name: "multi-element array prompt joined by newline",
			body: `{"model":"gpt-3.5-turbo-instruct","prompt":["Line one","Line two"]}`,
			want: "Line one\nLine two",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := convertCompletionsRequestToChatCompletions([]byte(tt.body))
			if got := gjson.GetBytes(out, "messages.0.content").String(); got != tt.want {
				t.Fatalf("messages.0.content = %q, want %q. Output: %s", got, tt.want, out)
			}
		})
	}
}

// TestConvertCompletionsRequestToChatCompletions_DropsEcho 验证 Completions API 独有的
// echo 字段不会被透传到 Chat Completions 上游，避免严格上游（如 z.ai GLM）以 400 拒绝。
func TestConvertCompletionsRequestToChatCompletions_DropsEcho(t *testing.T) {
	body := `{"model":"gpt-3.5-turbo-instruct","prompt":"hello","echo":true}`
	out := convertCompletionsRequestToChatCompletions([]byte(body))
	if gjson.GetBytes(out, "echo").Exists() {
		t.Fatalf("echo should be dropped (no Chat Completions equivalent). Output: %s", out)
	}
}

// TestConvertChatCompletionsStreamChunkToCompletions_EmptyChunkPolicy 覆盖
// /v1/completions 流式响应中空 text chunk 的三种处理策略：
//   - filter  (默认)：丢弃空 chunk（旧行为）
//   - preserve：保留空 chunk，text 为 ""
//   - mark    ：保留空 chunk，并加上 "empty": true 标记
//
// 风险点：旧的过滤行为会改变原始时间线，依赖空 chunk（如 role-only 首 chunk
// 或纯 reasoning chunk）做时序重建的客户端会拿到错位的输出。
func TestConvertChatCompletionsStreamChunkToCompletions_EmptyChunkPolicy(t *testing.T) {
	// role-only 首 chunk：delta 无 content 字段，也无 usage/finish_reason。
	roleOnlyChunk := []byte("{\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}")

	// 带文本的正常 chunk，用作对照。
	textChunk := []byte("{\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}")

	// usage-only chunk 不应被任何策略丢弃（用作时序结束信号）。
	usageChunk := []byte("{\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}")

	tests := []struct {
		name          string
		policy        string
		chunk         []byte
		wantNil       bool
		wantText      string
		wantEmptyFlag bool
	}{
		{
			name:    "filter policy drops role-only empty chunk",
			policy:  config.CompletionsEmptyChunkPolicyFilter,
			chunk:   roleOnlyChunk,
			wantNil: true,
		},
		{
			name:    "default (unknown) policy drops role-only empty chunk",
			policy:  "",
			chunk:   roleOnlyChunk,
			wantNil: true,
		},
		{
			name:     "preserve policy keeps role-only empty chunk with empty text",
			policy:   config.CompletionsEmptyChunkPolicyPreserve,
			chunk:    roleOnlyChunk,
			wantText: "",
		},
		{
			name:          "mark policy keeps role-only empty chunk and adds empty flag",
			policy:        config.CompletionsEmptyChunkPolicyMark,
			chunk:         roleOnlyChunk,
			wantText:      "",
			wantEmptyFlag: true,
		},
		{
			name:     "filter policy keeps normal text chunk",
			policy:   config.CompletionsEmptyChunkPolicyFilter,
			chunk:    textChunk,
			wantText: "hi",
		},
		{
			name:     "preserve policy keeps normal text chunk",
			policy:   config.CompletionsEmptyChunkPolicyPreserve,
			chunk:    textChunk,
			wantText: "hi",
		},
		{
			name:     "mark policy keeps normal text chunk without empty flag",
			policy:   config.CompletionsEmptyChunkPolicyMark,
			chunk:    textChunk,
			wantText: "hi",
		},
		{
			name:     "filter policy keeps usage-only chunk (timeline terminator)",
			policy:   config.CompletionsEmptyChunkPolicyFilter,
			chunk:    usageChunk,
			wantText: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := convertChatCompletionsStreamChunkToCompletions(tt.chunk, tt.policy)
			if tt.wantNil {
				if out != nil {
					t.Fatalf("expected nil output for policy=%q, got %s", tt.policy, out)
				}
				return
			}
			if out == nil {
				t.Fatalf("expected non-nil output for policy=%q, got nil", tt.policy)
			}
			// 校验 choices.0.text 字段。
			text := gjson.GetBytes(out, "choices.0.text").String()
			if text != tt.wantText {
				t.Fatalf("choices.0.text = %q, want %q. Output: %s", text, tt.wantText, out)
			}
			// 校验 empty 标记字段。
			emptyFlag := gjson.GetBytes(out, "choices.0.empty").Bool()
			if emptyFlag != tt.wantEmptyFlag {
				t.Fatalf("choices.0.empty = %v, want %v. Output: %s", emptyFlag, tt.wantEmptyFlag, out)
			}
		})
	}
}

// TestConvertChatCompletionsStreamChunkToCompletions_TimelinePreservation 验证
// preserve/mark 策略保留空 chunk 的时序：连续的 [empty, text, empty] 输入
// 应在输出中保持同样的次序，而 filter 策略会丢弃首尾空 chunk。
func TestConvertChatCompletionsStreamChunkToCompletions_TimelinePreservation(t *testing.T) {
	emptyChunk := []byte("{\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}")
	textChunk := []byte("{\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"},\"finish_reason\":null}]}")

	sequence := [][]byte{emptyChunk, textChunk, emptyChunk}

	// filter 策略：只剩中间 text chunk。
	var filterOut [][]byte
	for _, ch := range sequence {
		if out := convertChatCompletionsStreamChunkToCompletions(ch, config.CompletionsEmptyChunkPolicyFilter); out != nil {
			filterOut = append(filterOut, out)
		}
	}
	if len(filterOut) != 1 {
		t.Fatalf("filter policy should drop both empty chunks, got %d outputs", len(filterOut))
	}

	// preserve 策略：3 个全部保留。
	var preserveOut [][]byte
	for _, ch := range sequence {
		if out := convertChatCompletionsStreamChunkToCompletions(ch, config.CompletionsEmptyChunkPolicyPreserve); out != nil {
			preserveOut = append(preserveOut, out)
		}
	}
	if len(preserveOut) != 3 {
		t.Fatalf("preserve policy should keep all 3 chunks for timeline fidelity, got %d", len(preserveOut))
	}

	// preserve 策略下，第二个（中间）chunk 必须仍带文本 "x"，证明顺序未被错位。
	midText := gjson.GetBytes(preserveOut[1], "choices.0.text").String()
	if midText != "x" {
		t.Fatalf("preserve policy must keep chunk order; middle text = %q, want %q", midText, "x")
	}

	// mark 策略：3 个全部保留，且首尾 chunk 带 empty:true，中间不带。
	var markOut [][]byte
	for _, ch := range sequence {
		if out := convertChatCompletionsStreamChunkToCompletions(ch, config.CompletionsEmptyChunkPolicyMark); out != nil {
			markOut = append(markOut, out)
		}
	}
	if len(markOut) != 3 {
		t.Fatalf("mark policy should keep all 3 chunks, got %d", len(markOut))
	}
	if !gjson.GetBytes(markOut[0], "choices.0.empty").Bool() {
		t.Fatalf("mark policy: first empty chunk should carry empty=true")
	}
	if !gjson.GetBytes(markOut[2], "choices.0.empty").Bool() {
		t.Fatalf("mark policy: last empty chunk should carry empty=true")
	}
	if gjson.GetBytes(markOut[1], "choices.0.empty").Exists() {
		t.Fatalf("mark policy: non-empty text chunk must not carry empty flag")
	}
}
