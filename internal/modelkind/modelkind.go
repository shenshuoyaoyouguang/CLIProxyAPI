// Package modelkind identifies model families by name for routing decisions.
package modelkind

import "strings"

// IsDeepSeekModel reports whether the model name indicates a DeepSeek model.
func IsDeepSeekModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "deepseek-")
}
