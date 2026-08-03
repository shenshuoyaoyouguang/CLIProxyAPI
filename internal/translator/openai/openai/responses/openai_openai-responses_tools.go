package responses

import (
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func convertResponsesToolToOpenAIChatTools(tool gjson.Result) [][]byte {
	toolType := strings.TrimSpace(tool.Get("type").String())
	switch toolType {
	case "", "function":
		if tJSON, ok := convertResponsesFunctionToolToOpenAIChat(tool, ""); ok {
			return [][]byte{tJSON}
		}
	case "namespace":
		return convertResponsesNamespaceToolToOpenAIChat(tool)
	case "custom":
		if tJSON, ok := convertResponsesCustomToolToOpenAIChat(tool, ""); ok {
			return [][]byte{tJSON}
		}
	default:
		return nil
	}
	return nil
}

// convertResponsesCustomToolToOpenAIChat maps a Responses freeform ("custom")
// tool onto a Chat Completions function tool with a single freeform "input"
// string, mirroring the function-based shape Codex uses for apply_patch.
func convertResponsesCustomToolToOpenAIChat(tool gjson.Result, overrideName string) ([]byte, bool) {
	name := strings.TrimSpace(overrideName)
	if name == "" {
		name = responsesToolName(tool)
	}
	if name == "" {
		return nil, false
	}
	chatTool := []byte(`{"type":"function","function":{"name":"","description":"","parameters":{"type":"object","properties":{"input":{"type":"string"}},"required":["input"]}}}`)
	chatTool, _ = sjson.SetBytes(chatTool, "function.name", name)
	if description := responsesToolDescription(tool); description != "" {
		chatTool, _ = sjson.SetBytes(chatTool, "function.description", description)
	}
	return chatTool, true
}

func convertResponsesNamespaceToolToOpenAIChat(tool gjson.Result) [][]byte {
	namespaceName := strings.TrimSpace(tool.Get("name").String())
	children := tool.Get("tools")
	if !children.Exists() || !children.IsArray() {
		return nil
	}

	var out [][]byte
	children.ForEach(func(_, child gjson.Result) bool {
		childName := responsesToolName(child)
		qualifiedName := qualifyResponsesNamespaceToolName(namespaceName, childName)
		switch strings.TrimSpace(child.Get("type").String()) {
		case "", "function":
			if tJSON, ok := convertResponsesFunctionToolToOpenAIChat(child, qualifiedName); ok {
				out = append(out, tJSON)
			}
		case "custom":
			if tJSON, ok := convertResponsesCustomToolToOpenAIChat(child, qualifiedName); ok {
				out = append(out, tJSON)
			}
		}
		return true
	})
	return out
}

func convertResponsesFunctionToolToOpenAIChat(tool gjson.Result, overrideName string) ([]byte, bool) {
	name := strings.TrimSpace(overrideName)
	if name == "" {
		name = responsesToolName(tool)
	}
	if name == "" {
		return nil, false
	}

	chatTool := []byte(`{"type":"function","function":{"name":"","description":"","parameters":{}}}`)
	chatTool, _ = sjson.SetBytes(chatTool, "function.name", name)
	if description := responsesToolDescription(tool); description != "" {
		chatTool, _ = sjson.SetBytes(chatTool, "function.description", description)
	}
	if parameters := responsesToolParameters(tool); parameters.Exists() {
		chatTool, _ = sjson.SetRawBytes(chatTool, "function.parameters", []byte(parameters.Raw))
	}
	return chatTool, true
}

func responsesToolName(tool gjson.Result) string {
	if name := strings.TrimSpace(tool.Get("name").String()); name != "" {
		return name
	}
	return strings.TrimSpace(tool.Get("function.name").String())
}

func responsesToolDescription(tool gjson.Result) string {
	if description := tool.Get("description").String(); description != "" {
		return description
	}
	return tool.Get("function.description").String()
}

func responsesToolParameters(tool gjson.Result) gjson.Result {
	for _, path := range []string{
		"parameters",
		"parametersJsonSchema",
		"input_schema",
		"function.parameters",
		"function.parametersJsonSchema",
	} {
		if parameters := tool.Get(path); parameters.Exists() {
			return parameters
		}
	}
	return gjson.Result{}
}

// responsesToolOutputText flattens a tool output value that may be a plain
// string or an array of content parts ({"type":"input_text","text":...}) into
// a single text payload for a Chat Completions tool message.
func responsesToolOutputText(output gjson.Result) string {
	if output.Type == gjson.String {
		return output.String()
	}
	if output.IsArray() {
		var b strings.Builder
		output.ForEach(func(_, part gjson.Result) bool {
			if part.Type == gjson.String {
				b.WriteString(part.String())
				return true
			}
			if text := part.Get("text"); text.Exists() {
				b.WriteString(text.String())
			}
			return true
		})
		return b.String()
	}
	if output.Exists() {
		return output.Raw
	}
	return ""
}

// responsesCustomToolNames collects the names of freeform ("custom") tools
// declared in the original Responses request, both in the top-level "tools"
// field and in Codex Desktop "additional_tools" input items. Namespace child
// names use the qualified Chat Completions form.
func responsesCustomToolNames(requestRawJSON []byte) map[string]struct{} {
	names := make(map[string]struct{})
	var collect func(gjson.Result, string)
	collect = func(tools gjson.Result, namespaceName string) {
		if !tools.Exists() || !tools.IsArray() {
			return
		}
		tools.ForEach(func(_, tool gjson.Result) bool {
			switch strings.TrimSpace(tool.Get("type").String()) {
			case "custom":
				name := responsesToolName(tool)
				if namespaceName != "" {
					name = qualifyResponsesNamespaceToolName(namespaceName, name)
				}
				if name != "" {
					names[name] = struct{}{}
				}
			case "namespace":
				collect(tool.Get("tools"), strings.TrimSpace(tool.Get("name").String()))
			}
			return true
		})
	}
	root := gjson.ParseBytes(requestRawJSON)
	collect(root.Get("tools"), "")
	if input := root.Get("input"); input.Exists() && input.IsArray() {
		input.ForEach(func(_, item gjson.Result) bool {
			if item.Get("type").String() == "additional_tools" {
				collect(item.Get("tools"), "")
			}
			return true
		})
	}
	return names
}

func responsesSingleCustomToolName(requestRawJSON []byte) (string, bool) {
	customToolNames := responsesCustomToolNames(requestRawJSON)
	if len(customToolNames) != 1 {
		return "", false
	}

	toolCount := 0
	collect := func(tools gjson.Result) {
		if !tools.Exists() || !tools.IsArray() {
			return
		}
		tools.ForEach(func(_, tool gjson.Result) bool {
			toolCount += len(convertResponsesToolToOpenAIChatTools(tool))
			return true
		})
	}

	root := gjson.ParseBytes(requestRawJSON)
	collect(root.Get("tools"))
	if input := root.Get("input"); input.Exists() && input.IsArray() {
		input.ForEach(func(_, item gjson.Result) bool {
			if item.Get("type").String() == "additional_tools" {
				collect(item.Get("tools"))
			}
			return true
		})
	}
	for name := range customToolNames {
		return name, toolCount == 1
	}
	return "", false
}

// unwrapCustomToolInputRaw extracts the freeform input from the {"input": ...}
// wrapper produced for a converted custom tool. It reports whether the unwrapped
// value is a JSON literal (object/array/number/bool/null) that must be embedded
// via sjson.SetRawBytes to avoid re-escaping; string values are returned with
// raw=false so callers can embed them via sjson.SetBytes. It falls back to the
// raw arguments when the wrapper is absent.
func unwrapCustomToolInputRaw(arguments string) (string, bool) {
	if v := gjson.Get(arguments, "input"); v.Exists() {
		if v.Type == gjson.String {
			return v.String(), false
		}
		return v.Raw, true
	}
	// Unwrapped: object/array arguments are treated as raw JSON, everything
	// else as plain text.
	parsed := gjson.Parse(arguments)
	if parsed.IsObject() || parsed.IsArray() {
		return arguments, true
	}
	return arguments, false
}

// setCustomToolInputBytes writes a custom_tool_call freeform input to the
// given sjson path. Structured input is embedded as raw JSON (matching the
// function-tool parameters style); plain strings are embedded as JSON strings
// to avoid double-escaping structured input.
func setCustomToolInputBytes(item []byte, path, arguments string) []byte {
	value, raw := unwrapCustomToolInputRaw(arguments)
	if raw {
		item, _ = sjson.SetRawBytes(item, path, []byte(value))
	} else {
		item, _ = sjson.SetBytes(item, path, value)
	}
	return item
}

func qualifyResponsesNamespaceToolName(namespaceName, childName string) string {
	childName = strings.TrimSpace(childName)
	if childName == "" || namespaceName == "" || strings.HasPrefix(childName, "mcp__") {
		return childName
	}
	if strings.HasPrefix(childName, namespaceName) {
		return childName
	}
	if strings.HasSuffix(namespaceName, "__") {
		return namespaceName + childName
	}
	return namespaceName + "__" + childName
}

func splitResponsesQualifiedFunctionCallFromRequest(requestRawJSON []byte, qualifiedName string) (name, namespace string) {
	qualifiedName = strings.TrimSpace(qualifiedName)
	if qualifiedName == "" {
		return "", ""
	}

	var bestNamespace string
	var bestChild string
	collect := func(tools gjson.Result) {
		if !tools.Exists() || !tools.IsArray() {
			return
		}
		tools.ForEach(func(_, tool gjson.Result) bool {
			if strings.TrimSpace(tool.Get("type").String()) != "namespace" {
				return true
			}
			namespaceName := strings.TrimSpace(tool.Get("name").String())
			if namespaceName == "" {
				return true
			}
			children := tool.Get("tools")
			if !children.Exists() || !children.IsArray() {
				return true
			}
			children.ForEach(func(_, child gjson.Result) bool {
				childName := responsesToolName(child)
				if childName == "" {
					return true
				}
				if qualifyResponsesNamespaceToolName(namespaceName, childName) == qualifiedName {
					bestNamespace = namespaceName
					bestChild = childName
				}
				return true
			})
			return true
		})
	}

	root := gjson.ParseBytes(requestRawJSON)
	collect(root.Get("tools"))
	if input := root.Get("input"); input.Exists() && input.IsArray() {
		input.ForEach(func(_, item gjson.Result) bool {
			if item.Get("type").String() == "additional_tools" {
				collect(item.Get("tools"))
			}
			return true
		})
	}

	if bestNamespace == "" || bestChild == "" {
		return qualifiedName, ""
	}
	return bestChild, bestNamespace
}

func pickRequestJSON(originalRequestRawJSON, requestRawJSON []byte) []byte {
	if len(originalRequestRawJSON) > 0 && gjson.ValidBytes(originalRequestRawJSON) {
		return originalRequestRawJSON
	}
	if len(requestRawJSON) > 0 && gjson.ValidBytes(requestRawJSON) {
		return requestRawJSON
	}
	return nil
}

func applyResponsesFunctionCallNamespaceFields(item []byte, requestRawJSON []byte, qualifiedName string, itemPath string) []byte {
	name, namespace := splitResponsesQualifiedFunctionCallFromRequest(requestRawJSON, qualifiedName)
	namePath := "name"
	namespacePath := "namespace"
	if itemPath != "" {
		namePath = itemPath + ".name"
		namespacePath = itemPath + ".namespace"
	}
	item, _ = sjson.SetBytes(item, namePath, name)
	if namespace != "" {
		item, _ = sjson.SetBytes(item, namespacePath, namespace)
	} else {
		item, _ = sjson.DeleteBytes(item, namespacePath)
	}
	return item
}
