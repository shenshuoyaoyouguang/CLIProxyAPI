package interactions

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	translatorcommon "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/common"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type codexToInteractionsStreamState struct {
	Builder          *translatorcommon.InteractionsSSEBuilder
	HasOutputText    bool
	FunctionCallName string
	FunctionCallID   string
}

func ConvertCodexResponseToInteractions(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	_ = ctx
	_ = originalRequestRawJSON
	_ = requestRawJSON
	if param == nil {
		var local any
		param = &local
	}
	if *param == nil {
		*param = &codexToInteractionsStreamState{
			Builder: &translatorcommon.InteractionsSSEBuilder{
				ID:    fmt.Sprintf("interaction_%d", time.Now().UnixNano()),
				Model: modelName,
				SetUsage: func(p []byte, path string, u gjson.Result) []byte {
					return setCodexInteractionsUsage(p, path, u, true)
				},
			},
		}
	}
	st := (*param).(*codexToInteractionsStreamState)
	payload := codexStreamPayload(rawJSON)
	if bytes.Equal(payload, []byte("[DONE]")) {
		out := st.Builder.AppendStepStop(nil)
		if !st.Builder.Completed {
			out = appendCodexInteractionsCompleted(out, st, gjson.Result{})
		}
		return st.Builder.AppendDone(out)
	}
	if len(payload) == 0 {
		return nil
	}
	root := gjson.ParseBytes(payload)
	switch root.Get("type").String() {
	case "response.created":
		return appendCodexInteractionsCreated(nil, st, root.Get("response"))
	case "response.output_item.added":
		return codexOutputItemAddedToInteractions(st, root)
	case "response.output_text.delta":
		return codexOutputTextDeltaToInteractions(st, root)
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		return codexReasoningDeltaToInteractions(st, root)
	case "response.function_call_arguments.delta":
		return codexFunctionArgumentsDeltaToInteractions(st, root)
	case "response.output_item.done":
		return codexOutputItemDoneToInteractions(st, root.Get("item"))
	case "response.completed":
		out := appendCodexInteractionsCreated(nil, st, root.Get("response"))
		out = st.Builder.AppendStepStop(out)
		out = appendCodexInteractionsCompleted(out, st, root.Get("response"))
		return st.Builder.AppendDone(out)
	default:
		return nil
	}
}

func ConvertCodexResponseToInteractionsNonStream(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) []byte {
	_ = ctx
	_ = originalRequestRawJSON
	_ = requestRawJSON
	root := gjson.ParseBytes(rawJSON)
	response := root.Get("response")
	if !response.Exists() {
		response = root
	}
	out := []byte(`{"id":"","object":"interaction","status":"completed","model":"","steps":[]}`)
	id := response.Get("id").String()
	if id == "" {
		id = fmt.Sprintf("interaction_%d", time.Now().UnixNano())
	}
	out, _ = sjson.SetBytes(out, "id", id)
	if model := response.Get("model").String(); model != "" {
		out, _ = sjson.SetBytes(out, "model", model)
	} else {
		out, _ = sjson.SetBytes(out, "model", modelName)
	}
	response.Get("output").ForEach(func(_, item gjson.Result) bool {
		switch item.Get("type").String() {
		case "message":
			out = appendCodexMessageItemToInteractions(out, item)
		case "reasoning":
			out = appendCodexReasoningItemToInteractions(out, item)
		case "function_call", "tool_call":
			out = appendCodexFunctionCallItemToInteractions(out, item)
		case "image_generation_call":
			out = appendCodexImageItemToInteractions(out, item)
		}
		return true
	})
	out = setCodexInteractionsUsage(out, "usage", response.Get("usage"), false)
	return out
}

func codexStreamPayload(rawJSON []byte) []byte {
	rawJSON = bytes.TrimSpace(rawJSON)
	if bytes.HasPrefix(rawJSON, []byte("data:")) {
		rawJSON = bytes.TrimSpace(rawJSON[len("data:"):])
	}
	return rawJSON
}

func codexStreamEventType(rawJSON []byte) string {
	payload := codexStreamPayload(rawJSON)
	if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
		return ""
	}
	return gjson.GetBytes(payload, "type").String()
}

// appendCodexInteractionsCreated 同步响应中的 id/model/created_at 到 Builder，并通过 Builder 发送 created 事件。
func appendCodexInteractionsCreated(out [][]byte, st *codexToInteractionsStreamState, response gjson.Result) [][]byte {
	if st.Builder.Created {
		return out
	}
	if id := response.Get("id").String(); id != "" {
		st.Builder.ID = id
	}
	if model := response.Get("model").String(); model != "" {
		st.Builder.Model = model
	}
	if createdAt := response.Get("created_at"); createdAt.Exists() {
		st.Builder.CreatedAt = createdAt.Int()
	}
	return st.Builder.AppendCreated(out, st.Builder.Model)
}

// appendCodexInteractionsCompleted 通过 Builder 发送 completed 事件。
// created_at 已在 appendCodexInteractionsCreated 中同步到 Builder.CreatedAt，由 Builder 统一处理时间戳。
func appendCodexInteractionsCompleted(out [][]byte, st *codexToInteractionsStreamState, response gjson.Result) [][]byte {
	return st.Builder.AppendCompleted(out, st.Builder.Model, response.Get("usage"))
}

func codexOutputItemAddedToInteractions(st *codexToInteractionsStreamState, root gjson.Result) [][]byte {
	out := appendCodexInteractionsCreated(nil, st, root.Get("response"))
	item := root.Get("item")
	switch item.Get("type").String() {
	case "message":
		return ensureCodexInteractionsStep(out, st, "model_output", item)
	case "reasoning":
		return ensureCodexInteractionsStep(out, st, "thought", item)
	case "function_call", "tool_call":
		st.FunctionCallName = item.Get("name").String()
		st.FunctionCallID = codexItemCallID(item)
		return ensureCodexInteractionsStep(out, st, "function_call", item)
	}
	return out
}

func codexOutputTextDeltaToInteractions(st *codexToInteractionsStreamState, root gjson.Result) [][]byte {
	out := appendCodexInteractionsCreated(nil, st, root.Get("response"))
	out = ensureCodexInteractionsStep(out, st, "model_output", gjson.Result{})
	st.HasOutputText = true
	return st.Builder.AppendTextDelta(out, root.Get("delta").String())
}

func codexReasoningDeltaToInteractions(st *codexToInteractionsStreamState, root gjson.Result) [][]byte {
	out := appendCodexInteractionsCreated(nil, st, root.Get("response"))
	out = ensureCodexInteractionsStep(out, st, "thought", gjson.Result{})
	return st.Builder.AppendThoughtDelta(out, root.Get("delta").String())
}

func codexFunctionArgumentsDeltaToInteractions(st *codexToInteractionsStreamState, root gjson.Result) [][]byte {
	out := appendCodexInteractionsCreated(nil, st, root.Get("response"))
	out = ensureCodexInteractionsStep(out, st, "function_call", root.Get("item"))
	return st.Builder.AppendArgumentsDelta(out, root.Get("delta").String())
}

func codexOutputItemDoneToInteractions(st *codexToInteractionsStreamState, item gjson.Result) [][]byte {
	out := appendCodexInteractionsCreated(nil, st, gjson.Result{})
	switch item.Get("type").String() {
	case "message":
		if st.HasOutputText {
			return st.Builder.AppendStepStop(out)
		}
		out = appendCodexMessageItemToInteractionsStream(out, st, item)
		return st.Builder.AppendStepStop(out)
	case "reasoning":
		out = appendCodexReasoningItemToInteractionsStream(out, st, item)
		return st.Builder.AppendStepStop(out)
	case "function_call", "tool_call":
		out = appendCodexFunctionCallItemToInteractionsStream(out, st, item)
		return st.Builder.AppendStepStop(out)
	case "image_generation_call":
		out = appendCodexImageItemToInteractionsStream(out, st, item)
		return st.Builder.AppendStepStop(out)
	}
	return out
}

func ensureCodexInteractionsStep(out [][]byte, st *codexToInteractionsStreamState, stepType string, item gjson.Result) [][]byte {
	if st.Builder.ActiveStepOpen && st.Builder.ActiveStepType == stepType {
		return out
	}
	out = st.Builder.AppendStepStop(out)
	return appendCodexInteractionsStepStart(out, st, stepType, item)
}

// appendCodexInteractionsStepStart 构造 function_call 的 callID/name（含 codex 特有回退与自动生成逻辑），并通过 Builder 发送 step.start 事件。
func appendCodexInteractionsStepStart(out [][]byte, st *codexToInteractionsStreamState, stepType string, item gjson.Result) [][]byte {
	params := translatorcommon.StepStartParams{Type: stepType}
	if stepType == "function_call" {
		name := item.Get("name").String()
		if name == "" {
			name = st.FunctionCallName
		}
		callID := codexItemCallID(item)
		if callID == "" {
			callID = st.FunctionCallID
		}
		if callID == "" {
			callID = fmt.Sprintf("step_%d", time.Now().UnixNano())
		}
		params.CallID = callID
		params.Name = name
	}
	return st.Builder.AppendStepStart(out, params)
}

func appendCodexMessageItemToInteractions(out []byte, item gjson.Result) []byte {
	step := []byte(`{"type":"model_output","content":[]}`)
	item.Get("content").ForEach(func(_, content gjson.Result) bool {
		if contentItem := codexContentToInteractionsContent(content); len(contentItem) > 0 {
			step, _ = sjson.SetRawBytes(step, "content.-1", contentItem)
		}
		return true
	})
	if gjson.GetBytes(step, "content.#").Int() == 0 {
		return out
	}
	out, _ = sjson.SetRawBytes(out, "steps.-1", step)
	return out
}

func appendCodexReasoningItemToInteractions(out []byte, item gjson.Result) []byte {
	text := codexReasoningText(item)
	if text == "" {
		return out
	}
	step := []byte(`{"type":"thought","content":[{"type":"text","text":""}]}`)
	step, _ = sjson.SetBytes(step, "content.0.text", text)
	out, _ = sjson.SetRawBytes(out, "steps.-1", step)
	return out
}

func appendCodexFunctionCallItemToInteractions(out []byte, item gjson.Result) []byte {
	step := []byte(`{"type":"function_call","name":"","arguments":{}}`)
	step, _ = sjson.SetBytes(step, "name", item.Get("name").String())
	if callID := codexItemCallID(item); callID != "" {
		step, _ = sjson.SetBytes(step, "call_id", callID)
	}
	if args := codexArgumentsJSON(item.Get("arguments")); len(args) > 0 {
		step, _ = sjson.SetRawBytes(step, "arguments", args)
	}
	out, _ = sjson.SetRawBytes(out, "steps.-1", step)
	return out
}

func appendCodexImageItemToInteractions(out []byte, item gjson.Result) []byte {
	result := item.Get("result").String()
	if result == "" {
		return out
	}
	step := []byte(`{"type":"model_output","content":[{"type":"image","mime_type":"","data":""}]}`)
	step, _ = sjson.SetBytes(step, "content.0.mime_type", mimeTypeFromCodexOutputFormat(item.Get("output_format").String()))
	step, _ = sjson.SetBytes(step, "content.0.data", result)
	out, _ = sjson.SetRawBytes(out, "steps.-1", step)
	return out
}

func appendCodexMessageItemToInteractionsStream(out [][]byte, st *codexToInteractionsStreamState, item gjson.Result) [][]byte {
	item.Get("content").ForEach(func(_, content gjson.Result) bool {
		if text := codexContentText(content); text != "" {
			out = ensureCodexInteractionsStep(out, st, "model_output", item)
			out = st.Builder.AppendTextDelta(out, text)
		}
		return true
	})
	return out
}

func appendCodexReasoningItemToInteractionsStream(out [][]byte, st *codexToInteractionsStreamState, item gjson.Result) [][]byte {
	text := codexReasoningText(item)
	if text == "" {
		return out
	}
	out = ensureCodexInteractionsStep(out, st, "thought", item)
	return st.Builder.AppendThoughtDelta(out, text)
}

func appendCodexFunctionCallItemToInteractionsStream(out [][]byte, st *codexToInteractionsStreamState, item gjson.Result) [][]byte {
	out = ensureCodexInteractionsStep(out, st, "function_call", item)
	return st.Builder.AppendArgumentsDelta(out, item.Get("arguments").String())
}

func appendCodexImageItemToInteractionsStream(out [][]byte, st *codexToInteractionsStreamState, item gjson.Result) [][]byte {
	result := item.Get("result").String()
	if result == "" {
		return out
	}
	out = ensureCodexInteractionsStep(out, st, "model_output", item)
	delta := []byte(`{"index":0,"delta":{"content":{"type":"image","mime_type":"","data":""},"type":"content"},"event_type":"step.delta"}`)
	delta, _ = sjson.SetBytes(delta, "index", st.Builder.ActiveStepIndex)
	delta, _ = sjson.SetBytes(delta, "delta.content.mime_type", mimeTypeFromCodexOutputFormat(item.Get("output_format").String()))
	delta, _ = sjson.SetBytes(delta, "delta.content.data", result)
	return append(out, translatorcommon.SSEEventData("step.delta", delta))
}

func codexContentToInteractionsContent(content gjson.Result) []byte {
	if text := codexContentText(content); text != "" {
		item := []byte(`{"type":"text","text":""}`)
		item, _ = sjson.SetBytes(item, "text", text)
		return item
	}
	return nil
}

func codexContentText(content gjson.Result) string {
	for _, path := range []string{"text", "content"} {
		if value := content.Get(path); value.Exists() && value.Type == gjson.String {
			return value.String()
		}
	}
	return ""
}

func codexReasoningText(item gjson.Result) string {
	if content := item.Get("content"); content.Exists() {
		if content.Type == gjson.String {
			return content.String()
		}
		if content.IsArray() {
			var builder strings.Builder
			content.ForEach(func(_, part gjson.Result) bool {
				text := codexContentText(part)
				if text == "" {
					text = part.Get("summary_text").String()
				}
				if text == "" {
					return true
				}
				if builder.Len() > 0 {
					builder.WriteByte('\n')
				}
				builder.WriteString(text)
				return true
			})
			return builder.String()
		}
	}
	if summary := item.Get("summary"); summary.Exists() {
		if summary.Type == gjson.String {
			return summary.String()
		}
		if summary.IsArray() {
			var builder strings.Builder
			summary.ForEach(func(_, part gjson.Result) bool {
				text := codexContentText(part)
				if text == "" {
					return true
				}
				if builder.Len() > 0 {
					builder.WriteByte('\n')
				}
				builder.WriteString(text)
				return true
			})
			return builder.String()
		}
	}
	return ""
}

func codexItemCallID(item gjson.Result) string {
	if callID := strings.TrimSpace(item.Get("call_id").String()); callID != "" {
		return callID
	}
	return strings.TrimSpace(item.Get("id").String())
}

func codexArgumentsJSON(arguments gjson.Result) []byte {
	if !arguments.Exists() {
		return nil
	}
	if arguments.Type == gjson.String {
		parsed := gjson.Parse(arguments.String())
		if parsed.Exists() && parsed.IsObject() {
			return []byte(arguments.String())
		}
		return []byte(`{}`)
	}
	if arguments.IsObject() {
		return []byte(arguments.Raw)
	}
	return nil
}

func setCodexInteractionsUsage(out []byte, path string, usage gjson.Result, stream bool) []byte {
	if !usage.Exists() {
		return out
	}
	inputTokens := usage.Get("input_tokens").Int()
	outputTokens := usage.Get("output_tokens").Int()
	if inputTokens == 0 {
		inputTokens = usage.Get("prompt_tokens").Int()
	}
	if outputTokens == 0 {
		outputTokens = usage.Get("completion_tokens").Int()
	}
	totalTokens := usage.Get("total_tokens").Int()
	if totalTokens == 0 {
		totalTokens = inputTokens + outputTokens
	}
	reasoningTokens := usage.Get("output_tokens_details.reasoning_tokens").Int()
	if reasoningTokens == 0 {
		reasoningTokens = usage.Get("reasoning_tokens").Int()
	}
	cachedTokens := usage.Get("input_tokens_details.cached_tokens").Int()
	if cachedTokens == 0 {
		cachedTokens = usage.Get("cached_tokens").Int()
	}
	if stream {
		out, _ = sjson.SetBytes(out, path+".total_tokens", totalTokens)
		out, _ = sjson.SetBytes(out, path+".total_input_tokens", inputTokens)
		out, _ = sjson.SetRawBytes(out, path+".input_tokens_by_modality", []byte(fmt.Sprintf(`[{"modality":"text","tokens":%d}]`, inputTokens)))
		out, _ = sjson.SetBytes(out, path+".total_cached_tokens", cachedTokens)
		out, _ = sjson.SetBytes(out, path+".total_output_tokens", outputTokens)
		out, _ = sjson.SetBytes(out, path+".total_tool_use_tokens", 0)
		out, _ = sjson.SetBytes(out, path+".total_thought_tokens", reasoningTokens)
		return out
	}
	out, _ = sjson.SetBytes(out, path+".input_tokens", inputTokens)
	out, _ = sjson.SetBytes(out, path+".output_tokens", outputTokens)
	out, _ = sjson.SetBytes(out, path+".total_tokens", totalTokens)
	if reasoningTokens > 0 {
		out, _ = sjson.SetBytes(out, path+".reasoning_tokens", reasoningTokens)
	}
	if cachedTokens > 0 {
		out, _ = sjson.SetBytes(out, path+".cached_tokens", cachedTokens)
	}
	return out
}

func mimeTypeFromCodexOutputFormat(outputFormat string) string {
	if outputFormat == "" {
		return "image/png"
	}
	if strings.Contains(outputFormat, "/") {
		return outputFormat
	}
	switch strings.ToLower(outputFormat) {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	default:
		return "image/png"
	}
}
