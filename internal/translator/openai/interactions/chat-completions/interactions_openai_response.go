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
	Created         bool
	StatusUpdated   bool
	Completed       bool
	Done            bool
	CurrentStepType string
	CurrentStepID   string
	ToolCallIDs     map[int]string
	ToolCallNames   map[int]string
	ID              string
	StepIndex       int
	ActiveStepIndex int
	ActiveStepOpen  bool
	Usage           gjson.Result
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
		*param = &openAIToInteractionsStreamState{}
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
		out = appendInteractionsStepStop(out, st)
		if !st.Completed {
			out = appendInteractionsCompleted(out, st, modelName, gjson.Result{})
		}
		return appendInteractionsDone(out, st)
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
				out = appendInteractionsStepStop(out, st)
				out = appendInteractionsCompleted(out, st, modelName, root)
			}
			return out
		}
		choices.ForEach(func(_, choice gjson.Result) bool {
			delta := choice.Get("delta")
			if reasoning := delta.Get("reasoning_content"); reasoning.Exists() {
				for _, text := range openAIReasoningTexts(reasoning) {
					out = ensureInteractionsStep(out, st, modelName, "thought", root)
					out = appendInteractionsTextDelta(out, st, text, true)
				}
			}
			if content := delta.Get("content"); content.Exists() && content.String() != "" {
				out = ensureInteractionsStep(out, st, modelName, "model_output", root)
				out = appendInteractionsTextDelta(out, st, content.String(), false)
			}
			if toolCalls := delta.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
				toolCalls.ForEach(func(_, toolCall gjson.Result) bool {
					out = appendOpenAIToolCallDelta(out, st, modelName, root, toolCall)
					return true
				})
			}
			if finishReason := choice.Get("finish_reason"); finishReason.Exists() {
				out = appendInteractionsStepStop(out, st)
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
	if st.CurrentStepType != "function_call" || st.CurrentStepID != stepID {
		out = appendInteractionsStepStop(out, st)
		step := []byte(`{"type":"function_call","id":"","call_id":"","name":"","arguments":{}}`)
		step, _ = sjson.SetBytes(step, "id", stepID)
		step, _ = sjson.SetBytes(step, "call_id", stepID)
		step, _ = sjson.SetBytes(step, "name", stepName)
		out = appendInteractionsCreated(out, st, modelName, root)
		out = appendInteractionsStepStart(out, st, "function_call", gjson.ParseBytes(step))
	}
	if args := function.Get("arguments"); args.Exists() && args.String() != "" {
		out = appendInteractionsArgumentsDelta(out, st, args.String())
	}
	return out
}

func appendInteractionsCreated(out [][]byte, st *openAIToInteractionsStreamState, modelName string, root gjson.Result) [][]byte {
	if st.Created {
		return out
	}
	st.ID = firstNonEmpty(root.Get("id").String(), st.ID, fmt.Sprintf("interaction_%d", time.Now().UnixNano()))
	created := []byte(`{"interaction":{"id":"","status":"in_progress","object":"interaction","model":""},"event_type":"interaction.created"}`)
	created, _ = sjson.SetBytes(created, "interaction.id", st.ID)
	created, _ = sjson.SetBytes(created, "interaction.model", firstNonEmpty(modelName, root.Get("model").String()))
	out = append(out, translatorcommon.SSEEventData("interaction.created", created))
	st.Created = true
	return appendInteractionsStatusUpdate(out, st)
}

func appendInteractionsStatusUpdate(out [][]byte, st *openAIToInteractionsStreamState) [][]byte {
	if st.StatusUpdated {
		return out
	}
	statusUpdate := []byte(`{"interaction_id":"","status":"in_progress","event_type":"interaction.status_update"}`)
	statusUpdate, _ = sjson.SetBytes(statusUpdate, "interaction_id", st.ID)
	out = append(out, translatorcommon.SSEEventData("interaction.status_update", statusUpdate))
	st.StatusUpdated = true
	return out
}

func ensureInteractionsStep(out [][]byte, st *openAIToInteractionsStreamState, modelName, stepType string, step gjson.Result) [][]byte {
	out = appendInteractionsCreated(out, st, modelName, step)
	if st.ActiveStepOpen && st.CurrentStepType == stepType {
		return out
	}
	out = appendInteractionsStepStop(out, st)
	return appendInteractionsStepStart(out, st, stepType, step)
}

func appendInteractionsStepStart(out [][]byte, st *openAIToInteractionsStreamState, stepType string, step gjson.Result) [][]byte {
	index := st.StepIndex
	st.StepIndex++
	st.ActiveStepIndex = index
	st.CurrentStepType = stepType
	st.ActiveStepOpen = true
	payload := []byte(`{"index":0,"step":{"type":""},"event_type":"step.start"}`)
	payload, _ = sjson.SetBytes(payload, "index", index)
	payload, _ = sjson.SetBytes(payload, "step.type", stepType)
	if stepType == "function_call" {
		id := firstNonEmpty(step.Get("call_id").String(), step.Get("id").String(), st.CurrentStepID)
		st.CurrentStepID = id
		if id != "" {
			payload, _ = sjson.SetBytes(payload, "step.id", id)
			payload, _ = sjson.SetBytes(payload, "step.call_id", id)
		}
		payload, _ = sjson.SetBytes(payload, "step.name", step.Get("name").String())
		payload, _ = sjson.SetRawBytes(payload, "step.arguments", []byte(`{}`))
	} else {
		st.CurrentStepID = ""
	}
	return append(out, translatorcommon.SSEEventData("step.start", payload))
}

func appendInteractionsTextDelta(out [][]byte, st *openAIToInteractionsStreamState, text string, thought bool) [][]byte {
	if thought {
		payload := []byte(`{"index":0,"delta":{"content":{"text":"","type":"text"},"type":"thought_summary"},"event_type":"step.delta"}`)
		payload, _ = sjson.SetBytes(payload, "index", st.ActiveStepIndex)
		payload, _ = sjson.SetBytes(payload, "delta.content.text", text)
		return append(out, translatorcommon.SSEEventData("step.delta", payload))
	}
	payload := []byte(`{"index":0,"delta":{"text":"","type":"text"},"event_type":"step.delta"}`)
	payload, _ = sjson.SetBytes(payload, "index", st.ActiveStepIndex)
	payload, _ = sjson.SetBytes(payload, "delta.text", text)
	return append(out, translatorcommon.SSEEventData("step.delta", payload))
}

func appendInteractionsArgumentsDelta(out [][]byte, st *openAIToInteractionsStreamState, arguments string) [][]byte {
	payload := []byte(`{"index":0,"delta":{"arguments":"","type":"arguments_delta"},"event_type":"step.delta"}`)
	payload, _ = sjson.SetBytes(payload, "index", st.ActiveStepIndex)
	payload, _ = sjson.SetBytes(payload, "delta.arguments", arguments)
	return append(out, translatorcommon.SSEEventData("step.delta", payload))
}

func appendInteractionsStepStop(out [][]byte, st *openAIToInteractionsStreamState) [][]byte {
	if !st.ActiveStepOpen {
		return out
	}
	payload := []byte(`{"index":0,"event_type":"step.stop"}`)
	payload, _ = sjson.SetBytes(payload, "index", st.ActiveStepIndex)
	out = append(out, translatorcommon.SSEEventData("step.stop", payload))
	st.ActiveStepOpen = false
	st.CurrentStepType = ""
	st.CurrentStepID = ""
	return out
}

func appendInteractionsCompleted(out [][]byte, st *openAIToInteractionsStreamState, modelName string, root gjson.Result) [][]byte {
	if st.Completed {
		return out
	}
	if !st.Created {
		out = appendInteractionsCreated(out, st, modelName, root)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	payload := []byte(`{"interaction":{"id":"","status":"completed","usage":{},"created":"","updated":"","service_tier":"standard","object":"interaction","model":""},"event_type":"interaction.completed"}`)
	payload, _ = sjson.SetBytes(payload, "interaction.id", st.ID)
	payload, _ = sjson.SetBytes(payload, "interaction.created", now)
	payload, _ = sjson.SetBytes(payload, "interaction.updated", now)
	payload, _ = sjson.SetBytes(payload, "interaction.model", firstNonEmpty(modelName, root.Get("model").String()))
	usage := root.Get("usage")
	if !usage.Exists() {
		usage = st.Usage
	}
	payload = setInteractionsUsageFromOpenAIChat(payload, "interaction.usage", usage)
	out = append(out, translatorcommon.SSEEventData("interaction.completed", payload))
	st.Completed = true
	return out
}

func appendInteractionsDone(out [][]byte, st *openAIToInteractionsStreamState) [][]byte {
	if st.Done {
		return out
	}
	out = append(out, translatorcommon.SSEEventData("done", []byte("[DONE]")))
	st.Done = true
	return out
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
