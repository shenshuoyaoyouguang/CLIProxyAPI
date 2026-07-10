package chat_completions

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

type openAIToInteractionsStreamState struct {
	Builder       *translatorcommon.InteractionsSSEBuilder
	ToolCallIDs   map[int]string
	ToolCallNames map[int]string
	CurrentStepID string // provider 特有：跟踪当前 function_call 步骤的 call_id（Builder 不暴露此状态）
	Usage         gjson.Result
}

func ConvertOpenAIResponseToInteractions(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	_ = ctx
	_ = originalRequestRawJSON
	_ = requestRawJSON
	if param == nil {
		var local any
		param = &local
	}
	if *param == nil {
		*param = &openAIToInteractionsStreamState{
			Builder: &translatorcommon.InteractionsSSEBuilder{
				SetUsage: setInteractionsUsageFromOpenAIChat,
			},
		}
	}
	st := (*param).(*openAIToInteractionsStreamState)
	if st.ToolCallIDs == nil {
		st.ToolCallIDs = make(map[int]string)
	}
	if st.ToolCallNames == nil {
		st.ToolCallNames = make(map[int]string)
	}
	return convertOpenAIChatStreamToInteractions(modelName, rawJSON, st)
}

func ConvertOpenAIResponseToInteractionsNonStream(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) []byte {
	_ = ctx
	_ = originalRequestRawJSON
	_ = requestRawJSON
	root := gjson.ParseBytes(rawJSON)
	out := []byte(`{"id":"","status":"completed","object":"interaction","model":"","steps":[]}`)
	out, _ = sjson.SetBytes(out, "id", firstNonEmpty(root.Get("id").String(), fmt.Sprintf("interaction_%d", time.Now().UnixNano())))
	out, _ = sjson.SetBytes(out, "model", firstNonEmpty(modelName, root.Get("model").String()))
	choices := root.Get("choices")
	choices.ForEach(func(_, choice gjson.Result) bool {
		message := choice.Get("message")
		if reasoning := message.Get("reasoning_content"); reasoning.Exists() {
			for _, text := range openAIReasoningTexts(reasoning) {
				out, _ = sjson.SetRawBytes(out, "steps.-1", interactionsTextStep("thought", text))
			}
		}
		if content := message.Get("content"); content.Exists() && content.String() != "" {
			out, _ = sjson.SetRawBytes(out, "steps.-1", interactionsTextStep("model_output", content.String()))
		}
		if toolCalls := message.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
			toolCalls.ForEach(func(_, toolCall gjson.Result) bool {
				if step, ok := openAIToolCallToInteractionsStep(toolCall); ok {
					out, _ = sjson.SetRawBytes(out, "steps.-1", step)
				}
				return true
			})
		}
		if finishReason := choice.Get("finish_reason"); finishReason.Exists() {
			out, _ = sjson.SetBytes(out, "finish_reason", finishReason.String())
		}
		return true
	})
	out = setInteractionsUsageFromOpenAIChat(out, "usage", root.Get("usage"))
	return out
}

func convertOpenAIChatStreamToInteractions(modelName string, rawJSON []byte, st *openAIToInteractionsStreamState) [][]byte {
	payload := openAIChatSSEPayload(rawJSON)
	if len(payload) == 0 {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(payload), []byte("[DONE]")) {
		out := make([][]byte, 0, 3)
		out = st.Builder.AppendStepStop(out)
		if !st.Builder.Completed {
			syncInteractionIdentity(st.Builder, gjson.Result{}, modelName)
			out = st.Builder.AppendCompleted(out, modelName, st.Usage)
		}
		return st.Builder.AppendDone(out)
	}
	root := gjson.ParseBytes(payload)
	if !root.Exists() {
		return nil
	}
	if usage := root.Get("usage"); usage.Exists() {
		st.Usage = usage
	}
	out := make([][]byte, 0)
	if choices := root.Get("choices"); choices.Exists() && choices.IsArray() {
		if len(choices.Array()) == 0 {
			if root.Get("usage").Exists() {
				out = st.Builder.AppendStepStop(out)
				syncInteractionIdentity(st.Builder, root, modelName)
				out = st.Builder.AppendCompleted(out, modelName, root.Get("usage"))
			}
			return out
		}
		choices.ForEach(func(_, choice gjson.Result) bool {
			delta := choice.Get("delta")
			if reasoning := delta.Get("reasoning_content"); reasoning.Exists() {
				for _, text := range openAIReasoningTexts(reasoning) {
					syncInteractionIdentity(st.Builder, root, modelName)
					out = st.Builder.EnsureStep(out, modelName, translatorcommon.StepStartParams{Type: "thought"})
					out = st.Builder.AppendThoughtDelta(out, text)
				}
			}
			if content := delta.Get("content"); content.Exists() && content.String() != "" {
				syncInteractionIdentity(st.Builder, root, modelName)
				out = st.Builder.EnsureStep(out, modelName, translatorcommon.StepStartParams{Type: "model_output"})
				out = st.Builder.AppendTextDelta(out, content.String())
			}
			if toolCalls := delta.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
				toolCalls.ForEach(func(_, toolCall gjson.Result) bool {
					out = appendOpenAIToolCallDelta(out, st, modelName, root, toolCall)
					return true
				})
			}
			if finishReason := choice.Get("finish_reason"); finishReason.Exists() {
				out = st.Builder.AppendStepStop(out)
			}
			return true
		})
	}
	return out
}

func appendOpenAIToolCallDelta(out [][]byte, st *openAIToInteractionsStreamState, modelName string, root, toolCall gjson.Result) [][]byte {
	index := int(toolCall.Get("index").Int())
	if id := toolCall.Get("id").String(); id != "" {
		st.ToolCallIDs[index] = id
	}
	function := toolCall.Get("function")
	if name := function.Get("name").String(); name != "" {
		st.ToolCallNames[index] = name
	}
	stepID := firstNonEmpty(st.ToolCallIDs[index], fmt.Sprintf("call_%d", index))
	stepName := st.ToolCallNames[index]
	if st.Builder.ActiveStepType != "function_call" || st.CurrentStepID != stepID {
		out = st.Builder.AppendStepStop(out)
		syncInteractionIdentity(st.Builder, root, modelName)
		out = st.Builder.AppendCreated(out, modelName)
		params := translatorcommon.StepStartParams{
			Type:      "function_call",
			CallID:    stepID,
			Name:      stepName,
			Arguments: []byte(`{}`),
		}
		out = st.Builder.AppendStepStart(out, params)
		st.CurrentStepID = stepID
	}
	if args := function.Get("arguments"); args.Exists() && args.String() != "" {
		out = st.Builder.AppendArgumentsDelta(out, args.String())
	}
	return out
}

// syncInteractionIdentity 在触发 created/completed 前同步 Builder 的 ID/Model。
// ID 仅在未创建时同步（匹配原行为：首个携带 id 的 chunk 锁定 interaction id）。
func syncInteractionIdentity(b *translatorcommon.InteractionsSSEBuilder, root gjson.Result, modelName string) {
	if !b.Created {
		b.ID = firstNonEmpty(root.Get("id").String(), b.ID)
	}
	b.Model = firstNonEmpty(b.Model, modelName, root.Get("model").String())
}

func isOpenAIStreamDone(rawJSON []byte) bool {
	return bytes.Equal(bytes.TrimSpace(openAIChatSSEPayload(rawJSON)), []byte("[DONE]"))
}

func openAIChatSSEPayload(rawJSON []byte) []byte {
	trimmed := bytes.TrimSpace(rawJSON)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("[DONE]")) {
		return trimmed
	}
	if bytes.HasPrefix(trimmed, []byte("data:")) {
		return bytes.TrimSpace(trimmed[len("data:"):])
	}
	var dataLines [][]byte
	for _, line := range bytes.Split(trimmed, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, []byte("data:")) {
			dataLines = append(dataLines, bytes.TrimSpace(line[len("data:"):]))
		}
	}
	if len(dataLines) > 0 {
		return bytes.Join(dataLines, []byte("\n"))
	}
	return trimmed
}

func interactionsTextStep(stepType, text string) []byte {
	step := []byte(`{"type":"","content":[{"type":"text","text":""}]}`)
	step, _ = sjson.SetBytes(step, "type", stepType)
	step, _ = sjson.SetBytes(step, "content.0.text", text)
	return step
}

func openAIToolCallToInteractionsStep(toolCall gjson.Result) ([]byte, bool) {
	if toolType := toolCall.Get("type").String(); toolType != "" && toolType != "function" {
		return nil, false
	}
	function := toolCall.Get("function")
	if !function.Exists() {
		return nil, false
	}
	step := []byte(`{"type":"function_call","name":"","arguments":{}}`)
	if id := toolCall.Get("id").String(); id != "" {
		step, _ = sjson.SetBytes(step, "id", id)
		step, _ = sjson.SetBytes(step, "call_id", id)
	}
	step, _ = sjson.SetBytes(step, "name", function.Get("name").String())
	setRawJSONValue(&step, "arguments", function.Get("arguments"), []byte(`{}`))
	return step, true
}

func setInteractionsUsageFromOpenAIChat(out []byte, path string, usage gjson.Result) []byte {
	if !usage.Exists() {
		return out
	}
	if value := usage.Get("prompt_tokens"); value.Exists() {
		out, _ = sjson.SetBytes(out, path+".input_tokens", value.Int())
		out, _ = sjson.SetBytes(out, path+".total_input_tokens", value.Int())
	}
	if value := usage.Get("completion_tokens"); value.Exists() {
		out, _ = sjson.SetBytes(out, path+".output_tokens", value.Int())
		out, _ = sjson.SetBytes(out, path+".total_output_tokens", value.Int())
	}
	if value := usage.Get("total_tokens"); value.Exists() {
		out, _ = sjson.SetBytes(out, path+".total_tokens", value.Int())
	}
	if value := usage.Get("prompt_tokens_details.cached_tokens"); value.Exists() {
		out, _ = sjson.SetBytes(out, path+".cached_tokens", value.Int())
		out, _ = sjson.SetBytes(out, path+".total_cached_tokens", value.Int())
	}
	if value := usage.Get("completion_tokens_details.reasoning_tokens"); value.Exists() {
		out, _ = sjson.SetBytes(out, path+".reasoning_tokens", value.Int())
		out, _ = sjson.SetBytes(out, path+".total_thought_tokens", value.Int())
	}
	return out
}

func openAIReasoningTexts(reasoning gjson.Result) []string {
	if reasoning.Type == gjson.String {
		if reasoning.String() == "" {
			return nil
		}
		return []string{reasoning.String()}
	}
	texts := make([]string, 0)
	if reasoning.IsArray() {
		reasoning.ForEach(func(_, item gjson.Result) bool {
			if text := firstNonEmpty(item.Get("text").String(), item.Get("content").String()); text != "" {
				texts = append(texts, text)
			}
			return true
		})
	}
	return texts
}

func setRawJSONValue(out *[]byte, path string, value gjson.Result, fallback []byte) {
	if !value.Exists() {
		*out, _ = sjson.SetRawBytes(*out, path, fallback)
		return
	}
	raw := strings.TrimSpace(value.String())
	if value.Type == gjson.String && gjson.Valid(raw) {
		*out, _ = sjson.SetRawBytes(*out, path, []byte(raw))
		return
	}
	if value.Type == gjson.String {
		*out, _ = sjson.SetBytes(*out, path, value.String())
		return
	}
	*out, _ = sjson.SetRawBytes(*out, path, []byte(value.Raw))
}

type interactionsToOpenAIChatStreamState struct {
	ID              string
	Model           string
	Created         int64
	Started         bool
	Completed       bool
	SawToolCall     bool
	StepTypes       map[int]string
	ToolIDs         map[int]string
	ToolNames       map[int]string
	ToolArguments   map[int]*strings.Builder
	TextByStepIndex map[int]*strings.Builder
}

func ConvertInteractionsResponseToOpenAI(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	_ = ctx
	_ = originalRequestRawJSON
	_ = requestRawJSON
	if param == nil {
		var local any
		param = &local
	}
	if *param == nil {
		*param = &interactionsToOpenAIChatStreamState{Model: modelName}
	}
	st := (*param).(*interactionsToOpenAIChatStreamState)
	st.Model = firstNonEmpty(st.Model, modelName)
	st.ensureMaps()
	return convertInteractionsEventToOpenAIChat(modelName, rawJSON, st)
}

func ConvertInteractionsResponseToOpenAINonStream(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) []byte {
	_ = ctx
	_ = originalRequestRawJSON
	_ = requestRawJSON
	root := gjson.ParseBytes(rawJSON)
	interaction := root
	if nested := root.Get("interaction"); nested.Exists() {
		interaction = nested
	}
	out := []byte(`{"id":"","object":"chat.completion","created":0,"model":"","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"stop"}]}`)
	out, _ = sjson.SetBytes(out, "id", firstNonEmpty(interaction.Get("id").String(), root.Get("id").String(), fmt.Sprintf("chatcmpl_%d", time.Now().UnixNano())))
	out, _ = sjson.SetBytes(out, "created", time.Now().Unix())
	out, _ = sjson.SetBytes(out, "model", firstNonEmpty(interaction.Get("model").String(), modelName))
	steps := interaction.Get("steps")
	if !steps.Exists() {
		steps = root.Get("steps")
	}
	var textBuilder strings.Builder
	var reasoningBuilder strings.Builder
	sawToolCall := false
	steps.ForEach(func(_, step gjson.Result) bool {
		switch step.Get("type").String() {
		case "model_output":
			for _, text := range interactionsContentTextsForOpenAIChat(step.Get("content")) {
				textBuilder.WriteString(text)
			}
		case "thought":
			for _, text := range interactionsContentTextsForOpenAIChat(step.Get("content")) {
				reasoningBuilder.WriteString(text)
			}
		case "function_call":
			sawToolCall = true
			out, _ = sjson.SetRawBytes(out, "choices.0.message.tool_calls.-1", openAIChatToolCallFromInteractions(step, gjson.Result{}))
		}
		return true
	})
	if textBuilder.Len() > 0 {
		out, _ = sjson.SetBytes(out, "choices.0.message.content", textBuilder.String())
	}
	if reasoningBuilder.Len() > 0 {
		out, _ = sjson.SetBytes(out, "choices.0.message.reasoning_content", reasoningBuilder.String())
	}
	if sawToolCall {
		out, _ = sjson.SetBytes(out, "choices.0.message.content", nil)
		out, _ = sjson.SetBytes(out, "choices.0.finish_reason", "tool_calls")
	}
	out = setOpenAIChatUsageFromInteractions(out, "usage", translatorcommon.InteractionsUsage(root))
	return out
}

func convertInteractionsEventToOpenAIChat(modelName string, rawJSON []byte, st *interactionsToOpenAIChatStreamState) [][]byte {
	payload := openAIChatSSEPayload(rawJSON)
	if len(payload) == 0 || bytes.Equal(bytes.TrimSpace(payload), []byte("[DONE]")) {
		return nil
	}
	root := gjson.ParseBytes(payload)
	if !root.Exists() {
		return nil
	}
	switch root.Get("event_type").String() {
	case "interaction.created":
		interaction := root.Get("interaction")
		st.ID = firstNonEmpty(interaction.Get("id").String(), st.ID)
		st.Model = firstNonEmpty(interaction.Get("model").String(), st.Model, modelName)
		return ensureOpenAIChatStarted(nil, st)
	case "step.start":
		return interactionsStepStartToOpenAIChat(modelName, root, st)
	case "step.delta":
		return interactionsStepDeltaToOpenAIChat(modelName, root, st)
	case "interaction.completed", "finish":
		return appendOpenAIChatCompleted(nil, root, st)
	case "done":
		return nil
	}
	return nil
}

func interactionsStepStartToOpenAIChat(modelName string, root gjson.Result, st *interactionsToOpenAIChatStreamState) [][]byte {
	_ = modelName
	out := ensureOpenAIChatStarted(nil, st)
	index := int(root.Get("index").Int())
	step := root.Get("step")
	stepType := step.Get("type").String()
	st.StepTypes[index] = stepType
	switch stepType {
	case "function_call":
		st.SawToolCall = true
		st.ToolIDs[index] = firstNonEmpty(step.Get("call_id").String(), step.Get("id").String(), fmt.Sprintf("call_%d", index))
		st.ToolNames[index] = step.Get("name").String()
		if st.ToolArguments[index] == nil {
			st.ToolArguments[index] = &strings.Builder{}
		}
		if args := step.Get("arguments"); args.Exists() && strings.TrimSpace(args.Raw) != "{}" {
			st.ToolArguments[index].WriteString(jsonStringValue(args, "{}"))
		}
		return append(out, openAIChatToolCallStartChunk(st, index))
	default:
		return out
	}
}

func interactionsStepDeltaToOpenAIChat(modelName string, root gjson.Result, st *interactionsToOpenAIChatStreamState) [][]byte {
	_ = modelName
	index := int(root.Get("index").Int())
	delta := root.Get("delta")
	out := ensureOpenAIChatStarted(nil, st)
	switch delta.Get("type").String() {
	case "thought_summary":
		text := firstNonEmpty(delta.Get("content.text").String(), delta.Get("text").String())
		if text == "" {
			return out
		}
		return append(out, openAIChatDeltaChunk(st, "reasoning_content", text))
	case "arguments_delta":
		args := delta.Get("arguments").String()
		if st.ToolArguments[index] == nil {
			st.ToolArguments[index] = &strings.Builder{}
		}
		st.ToolArguments[index].WriteString(args)
		return append(out, openAIChatToolCallArgumentsChunk(st, index, args))
	default:
		text := delta.Get("text").String()
		if text == "" {
			return out
		}
		if st.TextByStepIndex[index] == nil {
			st.TextByStepIndex[index] = &strings.Builder{}
		}
		st.TextByStepIndex[index].WriteString(text)
		return append(out, openAIChatDeltaChunk(st, "content", text))
	}
}

func ensureOpenAIChatStarted(out [][]byte, st *interactionsToOpenAIChatStreamState) [][]byte {
	if st.Started {
		return out
	}
	chunk := openAIChatBaseChunk(st)
	chunk, _ = sjson.SetBytes(chunk, "choices.0.delta.role", "assistant")
	st.Started = true
	return append(out, chunk)
}

func appendOpenAIChatCompleted(out [][]byte, root gjson.Result, st *interactionsToOpenAIChatStreamState) [][]byte {
	if st.Completed {
		return out
	}
	out = ensureOpenAIChatStarted(out, st)
	chunk := openAIChatBaseChunk(st)
	finishReason := "stop"
	if st.SawToolCall {
		finishReason = "tool_calls"
	}
	chunk, _ = sjson.SetBytes(chunk, "choices.0.finish_reason", finishReason)
	chunk = setOpenAIChatUsageFromInteractions(chunk, "usage", translatorcommon.InteractionsUsage(root))
	st.Completed = true
	return append(out, chunk)
}

func openAIChatBaseChunk(st *interactionsToOpenAIChatStreamState) []byte {
	chunk := []byte(`{"id":"","object":"chat.completion.chunk","created":0,"model":"","choices":[{"index":0,"delta":{},"finish_reason":null}]}`)
	chunk, _ = sjson.SetBytes(chunk, "id", firstNonEmpty(st.ID, fmt.Sprintf("chatcmpl_%d", time.Now().UnixNano())))
	chunk, _ = sjson.SetBytes(chunk, "created", openAIChatCreated(st))
	chunk, _ = sjson.SetBytes(chunk, "model", st.Model)
	return chunk
}

func openAIChatDeltaChunk(st *interactionsToOpenAIChatStreamState, field, value string) []byte {
	chunk := openAIChatBaseChunk(st)
	chunk, _ = sjson.SetBytes(chunk, "choices.0.delta."+field, value)
	return chunk
}

func openAIChatToolCallStartChunk(st *interactionsToOpenAIChatStreamState, index int) []byte {
	chunk := openAIChatBaseChunk(st)
	toolCall := []byte(`{"index":0,"id":"","type":"function","function":{"name":"","arguments":""}}`)
	toolCall, _ = sjson.SetBytes(toolCall, "index", index)
	toolCall, _ = sjson.SetBytes(toolCall, "id", firstNonEmpty(st.ToolIDs[index], fmt.Sprintf("call_%d", index)))
	toolCall, _ = sjson.SetBytes(toolCall, "function.name", st.ToolNames[index])
	chunk, _ = sjson.SetRawBytes(chunk, "choices.0.delta.tool_calls.-1", toolCall)
	return chunk
}

func openAIChatToolCallArgumentsChunk(st *interactionsToOpenAIChatStreamState, index int, arguments string) []byte {
	chunk := openAIChatBaseChunk(st)
	toolCall := []byte(`{"index":0,"function":{"arguments":""}}`)
	toolCall, _ = sjson.SetBytes(toolCall, "index", index)
	toolCall, _ = sjson.SetBytes(toolCall, "function.arguments", arguments)
	chunk, _ = sjson.SetRawBytes(chunk, "choices.0.delta.tool_calls.-1", toolCall)
	return chunk
}

func openAIChatToolCallFromInteractions(step, fallbackArgs gjson.Result) []byte {
	toolCall := []byte(`{"id":"","type":"function","function":{"name":"","arguments":"{}"}}`)
	callID := firstNonEmpty(step.Get("call_id").String(), step.Get("id").String(), "call_0")
	toolCall, _ = sjson.SetBytes(toolCall, "id", callID)
	toolCall, _ = sjson.SetBytes(toolCall, "function.name", step.Get("name").String())
	args := step.Get("arguments")
	if !args.Exists() {
		args = fallbackArgs
	}
	toolCall, _ = sjson.SetBytes(toolCall, "function.arguments", jsonStringValue(args, "{}"))
	return toolCall
}

func setOpenAIChatUsageFromInteractions(out []byte, path string, usage gjson.Result) []byte {
	if !usage.Exists() {
		return out
	}
	if value, ok := interactionsUsageInt(usage, "input_tokens", "total_input_tokens"); ok {
		out, _ = sjson.SetBytes(out, path+".prompt_tokens", value)
	}
	if value, ok := interactionsUsageInt(usage, "output_tokens", "total_output_tokens"); ok {
		out, _ = sjson.SetBytes(out, path+".completion_tokens", value)
	}
	if value, ok := interactionsUsageInt(usage, "total_tokens"); ok {
		out, _ = sjson.SetBytes(out, path+".total_tokens", value)
	}
	if value, ok := interactionsUsageInt(usage, "cached_tokens", "total_cached_tokens"); ok {
		out, _ = sjson.SetBytes(out, path+".prompt_tokens_details.cached_tokens", value)
	}
	if value, ok := interactionsUsageInt(usage, "reasoning_tokens", "total_thought_tokens"); ok {
		out, _ = sjson.SetBytes(out, path+".completion_tokens_details.reasoning_tokens", value)
	}
	return out
}

func interactionsUsageInt(root gjson.Result, paths ...string) (int64, bool) {
	for _, path := range paths {
		if value := root.Get(path); value.Exists() {
			return value.Int(), true
		}
	}
	return 0, false
}

func interactionsContentTextsForOpenAIChat(content gjson.Result) []string {
	if !content.Exists() {
		return nil
	}
	if content.Type == gjson.String {
		return []string{content.String()}
	}
	var out []string
	content.ForEach(func(_, part gjson.Result) bool {
		if text := firstNonEmpty(part.Get("text").String(), part.Get("content.text").String()); text != "" {
			out = append(out, text)
		}
		return true
	})
	return out
}

func openAIChatCreated(st *interactionsToOpenAIChatStreamState) int64 {
	if st.Created == 0 {
		st.Created = time.Now().Unix()
	}
	return st.Created
}

func (st *interactionsToOpenAIChatStreamState) ensureMaps() {
	if st.StepTypes == nil {
		st.StepTypes = make(map[int]string)
	}
	if st.ToolIDs == nil {
		st.ToolIDs = make(map[int]string)
	}
	if st.ToolNames == nil {
		st.ToolNames = make(map[int]string)
	}
	if st.ToolArguments == nil {
		st.ToolArguments = make(map[int]*strings.Builder)
	}
	if st.TextByStepIndex == nil {
		st.TextByStepIndex = make(map[int]*strings.Builder)
	}
}
