package diff

import (
	"fmt"
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

// summary is the shared change-detection summary shape used by every provider.
// The exported aliases below preserve the historical public type names.
type summary struct {
	hash  string
	count int
}

// ExcludedModelsSummary summarizes an excluded-model list.
type ExcludedModelsSummary = summary

// OAuthRequestScopedErrorsSummary summarizes OAuth request-scoped errors per channel.
type OAuthRequestScopedErrorsSummary = summary

// OAuthModelAliasSummary summarizes OAuth model aliases per channel.
type OAuthModelAliasSummary = summary

// GeminiModelsSummary summarizes Gemini model aliases.
type GeminiModelsSummary = summary

// ClaudeModelsSummary summarizes Claude model aliases.
type ClaudeModelsSummary = summary

// CodexModelsSummary summarizes Codex model aliases.
type CodexModelsSummary = summary

// VertexModelsSummary summarizes Vertex-compatible model aliases.
type VertexModelsSummary = summary

// modelSummarySource is the field access the Gemini/Claude/Codex models share.
type modelSummarySource interface {
	GetName() string
	GetAlias() string
	GetDisplayName() string
	GetIsCompat() bool
	GetForceMapping() bool
	GetThinking() *registry.ThinkingSupport
}

// diffSummaryMapChanges compares two per-key summaries and reports label-keyed
// added/updated/removed change lines plus the affected keys.
func diffSummaryMapChanges(oldMap, newMap map[string]summary, label string) ([]string, []string) {
	keys := make(map[string]struct{}, len(oldMap)+len(newMap))
	for k := range oldMap {
		keys[k] = struct{}{}
	}
	for k := range newMap {
		keys[k] = struct{}{}
	}
	changes := make([]string, 0, len(keys))
	affected := make([]string, 0, len(keys))
	for key := range keys {
		oldInfo, okOld := oldMap[key]
		newInfo, okNew := newMap[key]
		switch {
		case okOld && !okNew:
			changes = append(changes, fmt.Sprintf("%s[%s]: removed", label, key))
			affected = append(affected, key)
		case !okOld && okNew:
			changes = append(changes, fmt.Sprintf("%s[%s]: added (%d entries)", label, key, newInfo.count))
			affected = append(affected, key)
		case okOld && okNew && oldInfo.hash != newInfo.hash:
			changes = append(changes, fmt.Sprintf("%s[%s]: updated (%d -> %d entries)", label, key, oldInfo.count, newInfo.count))
			affected = append(affected, key)
		}
	}
	sort.Strings(changes)
	sort.Strings(affected)
	return changes, affected
}

// summarizeModelPairs hashes a provider model list for change detection.
func summarizeModelPairs[T modelSummarySource](models []T, includeForceMapping bool) summary {
	keys := normalizeModelPairs(func(out func(key string)) {
		for _, model := range models {
			name := strings.TrimSpace(model.GetName())
			alias := strings.TrimSpace(model.GetAlias())
			if name == "" && alias == "" {
				continue
			}
			isCompat := "false"
			if model.GetIsCompat() {
				isCompat = "true"
			}
			var b strings.Builder
			b.WriteString(strings.ToLower(name))
			b.WriteString("|")
			b.WriteString(strings.ToLower(alias))
			b.WriteString("|")
			b.WriteString(strings.TrimSpace(model.GetDisplayName()))
			if includeForceMapping {
				force := "false"
				if model.GetForceMapping() {
					force = "true"
				}
				b.WriteString("|force-mapping=")
				b.WriteString(force)
			}
			b.WriteString("|is-compat=")
			b.WriteString(isCompat)
			b.WriteString(thinkingHashSuffix(model.GetThinking()))
			out(b.String())
		}
	})
	return summary{hash: hashJoined(keys), count: len(keys)}
}
