package chat_completions

import (
	"context"
	"testing"

	"github.com/tidwall/gjson"
)

func translateCodexStreamEvents(t *testing.T, events ...string) [][]byte {
	t.Helper()

	ctx := context.Background()
	var param any
	var chunks [][]byte
	for _, event := range events {
		out := ConvertCodexResponseToOpenAI(ctx, "gpt-5.4", nil, nil, []byte(event), &param)
		chunks = append(chunks, out...)
	}
	return chunks
}

func TestConvertCodexResponseToOpenAI_StreamSetsModelFromResponseCreated(t *testing.T) {
	ctx := context.Background()
	var param any

	modelName := "gpt-5.3-codex"

	out := ConvertCodexResponseToOpenAI(ctx, modelName, nil, nil, []byte(`data: {"type":"response.created","response":{"id":"resp_123","created_at":1700000000,"model":"gpt-5.3-codex"}}`), &param)
	if len(out) != 0 {
		t.Fatalf("expected no output for response.created, got %d chunks", len(out))
	}

	out = ConvertCodexResponseToOpenAI(ctx, modelName, nil, nil, []byte(`data: {"type":"response.output_text.delta","delta":"hello"}`), &param)
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}

	gotModel := gjson.GetBytes(out[0], "model").String()
	if gotModel != modelName {
		t.Fatalf("expected model %q, got %q", modelName, gotModel)
	}
}

func TestConvertCodexResponseToOpenAI_FirstChunkUsesRequestModelName(t *testing.T) {
	ctx := context.Background()
	var param any

	modelName := "gpt-5.3-codex"

	out := ConvertCodexResponseToOpenAI(ctx, modelName, nil, nil, []byte(`data: {"type":"response.output_text.delta","delta":"hello"}`), &param)
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}

	gotModel := gjson.GetBytes(out[0], "model").String()
	if gotModel != modelName {
		t.Fatalf("expected model %q, got %q", modelName, gotModel)
	}
}

func TestConvertCodexResponseToOpenAI_ToolCallChunkOmitsNullContentFields(t *testing.T) {
	ctx := context.Background()
	var param any

	out := ConvertCodexResponseToOpenAI(ctx, "gpt-5.4", nil, nil, []byte(`data: {"type":"response.output_item.added","item":{"type":"function_call","call_id":"call_123","name":"websearch"}}`), &param)
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}

	if gjson.GetBytes(out[0], "choices.0.delta.content").Exists() {
		t.Fatalf("expected content to be omitted, got %s", string(out[0]))
	}
	if gjson.GetBytes(out[0], "choices.0.delta.reasoning_content").Exists() {
		t.Fatalf("expected reasoning_content to be omitted, got %s", string(out[0]))
	}
	if !gjson.GetBytes(out[0], "choices.0.delta.tool_calls").Exists() {
		t.Fatalf("expected tool_calls to exist, got %s", string(out[0]))
	}
}

func TestConvertCodexResponseToOpenAI_ToolCallArgumentsDeltaOmitsNullContentFields(t *testing.T) {
	ctx := context.Background()
	var param any

	out := ConvertCodexResponseToOpenAI(ctx, "gpt-5.4", nil, nil, []byte(`data: {"type":"response.output_item.added","item":{"type":"function_call","call_id":"call_123","name":"websearch"}}`), &param)
	if len(out) != 1 {
		t.Fatalf("expected tool call announcement chunk, got %d", len(out))
	}

	out = ConvertCodexResponseToOpenAI(ctx, "gpt-5.4", nil, nil, []byte(`data: {"type":"response.function_call_arguments.delta","delta":"{\"query\":\"OpenAI\"}"}`), &param)
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}

	if gjson.GetBytes(out[0], "choices.0.delta.content").Exists() {
		t.Fatalf("expected content to be omitted, got %s", string(out[0]))
	}
	if gjson.GetBytes(out[0], "choices.0.delta.reasoning_content").Exists() {
		t.Fatalf("expected reasoning_content to be omitted, got %s", string(out[0]))
	}
	if !gjson.GetBytes(out[0], "choices.0.delta.tool_calls.0.function.arguments").Exists() {
		t.Fatalf("expected tool call arguments delta to exist, got %s", string(out[0]))
	}
}

func TestConvertCodexResponseToOpenAI_CustomToolCallStreamsInputDeltas(t *testing.T) {
	chunks := translateCodexStreamEvents(t,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"apply_patch","input":""}}`,
		`data: {"type":"response.custom_tool_call_input.delta","output_index":0,"item_id":"ctc_1","delta":"*** Begin "}`,
		`data: {"type":"response.custom_tool_call_input.delta","output_index":0,"item_id":"ctc_1","delta":"Patch"}`,
		`data: {"type":"response.custom_tool_call_input.done","output_index":0,"item_id":"ctc_1","input":"*** Begin Patch"}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"apply_patch","input":"*** Begin Patch"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","created_at":1700000000,"model":"gpt-5.4","status":"completed"}}`,
	)
	if len(chunks) != 4 {
		t.Fatalf("expected announcement, two argument deltas, and completed chunk, got %d chunks: %q", len(chunks), chunks)
	}

	announcement := chunks[0]
	if got := gjson.GetBytes(announcement, "choices.0.delta.tool_calls.0.id").String(); got != "call_1" {
		t.Fatalf("expected tool call id call_1, got %q; chunk=%s", got, string(announcement))
	}
	if got := gjson.GetBytes(announcement, "choices.0.delta.tool_calls.0.function.name").String(); got != "apply_patch" {
		t.Fatalf("expected tool name apply_patch, got %q; chunk=%s", got, string(announcement))
	}
	if got := gjson.GetBytes(announcement, "choices.0.delta.tool_calls.0.function.arguments").String(); got != "" {
		t.Fatalf("expected empty announced arguments, got %q; chunk=%s", got, string(announcement))
	}

	args := gjson.GetBytes(chunks[1], "choices.0.delta.tool_calls.0.function.arguments").String() +
		gjson.GetBytes(chunks[2], "choices.0.delta.tool_calls.0.function.arguments").String()
	if args != "*** Begin Patch" {
		t.Fatalf("expected reconstructed input %q, got %q", "*** Begin Patch", args)
	}
	if got := gjson.GetBytes(chunks[3], "choices.0.finish_reason").String(); got != "tool_calls" {
		t.Fatalf("expected finish_reason tool_calls, got %q; chunk=%s", got, string(chunks[3]))
	}
}

func TestConvertCodexResponseToOpenAI_CustomToolCallInputDoneEmitsFullInputWithoutDeltas(t *testing.T) {
	chunks := translateCodexStreamEvents(t,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"apply_patch","input":""}}`,
		`data: {"type":"response.custom_tool_call_input.done","output_index":0,"item_id":"ctc_1","input":"*** Begin Patch"}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"apply_patch","input":"*** Begin Patch"}}`,
	)
	if len(chunks) != 2 {
		t.Fatalf("expected announcement and one full input chunk, got %d chunks: %q", len(chunks), chunks)
	}
	if got := gjson.GetBytes(chunks[1], "choices.0.delta.tool_calls.0.function.arguments").String(); got != "*** Begin Patch" {
		t.Fatalf("expected full input %q, got %q; chunk=%s", "*** Begin Patch", got, string(chunks[1]))
	}
}

func TestConvertCodexResponseToOpenAI_CustomToolCallOutputItemDoneBackfillsArgumentsOnlyAfterAnnouncement(t *testing.T) {
	chunks := translateCodexStreamEvents(t,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"apply_patch","input":""}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"apply_patch","input":"*** Begin Patch"}}`,
	)
	if len(chunks) != 2 {
		t.Fatalf("expected announcement and output_item.done argument backfill, got %d chunks: %q", len(chunks), chunks)
	}

	backfill := chunks[1]
	if got := gjson.GetBytes(backfill, "choices.0.delta.tool_calls.0.function.arguments").String(); got != "*** Begin Patch" {
		t.Fatalf("expected backfilled input %q, got %q; chunk=%s", "*** Begin Patch", got, string(backfill))
	}
	if gjson.GetBytes(backfill, "choices.0.delta.tool_calls.0.id").Exists() {
		t.Fatalf("expected arguments-only backfill without id; chunk=%s", string(backfill))
	}
	if gjson.GetBytes(backfill, "choices.0.delta.tool_calls.0.function.name").Exists() {
		t.Fatalf("expected arguments-only backfill without name; chunk=%s", string(backfill))
	}
}

func TestConvertCodexResponseToOpenAI_CustomToolCallOutputItemDoneEmitsCompleteCallWithoutAdded(t *testing.T) {
	chunks := translateCodexStreamEvents(t,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"apply_patch","input":"*** Begin Patch"}}`,
	)
	if len(chunks) != 1 {
		t.Fatalf("expected one complete fallback chunk, got %d chunks: %q", len(chunks), chunks)
	}

	chunk := chunks[0]
	if got := gjson.GetBytes(chunk, "choices.0.delta.tool_calls.0.id").String(); got != "call_1" {
		t.Fatalf("expected tool call id call_1, got %q; chunk=%s", got, string(chunk))
	}
	if got := gjson.GetBytes(chunk, "choices.0.delta.tool_calls.0.function.name").String(); got != "apply_patch" {
		t.Fatalf("expected tool name apply_patch, got %q; chunk=%s", got, string(chunk))
	}
	if got := gjson.GetBytes(chunk, "choices.0.delta.tool_calls.0.function.arguments").String(); got != "*** Begin Patch" {
		t.Fatalf("expected full input %q, got %q; chunk=%s", "*** Begin Patch", got, string(chunk))
	}
}

func TestConvertCodexResponseToOpenAI_CustomToolCallAddedIgnoresUnexpectedInputBeforeDeltas(t *testing.T) {
	chunks := translateCodexStreamEvents(t,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"apply_patch","input":"stale input"}}`,
		`data: {"type":"response.custom_tool_call_input.delta","output_index":0,"item_id":"ctc_1","delta":"fresh input"}`,
		`data: {"type":"response.custom_tool_call_input.done","output_index":0,"item_id":"ctc_1","input":"fresh input"}`,
	)
	if len(chunks) != 2 {
		t.Fatalf("expected announcement and one input delta, got %d chunks: %q", len(chunks), chunks)
	}
	if got := gjson.GetBytes(chunks[0], "choices.0.delta.tool_calls.0.function.arguments").String(); got != "" {
		t.Fatalf("expected added chunk to announce empty arguments, got %q; chunk=%s", got, string(chunks[0]))
	}
	if got := gjson.GetBytes(chunks[1], "choices.0.delta.tool_calls.0.function.arguments").String(); got != "fresh input" {
		t.Fatalf("expected fresh delta input, got %q; chunk=%s", got, string(chunks[1]))
	}
}

func TestConvertCodexResponseToOpenAI_FunctionCallArgumentsDoneDoesNotDuplicateDelta(t *testing.T) {
	chunks := translateCodexStreamEvents(t,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":""}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{\"q\":\"x\"}"}`,
		`data: {"type":"response.function_call_arguments.done","output_index":0,"item_id":"fc_1","arguments":"{\"q\":\"x\"}"}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"x\"}"}}`,
	)
	if len(chunks) != 2 {
		t.Fatalf("expected announcement and one arguments delta, got %d chunks: %q", len(chunks), chunks)
	}
	if got := gjson.GetBytes(chunks[1], "choices.0.delta.tool_calls.0.function.arguments").String(); got != `{"q":"x"}` {
		t.Fatalf("expected function arguments delta, got %q; chunk=%s", got, string(chunks[1]))
	}
}

func TestConvertCodexResponseToOpenAI_MultipleSequentialCustomToolCallsResetArgumentState(t *testing.T) {
	chunks := translateCodexStreamEvents(t,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"apply_patch","input":""}}`,
		`data: {"type":"response.custom_tool_call_input.done","output_index":0,"item_id":"ctc_1","input":"first input"}`,
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"custom_tool_call","id":"ctc_2","call_id":"call_2","name":"apply_patch","input":""}}`,
		`data: {"type":"response.output_item.done","output_index":1,"item":{"type":"custom_tool_call","id":"ctc_2","call_id":"call_2","name":"apply_patch","input":"second input"}}`,
	)
	if len(chunks) != 4 {
		t.Fatalf("expected two announcements and two input chunks, got %d chunks: %q", len(chunks), chunks)
	}

	if got := gjson.GetBytes(chunks[0], "choices.0.delta.tool_calls.0.index").Int(); got != 0 {
		t.Fatalf("expected first tool index 0, got %d; chunk=%s", got, string(chunks[0]))
	}
	if got := gjson.GetBytes(chunks[1], "choices.0.delta.tool_calls.0.index").Int(); got != 0 {
		t.Fatalf("expected first input index 0, got %d; chunk=%s", got, string(chunks[1]))
	}
	if got := gjson.GetBytes(chunks[2], "choices.0.delta.tool_calls.0.index").Int(); got != 1 {
		t.Fatalf("expected second tool index 1, got %d; chunk=%s", got, string(chunks[2]))
	}
	if got := gjson.GetBytes(chunks[3], "choices.0.delta.tool_calls.0.index").Int(); got != 1 {
		t.Fatalf("expected second input index 1, got %d; chunk=%s", got, string(chunks[3]))
	}
	if got := gjson.GetBytes(chunks[3], "choices.0.delta.tool_calls.0.function.arguments").String(); got != "second input" {
		t.Fatalf("expected second input backfill, got %q; chunk=%s", got, string(chunks[3]))
	}
}

func TestConvertCodexResponseToOpenAI_StreamPartialImageEmitsDeltaImages(t *testing.T) {
	ctx := context.Background()
	var param any

	chunk := []byte(`data: {"type":"response.image_generation_call.partial_image","item_id":"ig_123","output_format":"png","partial_image_b64":"aGVsbG8=","partial_image_index":0}`)

	out := ConvertCodexResponseToOpenAI(ctx, "gpt-5.4", nil, nil, chunk, &param)
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}

	gotURL := gjson.GetBytes(out[0], "choices.0.delta.images.0.image_url.url").String()
	if gotURL != "data:image/png;base64,aGVsbG8=" {
		t.Fatalf("expected image url %q, got %q; chunk=%s", "data:image/png;base64,aGVsbG8=", gotURL, string(out[0]))
	}

	out = ConvertCodexResponseToOpenAI(ctx, "gpt-5.4", nil, nil, chunk, &param)
	if len(out) != 0 {
		t.Fatalf("expected duplicate image chunk to be suppressed, got %d", len(out))
	}
}

func TestConvertCodexResponseToOpenAI_StreamImageGenerationCallDoneEmitsDeltaImages(t *testing.T) {
	ctx := context.Background()
	var param any

	out := ConvertCodexResponseToOpenAI(ctx, "gpt-5.4", nil, nil, []byte(`data: {"type":"response.image_generation_call.partial_image","item_id":"ig_123","output_format":"png","partial_image_b64":"aGVsbG8=","partial_image_index":0}`), &param)
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}

	out = ConvertCodexResponseToOpenAI(ctx, "gpt-5.4", nil, nil, []byte(`data: {"type":"response.output_item.done","item":{"id":"ig_123","type":"image_generation_call","output_format":"png","result":"aGVsbG8="}}`), &param)
	if len(out) != 0 {
		t.Fatalf("expected output_item.done to be suppressed when identical to last partial image, got %d", len(out))
	}

	out = ConvertCodexResponseToOpenAI(ctx, "gpt-5.4", nil, nil, []byte(`data: {"type":"response.output_item.done","item":{"id":"ig_123","type":"image_generation_call","output_format":"jpeg","result":"Ymll"}}`), &param)
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}

	gotURL := gjson.GetBytes(out[0], "choices.0.delta.images.0.image_url.url").String()
	if gotURL != "data:image/jpeg;base64,Ymll" {
		t.Fatalf("expected image url %q, got %q; chunk=%s", "data:image/jpeg;base64,Ymll", gotURL, string(out[0]))
	}
}

func TestConvertCodexResponseToOpenAI_NonStreamImageGenerationCallAddsMessageImages(t *testing.T) {
	ctx := context.Background()

	raw := []byte(`{"type":"response.completed","response":{"id":"resp_123","created_at":1700000000,"model":"gpt-5.4","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2},"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]},{"type":"image_generation_call","output_format":"png","result":"aGVsbG8="}]}}`)
	out := ConvertCodexResponseToOpenAINonStream(ctx, "gpt-5.4", nil, nil, raw, nil)

	gotURL := gjson.GetBytes(out, "choices.0.message.images.0.image_url.url").String()
	if gotURL != "data:image/png;base64,aGVsbG8=" {
		t.Fatalf("expected image url %q, got %q; chunk=%s", "data:image/png;base64,aGVsbG8=", gotURL, string(out))
	}
}

func TestConvertCodexResponseToOpenAI_StreamForwardsCacheWriteTokens(t *testing.T) {
	ctx := context.Background()
	var param any

	// Seed response.created so response.completed can reuse response metadata.
	_ = ConvertCodexResponseToOpenAI(ctx, "gpt-5.4", nil, nil, []byte(`data: {"type":"response.created","response":{"id":"resp_123","created_at":1700000000,"model":"gpt-5.4"}}`), &param)

	chunk := []byte(`data: {"type":"response.completed","response":{"id":"resp_123","created_at":1700000000,"model":"gpt-5.4","usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":30,"cache_write_tokens":40},"output_tokens_details":{"reasoning_tokens":5}}}}`)
	out := ConvertCodexResponseToOpenAI(ctx, "gpt-5.4", nil, nil, chunk, &param)
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}

	assertUsageMapping(t, out[0], 40, true)
}

func TestConvertCodexResponseToOpenAI_StreamOmitsMissingCacheWriteTokens(t *testing.T) {
	ctx := context.Background()
	var param any

	_ = ConvertCodexResponseToOpenAI(ctx, "gpt-5.4", nil, nil, []byte(`data: {"type":"response.created","response":{"id":"resp_123","created_at":1700000000,"model":"gpt-5.4"}}`), &param)

	chunk := []byte(`data: {"type":"response.completed","response":{"id":"resp_123","created_at":1700000000,"model":"gpt-5.4","usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":30},"output_tokens_details":{"reasoning_tokens":5}}}}`)
	out := ConvertCodexResponseToOpenAI(ctx, "gpt-5.4", nil, nil, chunk, &param)
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}

	assertUsageMapping(t, out[0], 0, false)
}

func TestConvertCodexResponseToOpenAI_StreamPreservesExplicitZeroCacheWriteTokens(t *testing.T) {
	ctx := context.Background()
	var param any

	_ = ConvertCodexResponseToOpenAI(ctx, "gpt-5.4", nil, nil, []byte(`data: {"type":"response.created","response":{"id":"resp_123","created_at":1700000000,"model":"gpt-5.4"}}`), &param)

	chunk := []byte(`data: {"type":"response.completed","response":{"id":"resp_123","created_at":1700000000,"model":"gpt-5.4","usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":30,"cache_write_tokens":0},"output_tokens_details":{"reasoning_tokens":5}}}}`)
	out := ConvertCodexResponseToOpenAI(ctx, "gpt-5.4", nil, nil, chunk, &param)
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}

	assertUsageMapping(t, out[0], 0, true)
}

func TestConvertCodexResponseToOpenAI_NonStreamForwardsCacheWriteTokens(t *testing.T) {
	ctx := context.Background()
	raw := []byte(`{"type":"response.completed","response":{"id":"resp_123","created_at":1700000000,"model":"gpt-5.4","status":"completed","usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":30,"cache_write_tokens":40},"output_tokens_details":{"reasoning_tokens":5}},"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}}`)
	out := ConvertCodexResponseToOpenAINonStream(ctx, "gpt-5.4", nil, nil, raw, nil)
	assertUsageMapping(t, out, 40, true)
}

func TestConvertCodexResponseToOpenAI_NonStreamOmitsMissingCacheWriteTokens(t *testing.T) {
	ctx := context.Background()
	raw := []byte(`{"type":"response.completed","response":{"id":"resp_123","created_at":1700000000,"model":"gpt-5.4","status":"completed","usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":30},"output_tokens_details":{"reasoning_tokens":5}},"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}}`)
	out := ConvertCodexResponseToOpenAINonStream(ctx, "gpt-5.4", nil, nil, raw, nil)
	assertUsageMapping(t, out, 0, false)
}

func TestConvertCodexResponseToOpenAI_NonStreamPreservesExplicitZeroCacheWriteTokens(t *testing.T) {
	ctx := context.Background()
	raw := []byte(`{"type":"response.completed","response":{"id":"resp_123","created_at":1700000000,"model":"gpt-5.4","status":"completed","usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":30,"cache_write_tokens":0},"output_tokens_details":{"reasoning_tokens":5}},"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}}`)
	out := ConvertCodexResponseToOpenAINonStream(ctx, "gpt-5.4", nil, nil, raw, nil)
	assertUsageMapping(t, out, 0, true)
}

func assertUsageMapping(t *testing.T, payload []byte, wantCachedCreation int64, expectCachedCreation bool) {
	t.Helper()

	if got := gjson.GetBytes(payload, "usage.prompt_tokens").Int(); got != 100 {
		t.Fatalf("expected prompt_tokens=100, got %d; payload=%s", got, string(payload))
	}
	if got := gjson.GetBytes(payload, "usage.completion_tokens").Int(); got != 20 {
		t.Fatalf("expected completion_tokens=20, got %d; payload=%s", got, string(payload))
	}
	if got := gjson.GetBytes(payload, "usage.total_tokens").Int(); got != 120 {
		t.Fatalf("expected total_tokens=120, got %d; payload=%s", got, string(payload))
	}
	if got := gjson.GetBytes(payload, "usage.prompt_tokens_details.cached_tokens").Int(); got != 30 {
		t.Fatalf("expected cached_tokens=30, got %d; payload=%s", got, string(payload))
	}
	if got := gjson.GetBytes(payload, "usage.completion_tokens_details.reasoning_tokens").Int(); got != 5 {
		t.Fatalf("expected reasoning_tokens=5, got %d; payload=%s", got, string(payload))
	}

	gotCachedCreation := gjson.GetBytes(payload, "usage.prompt_tokens_details.cached_creation_tokens")
	if expectCachedCreation {
		if !gotCachedCreation.Exists() {
			t.Fatalf("expected cached_creation_tokens to exist, payload=%s", string(payload))
		}
		if gotCachedCreation.Int() != wantCachedCreation {
			t.Fatalf("expected cached_creation_tokens=%d, got %d; payload=%s", wantCachedCreation, gotCachedCreation.Int(), string(payload))
		}
		return
	}
	if gotCachedCreation.Exists() {
		t.Fatalf("expected cached_creation_tokens to be omitted, payload=%s", string(payload))
	}
}

func TestConvertCodexResponseToOpenAI_NonStreamMultiMessageEmptyTrailingKeepsContent(t *testing.T) {
	ctx := context.Background()
	raw := []byte(`{"type":"response.completed","response":{"id":"resp_1","created_at":1700000000,"model":"gpt-5.5","status":"completed","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15},"output":[` +
		`{"type":"reasoning","summary":[{"type":"summary_text","text":"thinking"}]},` +
		`{"type":"message","content":[{"type":"output_text","text":"the real answer"}]},` +
		`{"type":"reasoning","summary":[{"type":"summary_text","text":"thinking again"}]},` +
		`{"type":"message","content":[{"type":"output_text","text":""}]}` +
		`]}}`)
	out := ConvertCodexResponseToOpenAINonStream(ctx, "gpt-5.5", nil, nil, raw, nil)

	got := gjson.GetBytes(out, "choices.0.message.content")
	if !got.Exists() || got.Type == gjson.Null {
		t.Fatalf("content was dropped to null by trailing empty message; resp=%s", string(out))
	}
	if got.String() != "the real answer" {
		t.Fatalf("expected content %q, got %q; resp=%s", "the real answer", got.String(), string(out))
	}
}
