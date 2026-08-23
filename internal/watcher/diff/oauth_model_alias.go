package diff

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// SummarizeOAuthModelAlias summarizes OAuth model alias per channel.
func SummarizeOAuthModelAlias(entries map[string][]config.OAuthModelAlias) map[string]OAuthModelAliasSummary {
	if len(entries) == 0 {
		return nil
	}
	out := make(map[string]OAuthModelAliasSummary, len(entries))
	for k, v := range entries {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" {
			continue
		}
		out[key] = summarizeOAuthModelAliasList(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// DiffOAuthModelAliasChanges compares OAuth model alias maps.
func DiffOAuthModelAliasChanges(oldMap, newMap map[string][]config.OAuthModelAlias) ([]string, []string) {
	return diffSummaryMapChanges(SummarizeOAuthModelAlias(oldMap), SummarizeOAuthModelAlias(newMap), "oauth-model-alias")
}

func summarizeOAuthModelAliasList(list []config.OAuthModelAlias) OAuthModelAliasSummary {
	if len(list) == 0 {
		return OAuthModelAliasSummary{}
	}
	seen := make(map[string]struct{}, len(list))
	normalized := make([]string, 0, len(list))
	for _, alias := range list {
		name := strings.ToLower(strings.TrimSpace(alias.Name))
		aliasVal := strings.ToLower(strings.TrimSpace(alias.Alias))
		if name == "" || aliasVal == "" {
			continue
		}
		key := name + "->" + aliasVal
		if alias.Fork {
			key += "|fork"
		}
		if displayName := strings.TrimSpace(alias.DisplayName); displayName != "" {
			key += "|display-name=" + displayName
		}
		if alias.ForceMapping {
			key += "|force-mapping"
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, key)
	}
	if len(normalized) == 0 {
		return OAuthModelAliasSummary{}
	}
	sort.Strings(normalized)
	sum := sha256.Sum256([]byte(strings.Join(normalized, "|")))
	return OAuthModelAliasSummary{
		hash:  hex.EncodeToString(sum[:]),
		count: len(normalized),
	}
}
