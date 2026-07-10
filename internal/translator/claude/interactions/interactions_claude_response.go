package interactions

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	translatorcommon "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/common"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var claudeInteractionsDataTag = []byte("data:")

type claudeToInteractionsStreamState struct {
	// Builder 承载 interactions SSE 事件的公共流状态与构造逻辑。
	Builder            *translatorcommon.InteractionsSSEBuilder
	UsageRaw           []byte
	CurrentStepByIndex map[int]string
	ToolNames          map[int]string
	ToolIDs            map[int]string
	ToolArgs           map[int]*strings.Builder
}

func ConvertClaudeResponseToInteractions(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	_ = ctx
	_ = originalRequestRawJSON
	_ = requestRawJSON
	if param == nil {
		var local any
		param = &local
	}
	if *param == nil {
		*param = &claudeToInteractionsStreamState{
			Builder: &translatorcommon.InteractionsSSEBuilder{Model: modelName, SetUsage: setInteractionsUsageFromClaude},
		}
	}
	st := (*param).(*claudeToInteractionsStreamState)
	// 同步 Model 到 Builder，保留流式状态在多次调用间的连续性。
	st.Builder.Model = firstNonEmptyString(st.Builder.Model, modelName)
	st.ensureMaps()
	return convertClaudeEventToInteractions(modelName, rawJSON, st)
}

func ConvertClaudeResponseToInteractionsNonStream(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) []byte {
	_ = ctx
	_ = originalRequestRawJSON
	_ = requestRawJSON
	root := gjson.ParseBytes(rawJSON)
	if root.Exists() && root.Get("content").Exists() {
		return convertClaudeMessageToInteractions(modelName, root)
	}
	return convertClaudeSSEToInteractionsNonStream(modelName, rawJSON)
}

func convertClaudeMessageToInteractions(modelName string, root gjson.Result) []byte {
	out := []byte(`{"id":"","object":"interaction","status":"completed","model":"","steps":[]}`)
	out, _ = sjson.SetBytes(out, "id", firstNonEmptyString(root.Get("id").String(), fmt.Sprintf("interaction_%d", time.Now().UnixNano())))
	out, _ = sjson.SetBytes(out, "model", firstNonEmptyString(root.Get("model").String(), modelName))
	root.Get("content").ForEach(func(_, part gjson.Result) bool {
		if step := claudeContentBlockToInteractionsStep(part); len(step) > 0 {
			out, _ = sjson.SetRawBytes(out, "steps.-1", step)
		}
		return true
	})
	out = setInteractionsUsageFromClaude(out, "usage", root.Get("usage"))
	return out
}

func convertClaudeSSEToInteractionsNonStream(modelName string, rawJSON []byte) []byte {
	out := []byte(`{"id":"","object":"interaction","status":"completed","model":"","steps":[]}`)
	out, _ = sjson.SetBytes(out, "id", fmt.Sprintf("interaction_%d", time.Now().UnixNano()))
	out, _ = sjson.SetBytes(out, "model", modelName)
	// 非流式路径不使用 Builder，仅复用 usage 合并与工具追踪能力。
	st := &claudeToInteractionsStreamState{}
	st.ensureMaps()
	scanner := bufio.NewScanner(bytes.NewReader(rawJSON))
	buffer := make([]byte, 1024*1024)
	scanner.Buffer(buffer, 52_428_800)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if !bytes.HasPrefix(line, claudeInteractionsDataTag) {
			continue
		}
		payload := bytes.TrimSpace(line[len(claudeInteractionsDataTag):])
		if bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		root := gjson.ParseBytes(payload)
		switch root.Get("type").String() {
		case "message_start":
			msg := root.Get("message")
			if id := msg.Get("id").String(); id != "" {
				out, _ = sjson.SetBytes(out, "id", id)
			}
			if model := msg.Get("model").String(); model != "" {
				out, _ = sjson.SetBytes(out, "model", model)
			}
			mergeClaudeUsage(st, msg.Get("usage"))
		case "content_block_start":
			claudeNonStreamContentBlockStart(root, st)
		case "content_block_delta":
			claudeNonStreamContentBlockDelta(root, st)
		case "content_block_stop":
			if step := claudeNonStreamContentBlockStop(root, st); len(step) > 0 {
				out, _ = sjson.SetRawBytes(out, "steps.-1", step)
			}
		case "message_delta":
			mergeClaudeUsage(st, root.Get("usage"))
		}
	}
	out = setInteractionsUsageFromClaude(out, "usage", claudeMergedUsage(st))
	return out
}

func convertClaudeEventToInteractions(modelName string, rawJSON []byte, st *claudeToInteractionsStreamState) [][]byte {
	payload := claudeInteractionsSSEPayload(rawJSON)
	if len(payload) == 0 {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(payload), []byte("[DONE]")) {
		return st.Builder.AppendDone(nil)
	}
	root := gjson.ParseBytes(payload)
	switch root.Get("type").String() {
	case "message_start":
		msg := root.Get("message")
		st.Builder.ID = firstNonEmptyString(msg.Get("id").String(), st.Builder.ID, fmt.Sprintf("interaction_%d", time.Now().UnixNano()))
		st.Builder.Model = firstNonEmptyString(msg.Get("model").String(), st.Builder.Model, modelName)
		mergeClaudeUsage(st, msg.Get("usage"))
		return st.Builder.AppendCreated(nil, st.Builder.Model)
	case "content_block_start":
		return claudeContentBlockStartToInteractions(modelName, root, st)
	case "content_block_delta":
		return claudeContentBlockDeltaToInteractions(modelName, root, st)
	case "content_block_stop":
		return claudeContentBlockStopToInteractions(root, st)
	case "message_delta":
		mergeClaudeUsage(st, root.Get("usage"))
		out := st.Builder.AppendStepStop(nil)
		out = st.Builder.AppendCompleted(out, modelName, claudeCompletedUsage(st, root))
		return out
	case "message_stop":
		if st.Builder.Completed {
			return nil
		}
		return st.Builder.AppendCompleted(nil, modelName, claudeCompletedUsage(st, root))
	case "error":
		out := st.Builder.AppendCreated(nil, modelName)
		return st.Builder.AppendCompleted(out, modelName, claudeCompletedUsage(st, root))
	}
	return nil
}

func claudeContentBlockStartToInteractions(modelName string, root gjson.Result, st *claudeToInteractionsStreamState) [][]byte {
	out := st.Builder.AppendCreated(nil, modelName)
	out = st.Builder.AppendStepStop(out)
	index := int(root.Get("index").Int())
	block := root.Get("content_block")
	stepType := claudeBlockInteractionsStepType(block.Get("type").String())
	st.CurrentStepByIndex[index] = stepType
	if stepType == "function_call" {
		if name := block.Get("name").String(); name != "" {
			st.ToolNames[index] = name
		}
		if id := block.Get("id").String(); id != "" {
			st.ToolIDs[index] = id
		}
		if input := block.Get("input"); input.Exists() && input.IsObject() && input.Raw != "{}" {
			builder := &strings.Builder{}
			builder.WriteString(input.Raw)
			st.ToolArgs[index] = builder
		}
	}
	return st.Builder.AppendStepStart(out, claudeStepStartParams(stepType, block.Get("name").String(), block.Get("id").String()))
}

func claudeContentBlockDeltaToInteractions(modelName string, root gjson.Result, st *claudeToInteractionsStreamState) [][]byte {
	index := int(root.Get("index").Int())
	stepType := st.CurrentStepByIndex[index]
	if stepType == "" {
		stepType = claudeDeltaInteractionsStepType(root.Get("delta.type").String())
		out := st.Builder.AppendCreated(nil, modelName)
		out = st.Builder.AppendStepStop(out)
		out = st.Builder.AppendStepStart(out, claudeStepStartParams(stepType, "", ""))
		st.CurrentStepByIndex[index] = stepType
		return appendClaudeDeltaToInteractions(out, st, root.Get("delta"), index)
	}
	if !st.Builder.ActiveStepOpen || st.Builder.ActiveStepIndex != index {
		out := st.Builder.AppendCreated(nil, modelName)
		out = st.Builder.AppendStepStop(out)
		out = st.Builder.AppendStepStart(out, claudeStepStartParams(stepType, st.ToolNames[index], st.ToolIDs[index]))
		return appendClaudeDeltaToInteractions(out, st, root.Get("delta"), index)
	}
	return appendClaudeDeltaToInteractions(nil, st, root.Get("delta"), index)
}

func claudeContentBlockStopToInteractions(root gjson.Result, st *claudeToInteractionsStreamState) [][]byte {
	index := int(root.Get("index").Int())
	out := st.Builder.AppendStepStop(nil)
	delete(st.CurrentStepByIndex, index)
	delete(st.ToolNames, index)
	delete(st.ToolIDs, index)
	delete(st.ToolArgs, index)
	return out
}

func appendClaudeDeltaToInteractions(out [][]byte, st *claudeToInteractionsStreamState, delta gjson.Result, index int) [][]byte {
	switch delta.Get("type").String() {
	case "text_delta":
		return st.Builder.AppendTextDelta(out, delta.Get("text").String())
	case "thinking_delta":
		return st.Builder.AppendThoughtDelta(out, delta.Get("thinking").String())
	case "input_json_delta":
		if st.ToolArgs[index] == nil {
			st.ToolArgs[index] = &strings.Builder{}
		}
		partial := delta.Get("partial_json").String()
		st.ToolArgs[index].WriteString(partial)
		return st.Builder.AppendArgumentsDelta(out, partial)
	}
	return out
}

func claudeContentBlockToInteractionsStep(part gjson.Result) []byte {
	switch part.Get("type").String() {
	case "text":
		step := []byte(`{"type":"model_output","content":[]}`)
		content := []byte(`{"type":"text","text":""}`)
		content, _ = sjson.SetBytes(content, "text", part.Get("text").String())
		step, _ = sjson.SetRawBytes(step, "content.-1", content)
		return step
	case "thinking":
		step := []byte(`{"type":"thought","content":[]}`)
		content := []byte(`{"type":"text","text":""}`)
		content, _ = sjson.SetBytes(content, "text", part.Get("thinking").String())
		step, _ = sjson.SetRawBytes(step, "content.-1", content)
		return step
	case "tool_use":
		return claudeToolUseToInteractionsStep(part, strings.TrimSpace(part.Get("input").Raw))
	}
	return nil
}

func claudeToolUseToInteractionsStep(part gjson.Result, argsRaw string) []byte {
	step := []byte(`{"type":"function_call","name":"","arguments":{}}`)
	step, _ = sjson.SetBytes(step, "name", part.Get("name").String())
	if id := part.Get("id").String(); id != "" {
		step, _ = sjson.SetBytes(step, "id", id)
		step, _ = sjson.SetBytes(step, "call_id", id)
	}
	if argsRaw != "" && gjson.Valid(argsRaw) {
		step, _ = sjson.SetRawBytes(step, "arguments", []byte(argsRaw))
	}
	return step
}

func claudeNonStreamContentBlockStart(root gjson.Result, st *claudeToInteractionsStreamState) {
	index := int(root.Get("index").Int())
	block := root.Get("content_block")
	st.CurrentStepByIndex[index] = claudeBlockInteractionsStepType(block.Get("type").String())
	if block.Get("type").String() != "tool_use" {
		return
	}
	st.ToolNames[index] = block.Get("name").String()
	st.ToolIDs[index] = block.Get("id").String()
	if input := block.Get("input"); input.Exists() && input.IsObject() && input.Raw != "{}" {
		builder := &strings.Builder{}
		builder.WriteString(input.Raw)
		st.ToolArgs[index] = builder
	}
}

func claudeNonStreamContentBlockDelta(root gjson.Result, st *claudeToInteractionsStreamState) {
	index := int(root.Get("index").Int())
	delta := root.Get("delta")
	switch delta.Get("type").String() {
	case "text_delta", "thinking_delta":
		if st.ToolArgs[index] == nil {
			st.ToolArgs[index] = &strings.Builder{}
		}
		if delta.Get("type").String() == "text_delta" {
			st.ToolArgs[index].WriteString(delta.Get("text").String())
		} else {
			st.ToolArgs[index].WriteString(delta.Get("thinking").String())
		}
	case "input_json_delta":
		if st.ToolArgs[index] == nil {
			st.ToolArgs[index] = &strings.Builder{}
		}
		st.ToolArgs[index].WriteString(delta.Get("partial_json").String())
	}
}

func claudeNonStreamContentBlockStop(root gjson.Result, st *claudeToInteractionsStreamState) []byte {
	index := int(root.Get("index").Int())
	stepType := st.CurrentStepByIndex[index]
	builder := st.ToolArgs[index]
	text := ""
	if builder != nil {
		text = builder.String()
	}
	var step []byte
	switch stepType {
	case "thought":
		step = []byte(`{"type":"thought","content":[]}`)
		content := []byte(`{"type":"text","text":""}`)
		content, _ = sjson.SetBytes(content, "text", text)
		step, _ = sjson.SetRawBytes(step, "content.-1", content)
	case "function_call":
		part := []byte(`{"type":"tool_use","id":"","name":"","input":{}}`)
		part, _ = sjson.SetBytes(part, "id", st.ToolIDs[index])
		part, _ = sjson.SetBytes(part, "name", st.ToolNames[index])
		step = claudeToolUseToInteractionsStep(gjson.ParseBytes(part), strings.TrimSpace(text))
	default:
		step = []byte(`{"type":"model_output","content":[]}`)
		content := []byte(`{"type":"text","text":""}`)
		content, _ = sjson.SetBytes(content, "text", text)
		step, _ = sjson.SetRawBytes(step, "content.-1", content)
	}
	delete(st.CurrentStepByIndex, index)
	delete(st.ToolNames, index)
	delete(st.ToolIDs, index)
	delete(st.ToolArgs, index)
	return step
}

func mergeClaudeUsage(st *claudeToInteractionsStreamState, usage gjson.Result) {
	if !usage.Exists() {
		return
	}
	if len(st.UsageRaw) == 0 {
		st.UsageRaw = []byte(`{}`)
	}
	for _, key := range []string{
		"input_tokens",
		"output_tokens",
		"cache_read_input_tokens",
		"cache_creation_input_tokens",
		"thinking_tokens",
	} {
		value := usage.Get(key)
		if !value.Exists() {
			continue
		}
		st.UsageRaw, _ = sjson.SetRawBytes(st.UsageRaw, key, []byte(value.Raw))
	}
}

func claudeMergedUsage(st *claudeToInteractionsStreamState) gjson.Result {
	if len(st.UsageRaw) == 0 {
		return gjson.Result{}
	}
	return gjson.ParseBytes(st.UsageRaw)
}

func setInteractionsUsageFromClaude(out []byte, path string, usage gjson.Result) []byte {
	if !usage.Exists() {
		return out
	}
	inputTokens := usage.Get("input_tokens").Int()
	outputTokens := usage.Get("output_tokens").Int()
	cacheRead := usage.Get("cache_read_input_tokens").Int()
	cacheCreation := usage.Get("cache_creation_input_tokens").Int()
	thinkingTokens := usage.Get("thinking_tokens").Int()
	if usage.Get("input_tokens").Exists() {
		out, _ = sjson.SetBytes(out, path+".input_tokens", inputTokens)
		out, _ = sjson.SetBytes(out, path+".total_input_tokens", inputTokens)
	}
	if usage.Get("output_tokens").Exists() {
		out, _ = sjson.SetBytes(out, path+".output_tokens", outputTokens)
		out, _ = sjson.SetBytes(out, path+".total_output_tokens", outputTokens)
	}
	total := inputTokens + outputTokens
	if usage.Get("input_tokens").Exists() || usage.Get("output_tokens").Exists() {
		out, _ = sjson.SetBytes(out, path+".total_tokens", total)
	}
	if cacheRead != 0 || cacheCreation != 0 {
		out, _ = sjson.SetBytes(out, path+".cached_tokens", cacheRead+cacheCreation)
		out, _ = sjson.SetBytes(out, path+".total_cached_tokens", cacheRead+cacheCreation)
	}
	if thinkingTokens != 0 {
		out, _ = sjson.SetBytes(out, path+".reasoning_tokens", thinkingTokens)
		out, _ = sjson.SetBytes(out, path+".total_thought_tokens", thinkingTokens)
	}
	return out
}

// claudeStepStartParams 根据步骤类型与 function_call 元信息构造 Builder 入参。
// function_call 在 name/id 至少一个非空时由 Builder 生成完整字段，否则退化为仅 type。
func claudeStepStartParams(stepType, name, id string) translatorcommon.StepStartParams {
	params := translatorcommon.StepStartParams{Type: stepType}
	if stepType == "function_call" {
		params.Name = name
		params.CallID = id
	}
	return params
}

// claudeCompletedUsage 返回 completed 事件使用的 usage，优先取已合并的 usage，兜底取事件中的 usage。
func claudeCompletedUsage(st *claudeToInteractionsStreamState, root gjson.Result) gjson.Result {
	usage := claudeMergedUsage(st)
	if !usage.Exists() {
		usage = root.Get("usage")
	}
	return usage
}

func claudeInteractionsSSEPayload(rawJSON []byte) []byte {
	rawJSON = bytes.TrimSpace(rawJSON)
	if bytes.Equal(rawJSON, []byte("[DONE]")) {
		return rawJSON
	}
	if !bytes.HasPrefix(rawJSON, claudeInteractionsDataTag) {
		return nil
	}
	return bytes.TrimSpace(rawJSON[len(claudeInteractionsDataTag):])
}

func claudeBlockInteractionsStepType(blockType string) string {
	switch blockType {
	case "thinking":
		return "thought"
	case "tool_use":
		return "function_call"
	default:
		return "model_output"
	}
}

func claudeDeltaInteractionsStepType(deltaType string) string {
	switch deltaType {
	case "thinking_delta":
		return "thought"
	case "input_json_delta":
		return "function_call"
	default:
		return "model_output"
	}
}

func (st *claudeToInteractionsStreamState) ensureMaps() {
	if st.CurrentStepByIndex == nil {
		st.CurrentStepByIndex = make(map[int]string)
	}
	if st.ToolNames == nil {
		st.ToolNames = make(map[int]string)
	}
	if st.ToolIDs == nil {
		st.ToolIDs = make(map[int]string)
	}
	if st.ToolArgs == nil {
		st.ToolArgs = make(map[int]*strings.Builder)
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
