package diff

import (
	"sort"
	"strings"
)

// SummarizeExcludedModels normalizes and hashes an excluded-model list.
func SummarizeExcludedModels(list []string) ExcludedModelsSummary {
	if len(list) == 0 {
		return ExcludedModelsSummary{}
	}
	seen := make(map[string]struct{}, len(list))
	normalized := make([]string, 0, len(list))
	for _, entry := range list {
		if trimmed := strings.ToLower(strings.TrimSpace(entry)); trimmed != "" {
			if _, exists := seen[trimmed]; exists {
				continue
			}
			seen[trimmed] = struct{}{}
			normalized = append(normalized, trimmed)
		}
	}
	sort.Strings(normalized)
	return ExcludedModelsSummary{
		hash:  ComputeExcludedModelsHash(normalized),
		count: len(normalized),
	}
}

// SummarizeOAuthExcludedModels summarizes OAuth excluded models per provider.
func SummarizeOAuthExcludedModels(entries map[string][]string) map[string]ExcludedModelsSummary {
	if len(entries) == 0 {
		return nil
	}
	out := make(map[string]ExcludedModelsSummary, len(entries))
	for k, v := range entries {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" {
			continue
		}
		out[key] = SummarizeExcludedModels(v)
	}
	return out
}

// DiffOAuthExcludedModelChanges compares OAuth excluded models maps.
func DiffOAuthExcludedModelChanges(oldMap, newMap map[string][]string) ([]string, []string) {
	return diffSummaryMapChanges(SummarizeOAuthExcludedModels(oldMap), SummarizeOAuthExcludedModels(newMap), "oauth-excluded-models")
}
