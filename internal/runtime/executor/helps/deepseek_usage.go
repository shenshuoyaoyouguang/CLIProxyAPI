package helps

import (
	"bytes"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// NormalizeDeepSeekOpenAIUsage adds standard cached token fields to DeepSeek
// OpenAI-compatible responses while preserving the original cache metrics.
func NormalizeDeepSeekOpenAIUsage(body []byte) []byte {
	payload := jsonPayload(body)
	if len(payload) == 0 {
		trimmed := bytes.TrimSpace(body)
		if len(trimmed) == 0 || !gjson.ValidBytes(trimmed) {
			return body
		}
		payload = trimmed
	}

	updated, changed := normalizeDeepSeekOpenAIUsageJSON(payload)
	if !changed {
		return body
	}

	trimmedBody := bytes.TrimSpace(body)
	if bytes.HasPrefix(trimmedBody, []byte("data:")) {
		prefixIndex := bytes.Index(body, []byte("data:"))
		if prefixIndex < 0 {
			return updated
		}
		rebuilt := append([]byte(nil), body[:prefixIndex]...)
		rebuilt = append(rebuilt, []byte("data: ")...)
		rebuilt = append(rebuilt, updated...)
		return rebuilt
	}

	return updated
}

func normalizeDeepSeekOpenAIUsageJSON(body []byte) ([]byte, bool) {
	usageNode := gjson.GetBytes(body, "usage")
	if !usageNode.Exists() || !usageNode.IsObject() {
		return body, false
	}
	if usageNode.Get("prompt_tokens_details.cached_tokens").Exists() ||
		usageNode.Get("input_tokens_details.cached_tokens").Exists() {
		return body, false
	}

	hitTokens := usageNode.Get("prompt_cache_hit_tokens")
	if !hitTokens.Exists() {
		return body, false
	}

	updated, err := sjson.SetBytes(body, "usage.prompt_tokens_details.cached_tokens", hitTokens.Int())
	if err != nil {
		return body, false
	}
	updated, err = sjson.SetBytes(updated, "usage.input_tokens_details.cached_tokens", hitTokens.Int())
	if err != nil {
		return body, false
	}
	return updated, true
}
