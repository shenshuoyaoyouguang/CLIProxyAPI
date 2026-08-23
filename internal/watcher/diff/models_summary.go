package diff

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// SummarizeGeminiModels hashes Gemini model aliases for change detection.
func SummarizeGeminiModels(models []config.GeminiModel) GeminiModelsSummary {
	return summarizeModelPairs(models, false)
}

// SummarizeClaudeModels hashes Claude model aliases for change detection.
func SummarizeClaudeModels(models []config.ClaudeModel) ClaudeModelsSummary {
	return summarizeModelPairs(models, false)
}

// SummarizeCodexModels hashes Codex model aliases for change detection.
func SummarizeCodexModels(models []config.CodexModel) CodexModelsSummary {
	return summarizeModelPairs(models, true)
}

// SummarizeVertexModels hashes Vertex-compatible model aliases for change detection.
func SummarizeVertexModels(models []config.VertexCompatModel) VertexModelsSummary {
	if len(models) == 0 {
		return VertexModelsSummary{}
	}
	names := make([]string, 0, len(models))
	for _, model := range models {
		name := strings.TrimSpace(model.Name)
		alias := strings.TrimSpace(model.Alias)
		if name == "" && alias == "" {
			continue
		}
		if alias != "" {
			name = alias
		}
		names = append(names, name+"|"+strings.TrimSpace(model.DisplayName)+thinkingHashSuffix(model.Thinking))
	}
	if len(names) == 0 {
		return VertexModelsSummary{}
	}
	sort.Strings(names)
	sum := sha256.Sum256([]byte(strings.Join(names, "|")))
	return VertexModelsSummary{
		hash:  hex.EncodeToString(sum[:]),
		count: len(names),
	}
}
