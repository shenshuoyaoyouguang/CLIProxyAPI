package modelkind

import "strings"

// IsDeepSeekModel reports whether the model name indicates a DeepSeek model.
func IsDeepSeekModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "deepseek-")
}

// IsMIMOModel reports whether the model name indicates a Xiaomi MiMo model.
func IsMIMOModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "mimo-")
}
