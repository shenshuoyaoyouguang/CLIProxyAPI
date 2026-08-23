package common

import (
	"crypto/rand"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const tooluLetters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// GenerateClaudeToolCallID generates a random tool use ID prefixed with toolu_
// using rejection sampling to guarantee a uniform distribution across the 62 alphanumeric characters.
func GenerateClaudeToolCallID() string {
	const maxValidByte = 256 - (256 % len(tooluLetters)) // 248: exact multiple of 62
	var b strings.Builder
	b.Grow(len("toolu_") + 24)
	b.WriteString("toolu_")

	var buf [32]byte
	n := 0
	for n < 24 {
		_, _ = rand.Read(buf[:])
		for _, bVal := range buf {
			if int(bVal) < maxValidByte {
				b.WriteByte(tooluLetters[int(bVal)%len(tooluLetters)])
				n++
				if n == 24 {
					break
				}
			}
		}
	}
	return b.String()
}

// RequestModelName returns the model name from the original request, falling
// back to the translated request when the original request is unavailable.
func RequestModelName(originalRequestRawJSON, requestRawJSON []byte) string {
	for _, rawJSON := range [][]byte{originalRequestRawJSON, requestRawJSON} {
		if modelName := requestModelName(rawJSON); modelName != "" {
			return modelName
		}
	}
	return ""
}

func requestModelName(rawJSON []byte) string {
	if len(rawJSON) == 0 || !gjson.ValidBytes(rawJSON) {
		return ""
	}

	root := gjson.ParseBytes(rawJSON)
	for _, path := range []string{"model", "request.model"} {
		model := root.Get(path)
		if model.Type == gjson.String && strings.TrimSpace(model.String()) != "" {
			return model.String()
		}
	}
	return ""
}

// SetRawIfDifferent sets a raw JSON value at path only when the current value
// differs, leaving the document untouched on sjson errors.
func SetRawIfDifferent(out []byte, path string, value gjson.Result) []byte {
	current := gjson.GetBytes(out, path)
	if current.Exists() && current.Raw == value.Raw {
		return out
	}
	updated, errSet := sjson.SetRawBytes(out, path, []byte(value.Raw))
	if errSet != nil {
		return out
	}
	return updated
}

// NormalizeCodexServiceTier maps a Codex service_tier string to its canonical
// form: "fast" and "priority" (any case/whitespace) map to "priority";
// anything missing or unrecognized maps to "".
func NormalizeCodexServiceTier(result gjson.Result) string {
	if !result.Exists() || result.Type != gjson.String {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(result.String())) {
	case "fast", "priority":
		return "priority"
	default:
		return ""
	}
}
