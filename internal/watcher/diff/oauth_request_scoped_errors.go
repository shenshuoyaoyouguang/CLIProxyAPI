package diff

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// SummarizeOAuthRequestScopedErrors summarizes OAuth request-scoped errors per channel.
func SummarizeOAuthRequestScopedErrors(entries map[string][]config.RequestScopedErrorRule) map[string]OAuthRequestScopedErrorsSummary {
	if len(entries) == 0 {
		return nil
	}
	out := make(map[string]OAuthRequestScopedErrorsSummary, len(entries))
	for k, v := range entries {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" {
			continue
		}
		out[key] = summarizeOAuthRequestScopedErrorsList(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// DiffOAuthRequestScopedErrorsChanges compares OAuth request-scoped error maps.
func DiffOAuthRequestScopedErrorsChanges(oldMap, newMap map[string][]config.RequestScopedErrorRule) ([]string, []string) {
	return diffSummaryMapChanges(SummarizeOAuthRequestScopedErrors(oldMap), SummarizeOAuthRequestScopedErrors(newMap), "oauth-request-scoped-errors")
}

func summarizeOAuthRequestScopedErrorsList(list []config.RequestScopedErrorRule) OAuthRequestScopedErrorsSummary {
	if len(list) == 0 {
		return OAuthRequestScopedErrorsSummary{}
	}
	var b strings.Builder
	valid := 0
	for _, entry := range list {
		if entry.Status <= 0 || (len(entry.Match) == 0 && len(entry.MatchRegexr) == 0) || entry.Action == "" {
			continue
		}
		valid++
		b.WriteString(fmt.Sprintf("%d|%s|%s|%s\n", entry.Status, strings.Join(entry.Match, ","), strings.Join(entry.MatchRegexr, ","), entry.Action))
	}
	if valid == 0 {
		return OAuthRequestScopedErrorsSummary{}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return OAuthRequestScopedErrorsSummary{
		hash:  hex.EncodeToString(sum[:]),
		count: valid,
	}
}
