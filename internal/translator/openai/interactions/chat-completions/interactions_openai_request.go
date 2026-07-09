package chat_completions

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func ConvertInteractionsRequestToOpenAI(modelName string, inputRawJSON []byte, stream bool) []byte {
	root := gjson.ParseBytes(inputRawJSON)
	out := []byte(`{"model":"","messages":[]}`)
	out, _ = sjson.SetBytes(out, "model", firstNonEmpty(modelName, root.Get("model").String()))
	if stream || root.Get("stream").Bool() {
		out, _ = sjson.SetBytes(out, "stream", true)
	}
	out = copyInteractionsSystemToOpenAI(out, root)
	out = appendInteractionsInputToOpenAIMessages(out, root.Get("input"))
	out = copyInteractionsToolsToOpenAI(out, root)
	out = copyInteractionsGenerationConfigToOpenAI(out, root)
	out = copyInteractionsOpenAITopLevel(out, root)
	return out
}

func copyInteractionsSystemToOpenAI(out []byte, root gjson.Result) []byte {
	text := interactionsText(root.Get("system_instruction"))
	if text == "" {
		return out
	}
	msg := []byte(`{"role":"system","content":""}`)
	msg, _ = sjson.SetBytes(msg, "content", text)
	out, _ = sjson.SetRawBytes(out, "messages.-1", msg)
	return out
}

func appendInteractionsInputToOpenAIMessages(out []byte, input gjson.Result) []byte {
	if input.Type == gjson.String {
		msg := []byte(`{"role":"user","content":""}`)
		msg, _ = sjson.SetBytes(msg, "content", input.String())
		out, _ = sjson.SetRawBytes(out, "messages.-1", msg)
		return out
	}
	if input.IsArray() {
		input.ForEach(func(_, step gjson.Result) bool {
			out = appendInteractionsStepToOpenAI(out, step, "user")
			return true
		})
		return out
	}
	if input.IsObject() {
		return appendInteractionsStepToOpenAI(out, input, "user")
	}
	return out
}

func appendInteractionsStepToOpenAI(out []byte, step gjson.Result, defaultRole string) []byte {
	switch step.Get("type").String() {
	case "user_input":
		return appendInteractionsMessageToOpenAI(out, step, "user")
	case "model_output":
		return appendInteractionsMessageToOpenAI(out, step, "assistant")
	case "thought":
		return appendInteractionsThoughtToOpenAI(out, step)
	case "function_call":
		return appendInteractionsFunctionCallToOpenAI(out, step)
	case "function_result":
		return appendInteractionsFunctionResultToOpenAI(out, step)
	default:
		if step.Type == gjson.String {
			msg := []byte(`{"role":"","content":""}`)
			msg, _ = sjson.SetBytes(msg, "role", defaultRole)
			msg, _ = sjson.SetBytes(msg, "content", step.String())
			out, _ = sjson.SetRawBytes(out, "messages.-1", msg)
		}
	}
	return out
}

func appendInteractionsMessageToOpenAI(out []byte, step gjson.Result, role string) []byte {
	msg := []byte(`{"role":"","content":""}`)
	msg, _ = sjson.SetBytes(msg, "role", role)
	content := step.Get("content")
	if content.Type == gjson.String {
		msg, _ = sjson.SetBytes(msg, "content", content.String())
		out, _ = sjson.SetRawBytes(out, "messages.-1", msg)
		return out
	}
	msg = appendInteractionsContentToOpenAIMessage(msg, content, role)
	out, _ = sjson.SetRawBytes(out, "messages.-1", msg)
	return out
}

func appendInteractionsThoughtToOpenAI(out []byte, step gjson.Result) []byte {
	msg := []byte(`{"role":"assistant","content":"","reasoning_content":""}`)
	msg, _ = sjson.SetBytes(msg, "reasoning_content", interactionsText(step.Get("content")))
	out, _ = sjson.SetRawBytes(out, "messages.-1", msg)
	return out
}

func appendInteractionsContentToOpenAIMessage(msg []byte, content gjson.Result, role string) []byte {
	if !content.Exists() {
		return msg
	}
	if content.Type == gjson.String {
		msg, _ = sjson.SetBytes(msg, "content", content.String())
		return msg
	}
	contentWrapper := []byte(`{"items":[]}`)
	textOnly := true
	var textBuilder strings.Builder
	appendPart := func(part gjson.Result) {
		converted, ok := interactionsContentPartToOpenAI(part, role)
		if !ok {
			return
		}
		if gjson.GetBytes(converted, "type").String() == "text" {
			textBuilder.WriteString(gjson.GetBytes(converted, "text").String())
		} else {
			textOnly = false
		}
		contentWrapper, _ = sjson.SetRawBytes(contentWrapper, "items.-1", converted)
	}
	if content.IsArray() {
		content.ForEach(func(_, part gjson.Result) bool {
			appendPart(part)
			return true
		})
	} else if content.IsObject() {
		appendPart(content)
	}
	if count := gjson.GetBytes(contentWrapper, "items.#").Int(); count > 0 {
		if textOnly {
			msg, _ = sjson.SetBytes(msg, "content", textBuilder.String())
		} else {
			msg, _ = sjson.SetRawBytes(msg, "content", []byte(gjson.GetBytes(contentWrapper, "items").Raw))
		}
	}
	return msg
}

func appendInteractionsFunctionCallToOpenAI(out []byte, step gjson.Result) []byte {
	msg := []byte(`{"role":"assistant","content":"","tool_calls":[]}`)
	toolCall := []byte(`{"id":"","type":"function","function":{"name":"","arguments":"{}"}}`)
	callID := firstNonEmpty(step.Get("call_id").String(), step.Get("id").String(), "call_0")
	toolCall, _ = sjson.SetBytes(toolCall, "id", callID)
	toolCall, _ = sjson.SetBytes(toolCall, "function.name", step.Get("name").String())
	toolCall, _ = sjson.SetBytes(toolCall, "function.arguments", jsonStringValue(step.Get("arguments"), "{}"))
	msg, _ = sjson.SetRawBytes(msg, "tool_calls.-1", toolCall)
	out, _ = sjson.SetRawBytes(out, "messages.-1", msg)
	return out
}

func appendInteractionsFunctionResultToOpenAI(out []byte, step gjson.Result) []byte {
	msg := []byte(`{"role":"tool","tool_call_id":"","content":""}`)
	msg, _ = sjson.SetBytes(msg, "tool_call_id", firstNonEmpty(step.Get("call_id").String(), step.Get("id").String()))
	msg, _ = sjson.SetBytes(msg, "content", jsonStringValue(firstExistingResult(step.Get("result"), step.Get("output")), ""))
	out, _ = sjson.SetRawBytes(out, "messages.-1", msg)
	return out
}

func copyInteractionsToolsToOpenAI(out []byte, root gjson.Result) []byte {
	tools := root.Get("tools")
	if !tools.Exists() || !tools.IsArray() {
		return out
	}
	tools.ForEach(func(_, tool gjson.Result) bool {
		if converted, ok := openAIToolFromInteractionsTool(tool); ok {
			out, _ = sjson.SetRawBytes(out, "tools.-1", converted)
		}
		if decls := firstExistingResult(tool.Get("function_declarations"), tool.Get("functionDeclarations")); decls.Exists() && decls.IsArray() {
			decls.ForEach(func(_, decl gjson.Result) bool {
				if converted, ok := openAIToolFromInteractionsTool(decl); ok {
					out, _ = sjson.SetRawBytes(out, "tools.-1", converted)
				}
				return true
			})
		}
		return true
	})
	return out
}

func copyInteractionsGenerationConfigToOpenAI(out []byte, root gjson.Result) []byte {
	gen := root.Get("generation_config")
	if !gen.Exists() {
		gen = root.Get("generationConfig")
	}
	copyNumber(&out, "temperature", firstExistingResult(gen.Get("temperature"), root.Get("temperature")))
	copyNumber(&out, "max_tokens", firstExistingResult(gen.Get("max_output_tokens"), gen.Get("maxOutputTokens"), root.Get("max_tokens"), root.Get("max_completion_tokens")))
	copyNumber(&out, "top_p", firstExistingResult(gen.Get("top_p"), gen.Get("topP"), root.Get("top_p")))
	copyNumber(&out, "top_k", firstExistingResult(gen.Get("top_k"), gen.Get("topK")))
	copyNumber(&out, "n", firstExistingResult(gen.Get("candidate_count"), gen.Get("candidateCount"), root.Get("n")))
	if stop := firstExistingResult(gen.Get("stop_sequences"), gen.Get("stopSequences"), root.Get("stop")); stop.Exists() {
		out, _ = sjson.SetRawBytes(out, "stop", []byte(stop.Raw))
	}
	if toolChoice := firstExistingResult(gen.Get("tool_choice"), root.Get("tool_choice")); toolChoice.Exists() {
		out, _ = sjson.SetRawBytes(out, "tool_choice", []byte(toolChoice.Raw))
	}
	if effort := interactionsReasoningEffort(root, gen); effort != "" {
		out, _ = sjson.SetBytes(out, "reasoning_effort", effort)
	}
	if responseModalities := root.Get("response_modalities"); responseModalities.Exists() {
		out, _ = sjson.SetRawBytes(out, "modalities", []byte(responseModalities.Raw))
	}
	return out
}

func copyInteractionsOpenAITopLevel(out []byte, root gjson.Result) []byte {
	if format := root.Get("response_format"); format.Exists() {
		out, _ = sjson.SetRawBytes(out, "response_format", []byte(format.Raw))
	}
	if serviceTier := root.Get("service_tier"); serviceTier.Exists() && serviceTier.Type == gjson.String {
		out, _ = sjson.SetBytes(out, "service_tier", serviceTier.String())
	}
	for _, key := range []string{"parallel_tool_calls", "seed", "user"} {
		if value := root.Get(key); value.Exists() {
			out, _ = sjson.SetRawBytes(out, key, []byte(value.Raw))
		}
	}
	return out
}

func interactionsContentPartToOpenAI(part gjson.Result, role string) ([]byte, bool) {
	partType := part.Get("type").String()
	if partType == "" && part.Get("text").Exists() {
		partType = "text"
	}
	switch partType {
	case "text":
		out := []byte(`{"type":"text","text":""}`)
		out, _ = sjson.SetBytes(out, "text", part.Get("text").String())
		return out, true
	case "image":
		out := []byte(`{"type":"image_url","image_url":{"url":""}}`)
		out, _ = sjson.SetBytes(out, "image_url.url", interactionsMediaDataURL(part, "application/octet-stream"))
		return out, true
	case "audio":
		out := []byte(`{"type":"input_audio","input_audio":{"data":"","format":""}}`)
		out, _ = sjson.SetBytes(out, "input_audio.data", part.Get("data").String())
		out, _ = sjson.SetBytes(out, "input_audio.format", openAIInputAudioFormatFromMIME(part.Get("mime_type").String()))
		return out, true
	case "video":
		out := []byte(`{"type":"video_url","video_url":{"url":""}}`)
		out, _ = sjson.SetBytes(out, "video_url.url", interactionsMediaDataURL(part, "video/mp4"))
		return out, true
	case "document", "file":
		out := []byte(`{"type":"file","file":{"filename":"","file_data":""}}`)
		out, _ = sjson.SetBytes(out, "file.filename", firstNonEmpty(part.Get("filename").String(), openAIFileNameFromMIME(part.Get("mime_type").String())))
		out, _ = sjson.SetBytes(out, "file.file_data", part.Get("data").String())
		if url := firstNonEmpty(part.Get("file_url").String(), part.Get("url").String()); url != "" {
			out, _ = sjson.DeleteBytes(out, "file.file_data")
			out, _ = sjson.SetBytes(out, "file.file_url", url)
		}
		return out, true
	default:
		_ = role
	}
	return nil, false
}

func openAIToolFromInteractionsTool(tool gjson.Result) ([]byte, bool) {
	name := firstNonEmpty(tool.Get("name").String(), tool.Get("function.name").String())
	if name == "" {
		return nil, false
	}
	out := []byte(`{"type":"function","function":{"name":""}}`)
	out, _ = sjson.SetBytes(out, "function.name", name)
	if desc := firstExistingResult(tool.Get("description"), tool.Get("function.description")); desc.Exists() {
		out, _ = sjson.SetBytes(out, "function.description", desc.String())
	}
	if params := firstExistingResult(tool.Get("parameters"), tool.Get("function.parameters"), tool.Get("parametersJsonSchema")); params.Exists() {
		out, _ = sjson.SetRawBytes(out, "function.parameters", []byte(params.Raw))
	}
	return out, true
}

func interactionsText(value gjson.Result) string {
	if !value.Exists() {
		return ""
	}
	if value.Type == gjson.String {
		return value.String()
	}
	if text := value.Get("text"); text.Exists() {
		return text.String()
	}
	for _, path := range []string{"content", "parts"} {
		parts := value.Get(path)
		if !parts.Exists() || !parts.IsArray() {
			continue
		}
		var builder strings.Builder
		parts.ForEach(func(_, part gjson.Result) bool {
			builder.WriteString(firstNonEmpty(part.Get("text").String(), part.Get("content.text").String()))
			return true
		})
		return builder.String()
	}
	return ""
}

func interactionsReasoningEffort(root, gen gjson.Result) string {
	for _, value := range []gjson.Result{
		gen.Get("reasoning_effort"),
		gen.Get("thinking_level"),
		gen.Get("thinkingLevel"),
		gen.Get("thinking_config.thinking_level"),
		gen.Get("thinkingConfig.thinkingLevel"),
		root.Get("reasoning_effort"),
	} {
		if value.Exists() && value.Type == gjson.String {
			return strings.ToLower(strings.TrimSpace(value.String()))
		}
	}
	return ""
}

func interactionsMediaDataURL(part gjson.Result, fallbackMimeType string) string {
	if url := firstNonEmpty(part.Get("image_url").String(), part.Get("file_data").String(), part.Get("url").String()); url != "" {
		return url
	}
	data := part.Get("data").String()
	if data == "" {
		return ""
	}
	mimeType := firstNonEmpty(part.Get("mime_type").String(), fallbackMimeType)
	return "data:" + mimeType + ";base64," + data
}

func openAIInputAudioFormatFromMIME(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "audio/wav", "audio/wave", "audio/x-wav":
		return "wav"
	case "audio/flac":
		return "flac"
	case "audio/opus", "audio/ogg":
		return "opus"
	case "audio/pcm", "audio/l16":
		return "pcm16"
	default:
		return "mp3"
	}
}

func openAIFileNameFromMIME(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "application/pdf":
		return "document.pdf"
	case "text/plain":
		return "document.txt"
	case "text/csv":
		return "document.csv"
	case "application/json":
		return "document.json"
	default:
		if _, suffix, ok := strings.Cut(mimeType, "/"); ok && suffix != "" {
			return fmt.Sprintf("document.%s", strings.ReplaceAll(suffix, "+", "."))
		}
		return "document.bin"
	}
}

func copyNumber(out *[]byte, path string, value gjson.Result) {
	if value.Exists() {
		*out, _ = sjson.SetRawBytes(*out, path, []byte(value.Raw))
	}
}

func jsonStringValue(value gjson.Result, fallback string) string {
	if !value.Exists() {
		return fallback
	}
	if value.Type == gjson.String {
		return value.String()
	}
	return value.Raw
}

func firstExistingResult(values ...gjson.Result) gjson.Result {
	for _, value := range values {
		if value.Exists() {
			return value
		}
	}
	return gjson.Result{}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func ConvertOpenAIRequestToInteractions(modelName string, inputRawJSON []byte, stream bool) []byte {
	root := gjson.ParseBytes(inputRawJSON)
	out := []byte(`{"model":"","input":[]}`)
	out, _ = sjson.SetBytes(out, "model", firstNonEmpty(modelName, root.Get("model").String()))
	if streamValue, ok := openAIRequestStreamValue(root, stream); ok {
		out, _ = sjson.SetBytes(out, "stream", streamValue)
	}
	out = appendOpenAIMessagesToInteractions(out, root.Get("messages"))
	out = copyOpenAIChatGenerationConfigToInteractions(out, root)
	out = appendOpenAIChatToolsToInteractions(out, root.Get("tools"))
	return out
}

func openAIRequestStreamValue(root gjson.Result, stream bool) (bool, bool) {
	if value := root.Get("stream"); value.Exists() {
		return value.Bool(), true
	}
	if stream {
		return true, true
	}
	return false, false
}

func appendOpenAIMessagesToInteractions(out []byte, messages gjson.Result) []byte {
	if !messages.Exists() || !messages.IsArray() {
		return out
	}
	var systemBuilder strings.Builder
	messages.ForEach(func(_, message gjson.Result) bool {
		role := strings.ToLower(strings.TrimSpace(message.Get("role").String()))
		switch role {
		case "system", "developer":
			if text := openAIChatContentText(message.Get("content")); text != "" {
				if systemBuilder.Len() > 0 {
					systemBuilder.WriteByte('\n')
				}
				systemBuilder.WriteString(text)
			}
		default:
			out = appendOpenAIMessageToInteractions(out, message)
		}
		return true
	})
	if systemBuilder.Len() > 0 {
		out, _ = sjson.SetBytes(out, "system_instruction", systemBuilder.String())
	}
	return out
}

func appendOpenAIMessageToInteractions(out []byte, message gjson.Result) []byte {
	role := strings.ToLower(strings.TrimSpace(message.Get("role").String()))
	switch role {
	case "assistant":
		if reasoning := message.Get("reasoning_content"); reasoning.Exists() {
			for _, text := range openAIReasoningTexts(reasoning) {
				out, _ = sjson.SetRawBytes(out, "input.-1", interactionsTextStep("thought", text))
			}
		}
		if step, ok := openAIChatContentStep("model_output", message.Get("content")); ok {
			out, _ = sjson.SetRawBytes(out, "input.-1", step)
		}
		if toolCalls := message.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
			toolCalls.ForEach(func(_, toolCall gjson.Result) bool {
				if step, ok := openAIToolCallToInteractionsStep(toolCall); ok {
					out, _ = sjson.SetRawBytes(out, "input.-1", step)
				}
				return true
			})
		}
	case "tool", "function":
		out, _ = sjson.SetRawBytes(out, "input.-1", openAIToolResultToInteractions(message))
	default:
		if step, ok := openAIChatContentStep("user_input", message.Get("content")); ok {
			out, _ = sjson.SetRawBytes(out, "input.-1", step)
		}
	}
	return out
}

func openAIChatContentStep(stepType string, content gjson.Result) ([]byte, bool) {
	step := []byte(`{"type":"","content":[]}`)
	step, _ = sjson.SetBytes(step, "type", stepType)
	if content.Type == gjson.String {
		if content.String() == "" {
			return nil, false
		}
		part := []byte(`{"type":"text","text":""}`)
		part, _ = sjson.SetBytes(part, "text", content.String())
		step, _ = sjson.SetRawBytes(step, "content.-1", part)
		return step, true
	}
	appendPart := func(part gjson.Result) {
		if converted, ok := openAIChatContentPartToInteractions(part); ok {
			step, _ = sjson.SetRawBytes(step, "content.-1", converted)
		}
	}
	if content.IsArray() {
		content.ForEach(func(_, part gjson.Result) bool {
			appendPart(part)
			return true
		})
	} else if content.IsObject() {
		appendPart(content)
	}
	return step, gjson.GetBytes(step, "content.#").Int() > 0
}

func openAIChatContentPartToInteractions(part gjson.Result) ([]byte, bool) {
	partType := strings.ToLower(strings.TrimSpace(part.Get("type").String()))
	if partType == "" && part.Get("text").Exists() {
		partType = "text"
	}
	switch partType {
	case "text", "input_text", "output_text":
		out := []byte(`{"type":"text","text":""}`)
		out, _ = sjson.SetBytes(out, "text", part.Get("text").String())
		return out, true
	case "image_url", "input_image", "image":
		return openAIChatImagePartToInteractions(part), true
	case "input_audio", "audio":
		out := []byte(`{"type":"audio","data":""}`)
		audio := part.Get("input_audio")
		data := firstNonEmpty(audio.Get("data").String(), part.Get("data").String())
		if data == "" {
			return nil, false
		}
		out, _ = sjson.SetBytes(out, "data", data)
		if format := firstNonEmpty(audio.Get("format").String(), part.Get("format").String()); format != "" {
			out, _ = sjson.SetBytes(out, "mime_type", openAIInputAudioMIMEType(format))
		}
		return out, true
	case "file", "input_file", "document":
		file := part.Get("file")
		out := []byte(`{"type":"document"}`)
		if filename := firstNonEmpty(file.Get("filename").String(), part.Get("filename").String()); filename != "" {
			out, _ = sjson.SetBytes(out, "filename", filename)
		}
		if data := firstNonEmpty(file.Get("file_data").String(), part.Get("file_data").String(), part.Get("data").String()); data != "" {
			out, _ = sjson.SetBytes(out, "data", data)
		}
		if url := firstNonEmpty(file.Get("file_url").String(), part.Get("file_url").String(), part.Get("url").String()); url != "" {
			out, _ = sjson.SetBytes(out, "file_url", url)
		}
		return out, true
	}
	return nil, false
}

func openAIChatImagePartToInteractions(part gjson.Result) []byte {
	out := []byte(`{"type":"image"}`)
	imageURL := firstNonEmpty(part.Get("image_url.url").String(), part.Get("image_url").String(), part.Get("url").String())
	if mimeType, data, ok := openAIChatParseDataURL(imageURL); ok {
		out, _ = sjson.SetBytes(out, "mime_type", mimeType)
		out, _ = sjson.SetBytes(out, "data", data)
		return out
	}
	if data := part.Get("data").String(); data != "" {
		out, _ = sjson.SetBytes(out, "data", data)
		if mimeType := part.Get("mime_type").String(); mimeType != "" {
			out, _ = sjson.SetBytes(out, "mime_type", mimeType)
		}
		return out
	}
	if imageURL != "" {
		out, _ = sjson.SetBytes(out, "image_url", imageURL)
	}
	return out
}

func openAIToolResultToInteractions(message gjson.Result) []byte {
	out := []byte(`{"type":"function_result","result":""}`)
	if callID := firstNonEmpty(message.Get("tool_call_id").String(), message.Get("id").String()); callID != "" {
		out, _ = sjson.SetBytes(out, "id", callID)
		out, _ = sjson.SetBytes(out, "call_id", callID)
	}
	if name := message.Get("name").String(); name != "" {
		out, _ = sjson.SetBytes(out, "name", name)
	}
	content := message.Get("content")
	if content.Exists() && content.Type == gjson.String {
		out, _ = sjson.SetBytes(out, "result", content.String())
	} else if content.Exists() {
		out, _ = sjson.SetRawBytes(out, "result", []byte(content.Raw))
	}
	return out
}

func copyOpenAIChatGenerationConfigToInteractions(out []byte, root gjson.Result) []byte {
	copyNumber(&out, "generation_config.max_output_tokens", firstExistingResult(root.Get("max_completion_tokens"), root.Get("max_tokens")))
	copyNumber(&out, "generation_config.temperature", root.Get("temperature"))
	copyNumber(&out, "generation_config.top_p", root.Get("top_p"))
	copyNumber(&out, "generation_config.presence_penalty", root.Get("presence_penalty"))
	copyNumber(&out, "generation_config.frequency_penalty", root.Get("frequency_penalty"))
	copyNumber(&out, "generation_config.candidate_count", root.Get("n"))
	if stop := root.Get("stop"); stop.Exists() {
		out, _ = sjson.SetRawBytes(out, "generation_config.stop_sequences", []byte(stop.Raw))
	}
	if toolChoice := root.Get("tool_choice"); toolChoice.Exists() {
		out, _ = sjson.SetRawBytes(out, "generation_config.tool_choice", []byte(toolChoice.Raw))
	}
	if effort := root.Get("reasoning_effort"); effort.Exists() && effort.Type == gjson.String {
		out, _ = sjson.SetBytes(out, "generation_config.thinking_level", strings.ToLower(strings.TrimSpace(effort.String())))
	}
	if responseFormat := root.Get("response_format"); responseFormat.Exists() {
		out, _ = sjson.SetRawBytes(out, "response_format", []byte(responseFormat.Raw))
	}
	if modalities := root.Get("modalities"); modalities.Exists() {
		out, _ = sjson.SetRawBytes(out, "response_modalities", []byte(modalities.Raw))
	}
	if serviceTier := root.Get("service_tier"); serviceTier.Exists() && serviceTier.Type == gjson.String {
		out, _ = sjson.SetBytes(out, "service_tier", serviceTier.String())
	}
	return out
}

func appendOpenAIChatToolsToInteractions(out []byte, tools gjson.Result) []byte {
	if !tools.Exists() || !tools.IsArray() {
		return out
	}
	tools.ForEach(func(_, tool gjson.Result) bool {
		if converted, ok := openAIChatToolToInteractions(tool); ok {
			out, _ = sjson.SetRawBytes(out, "tools.-1", converted)
		}
		return true
	})
	return out
}

func openAIChatToolToInteractions(tool gjson.Result) ([]byte, bool) {
	toolType := strings.ToLower(strings.TrimSpace(tool.Get("type").String()))
	if toolType != "" && toolType != "function" {
		return nil, false
	}
	name := firstNonEmpty(tool.Get("function.name").String(), tool.Get("name").String())
	if name == "" {
		return nil, false
	}
	out := []byte(`{"type":"function","name":""}`)
	out, _ = sjson.SetBytes(out, "name", name)
	if desc := firstExistingResult(tool.Get("function.description"), tool.Get("description")); desc.Exists() {
		out, _ = sjson.SetBytes(out, "description", desc.String())
	}
	if parameters := firstExistingResult(tool.Get("function.parameters"), tool.Get("parameters")); parameters.Exists() {
		out, _ = sjson.SetRawBytes(out, "parameters", []byte(parameters.Raw))
	}
	return out, true
}

func openAIChatContentText(content gjson.Result) string {
	if content.Type == gjson.String {
		return content.String()
	}
	if content.IsObject() {
		return content.Get("text").String()
	}
	if !content.IsArray() {
		return ""
	}
	var builder strings.Builder
	content.ForEach(func(_, part gjson.Result) bool {
		if text := part.Get("text").String(); text != "" {
			builder.WriteString(text)
		}
		return true
	})
	return builder.String()
}

func openAIInputAudioMIMEType(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "wav":
		return "audio/wav"
	case "flac":
		return "audio/flac"
	case "opus":
		return "audio/opus"
	case "pcm16":
		return "audio/pcm"
	default:
		return "audio/mpeg"
	}
}

func openAIChatParseDataURL(value string) (string, string, bool) {
	if !strings.HasPrefix(value, "data:") {
		return "", "", false
	}
	meta, data, ok := strings.Cut(strings.TrimPrefix(value, "data:"), ",")
	if !ok {
		return "", "", false
	}
	mimeType, encoding, _ := strings.Cut(meta, ";")
	if !strings.EqualFold(encoding, "base64") || strings.TrimSpace(mimeType) == "" || data == "" {
		return "", "", false
	}
	return mimeType, data, true
}
