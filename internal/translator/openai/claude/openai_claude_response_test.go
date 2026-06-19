package claude

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
	"google.golang.org/protobuf/encoding/protowire"
)

type sseEvent struct {
	Type    string
	Payload string
}

func runStream(t *testing.T, originalReq string, chunks ...string) []sseEvent {
	t.Helper()

	var paramAny any
	var emitted [][]byte
	for _, chunk := range chunks {
		emitted = append(emitted, ConvertOpenAIResponseToClaude(
			context.Background(),
			"",
			[]byte(originalReq),
			nil,
			[]byte("data: "+chunk),
			&paramAny,
		)...)
	}
	emitted = append(emitted, ConvertOpenAIResponseToClaude(
		context.Background(),
		"",
		[]byte(originalReq),
		nil,
		[]byte("data: [DONE]"),
		&paramAny,
	)...)

	var events []sseEvent
	for _, raw := range emitted {
		s := string(raw)
		if !strings.HasPrefix(s, "event: ") {
			continue
		}
		nl := strings.Index(s, "\n")
		if nl < 0 {
			continue
		}
		typ := strings.TrimPrefix(s[:nl], "event: ")
		rest := s[nl+1:]
		if !strings.HasPrefix(rest, "data: ") {
			continue
		}
		payload := strings.TrimRight(strings.TrimPrefix(rest, "data: "), "\n")
		events = append(events, sseEvent{Type: typ, Payload: payload})
	}
	return events
}

func countByType(events []sseEvent, typ string) int {
	n := 0
	for _, e := range events {
		if e.Type == typ {
			n++
		}
	}
	return n
}

func toolUseStarts(events []sseEvent) []sseEvent {
	var out []sseEvent
	for _, e := range events {
		if e.Type != "content_block_start" {
			continue
		}
		if gjson.Get(e.Payload, "content_block.type").String() == "tool_use" {
			out = append(out, e)
		}
	}
	return out
}

func blockIndices(events []sseEvent) []int64 {
	var idx []int64
	for _, e := range events {
		if e.Type == "content_block_start" {
			idx = append(idx, gjson.Get(e.Payload, "index").Int())
		}
	}
	return idx
}

func lastStopReason(events []sseEvent) string {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == "message_delta" {
			return gjson.Get(events[i].Payload, "delta.stop_reason").String()
		}
	}
	return ""
}

const streamReq = `{"stream":true}`

func TestConvertOpenAIResponseToClaude_StreamIgnoresNullToolNameDelta(t *testing.T) {
	originalRequest := []byte(streamReq)
	var param any

	firstChunks := ConvertOpenAIResponseToClaude(
		context.Background(),
		"test-model",
		originalRequest,
		nil,
		[]byte(`data: {"id":"chatcmpl_1","model":"test-model","created":1,"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":""}}]},"finish_reason":null}]}`),
		&param,
	)
	firstOutput := bytes.Join(firstChunks, nil)
	if !bytes.Contains(firstOutput, []byte(`"name":"read_file"`)) {
		t.Fatalf("expected first chunk to start read_file tool block, got %s", string(firstOutput))
	}

	secondChunks := ConvertOpenAIResponseToClaude(
		context.Background(),
		"test-model",
		originalRequest,
		nil,
		[]byte(`data: {"id":"chatcmpl_1","model":"test-model","created":1,"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":null,"arguments":"{\"path\":\"/tmp/a\"}"}}]},"finish_reason":null}]}`),
		&param,
	)
	secondOutput := bytes.Join(secondChunks, nil)
	if bytes.Contains(secondOutput, []byte(`content_block_start`)) {
		t.Fatalf("did not expect null tool name delta to start a new content block, got %s", string(secondOutput))
	}
	if bytes.Contains(secondOutput, []byte(`"name":""`)) {
		t.Fatalf("did not expect null tool name delta to emit an empty tool name, got %s", string(secondOutput))
	}
}

func TestStreamingTool_EmptyNameThroughout(t *testing.T) {
	events := runStream(t, streamReq,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","function":{"name":"","arguments":""}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"","arguments":"{\"x\":1}"}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)

	// When name is empty throughout but arguments exist, a synthetic name is
	// generated so the tool call is not silently dropped.
	starts := toolUseStarts(events)
	if len(starts) != 1 {
		t.Fatalf("expected one tool_use content_block_start with synthetic name, got %d (events=%+v)", len(starts), events)
	}
	if name := gjson.Get(starts[0].Payload, "content_block.name").String(); name != "tool_0" {
		t.Fatalf("expected synthetic name tool_0, got %q", name)
	}
	if got := countByType(events, "content_block_delta"); got != 1 {
		t.Fatalf("expected one content_block_delta, got %d", got)
	}
	if got := countByType(events, "content_block_stop"); got != 1 {
		t.Fatalf("expected one content_block_stop, got %d", got)
	}
	if got := lastStopReason(events); got != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use", got)
	}
}

func TestStreamingTool_EmptyNameInfersSingleClaudeTool(t *testing.T) {
	originalReq := `{
		"stream":true,
		"tools":[{"name":"Read","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}},"required":["file_path"]}}]
	}`
	events := runStream(t, originalReq,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","function":{"name":"","arguments":""}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"file_path\":\"/tmp/a\"}"}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)

	starts := toolUseStarts(events)
	if len(starts) != 1 {
		t.Fatalf("expected one tool_use content_block_start, got %d (events=%+v)", len(starts), events)
	}
	if name := gjson.Get(starts[0].Payload, "content_block.name").String(); name != "Read" {
		t.Fatalf("expected inferred tool name Read, got %q", name)
	}
	if got := lastStopReason(events); got != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use", got)
	}
}

func TestStreamingTool_EmptyNameInfersForcedClaudeToolChoice(t *testing.T) {
	originalReq := `{
		"stream":true,
		"tool_choice":{"type":"tool","name":"Bash"},
		"tools":[
			{"name":"Read","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}},"required":["file_path"]}},
			{"name":"Bash","input_schema":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}
		]
	}`
	events := runStream(t, originalReq,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","function":{"name":"","arguments":""}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"command\":\"pwd\"}"}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)

	starts := toolUseStarts(events)
	if len(starts) != 1 {
		t.Fatalf("expected one tool_use content_block_start, got %d (events=%+v)", len(starts), events)
	}
	if name := gjson.Get(starts[0].Payload, "content_block.name").String(); name != "Bash" {
		t.Fatalf("expected inferred tool name Bash, got %q", name)
	}
}

func TestStreamingTool_EmptyNameInfersSingleZeroParameterClaudeTool(t *testing.T) {
	originalReq := `{
		"stream":true,
		"tools":[{"name":"List","input_schema":{"type":"object","properties":{}}}]
	}`
	events := runStream(t, originalReq,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","function":{"name":"","arguments":""}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)

	starts := toolUseStarts(events)
	if len(starts) != 1 {
		t.Fatalf("expected one zero-parameter tool_use content_block_start, got %d (events=%+v)", len(starts), events)
	}
	if name := gjson.Get(starts[0].Payload, "content_block.name").String(); name != "List" {
		t.Fatalf("expected inferred tool name List, got %q", name)
	}
	if got := lastStopReason(events); got != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use", got)
	}
}

func TestStreamingTool_EmptyNameInfersForcedZeroParameterClaudeToolChoice(t *testing.T) {
	originalReq := `{
		"stream":true,
		"tool_choice":{"type":"tool","name":"List"},
		"tools":[
			{"name":"Read","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}},"required":["file_path"]}},
			{"name":"List","input_schema":{"type":"object","properties":{}}}
		]
	}`
	events := runStream(t, originalReq,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","function":{"name":"","arguments":""}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)

	starts := toolUseStarts(events)
	if len(starts) != 1 {
		t.Fatalf("expected one forced zero-parameter tool_use content_block_start, got %d (events=%+v)", len(starts), events)
	}
	if name := gjson.Get(starts[0].Payload, "content_block.name").String(); name != "List" {
		t.Fatalf("expected inferred tool name List, got %q", name)
	}
}

func TestStreamingTool_NullName(t *testing.T) {
	events := runStream(t, streamReq,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","function":{"name":null,"arguments":""}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)
	if got := len(toolUseStarts(events)); got != 0 {
		t.Fatalf("null name must not produce a tool_use start; got %d", got)
	}
	if got := countByType(events, "content_block_stop"); got != 0 {
		t.Fatalf("null name must not produce content_block_stop; got %d", got)
	}
}

func TestStreamingTool_NonStringName(t *testing.T) {
	events := runStream(t, streamReq,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","function":{"name":123,"arguments":""}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)
	if got := len(toolUseStarts(events)); got != 0 {
		t.Fatalf("non-string name must not produce a tool_use start; got %d", got)
	}
}

func TestStreamingTool_RepeatedName(t *testing.T) {
	events := runStream(t, streamReq,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","function":{"name":"do_it","arguments":""}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"do_it","arguments":"{\"x\""}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"do_it","arguments":":1}"}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)

	starts := toolUseStarts(events)
	if len(starts) != 1 {
		t.Fatalf("expected exactly one tool_use start, got %d", len(starts))
	}
	if name := gjson.Get(starts[0].Payload, "content_block.name").String(); name != "do_it" {
		t.Fatalf("announced tool name = %q, want %q", name, "do_it")
	}
	if got := countByType(events, "content_block_stop"); got != 1 {
		t.Fatalf("expected exactly one content_block_stop, got %d", got)
	}
}

func TestStreamingTool_MixedSuppressedAndValid(t *testing.T) {
	events := runStream(t, streamReq,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[
			{"index":0,"id":"call_skip","function":{"name":"","arguments":""}},
			{"index":1,"id":"call_real","function":{"name":"do_it","arguments":""}}
		]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[
			{"index":1,"function":{"arguments":"{}"}}
		]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)

	starts := toolUseStarts(events)
	if len(starts) != 1 {
		t.Fatalf("expected exactly one tool_use start, got %d", len(starts))
	}
	if got := countByType(events, "content_block_stop"); got != 1 {
		t.Fatalf("expected exactly one content_block_stop, got %d", got)
	}

	indices := blockIndices(events)
	if len(indices) == 0 || indices[0] != 0 {
		t.Fatalf("first content_block_start index must be 0, got %v", indices)
	}
}

func TestStreamingTool_EmptyIDDeferStart(t *testing.T) {
	events := runStream(t, streamReq,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"","function":{"name":"do_it","arguments":""}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_real","function":{"arguments":"{}"}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)

	starts := toolUseStarts(events)
	if len(starts) != 1 {
		t.Fatalf("expected exactly one tool_use start once id arrived, got %d", len(starts))
	}
	if id := gjson.Get(starts[0].Payload, "content_block.id").String(); id != "call_real" {
		t.Fatalf("announced tool id = %q, want %q", id, "call_real")
	}
}

func TestStreamingTool_IDInDeltaWithoutFunction(t *testing.T) {
	events := runStream(t, streamReq,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"function":{"name":"do_it"}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_real"}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)

	starts := toolUseStarts(events)
	if len(starts) != 1 {
		t.Fatalf("expected exactly one tool_use start when id arrives in a function-less delta, got %d", len(starts))
	}
	if id := gjson.Get(starts[0].Payload, "content_block.id").String(); id != "call_real" {
		t.Fatalf("announced tool id = %q, want %q", id, "call_real")
	}
	if name := gjson.Get(starts[0].Payload, "content_block.name").String(); name != "do_it" {
		t.Fatalf("announced tool name = %q, want %q", name, "do_it")
	}
	if got := countByType(events, "content_block_stop"); got != 1 {
		t.Fatalf("expected exactly one content_block_stop, got %d", got)
	}
}

func TestStreamingTool_StopReasonWithEmittedTool(t *testing.T) {
	events := runStream(t, streamReq,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","function":{"name":"do_it","arguments":"{}"}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
	)
	if got := lastStopReason(events); got != "tool_use" {
		t.Fatalf("stop_reason = %q, want %q", got, "tool_use")
	}
}

func TestStreamingTool_StopReasonWhenIDNeverArrives(t *testing.T) {
	events := runStream(t, streamReq,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"function":{"name":"do_it","arguments":""}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)

	starts := toolUseStarts(events)
	if len(starts) != 1 {
		t.Fatalf("expected one belated tool_use start with synthetic id, got %d", len(starts))
	}
	id := gjson.Get(starts[0].Payload, "content_block.id").String()
	if !strings.HasPrefix(id, "toolu_") {
		t.Fatalf("synthetic id should match toolu_<nanos>_<n>, got %q", id)
	}
	if name := gjson.Get(starts[0].Payload, "content_block.name").String(); name != "do_it" {
		t.Fatalf("announced tool name = %q, want %q", name, "do_it")
	}
	if got := lastStopReason(events); got != "tool_use" {
		t.Fatalf("stop_reason = %q, want %q", got, "tool_use")
	}
}

func TestStreamingTool_BelatedStartsUseOpenAIToolIndexOrder(t *testing.T) {
	events := runStream(t, streamReq,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[
			{"index":2,"function":{"name":"third_tool","arguments":"{}"}},
			{"index":0,"function":{"name":"first_tool","arguments":"{}"}},
			{"index":1,"function":{"name":"second_tool","arguments":"{}"}}
		]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)

	starts := toolUseStarts(events)
	if len(starts) != 3 {
		t.Fatalf("expected three belated tool_use starts, got %d", len(starts))
	}

	wantNames := []string{"first_tool", "second_tool", "third_tool"}
	for i, wantName := range wantNames {
		if name := gjson.Get(starts[i].Payload, "content_block.name").String(); name != wantName {
			t.Fatalf("tool_use start %d name = %q, want %q (starts=%+v)", i, name, wantName, starts)
		}
		if blockIndex := gjson.Get(starts[i].Payload, "index").Int(); blockIndex != int64(i) {
			t.Fatalf("tool_use start %d block index = %d, want %d", i, blockIndex, i)
		}
	}
}

func TestStreamingTool_LateIDAfterFinalization(t *testing.T) {
	events := runStream(t, streamReq,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"function":{"name":"do_it"}}]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_late"}]}}]}`,
	)

	starts := toolUseStarts(events)
	if len(starts) != 1 {
		t.Fatalf("expected one belated tool_use start, got %d", len(starts))
	}

	var sawMessageStop bool
	for _, e := range events {
		if e.Type == "message_stop" {
			sawMessageStop = true
			continue
		}
		if sawMessageStop {
			switch e.Type {
			case "content_block_start", "content_block_delta", "content_block_stop":
				t.Fatalf("event %q emitted after message_stop (events=%+v)", e.Type, events)
			}
		}
	}
}

func TestStreamingTool_StopReasonMixedSuppressedAndValid(t *testing.T) {
	events := runStream(t, streamReq,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[
			{"index":0,"id":"call_skip","function":{"name":"","arguments":""}},
			{"index":1,"id":"call_real","function":{"name":"do_it","arguments":"{}"}}
		]}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)
	if got := lastStopReason(events); got != "tool_use" {
		t.Fatalf("stop_reason = %q, want %q", got, "tool_use")
	}
}

func TestStreamingTool_ArgumentChunkEmitsInputDeltaImmediately(t *testing.T) {
	originalRequest := []byte(`{
		"stream":true,
		"tools":[{"name":"ToolSearch","input_schema":{"type":"object","properties":{"query":{"type":"string"},"max_results":{"type":"number"}},"required":["query","max_results"]}}]
	}`)
	var param any

	first := ConvertOpenAIResponseToClaude(
		context.Background(),
		"m",
		originalRequest,
		nil,
		[]byte(`data: {"id":"c1","model":"m","created":1,"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_search","type":"function","function":{"name":"ToolSearch","arguments":""}}]},"finish_reason":null}]}`),
		&param,
	)
	if !bytes.Contains(bytes.Join(first, nil), []byte(`"name":"ToolSearch"`)) {
		t.Fatalf("expected ToolSearch content_block_start, got %s", bytes.Join(first, nil))
	}

	second := ConvertOpenAIResponseToClaude(
		context.Background(),
		"m",
		originalRequest,
		nil,
		[]byte(`data: {"id":"c1","model":"m","created":1,"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"max_results\": "}}]},"finish_reason":null}]}`),
		&param,
	)
	if second == nil {
		t.Fatal("argument-only tool chunk returned nil; registry would fall back to raw OpenAI SSE")
	}
	out := bytes.Join(second, nil)
	if !bytes.Contains(out, []byte(`"type":"input_json_delta"`)) {
		t.Fatalf("expected immediate input_json_delta for argument chunk, got %s", out)
	}
	if bytes.Contains(out, []byte(`"choices"`)) {
		t.Fatalf("raw OpenAI chunk leaked into Claude stream: %s", out)
	}

	third := ConvertOpenAIResponseToClaude(
		context.Background(),
		"m",
		originalRequest,
		nil,
		[]byte(`data: {"id":"c1","model":"m","created":1,"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"6, \"query\": \"TaskCreate TaskGet TaskList TaskOutput TaskStop TaskUpdate\"}"}}]},"finish_reason":null}]}`),
		&param,
	)
	combined := string(bytes.Join(append(second, third...), nil))
	if !strings.Contains(combined, `"query\": \"TaskCreate TaskGet TaskList TaskOutput TaskStop TaskUpdate\"`) {
		t.Fatalf("ToolSearch query was not streamed in input_json_delta chunks: %s", combined)
	}
}

func TestStreamingNoOutputChunkDoesNotFallbackToRawOpenAI(t *testing.T) {
	var param any
	originalRequest := []byte(streamReq)
	translatedRequest := []byte(`{"stream":true}`)

	_ = sdktranslator.TranslateStream(
		context.Background(),
		sdktranslator.FormatOpenAI,
		sdktranslator.FormatClaude,
		"m",
		originalRequest,
		translatedRequest,
		[]byte(`data: {"id":"c1","model":"m","created":1,"choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`),
		&param,
	)

	chunks := sdktranslator.TranslateStream(
		context.Background(),
		sdktranslator.FormatOpenAI,
		sdktranslator.FormatClaude,
		"m",
		originalRequest,
		translatedRequest,
		[]byte(`data: {"id":"c1","model":"m","created":1,"choices":[{"index":0,"delta":{"content":null},"finish_reason":null}]}`),
		&param,
	)
	if chunks == nil {
		t.Fatal("translator returned nil for a consumed no-output chunk")
	}
	if len(chunks) != 0 {
		t.Fatalf("expected consumed no-output chunk to be dropped, got %s", bytes.Join(chunks, nil))
	}
}

func TestOpenAIResponseToClaude_NonStreamReasoningContentDoesNotSynthesizeSignature(t *testing.T) {
	resp := []byte(`{"id":"r1","model":"deepseek-reasoner","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","reasoning_content":"plain reasoning","content":"answer"}}]}`)

	var param any
	out := ConvertOpenAIResponseToClaudeNonStream(context.Background(), "deepseek-reasoner", nil, nil, resp, &param)
	thinking := firstThinkingBlock(out)

	if got := thinking.Get("type").String(); got != "thinking" {
		t.Fatalf("thinking block type = %q, want thinking. Output: %s", got, string(out))
	}
	if got := thinking.Get("thinking").String(); got != "plain reasoning" {
		t.Fatalf("thinking = %q, want plain reasoning. Output: %s", got, string(out))
	}
	if thinking.Get("signature").Exists() {
		t.Fatalf("plain reasoning_content must not synthesize signature. Output: %s", string(out))
	}
}

func TestOpenAIResponseToClaude_NonStreamReasoningContentPreservesClaudeSignature(t *testing.T) {
	signature := validClaudeResponseThinkingSignature()
	resp := []byte(`{
		"id":"r1",
		"model":"m",
		"choices":[{
			"index":0,
			"finish_reason":"stop",
			"message":{
				"role":"assistant",
				"reasoning_content":{"text":"signed reasoning","signature":"claude#` + signature + `"},
				"content":"answer"
			}
		}]
	}`)

	var param any
	out := ConvertOpenAIResponseToClaudeNonStream(context.Background(), "m", nil, nil, resp, &param)
	thinking := firstThinkingBlock(out)

	if got := thinking.Get("thinking").String(); got != "signed reasoning" {
		t.Fatalf("thinking = %q, want signed reasoning. Output: %s", got, string(out))
	}
	if got := thinking.Get("signature").String(); got != signature {
		t.Fatalf("signature = %q, want normalized %q. Output: %s", got, signature, string(out))
	}
}

func TestOpenAIResponseToClaude_StreamReasoningContentPreservesClaudeSignatureDelta(t *testing.T) {
	signature := validClaudeResponseThinkingSignature()
	events := runStream(t, streamReq,
		`{"id":"c1","model":"m","created":1,"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":{"text":"signed","signature":"claude#`+signature+`"}}}]}`,
		`{"id":"c1","model":"m","created":1,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	)

	var sawSignature bool
	for _, event := range events {
		if event.Type != "content_block_delta" {
			continue
		}
		if gjson.Get(event.Payload, "delta.type").String() == "signature_delta" {
			sawSignature = true
			if got := gjson.Get(event.Payload, "delta.signature").String(); got != signature {
				t.Fatalf("signature_delta = %q, want %q", got, signature)
			}
		}
	}
	if !sawSignature {
		t.Fatalf("expected signature_delta event, got %+v", events)
	}
}

func TestOpenAIResponseToClaude_StreamSignatureOnlyReasoningEmitsThinkingBlock(t *testing.T) {
	signature := validClaudeResponseThinkingSignature()
	events := runStream(t, streamReq,
		`{"id":"c1","model":"m","created":1,"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":{"signature":"claude#`+signature+`"}}}]}`,
		`{"id":"c1","model":"m","created":1,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	)

	var got []string
	var gotSignature string
	for _, event := range events {
		switch event.Type {
		case "content_block_start":
			if gjson.Get(event.Payload, "content_block.type").String() == "thinking" {
				got = append(got, "thinking_start")
			}
		case "content_block_delta":
			switch gjson.Get(event.Payload, "delta.type").String() {
			case "thinking_delta":
				got = append(got, "thinking_delta")
			case "signature_delta":
				got = append(got, "signature_delta")
				gotSignature = gjson.Get(event.Payload, "delta.signature").String()
			}
		case "content_block_stop":
			got = append(got, "thinking_stop")
		}
	}

	if gotSignature != signature {
		t.Fatalf("signature_delta = %q, want %q. Events: %+v", gotSignature, signature, events)
	}
	if gotJoined := strings.Join(got, ","); gotJoined != "thinking_start,signature_delta,thinking_stop" {
		t.Fatalf("event sequence = %s, want thinking_start,signature_delta,thinking_stop. Events: %+v", gotJoined, events)
	}
}

func TestOpenAIResponseToClaude_StreamEmitsSignatureBeforeTextSwitch(t *testing.T) {
	signature := validClaudeResponseThinkingSignature()
	events := runStream(t, streamReq,
		`{"id":"c1","model":"m","created":1,"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"thinking text"}}]}`,
		`{"id":"c1","model":"m","created":1,"choices":[{"index":0,"delta":{"reasoning_content":{"signature":"claude#`+signature+`"}}}]}`,
		`{"id":"c1","model":"m","created":1,"choices":[{"index":0,"delta":{"content":"visible answer"}}]}`,
		`{"id":"c1","model":"m","created":1,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	)

	var got []string
	for _, event := range events {
		switch event.Type {
		case "content_block_start":
			switch gjson.Get(event.Payload, "content_block.type").String() {
			case "thinking":
				got = append(got, "thinking_start")
			case "text":
				got = append(got, "text_start")
			}
		case "content_block_delta":
			switch gjson.Get(event.Payload, "delta.type").String() {
			case "thinking_delta":
				got = append(got, "thinking_delta")
			case "signature_delta":
				got = append(got, "signature_delta")
			case "text_delta":
				got = append(got, "text_delta")
			}
		case "content_block_stop":
			got = append(got, "block_stop")
		}
	}

	want := "thinking_start,thinking_delta,signature_delta,block_stop,text_start,text_delta,block_stop"
	if gotJoined := strings.Join(got, ","); gotJoined != want {
		t.Fatalf("event sequence = %s, want %s. Events: %+v", gotJoined, want, events)
	}
}

func TestOpenAIResponseToClaude_StreamMultipleSignedReasoningPartsKeepEachSignature(t *testing.T) {
	signature1 := validClaudeResponseThinkingSignatureForModel("claude-sonnet-4-6-a")
	signature2 := validClaudeResponseThinkingSignatureForModel("claude-sonnet-4-6-b")
	events := runStream(t, streamReq,
		`{"id":"c1","model":"m","created":1,"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":[{"text":"first","signature":"claude#`+signature1+`"},{"text":"second","signature":"claude#`+signature2+`"}]}}]}`,
		`{"id":"c1","model":"m","created":1,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	)

	var signatures []string
	for _, event := range events {
		if event.Type != "content_block_delta" || gjson.Get(event.Payload, "delta.type").String() != "signature_delta" {
			continue
		}
		signatures = append(signatures, gjson.Get(event.Payload, "delta.signature").String())
	}

	if len(signatures) != 2 {
		t.Fatalf("expected 2 signature_delta events, got %d (%v). Events: %+v", len(signatures), signatures, events)
	}
	if signatures[0] != signature1 || signatures[1] != signature2 {
		t.Fatalf("signatures = %v, want [%s %s]. Events: %+v", signatures, signature1, signature2, events)
	}
}

func TestOpenAIResponseToClaude_StreamNullReasoningContentDoesNotEmitThinking(t *testing.T) {
	events := runStream(t, streamReq,
		`{"id":"c1","model":"m","created":1,"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":null,"content":"visible"}}]}`,
		`{"id":"c1","model":"m","created":1,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	)

	for _, event := range events {
		if event.Type == "content_block_delta" && gjson.Get(event.Payload, "delta.type").String() == "thinking_delta" {
			t.Fatalf("null reasoning_content should not emit thinking_delta: %+v", events)
		}
	}
}

// nonStreamToolUses extracts tool_use content blocks from a non-streaming
// Anthropic message payload.
func nonStreamToolUses(payload []byte) []gjson.Result {
	var out []gjson.Result
	gjson.GetBytes(payload, "content").ForEach(func(_, block gjson.Result) bool {
		if block.Get("type").String() == "tool_use" {
			out = append(out, block)
		}
		return true
	})
	return out
}

func firstThinkingBlock(payload []byte) gjson.Result {
	var out gjson.Result
	gjson.GetBytes(payload, "content").ForEach(func(_, block gjson.Result) bool {
		if block.Get("type").String() == "thinking" {
			out = block
			return false
		}
		return true
	})
	return out
}

func TestNonStreamingTool_EmptyNameInferredFromSingleClaudeTool(t *testing.T) {
	originalReq := []byte(`{
		"tools":[{"name":"Read","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}},"required":["file_path"]}}]
	}`)
	resp := []byte(`{"id":"r1","model":"m","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call_a","type":"function","function":{"name":"","arguments":"{\"file_path\":\"/tmp/a\"}"}}]}}]}`)

	var param any
	out := ConvertOpenAIResponseToClaudeNonStream(context.Background(), "m", originalReq, nil, resp, &param)

	tools := nonStreamToolUses(out)
	if len(tools) != 1 {
		t.Fatalf("expected one tool_use block, got %d (out=%s)", len(tools), string(out))
	}
	if name := tools[0].Get("name").String(); name != "Read" {
		t.Fatalf("expected inferred tool name Read, got %q", name)
	}
}

func TestNonStreamingTool_EmptyNameSyntheticFallback(t *testing.T) {
	originalReq := []byte(`{
		"tools":[
			{"name":"Read","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}}}},
			{"name":"Write","input_schema":{"type":"object","properties":{"content":{"type":"string"}}}}
		]
	}`)
	resp := []byte(`{"id":"r1","model":"m","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call_a","type":"function","function":{"name":"","arguments":"{\"unmatched\":1}"}}]}}]}`)

	var param any
	out := ConvertOpenAIResponseToClaudeNonStream(context.Background(), "m", originalReq, nil, resp, &param)

	tools := nonStreamToolUses(out)
	if len(tools) != 1 {
		t.Fatalf("expected one tool_use block, got %d (out=%s)", len(tools), string(out))
	}
	if name := tools[0].Get("name").String(); name != "tool_0" {
		t.Fatalf("expected synthetic name tool_0, got %q", name)
	}
}

func TestNonStreamingTool_EmptyNameAndEmptyArgsSkipped(t *testing.T) {
	originalReq := []byte(`{
		"tools":[
			{"name":"Read","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}}}},
			{"name":"Write","input_schema":{"type":"object","properties":{"content":{"type":"string"}}}}
		]
	}`)
	resp := []byte(`{"id":"r1","model":"m","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call_a","type":"function","function":{"name":"","arguments":""}}]}}]}`)

	var param any
	out := ConvertOpenAIResponseToClaudeNonStream(context.Background(), "m", originalReq, nil, resp, &param)

	if tools := nonStreamToolUses(out); len(tools) != 0 {
		t.Fatalf("expected empty-name empty-args tool_use to be skipped, got %d (out=%s)", len(tools), string(out))
	}
}

func validClaudeResponseThinkingSignature() string {
	return validClaudeResponseThinkingSignatureForModel("claude-sonnet-4-6")
}

func validClaudeResponseThinkingSignatureForModel(model string) string {
	channelBlock := []byte{}
	channelBlock = protowire.AppendTag(channelBlock, 1, protowire.VarintType)
	channelBlock = protowire.AppendVarint(channelBlock, 12)
	channelBlock = protowire.AppendTag(channelBlock, 2, protowire.VarintType)
	channelBlock = protowire.AppendVarint(channelBlock, 2)
	channelBlock = protowire.AppendTag(channelBlock, 6, protowire.BytesType)
	channelBlock = protowire.AppendString(channelBlock, model)

	container := []byte{}
	container = protowire.AppendTag(container, 1, protowire.BytesType)
	container = protowire.AppendBytes(container, channelBlock)

	payload := []byte{}
	payload = protowire.AppendTag(payload, 2, protowire.BytesType)
	payload = protowire.AppendBytes(payload, container)
	payload = protowire.AppendTag(payload, 3, protowire.VarintType)
	payload = protowire.AppendVarint(payload, 1)
	return base64.StdEncoding.EncodeToString(payload)
}

func TestStreamFalseDispatch_EmptyNameInferred(t *testing.T) {
	originalReq := []byte(`{
		"stream":false,
		"tools":[{"name":"Read","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}},"required":["file_path"]}}]
	}`)
	resp := []byte(`data: {"id":"r1","model":"m","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call_a","type":"function","function":{"name":"","arguments":"{\"file_path\":\"/tmp/a\"}"}}]}}]}`)

	var param any
	chunks := ConvertOpenAIResponseToClaude(context.Background(), "m", originalReq, nil, resp, &param)
	out := bytes.Join(chunks, nil)

	tools := nonStreamToolUses(out)
	if len(tools) != 1 {
		t.Fatalf("expected one tool_use block, got %d (out=%s)", len(tools), string(out))
	}
	if name := tools[0].Get("name").String(); name != "Read" {
		t.Fatalf("expected inferred tool name Read, got %q", name)
	}
}

func TestStreamFalseDispatch_EmptyNameAndEmptyArgsSkipped(t *testing.T) {
	originalReq := []byte(`{
		"stream":false,
		"tools":[
			{"name":"Read","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}}}},
			{"name":"Write","input_schema":{"type":"object","properties":{"content":{"type":"string"}}}}
		]
	}`)
	resp := []byte(`data: {"id":"r1","model":"m","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call_a","type":"function","function":{"name":"","arguments":""}}]}}]}`)

	var param any
	chunks := ConvertOpenAIResponseToClaude(context.Background(), "m", originalReq, nil, resp, &param)
	out := bytes.Join(chunks, nil)

	if tools := nonStreamToolUses(out); len(tools) != 0 {
		t.Fatalf("expected empty-name empty-args tool_use to be skipped, got %d (out=%s)", len(tools), string(out))
	}
}
