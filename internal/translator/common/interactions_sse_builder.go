package common

import (
	"fmt"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// StepStartParams 描述 AppendStepStart 的入参。
type StepStartParams struct {
	Type      string // "model_output" / "thought" / "function_call" / "function_result"
	CallID    string // function_call 专用，同时写入 step.id 和 step.call_id
	Name      string // function_call 专用
	Arguments []byte // function_call 初始 arguments，nil 时默认 {}
	Extra     []byte // 附加字段原样合并到 step 对象（如 thought_signature），可为 nil
}

// InteractionsSSEBuilder 封装 interactions 响应 SSE 事件的公共构造逻辑与流状态。
type InteractionsSSEBuilder struct {
	ID              string
	Model           string
	CreatedAt       int64 // completed 事件的 created/updated 时间戳来源；为 0 时使用 time.Now()。
	Created         bool
	StatusUpdated   bool
	Completed       bool
	Done            bool
	ActiveStepOpen  bool
	ActiveStepType  string
	ActiveStepIndex int
	StepIndex       int
	// SetUsage 注入 provider 特有的 usage 写入逻辑。
	// 第三参数语义由注入函数决定：openai/codex/claude 传入 usage 对象，
	// antigravity/gemini 传入整个 root 响应对象（由注入函数内部提取 usage）。
	SetUsage func(payload []byte, path string, usage gjson.Result) []byte
}

// AppendCreated 追加 interaction.created 事件，幂等，并自动补发 status_update。
func (b *InteractionsSSEBuilder) AppendCreated(out [][]byte, modelName string) [][]byte {
	if b.Created {
		return out
	}
	if b.ID == "" {
		b.ID = fmt.Sprintf("interaction_%d", time.Now().UnixNano())
	}
	model := b.Model
	if model == "" {
		model = modelName
	}
	payload := []byte(`{"interaction":{"id":"","status":"in_progress","object":"interaction","model":""},"event_type":"interaction.created"}`)
	payload, _ = sjson.SetBytes(payload, "interaction.id", b.ID)
	payload, _ = sjson.SetBytes(payload, "interaction.model", model)
	out = append(out, SSEEventData("interaction.created", payload))
	b.Created = true
	return b.AppendStatusUpdate(out)
}

// AppendStatusUpdate 追加 interaction.status_update 事件，幂等。
func (b *InteractionsSSEBuilder) AppendStatusUpdate(out [][]byte) [][]byte {
	if b.StatusUpdated {
		return out
	}
	payload := []byte(`{"interaction_id":"","status":"in_progress","event_type":"interaction.status_update"}`)
	payload, _ = sjson.SetBytes(payload, "interaction_id", b.ID)
	out = append(out, SSEEventData("interaction.status_update", payload))
	b.StatusUpdated = true
	return out
}

// AppendStepStart 追加 step.start 事件，递增 StepIndex 并打开新步骤。
func (b *InteractionsSSEBuilder) AppendStepStart(out [][]byte, params StepStartParams) [][]byte {
	b.ActiveStepIndex = b.StepIndex
	b.StepIndex++
	b.ActiveStepType = params.Type
	b.ActiveStepOpen = true
	payload := []byte(`{"index":0,"step":{"type":""},"event_type":"step.start"}`)
	payload, _ = sjson.SetBytes(payload, "index", b.ActiveStepIndex)
	if params.Type == "function_call" && (params.CallID != "" || params.Name != "") {
		step := []byte(`{"type":"function_call","id":"","call_id":"","name":"","arguments":{}}`)
		step, _ = sjson.SetBytes(step, "id", params.CallID)
		step, _ = sjson.SetBytes(step, "call_id", params.CallID)
		step, _ = sjson.SetBytes(step, "name", params.Name)
		if params.Arguments != nil {
			step, _ = sjson.SetRawBytes(step, "arguments", params.Arguments)
		}
		payload, _ = sjson.SetRawBytes(payload, "step", step)
	} else if len(params.Extra) > 0 && gjson.ValidBytes(params.Extra) {
		step := []byte(`{"type":""}`)
		step, _ = sjson.SetBytes(step, "type", params.Type)
		gjson.ParseBytes(params.Extra).ForEach(func(key, value gjson.Result) bool {
			step, _ = sjson.SetRawBytes(step, key.String(), []byte(value.Raw))
			return true
		})
		payload, _ = sjson.SetRawBytes(payload, "step", step)
	} else {
		payload, _ = sjson.SetBytes(payload, "step.type", params.Type)
	}
	return append(out, SSEEventData("step.start", payload))
}

// AppendTextDelta 追加文本类型的 step.delta 事件。
func (b *InteractionsSSEBuilder) AppendTextDelta(out [][]byte, text string) [][]byte {
	payload := []byte(`{"index":0,"delta":{"text":"","type":"text"},"event_type":"step.delta"}`)
	payload, _ = sjson.SetBytes(payload, "index", b.ActiveStepIndex)
	payload, _ = sjson.SetBytes(payload, "delta.text", text)
	return append(out, SSEEventData("step.delta", payload))
}

// AppendThoughtDelta 追加 thought_summary 类型的 step.delta 事件。
func (b *InteractionsSSEBuilder) AppendThoughtDelta(out [][]byte, text string) [][]byte {
	payload := []byte(`{"index":0,"delta":{"content":{"text":"","type":"text"},"type":"thought_summary"},"event_type":"step.delta"}`)
	payload, _ = sjson.SetBytes(payload, "index", b.ActiveStepIndex)
	payload, _ = sjson.SetBytes(payload, "delta.content.text", text)
	return append(out, SSEEventData("step.delta", payload))
}

// AppendArgumentsDelta 追加 arguments_delta 类型的 step.delta 事件。
func (b *InteractionsSSEBuilder) AppendArgumentsDelta(out [][]byte, arguments string) [][]byte {
	payload := []byte(`{"index":0,"delta":{"arguments":"","type":"arguments_delta"},"event_type":"step.delta"}`)
	payload, _ = sjson.SetBytes(payload, "index", b.ActiveStepIndex)
	payload, _ = sjson.SetBytes(payload, "delta.arguments", arguments)
	return append(out, SSEEventData("step.delta", payload))
}

// AppendStepStop 追加 step.stop 事件，仅当当前有打开的步骤时发送，不递增 StepIndex。
func (b *InteractionsSSEBuilder) AppendStepStop(out [][]byte) [][]byte {
	if !b.ActiveStepOpen {
		return out
	}
	payload := []byte(`{"index":0,"event_type":"step.stop"}`)
	payload, _ = sjson.SetBytes(payload, "index", b.ActiveStepIndex)
	out = append(out, SSEEventData("step.stop", payload))
	b.ActiveStepOpen = false
	b.ActiveStepType = ""
	return out
}

// AppendCompleted 追加 interaction.completed 事件，幂等，必要时自动补发 created。
// interaction.created 使用 CreatedAt（若 > 0），否则使用当前时间；interaction.updated 始终使用当前时间。
func (b *InteractionsSSEBuilder) AppendCompleted(out [][]byte, modelName string, usage gjson.Result) [][]byte {
	if b.Completed {
		return out
	}
	if !b.Created {
		out = b.AppendCreated(out, modelName)
	}
	created := time.Now().UTC()
	if b.CreatedAt > 0 {
		created = time.Unix(b.CreatedAt, 0).UTC()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	model := b.Model
	if model == "" {
		model = modelName
	}
	payload := []byte(`{"interaction":{"id":"","status":"completed","usage":{},"created":"","updated":"","service_tier":"standard","object":"interaction","model":""},"event_type":"interaction.completed"}`)
	payload, _ = sjson.SetBytes(payload, "interaction.id", b.ID)
	payload, _ = sjson.SetBytes(payload, "interaction.created", created.Format(time.RFC3339))
	payload, _ = sjson.SetBytes(payload, "interaction.updated", now)
	payload, _ = sjson.SetBytes(payload, "interaction.model", model)
	if b.SetUsage != nil {
		payload = b.SetUsage(payload, "interaction.usage", usage)
	}
	out = append(out, SSEEventData("interaction.completed", payload))
	b.Completed = true
	return out
}

// AppendDone 追加 done 事件，幂等。
func (b *InteractionsSSEBuilder) AppendDone(out [][]byte) [][]byte {
	if b.Done {
		return out
	}
	out = append(out, SSEEventData("done", []byte("[DONE]")))
	b.Done = true
	return out
}

// EnsureCreated 确保 created 事件已发送。
func (b *InteractionsSSEBuilder) EnsureCreated(out [][]byte, modelName string) [][]byte {
	return b.AppendCreated(out, modelName)
}

// EnsureStep 确保当前打开的步骤为指定类型，否则先 stop 再 start。
func (b *InteractionsSSEBuilder) EnsureStep(out [][]byte, modelName string, params StepStartParams) [][]byte {
	out = b.EnsureCreated(out, modelName)
	if b.ActiveStepOpen && b.ActiveStepType == params.Type {
		return out
	}
	out = b.AppendStepStop(out)
	return b.AppendStepStart(out, params)
}

// Reset 重置所有流状态字段为零值，保留 SetUsage 注入函数。
func (b *InteractionsSSEBuilder) Reset() {
	b.ID = ""
	b.Model = ""
	b.CreatedAt = 0
	b.Created = false
	b.StatusUpdated = false
	b.Completed = false
	b.Done = false
	b.ActiveStepOpen = false
	b.ActiveStepType = ""
	b.ActiveStepIndex = 0
	b.StepIndex = 0
}
