package claude

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// collectClaudeSSEEvents parses a concatenated Claude SSE byte stream into an
// ordered list of (event, dataPayload) pairs. It is the shared harness for the
// streaming migration regression tests below.
func collectClaudeSSEEvents(t *testing.T, out []byte) []claudeSSEEvent {
	t.Helper()
	var events []claudeSSEEvent
	var currentEvent string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "event: "):
			currentEvent = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			events = append(events, claudeSSEEvent{
				Event: currentEvent,
				Data:  strings.TrimPrefix(line, "data: "),
			})
		}
	}
	return events
}

type claudeSSEEvent struct {
	Event string
	Data  string
}

// assertContentBlockIndexBalance verifies every content_block_start has a
// matching content_block_stop at the same index and that no stop precedes its
// start. This is the invariant the ClaudeSSEBuilder migration must preserve for
// each provider path.
func assertContentBlockIndexBalance(t *testing.T, events []claudeSSEEvent) {
	t.Helper()
	open := map[int64]bool{}
	closedCount := map[int64]int{}
	for _, ev := range events {
		data := gjson.Parse(ev.Data)
		switch data.Get("type").String() {
		case "content_block_start":
			idx := data.Get("index").Int()
			if open[idx] {
				t.Fatalf("index %d opened twice without an intervening stop:\n%v", idx, events)
			}
			open[idx] = true
		case "content_block_stop":
			idx := data.Get("index").Int()
			if !open[idx] {
				t.Fatalf("content_block_stop for index %d that was never open:\n%v", idx, events)
			}
			open[idx] = false
			closedCount[idx]++
			if closedCount[idx] > 1 {
				t.Fatalf("index %d stopped more than once:\n%v", idx, events)
			}
		}
	}
	for idx, stillOpen := range open {
		if stillOpen {
			t.Fatalf("index %d left open at end of stream:\n%v", idx, events)
		}
	}
}

func TestConvertGeminiResponseToClaude_SignatureOnlyPartDoesNotOpenEmptyTextBlock(t *testing.T) {
	requestJSON := []byte(`{"model":"gemini-test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	thinkingChunk := []byte(`{
		"candidates": [{
			"content": {
				"parts": [{"text": "thinking text", "thought": true}]
			}
		}],
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)
	signatureChunk := []byte(`{
		"candidates": [{
			"content": {
				"parts": [{"text": "", "thoughtSignature": "sig-test"}]
			},
			"finishReason": "STOP"
		}],
		"usageMetadata": {
			"promptTokenCount": 10,
			"thoughtsTokenCount": 2,
			"totalTokenCount": 12
		},
		"modelVersion": "gemini-test",
		"responseId": "resp-test"
	}`)

	var param any
	ctx := context.Background()
	output := bytes.Join(ConvertGeminiResponseToClaude(ctx, "gemini-test", requestJSON, requestJSON, thinkingChunk, &param), nil)
	output = append(output, bytes.Join(ConvertGeminiResponseToClaude(ctx, "gemini-test", requestJSON, requestJSON, signatureChunk, &param), nil)...)
	output = append(output, bytes.Join(ConvertGeminiResponseToClaude(ctx, "gemini-test", requestJSON, requestJSON, []byte("[DONE]"), &param), nil)...)
	outputText := string(output)

	if strings.Contains(outputText, `"content_block":{"type":"text"`) {
		t.Fatalf("signature-only part must not open an empty text block: %s", outputText)
	}
	if strings.Contains(outputText, `"type":"content_block_stop","index":1`) {
		t.Fatalf("signature-only part must not produce a stop for unopened index 1: %s", outputText)
	}
	if !strings.Contains(outputText, `"type":"signature_delta"`) || !strings.Contains(outputText, `"signature":"sig-test"`) {
		t.Fatalf("signature-only part must be emitted as a thinking signature delta: %s", outputText)
	}
	if got := strings.Count(outputText, `"type":"content_block_stop","index":0`); got != 1 {
		t.Fatalf("expected exactly one stop for thinking index 0, got %d: %s", got, outputText)
	}
	if !strings.Contains(outputText, `"type":"message_delta"`) || !strings.Contains(outputText, `"output_tokens":2`) {
		t.Fatalf("finish chunk without candidatesTokenCount must still emit final message_delta: %s", outputText)
	}
	if !strings.Contains(outputText, `"type":"message_stop"`) {
		t.Fatalf("DONE chunk must still emit message_stop after final events: %s", outputText)
	}
}

// TestConvertGeminiResponseToClaude_StreamThinkingTextToolBalance drives a full
// thinking -> text -> tool_call stream through the builder migration and asserts
// the event ordering, block-index balance, and terminal events are intact.
func TestConvertGeminiResponseToClaude_StreamThinkingTextToolBalance(t *testing.T) {
	requestJSON := []byte(`{"model":"gemini-test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	chunks := [][]byte{
		[]byte(`{"modelVersion":"gemini-test","responseId":"resp-1","candidates":[{"content":{"parts":[{"text":"pondering","thought":true}]}}]}`),
		[]byte(`{"candidates":[{"content":{"parts":[{"text":"the answer is"}]}}]}`),
		[]byte(`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","args":{"q":"x"}}}]}}]}`),
		[]byte(`{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"thoughtsTokenCount":2}}`),
	}

	var param any
	ctx := context.Background()
	var out []byte
	for _, chunk := range chunks {
		out = append(out, bytes.Join(ConvertGeminiResponseToClaude(ctx, "gemini-test", requestJSON, requestJSON, chunk, &param), nil)...)
	}
	out = append(out, bytes.Join(ConvertGeminiResponseToClaude(ctx, "gemini-test", requestJSON, requestJSON, []byte("[DONE]"), &param), nil)...)

	events := collectClaudeSSEEvents(t, out)
	assertContentBlockIndexBalance(t, events)

	// message_start exactly once and first.
	if len(events) == 0 || events[0].Event != "message_start" {
		t.Fatalf("first event = %q, want message_start:\n%v", events[0].Event, events)
	}
	if got := countClaudeEvent(events, "message_start"); got != 1 {
		t.Fatalf("message_start count = %d, want 1", got)
	}

	// Distinct blocks for thinking (0), text (1), tool_use (2).
	blockTypes := map[int64]string{}
	for _, ev := range events {
		data := gjson.Parse(ev.Data)
		if data.Get("type").String() == "content_block_start" {
			blockTypes[data.Get("index").Int()] = data.Get("content_block.type").String()
		}
	}
	for idx, want := range map[int64]string{0: "thinking", 1: "text", 2: "tool_use"} {
		if blockTypes[idx] != want {
			t.Fatalf("block %d type = %q, want %q:\n%v", idx, blockTypes[idx], want, events)
		}
	}

	// Tool-bearing stream must map to stop_reason=tool_use.
	delta := findClaudeEventData(events, "message_delta")
	if delta == "" {
		t.Fatalf("missing message_delta:\n%v", events)
	}
	if got := gjson.Get(delta, "delta.stop_reason").String(); got != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use", got)
	}
	if got := gjson.Get(delta, "usage.output_tokens").Int(); got != 7 {
		t.Fatalf("output_tokens = %d, want 7 (candidates+thoughts)", got)
	}
	if got := countClaudeEvent(events, "message_stop"); got != 1 {
		t.Fatalf("message_stop count = %d, want 1", got)
	}
}

func countClaudeEvent(events []claudeSSEEvent, event string) int {
	n := 0
	for _, ev := range events {
		if ev.Event == event {
			n++
		}
	}
	return n
}

func findClaudeEventData(events []claudeSSEEvent, event string) string {
	for _, ev := range events {
		if ev.Event == event {
			return ev.Data
		}
	}
	return ""
}
