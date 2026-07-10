package common

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// sseEvent 从 SSE 帧中提取事件名。
func sseEvent(frame []byte) string {
	const prefix = "event: "
	idx := bytes.Index(frame, []byte(prefix))
	if idx < 0 {
		return ""
	}
	rest := frame[idx+len(prefix):]
	end := bytes.IndexByte(rest, '\n')
	if end < 0 {
		return string(rest)
	}
	return string(rest[:end])
}

// ssePayload 从 SSE 帧中提取 data: 之后的有效载荷。
func ssePayload(frame []byte) []byte {
	const prefix = "data: "
	idx := bytes.Index(frame, []byte(prefix))
	if idx < 0 {
		return nil
	}
	return frame[idx+len(prefix):]
}

// TestInteractionsSSEBuilder_ByteEquivalence 验证 9 类事件输出与 golden 字节串匹配。
func TestInteractionsSSEBuilder_ByteEquivalence(t *testing.T) {
	// created（自动补发 status_update）
	b := &InteractionsSSEBuilder{ID: "interaction_test", Model: "gpt-4"}
	out := b.AppendCreated(nil, "gpt-4")
	if len(out) != 2 {
		t.Fatalf("expected 2 frames (created+status_update), got %d", len(out))
	}
	wantCreated := []byte(`event: interaction.created` + "\n" +
		`data: {"interaction":{"id":"interaction_test","status":"in_progress","object":"interaction","model":"gpt-4"},"event_type":"interaction.created"}`)
	if !bytes.Equal(out[0], wantCreated) {
		t.Fatalf("created mismatch:\n got: %s\nwant: %s", out[0], wantCreated)
	}
	wantStatus := []byte(`event: interaction.status_update` + "\n" +
		`data: {"interaction_id":"interaction_test","status":"in_progress","event_type":"interaction.status_update"}`)
	if !bytes.Equal(out[1], wantStatus) {
		t.Fatalf("status_update mismatch:\n got: %s\nwant: %s", out[1], wantStatus)
	}

	// step.start（model_output，index=0）
	out = b.AppendStepStart(nil, StepStartParams{Type: "model_output"})
	wantStepStart := []byte(`event: step.start` + "\n" +
		`data: {"index":0,"step":{"type":"model_output"},"event_type":"step.start"}`)
	if len(out) != 1 || !bytes.Equal(out[0], wantStepStart) {
		t.Fatalf("step.start mismatch:\n got: %s\nwant: %s", out, wantStepStart)
	}

	// text delta
	out = b.AppendTextDelta(nil, "hello")
	wantText := []byte(`event: step.delta` + "\n" +
		`data: {"index":0,"delta":{"text":"hello","type":"text"},"event_type":"step.delta"}`)
	if len(out) != 1 || !bytes.Equal(out[0], wantText) {
		t.Fatalf("text delta mismatch:\n got: %s\nwant: %s", out, wantText)
	}

	// thought_summary delta
	out = b.AppendThoughtDelta(nil, "thinking")
	wantThought := []byte(`event: step.delta` + "\n" +
		`data: {"index":0,"delta":{"content":{"text":"thinking","type":"text"},"type":"thought_summary"},"event_type":"step.delta"}`)
	if len(out) != 1 || !bytes.Equal(out[0], wantThought) {
		t.Fatalf("thought delta mismatch:\n got: %s\nwant: %s", out, wantThought)
	}

	// arguments delta
	out = b.AppendArgumentsDelta(nil, "partial")
	wantArgs := []byte(`event: step.delta` + "\n" +
		`data: {"index":0,"delta":{"arguments":"partial","type":"arguments_delta"},"event_type":"step.delta"}`)
	if len(out) != 1 || !bytes.Equal(out[0], wantArgs) {
		t.Fatalf("arguments delta mismatch:\n got: %s\nwant: %s", out, wantArgs)
	}

	// step.stop（index=0）
	out = b.AppendStepStop(nil)
	wantStop := []byte(`event: step.stop` + "\n" +
		`data: {"index":0,"event_type":"step.stop"}`)
	if len(out) != 1 || !bytes.Equal(out[0], wantStop) {
		t.Fatalf("step.stop mismatch:\n got: %s\nwant: %s", out, wantStop)
	}

	// done
	out = b.AppendDone(nil)
	wantDone := []byte(`event: done` + "\n" + `data: [DONE]`)
	if len(out) != 1 || !bytes.Equal(out[0], wantDone) {
		t.Fatalf("done mismatch:\n got: %s\nwant: %s", out, wantDone)
	}

	// completed（时间戳非确定性，从实际输出中取值后重建 golden）
	b2 := &InteractionsSSEBuilder{ID: "interaction_test", Model: "gpt-4"}
	out = b2.AppendCompleted(nil, "gpt-4", gjson.Result{})
	if len(out) != 3 {
		t.Fatalf("expected 3 frames (created+status_update+completed), got %d", len(out))
	}
	if sseEvent(out[0]) != "interaction.created" {
		t.Fatalf("expected first frame interaction.created, got %s", sseEvent(out[0]))
	}
	completedFrame := out[2]
	payload := ssePayload(completedFrame)
	created := gjson.GetBytes(payload, "interaction.created").String()
	updated := gjson.GetBytes(payload, "interaction.updated").String()
	wantCompleted := []byte(`event: interaction.completed` + "\n" +
		`data: {"interaction":{"id":"interaction_test","status":"completed","usage":{},"created":"` + created +
		`","updated":"` + updated + `","service_tier":"standard","object":"interaction","model":"gpt-4"},"event_type":"interaction.completed"}`)
	if !bytes.Equal(completedFrame, wantCompleted) {
		t.Fatalf("completed mismatch:\n got: %s\nwant: %s", completedFrame, wantCompleted)
	}
}

// TestInteractionsSSEBuilder_Idempotent 验证幂等性。
func TestInteractionsSSEBuilder_Idempotent(t *testing.T) {
	b := &InteractionsSSEBuilder{ID: "interaction_test", Model: "gpt-4"}
	out := b.AppendCreated(nil, "gpt-4")
	out = b.AppendCreated(out, "gpt-4") // 幂等
	out = b.AppendStatusUpdate(out)     // 幂等
	if len(out) != 2 {
		t.Fatalf("expected 2 frames after idempotent created/status_update, got %d", len(out))
	}
	out = b.AppendCompleted(out, "gpt-4", gjson.Result{})
	out = b.AppendCompleted(out, "gpt-4", gjson.Result{}) // 幂等
	if len(out) != 3 {
		t.Fatalf("expected 3 frames after idempotent completed, got %d", len(out))
	}
	out = b.AppendDone(out)
	out = b.AppendDone(out) // 幂等
	if len(out) != 4 {
		t.Fatalf("expected 4 frames after idempotent done, got %d", len(out))
	}

	// StepStop 在 ActiveStepOpen=false 时不发送
	b2 := &InteractionsSSEBuilder{ID: "interaction_test", Model: "gpt-4"}
	b2.AppendCreated(nil, "gpt-4")
	if got := b2.AppendStepStop(nil); len(got) != 0 {
		t.Fatalf("expected 0 frames for step.stop with no active step, got %d", len(got))
	}
}

// TestInteractionsSSEBuilder_StepIndexSequence 验证 StepIndex 在 start 时递增。
func TestInteractionsSSEBuilder_StepIndexSequence(t *testing.T) {
	b := &InteractionsSSEBuilder{ID: "interaction_test", Model: "gpt-4"}
	b.AppendCreated(nil, "gpt-4")
	var out [][]byte
	out = b.AppendStepStart(out, StepStartParams{Type: "model_output"})
	out = b.AppendStepStop(out)
	out = b.AppendStepStart(out, StepStartParams{Type: "thought"})
	out = b.AppendStepStop(out)
	if len(out) != 4 {
		t.Fatalf("expected 4 frames, got %d", len(out))
	}
	if idx := gjson.GetBytes(ssePayload(out[0]), "index").Int(); idx != 0 {
		t.Fatalf("first step.start index = %d, want 0", idx)
	}
	if idx := gjson.GetBytes(ssePayload(out[1]), "index").Int(); idx != 0 {
		t.Fatalf("first step.stop index = %d, want 0", idx)
	}
	if idx := gjson.GetBytes(ssePayload(out[2]), "index").Int(); idx != 1 {
		t.Fatalf("second step.start index = %d, want 1", idx)
	}
	if idx := gjson.GetBytes(ssePayload(out[3]), "index").Int(); idx != 1 {
		t.Fatalf("second step.stop index = %d, want 1", idx)
	}
}

// TestInteractionsSSEBuilder_EnsureStepSameType 类型相同时不重复发送 step.start。
func TestInteractionsSSEBuilder_EnsureStepSameType(t *testing.T) {
	b := &InteractionsSSEBuilder{ID: "interaction_test", Model: "gpt-4"}
	var out [][]byte
	out = b.AppendStepStart(out, StepStartParams{Type: "model_output"})
	out = b.EnsureStep(out, "gpt-4", StepStartParams{Type: "model_output"})
	startCount := 0
	for _, frame := range out {
		if sseEvent(frame) == "step.start" {
			startCount++
		}
	}
	if startCount != 1 {
		t.Fatalf("expected 1 step.start frame, got %d", startCount)
	}
}

// TestInteractionsSSEBuilder_EnsureStepDifferentType 类型不同时先 stop 再 start。
func TestInteractionsSSEBuilder_EnsureStepDifferentType(t *testing.T) {
	b := &InteractionsSSEBuilder{ID: "interaction_test", Model: "gpt-4"}
	var out [][]byte
	out = b.AppendStepStart(out, StepStartParams{Type: "model_output"})
	out = b.EnsureStep(out, "gpt-4", StepStartParams{Type: "thought"})
	startCount, stopCount := 0, 0
	for _, frame := range out {
		switch sseEvent(frame) {
		case "step.start":
			startCount++
		case "step.stop":
			stopCount++
		}
	}
	if startCount != 2 {
		t.Fatalf("expected 2 step.start frames, got %d", startCount)
	}
	if stopCount != 1 {
		t.Fatalf("expected 1 step.stop frame, got %d", stopCount)
	}
	lastStart := ""
	for i := len(out) - 1; i >= 0; i-- {
		if sseEvent(out[i]) == "step.start" {
			lastStart = gjson.GetBytes(ssePayload(out[i]), "step.type").String()
			break
		}
	}
	if lastStart != "thought" {
		t.Fatalf("expected last step.start type thought, got %s", lastStart)
	}
}

// TestInteractionsSSEBuilder_SetUsageNil SetUsage 为 nil 时保留空 usage 对象。
func TestInteractionsSSEBuilder_SetUsageNil(t *testing.T) {
	b := &InteractionsSSEBuilder{ID: "interaction_test", Model: "gpt-4"}
	out := b.AppendCompleted(nil, "gpt-4", gjson.Result{})
	var payload []byte
	for _, frame := range out {
		if sseEvent(frame) == "interaction.completed" {
			payload = ssePayload(frame)
			break
		}
	}
	if payload == nil {
		t.Fatal("missing interaction.completed event")
	}
	if got := gjson.GetBytes(payload, "interaction.usage").Raw; got != "{}" {
		t.Fatalf("expected empty usage {}, got %s", got)
	}
}

// TestInteractionsSSEBuilder_SetUsageInjected SetUsage 非 nil 时调用注入函数。
func TestInteractionsSSEBuilder_SetUsageInjected(t *testing.T) {
	called := false
	b := &InteractionsSSEBuilder{
		ID:    "interaction_test",
		Model: "gpt-4",
		SetUsage: func(payload []byte, path string, usage gjson.Result) []byte {
			called = true
			if path != "interaction.usage" {
				t.Fatalf("expected path interaction.usage, got %s", path)
			}
			payload, _ = sjson.SetBytes(payload, path+".input_tokens", 42)
			return payload
		},
	}
	out := b.AppendCompleted(nil, "gpt-4", gjson.Result{})
	if !called {
		t.Fatal("SetUsage was not called")
	}
	var payload []byte
	for _, frame := range out {
		if sseEvent(frame) == "interaction.completed" {
			payload = ssePayload(frame)
			break
		}
	}
	if got := gjson.GetBytes(payload, "interaction.usage.input_tokens").Int(); got != 42 {
		t.Fatalf("expected input_tokens 42, got %d", got)
	}
}

// TestInteractionsSSEBuilder_CompletedAutoCreated 未调用 AppendCreated 时由 AppendCompleted 自动补发。
func TestInteractionsSSEBuilder_CompletedAutoCreated(t *testing.T) {
	b := &InteractionsSSEBuilder{ID: "interaction_test", Model: "gpt-4"}
	out := b.AppendCompleted(nil, "gpt-4", gjson.Result{})
	if len(out) != 3 {
		t.Fatalf("expected 3 frames (created, status_update, completed), got %d", len(out))
	}
	if sseEvent(out[0]) != "interaction.created" {
		t.Fatalf("expected first event interaction.created, got %s", sseEvent(out[0]))
	}
	if sseEvent(out[1]) != "interaction.status_update" {
		t.Fatalf("expected second event interaction.status_update, got %s", sseEvent(out[1]))
	}
	if sseEvent(out[2]) != "interaction.completed" {
		t.Fatalf("expected third event interaction.completed, got %s", sseEvent(out[2]))
	}
}

// TestInteractionsSSEBuilder_StepStartExtraMerge Extra 字段逐字段合并到 step 对象。
func TestInteractionsSSEBuilder_StepStartExtraMerge(t *testing.T) {
	b := &InteractionsSSEBuilder{ID: "interaction_test", Model: "gpt-4"}
	out := b.AppendStepStart(nil, StepStartParams{
		Type:  "thought",
		Extra: []byte(`{"signature":"abc"}`),
	})
	if len(out) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(out))
	}
	step := gjson.GetBytes(ssePayload(out[0]), "step")
	if step.Get("type").String() != "thought" {
		t.Fatalf("expected step.type thought, got %s", step.Get("type").String())
	}
	if step.Get("signature").String() != "abc" {
		t.Fatalf("expected step.signature abc, got %s", step.Get("signature").String())
	}
}

// TestInteractionsSSEBuilder_FunctionCallStep function_call 步骤包含 id/call_id/name/arguments。
func TestInteractionsSSEBuilder_FunctionCallStep(t *testing.T) {
	b := &InteractionsSSEBuilder{ID: "interaction_test", Model: "gpt-4"}
	out := b.AppendStepStart(nil, StepStartParams{
		Type:   "function_call",
		CallID: "call_0",
		Name:   "get_weather",
	})
	if len(out) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(out))
	}
	step := gjson.GetBytes(ssePayload(out[0]), "step")
	if step.Get("type").String() != "function_call" {
		t.Fatalf("expected type function_call, got %s", step.Get("type").String())
	}
	if step.Get("id").String() != "call_0" {
		t.Fatalf("expected id call_0, got %s", step.Get("id").String())
	}
	if step.Get("call_id").String() != "call_0" {
		t.Fatalf("expected call_id call_0, got %s", step.Get("call_id").String())
	}
	if step.Get("name").String() != "get_weather" {
		t.Fatalf("expected name get_weather, got %s", step.Get("name").String())
	}
	if got := step.Get("arguments").Raw; got != "{}" {
		t.Fatalf("expected arguments {}, got %s", got)
	}
}

// TestInteractionsSSEBuilder_ResetPreservesSetUsage Reset 保留 SetUsage，其余字段归零。
func TestInteractionsSSEBuilder_ResetPreservesSetUsage(t *testing.T) {
	fn := func(payload []byte, path string, usage gjson.Result) []byte { return payload }
	b := &InteractionsSSEBuilder{
		ID:              "interaction_test",
		Model:           "gpt-4",
		CreatedAt:       1234567890,
		Created:         true,
		StatusUpdated:   true,
		Completed:       true,
		Done:            true,
		ActiveStepOpen:  true,
		ActiveStepType:  "model_output",
		ActiveStepIndex: 5,
		StepIndex:       6,
		SetUsage:        fn,
	}
	b.Reset()
	if b.ID != "" || b.Model != "" || b.CreatedAt != 0 || b.Created || b.StatusUpdated || b.Completed || b.Done ||
		b.ActiveStepOpen || b.ActiveStepType != "" || b.ActiveStepIndex != 0 || b.StepIndex != 0 {
		t.Fatalf("Reset did not zero state fields: %+v", b)
	}
	if b.SetUsage == nil {
		t.Fatal("Reset should preserve SetUsage, got nil")
	}
	if got := b.SetUsage([]byte("p"), "path", gjson.Result{}); !bytes.Equal(got, []byte("p")) {
		t.Fatalf("SetUsage should remain callable after Reset, got %s", got)
	}
}

// TestInteractionsSSEBuilder_IDFallback ID 为空时使用 interaction_<nano> 兜底。
func TestInteractionsSSEBuilder_IDFallback(t *testing.T) {
	b := &InteractionsSSEBuilder{}
	out := b.AppendCreated(nil, "gpt-4")
	id := gjson.GetBytes(ssePayload(out[0]), "interaction.id").String()
	if !strings.HasPrefix(id, "interaction_") {
		t.Fatalf("expected fallback id interaction_<nano>, got %s", id)
	}
	if b.ID != id {
		t.Fatalf("expected b.ID to be updated to %s, got %s", id, b.ID)
	}
}

// TestInteractionsSSEBuilder_ModelFallback b.Model 优先，为空时使用 modelName。
func TestInteractionsSSEBuilder_ModelFallback(t *testing.T) {
	// b.Model 为空时使用 modelName
	b := &InteractionsSSEBuilder{ID: "interaction_test"}
	out := b.AppendCreated(nil, "gpt-4")
	if got := gjson.GetBytes(ssePayload(out[0]), "interaction.model").String(); got != "gpt-4" {
		t.Fatalf("expected model gpt-4 from modelName, got %s", got)
	}
	// b.Model 非空时优先使用
	b2 := &InteractionsSSEBuilder{ID: "interaction_test", Model: "claude-3"}
	out2 := b2.AppendCreated(nil, "gpt-4")
	if got := gjson.GetBytes(ssePayload(out2[0]), "interaction.model").String(); got != "claude-3" {
		t.Fatalf("expected model claude-3 from b.Model, got %s", got)
	}
}

// TestInteractionsSSEBuilder_CreatedAtOverride CreatedAt > 0 时覆盖 interaction.created 时间戳。
func TestInteractionsSSEBuilder_CreatedAtOverride(t *testing.T) {
	b := &InteractionsSSEBuilder{ID: "interaction_test", Model: "gpt-4", CreatedAt: 1609459200}
	out := b.AppendCompleted(nil, "gpt-4", gjson.Result{})
	completedFrame := out[len(out)-1]
	payload := ssePayload(completedFrame)
	created := gjson.GetBytes(payload, "interaction.created").String()
	wantCreated := time.Unix(1609459200, 0).UTC().Format(time.RFC3339)
	if created != wantCreated {
		t.Fatalf("expected interaction.created = %s (from CreatedAt), got %s", wantCreated, created)
	}
	// interaction.updated 始终使用当前时间，不等于 CreatedAt 的时间
	updated := gjson.GetBytes(payload, "interaction.updated").String()
	if updated == wantCreated {
		t.Fatalf("interaction.updated should use current time, not CreatedAt; both are %s", updated)
	}
}
