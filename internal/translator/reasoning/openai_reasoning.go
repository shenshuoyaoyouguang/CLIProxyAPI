package reasoning

import (
	"strings"

	sigcompat "github.com/router-for-me/CLIProxyAPI/v7/internal/signature"
	"github.com/tidwall/gjson"
)

type OpenAIReasoningPart struct {
	Text      string
	Signature string
}

type OpenAIReasoningOptions struct {
	TargetProvider         sigcompat.SignatureProvider
	SignatureBlockKind     sigcompat.SignatureBlockKind
	IncludeJSONRawFallback bool
}

func CollectOpenAIReasoningParts(node gjson.Result, opts OpenAIReasoningOptions) []OpenAIReasoningPart {
	var parts []OpenAIReasoningPart
	if !node.Exists() {
		return parts
	}

	if node.IsArray() {
		node.ForEach(func(_, value gjson.Result) bool {
			parts = append(parts, CollectOpenAIReasoningParts(value, opts)...)
			return true
		})
		return parts
	}

	switch node.Type {
	case gjson.String:
		if text := node.String(); text != "" {
			parts = append(parts, OpenAIReasoningPart{Text: text})
		}
	case gjson.JSON:
		text := FirstStringValue(node, "text", "thinking", "content")
		signature := CompatibleSignature(opts.TargetProvider, FirstStringValue(node, "signature", "encrypted_content"), opts.SignatureBlockKind)
		if text != "" || signature != "" {
			parts = append(parts, OpenAIReasoningPart{Text: text, Signature: signature})
		} else if shouldUseRawFallback(node, opts) {
			parts = append(parts, OpenAIReasoningPart{Text: node.Raw})
		}
	default:
		if shouldUseRawFallback(node, opts) {
			parts = append(parts, OpenAIReasoningPart{Text: node.Raw})
		}
	}
	return parts
}

func CollectOpenAIReasoningPartsFromMessage(content, reasoning gjson.Result, opts OpenAIReasoningOptions) []OpenAIReasoningPart {
	parts := CollectOpenAIReasoningParts(reasoning, opts)
	if !content.IsArray() {
		return parts
	}

	content.ForEach(func(_, part gjson.Result) bool {
		switch part.Get("type").String() {
		case "reasoning", "thinking":
			parts = mergeOpenAIReasoningParts(parts, CollectOpenAIReasoningParts(part, opts)...)
		}
		return true
	})
	return parts
}

func CollectOpenAIReasoningTexts(node gjson.Result, opts OpenAIReasoningOptions) []string {
	parts := CollectOpenAIReasoningParts(node, opts)
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return texts
}

func JoinOpenAIReasoningTexts(parts []OpenAIReasoningPart) string {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n\n")
}

func FirstStringValue(node gjson.Result, paths ...string) string {
	for _, path := range paths {
		value := node.Get(path)
		if value.Exists() && value.Type == gjson.String {
			if text := value.String(); text != "" {
				return text
			}
		}
	}
	return ""
}

func CompatibleSignature(targetProvider sigcompat.SignatureProvider, rawSignature string, blockKind sigcompat.SignatureBlockKind) string {
	if targetProvider == "" {
		return ""
	}
	if blockKind == "" {
		blockKind = sigcompat.SignatureBlockKindUnknown
	}
	signature, ok := sigcompat.CompatibleSignatureForProviderBlock(targetProvider, rawSignature, blockKind)
	if !ok {
		return ""
	}
	return signature
}

func shouldUseRawFallback(node gjson.Result, opts OpenAIReasoningOptions) bool {
	return opts.IncludeJSONRawFallback &&
		node.Type == gjson.Number &&
		node.Raw != "" &&
		!strings.HasPrefix(node.Raw, "{") &&
		!strings.HasPrefix(node.Raw, "[")
}

func mergeOpenAIReasoningParts(parts []OpenAIReasoningPart, incoming ...OpenAIReasoningPart) []OpenAIReasoningPart {
	for _, next := range incoming {
		if next.Text == "" && next.Signature == "" {
			continue
		}

		replaced := false
		skip := false
		for i := range parts {
			current := parts[i]
			if current.Text == next.Text && current.Signature == next.Signature {
				skip = true
				break
			}
			if current.Text != next.Text {
				continue
			}

			switch {
			case current.Signature != "" && next.Signature == "":
				skip = true
			case current.Signature == "" && next.Signature != "":
				parts[i] = next
				replaced = true
			}
			if skip || replaced {
				break
			}
		}

		if skip || replaced {
			continue
		}
		parts = append(parts, next)
	}
	return parts
}
