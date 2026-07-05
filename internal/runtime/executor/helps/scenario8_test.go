package helps

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// TestScenario8_NonStream_ToolUseStopReasonCorrection validates the scenario 8
// fix: when a non-stream Claude response contains a tool_use block but the
// stop_reason is a non-tool value (e.g. "end_turn" mapped from OpenAI's
// "stop" finish_reason), NormalizeNonStreamContentOrder must correct it to
// "tool_use".
//
// Run with:
//
//	go test ./internal/runtime/executor/helps/ -run TestScenario8 -v
func TestScenario8_NonStream_ToolUseStopReasonCorrection(t *testing.T) {
	fmt.Println("============================================================")
	fmt.Println("场景 8 测试: 非流式响应中 tool_use 块 + stop_reason 异常修正")
	fmt.Println("============================================================")

	cases := []struct {
		name           string
		input          string
		expectedReason string
		description    string
	}{
		{
			name: "A_end_turn_with_tool_use",
			// Simulates: OpenAI finish_reason="stop" → translator maps to
			// stop_reason="end_turn", but content has tool_use block.
			// Normalizer must correct "end_turn" → "tool_use".
			input: `{
				"type":"message",
				"role":"assistant",
				"content":[
					{"type":"text","text":"Let me search that for you."},
					{"type":"tool_use","id":"toolu_01","name":"web_search","input":{"query":"go sse"}}
				],
				"stop_reason":"end_turn",
				"stop_sequence":null,
				"usage":{"input_tokens":25,"output_tokens":18}
			}`,
			expectedReason: "tool_use",
			description:    "stop_reason=end_turn + tool_use 块 → 应修正为 tool_use",
		},
		{
			name: "B_null_stop_reason_with_tool_use",
			input: `{
				"type":"message",
				"role":"assistant",
				"content":[
					{"type":"tool_use","id":"toolu_02","name":"calculator","input":{"expr":"1+1"}}
				],
				"stop_reason":null,
				"usage":{"input_tokens":10,"output_tokens":5}
			}`,
			expectedReason: "tool_use",
			description:    "stop_reason=null + tool_use 块 → 应填充为 tool_use",
		},
		{
			name: "C_missing_stop_reason_with_tool_use",
			input: `{
				"type":"message",
				"role":"assistant",
				"content":[
					{"type":"text","text":"Calling tool"},
					{"type":"tool_use","id":"toolu_03","name":"get_weather","input":{"city":"SF"}}
				],
				"usage":{"input_tokens":15,"output_tokens":8}
			}`,
			expectedReason: "tool_use",
			description:    "stop_reason 缺失 + tool_use 块 → 应填充为 tool_use",
		},
		{
			name: "D_tool_use_preserved",
			// Already correct: stop_reason="tool_use" should be preserved.
			input: `{
				"type":"message",
				"role":"assistant",
				"content":[
					{"type":"tool_use","id":"toolu_04","name":"search","input":{"q":"test"}}
				],
				"stop_reason":"tool_use",
				"usage":{"input_tokens":10,"output_tokens":3}
			}`,
			expectedReason: "tool_use",
			description:    "stop_reason=tool_use + tool_use 块 → 保持不变",
		},
		{
			name: "E_max_tokens_preserved_with_tool_use",
			// max_tokens is legitimate even with tool_use (truncated tool call).
			input: `{
				"type":"message",
				"role":"assistant",
				"content":[
					{"type":"tool_use","id":"toolu_05","name":"broken_tool","input":{}}
				],
				"stop_reason":"max_tokens",
				"usage":{"input_tokens":10,"output_tokens":4096}
			}`,
			expectedReason: "max_tokens",
			description:    "stop_reason=max_tokens + tool_use 块 → 保持不变(合法截断)",
		},
		{
			name: "F_end_turn_no_tool_use",
			// No tool_use block: stop_reason should be preserved as-is.
			input: `{
				"type":"message",
				"role":"assistant",
				"content":[
					{"type":"text","text":"Hello world"}
				],
				"stop_reason":"end_turn",
				"usage":{"input_tokens":5,"output_tokens":2}
			}`,
			expectedReason: "end_turn",
			description:    "stop_reason=end_turn 无 tool_use 块 → 保持不变",
		},
		{
			name: "G_end_turn_with_text_and_thinking_and_tool_use",
			// Complex: text + thinking + tool_use, stop_reason=end_turn
			// Should reorder to [text, thinking, tool_use] and fix stop_reason.
			input: `{
				"type":"message",
				"role":"assistant",
				"content":[
					{"type":"tool_use","id":"toolu_06","name":"run_code","input":{"code":"print(1)"}},
					{"type":"thinking","thinking":"I need to run code"},
					{"type":"text","text":"Running your code now."}
				],
				"stop_reason":"end_turn",
				"usage":{"input_tokens":30,"output_tokens":50}
			}`,
			expectedReason: "tool_use",
			description:    "text+thinking+tool_use 乱序 + stop_reason=end_turn → 排序+修正为 tool_use",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte(tc.input)
			output := NormalizeNonStreamContentOrder(input)

			sr := gjson.GetBytes(output, "stop_reason")
			if !sr.Exists() {
				t.Fatalf("stop_reason missing after normalization; output=%s", string(output))
			}
			got := sr.String()
			fmt.Printf("  [%s] %s\n", tc.name, tc.description)
			fmt.Printf("    输入 stop_reason: %s → 输出 stop_reason: %s (期望: %s)\n",
				gjson.GetBytes(input, "stop_reason").String(), got, tc.expectedReason)
			fmt.Printf("    输出 content 类型顺序: ")
			for _, b := range gjson.GetBytes(output, "content").Array() {
				fmt.Printf("%s ", b.Get("type").String())
			}
			fmt.Println()
			if got != tc.expectedReason {
				t.Errorf("stop_reason = %q, want %q; output=%s", got, tc.expectedReason, string(output))
			}
			fmt.Println()
		})
	}
}

// TestScenario8_Stream_ToolUseEventOrder constructs a complex SSE stream
// containing a tool_use content block and verifies that the SSENormalizer
// correctly orders events and that message_delta is emitted after all
// content_block_stop events.
//
// This simulates a real-world tool-call SSE stream from an OpenAI-compatible
// upstream that includes:
//   - message_start
//   - text content_block (start, delta, stop)
//   - tool_use content_block (start, input_json_delta, stop)
//   - message_delta with stop_reason="tool_use"
//   - message_stop
func TestScenario8_Stream_ToolUseEventOrder(t *testing.T) {
	fmt.Println("============================================================")
	fmt.Println("场景 8 流式测试: tool_use SSE 流事件顺序验证")
	fmt.Println("============================================================")

	// Construct a complex but well-ordered Anthropic SSE stream with tool_use.
	// This simulates what the OpenAI→Claude translator would emit for a
	// tool-call response.
	normalizer := NewSSENormalizer()

	// Chunk 1: message_start + text block + tool_use block (interleaved start)
	chunk1 := []byte(
		"event: message_start\n" +
			`data: {"type":"message_start","message":{"id":"msg_01","type":"message","role":"assistant","content":[],"model":"claude-3","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":50,"output_tokens":0}}}` + "\n\n" +
			"event: content_block_start\n" +
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
			"event: content_block_delta\n" +
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me calculate that."}}` + "\n\n" +
			"event: content_block_stop\n" +
			`data: {"type":"content_block_stop","index":0}` + "\n\n" +
			"event: content_block_start\n" +
			`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_01","name":"calculator","input":{}}}` + "\n\n")

	out1 := normalizer.Process(chunk1)
	fmt.Println("--- Chunk 1: message_start + text block + tool_use start ---")
	for _, o := range splitFrames(out1) {
		fmt.Printf("  %s", visualizeSSEFrame(o))
	}

	// Chunk 2: tool_use input_json_delta (streaming the tool input JSON)
	chunk2 := []byte(
		"event: content_block_delta\n" +
			`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"expr\":"}}` + "\n\n" +
			"event: content_block_delta\n" +
			`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"1+1\"}"}}` + "\n\n")

	out2 := normalizer.Process(chunk2)
	fmt.Println("\n--- Chunk 2: tool_use input_json_delta ---")
	for _, o := range splitFrames(out2) {
		fmt.Printf("  %s", visualizeSSEFrame(o))
	}

	// Chunk 3: content_block_stop for tool_use + message_delta + message_stop
	chunk3 := []byte(
		"event: content_block_stop\n" +
			`data: {"type":"content_block_stop","index":1}` + "\n\n" +
			"event: message_delta\n" +
			`data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":50,"output_tokens":15}}` + "\n\n" +
			"event: message_stop\n" +
			`data: {"type":"message_stop"}` + "\n\n")

	out3 := normalizer.Process(chunk3)
	fmt.Println("\n--- Chunk 3: tool_use stop + message_delta + message_stop ---")
	for _, o := range splitFrames(out3) {
		fmt.Printf("  %s", visualizeSSEFrame(o))
	}

	// Flush (should produce nothing if stream was complete)
	flushOut := normalizer.Flush()
	if len(flushOut) > 0 {
		fmt.Println("\n--- Flush (补发事件) ---")
		for _, o := range splitFrames(flushOut) {
			fmt.Printf("  %s", visualizeSSEFrame(o))
		}
	} else {
		fmt.Println("\n--- Flush: 无补发事件(流完整) ---")
	}

	// Collect all output and verify event order. Process/Flush now return
	// concatenated frames per call, so a plain []byte concatenation is enough.
	joined := make([]byte, 0, len(out1)+len(out2)+len(out3)+len(flushOut))
	joined = append(joined, out1...)
	joined = append(joined, out2...)
	joined = append(joined, out3...)
	joined = append(joined, flushOut...)

	// Parse events
	events := parseFramesForTest(joined)
	fmt.Printf("\n--- 完整事件序列(%d 个事件)---\n", len(events))
	for i, ev := range events {
		fmt.Printf("  [%d] %s", i, ev)
	}

	// Verify protocol-compliant order
	expectedOrder := []string{
		"message_start",
		"content_block_start", // text
		"content_block_delta", // text_delta
		"content_block_stop",  // text
		"content_block_start", // tool_use
		"content_block_delta", // input_json_delta (1st)
		"content_block_delta", // input_json_delta (2nd)
		"content_block_stop",  // tool_use
		"message_delta",
		"message_stop",
	}
	if len(events) != len(expectedOrder) {
		t.Fatalf("event count = %d, want %d", len(events), len(expectedOrder))
	}
	for i, et := range expectedOrder {
		if !strings.Contains(events[i], "event: "+et) {
			t.Errorf("event[%d] = %q, want type %q", i, events[i], et)
		}
	}
	fmt.Println("\n✓ 事件顺序符合 Anthropic 协议要求")

	// Verify stop_reason in message_delta
	for _, ev := range events {
		if strings.Contains(ev, "event: message_delta") {
			if !strings.Contains(ev, `"stop_reason":"tool_use"`) {
				t.Errorf("message_delta missing stop_reason=tool_use: %s", ev)
			}
			fmt.Println("✓ message_delta 中 stop_reason=tool_use 正确")
		}
	}

	// Verify message_stop is last
	if last := events[len(events)-1]; !strings.Contains(last, "event: message_stop") {
		t.Errorf("last event should be message_stop, got: %s", last)
	}
	fmt.Println("✓ message_stop 是最后一个事件")
}

// TestScenario8_Stream_MisorderedMessageDelta constructs a stream where
// message_delta arrives BEFORE content_block_stop for the tool_use block.
// The normalizer must buffer message_delta and only release it after the
// block is stopped.
func TestScenario8_Stream_MisorderedMessageDelta(t *testing.T) {
	fmt.Println("============================================================")
	fmt.Println("场景 3+8 流式测试: message_delta 早于 content_block_stop 到达")
	fmt.Println("============================================================")

	normalizer := NewSSENormalizer()

	// Construct a stream where message_delta arrives before the tool_use
	// content_block_stop. This violates protocol ordering.
	chunk := []byte(
		"event: message_start\n" +
			`data: {"type":"message_start","message":{"id":"msg_02","type":"message","role":"assistant","content":[],"model":"claude-3","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":30,"output_tokens":0}}}` + "\n\n" +
			"event: content_block_start\n" +
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_02","name":"search","input":{}}}` + "\n\n" +
			"event: content_block_delta\n" +
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"q\":\"test\"}"}}` + "\n\n" +
			// BUG: message_delta arrives before content_block_stop!
			"event: message_delta\n" +
			`data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":30,"output_tokens":10}}` + "\n\n" +
			// message_stop also early — but normalizer buffers message_delta,
			// so message_stop is emitted (it's not buffered). This tests
			// that message_delta is correctly held back.
			"event: content_block_stop\n" +
			`data: {"type":"content_block_stop","index":0}` + "\n\n" +
			"event: message_stop\n" +
			`data: {"type":"message_stop"}` + "\n\n")

	out := normalizer.Process(chunk)
	flushOut := normalizer.Flush()
	// Process/Flush now return a single concatenated frame per call; join them.
	joined := make([]byte, 0, len(out)+len(flushOut))
	joined = append(joined, out...)
	joined = append(joined, flushOut...)
	events := parseFramesForTest(joined)

	fmt.Printf("--- 事件序列(%d 个事件)---\n", len(events))
	for i, ev := range events {
		fmt.Printf("  [%d] %s", i, ev)
	}

	// Find indices of content_block_stop and message_delta
	cbStopIdx := -1
	mdIdx := -1
	for i, ev := range events {
		if strings.Contains(ev, "event: content_block_stop") && cbStopIdx == -1 {
			cbStopIdx = i
		}
		if strings.Contains(ev, "event: message_delta") && mdIdx == -1 {
			mdIdx = i
		}
	}
	if cbStopIdx == -1 {
		t.Fatal("content_block_stop not found in output")
	}
	if mdIdx == -1 {
		t.Fatal("message_delta not found in output")
	}
	fmt.Printf("\ncontent_block_stop 位置: [%d]\n", cbStopIdx)
	fmt.Printf("message_delta 位置:     [%d]\n", mdIdx)
	if mdIdx <= cbStopIdx {
		t.Errorf("message_delta (idx=%d) must appear AFTER content_block_stop (idx=%d)", mdIdx, cbStopIdx)
	} else {
		fmt.Println("✓ message_delta 正确出现在 content_block_stop 之后")
	}

	// Verify message_delta appears BEFORE message_stop
	msIdx := -1
	for i, ev := range events {
		if strings.Contains(ev, "event: message_stop") && msIdx == -1 {
			msIdx = i
		}
	}
	if msIdx == -1 {
		t.Fatal("message_stop not found in output")
	}
	fmt.Printf("message_stop 位置:     [%d]\n", msIdx)
	if msIdx <= mdIdx {
		t.Errorf("message_stop (idx=%d) must appear AFTER message_delta (idx=%d)", msIdx, mdIdx)
	} else {
		fmt.Println("✓ message_stop 正确出现在 message_delta 之后")
	}

	// Verify no duplicate message_delta
	deltaCount := 0
	for _, ev := range events {
		if strings.Contains(ev, "event: message_delta") {
			deltaCount++
		}
	}
	if deltaCount != 1 {
		t.Errorf("expected exactly 1 message_delta, got %d", deltaCount)
	} else {
		fmt.Println("✓ 无重复 message_delta")
	}
}

// TestScenario8_NonStream_ComplexToolUseChain tests a complex response with
// multiple tool_use blocks (tool chain) where stop_reason was incorrectly
// set to "end_turn" by the translator.
func TestScenario8_NonStream_ComplexToolUseChain(t *testing.T) {
	fmt.Println("============================================================")
	fmt.Println("场景 8 复杂测试: 多 tool_use 块链式调用 + stop_reason 修正")
	fmt.Println("============================================================")

	// Simulate a complex tool-chain response where the upstream returned
	// finish_reason="stop" (mapped to "end_turn") but the content contains
	// TWO tool_use blocks (e.g. search → then execute).
	input := []byte(`{
		"type":"message",
		"role":"assistant",
		"model":"claude-3-5-sonnet",
		"content":[
			{"type":"thinking","thinking":"I should search first, then execute the result."},
			{"type":"tool_use","id":"toolu_a","name":"web_search","input":{"query":"rust async"}},
			{"type":"text","text":"Found results. Now executing."},
			{"type":"tool_use","id":"toolu_b","name":"run_code","input":{"code":"println!(\"hello\")"}}
		],
		"stop_reason":"end_turn",
		"stop_sequence":null,
		"usage":{"input_tokens":100,"output_tokens":80}
	}`)

	output := NormalizeNonStreamContentOrder(input)

	fmt.Println("--- 输入 content 类型顺序 ---")
	fmt.Println("  thinking, tool_use, text, tool_use")
	fmt.Println("--- 输出 content 类型顺序 ---")
	for _, b := range gjson.GetBytes(output, "content").Array() {
		fmt.Printf("  %s (id=%s)\n", b.Get("type").String(), b.Get("id").String())
	}

	// Verify content ordering: text → thinking → tool_use → tool_use
	blocks := gjson.GetBytes(output, "content").Array()
	if len(blocks) != 4 {
		t.Fatalf("expected 4 content blocks, got %d", len(blocks))
	}
	expectedTypes := []string{"text", "thinking", "tool_use", "tool_use"}
	for i, et := range expectedTypes {
		if got := blocks[i].Get("type").String(); got != et {
			t.Errorf("content[%d].type = %q, want %q", i, got, et)
		}
	}
	fmt.Println("✓ content 顺序: text → thinking → tool_use → tool_use")

	// Verify stop_reason corrected to "tool_use"
	sr := gjson.GetBytes(output, "stop_reason").String()
	fmt.Printf("stop_reason: %q (输入 end_turn → 输出 %s)\n", sr, sr)
	if sr != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", sr)
	}
	fmt.Println("✓ stop_reason 从 end_turn 修正为 tool_use")

	// Verify usage preserved
	usage := gjson.GetBytes(output, "usage")
	if got := usage.Get("input_tokens").Int(); got != 100 {
		t.Errorf("usage.input_tokens = %d, want 100", got)
	}
	if got := usage.Get("output_tokens").Int(); got != 80 {
		t.Errorf("usage.output_tokens = %d, want 80", got)
	}
	fmt.Println("✓ usage 字段保留不变 (input=100, output=80)")
}

// visualizeSSEFrame renders an SSE frame in a readable format.
func visualizeSSEFrame(frame []byte) string {
	var sb strings.Builder
	lines := bytes.Split(frame, []byte("\n"))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		sb.WriteString("    ")
		sb.Write(line)
		sb.WriteString("\n")
	}
	return sb.String()
}

// parseFramesForTest parses SSE bytes into readable event strings.
func parseFramesForTest(data []byte) []string {
	frames := bytes.Split(data, []byte("\n\n"))
	var events []string
	for _, frame := range frames {
		frame = bytes.TrimSpace(frame)
		if len(frame) == 0 {
			continue
		}
		var eventType string
		var payload string
		for _, line := range bytes.Split(frame, []byte("\n")) {
			line = bytes.TrimRight(line, "\r")
			if bytes.HasPrefix(line, []byte("event:")) {
				eventType = strings.TrimSpace(string(line[6:]))
			}
			if bytes.HasPrefix(line, []byte("data:")) {
				payload = strings.TrimSpace(string(line[5:]))
			}
		}
		if eventType != "" {
			events = append(events, fmt.Sprintf("event: %s | data: %s\n", eventType, payload))
		}
	}
	return events
}
