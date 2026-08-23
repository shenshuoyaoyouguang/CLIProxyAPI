package management

import (
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/watcher/synthesizer"
)

type geminiKeyWithAuthIndex struct {
	config.GeminiKey
	AuthIndex string `json:"auth-index,omitempty"`
}

type claudeKeyWithAuthIndex struct {
	config.ClaudeKey
	AuthIndex string `json:"auth-index,omitempty"`
}

type codexKeyWithAuthIndex struct {
	config.CodexKey
	AuthIndex string `json:"auth-index,omitempty"`
}

type xaiKeyWithAuthIndex struct {
	config.XAIKey
	AuthIndex string `json:"auth-index,omitempty"`
}

type vertexCompatKeyWithAuthIndex struct {
	config.VertexCompatKey
	AuthIndex string `json:"auth-index,omitempty"`
}

type openAICompatibilityAPIKeyWithAuthIndex struct {
	config.OpenAICompatibilityAPIKey
	AuthIndex string `json:"auth-index,omitempty"`
}

type openAICompatibilityWithAuthIndex struct {
	Name                  string                                   `json:"name"`
	Priority              int                                      `json:"priority,omitempty"`
	Disabled              bool                                     `json:"disabled"`
	Prefix                string                                   `json:"prefix,omitempty"`
	BaseURL               string                                   `json:"base-url"`
	APIKeyEntries         []openAICompatibilityAPIKeyWithAuthIndex `json:"api-key-entries,omitempty"`
	Models                []config.OpenAICompatibilityModel        `json:"models,omitempty"`
	Headers               map[string]string                        `json:"headers,omitempty"`
	SupportPromptCacheKey bool                                     `json:"support-prompt-cache-key,omitempty"`
	DisableCooling        *bool                                    `json:"disable-cooling,omitempty"`
	RequestRetry          *int                                     `json:"request-retry,omitempty"`
	RequestScopedErrors   []config.RequestScopedErrorRule          `json:"request-scoped-errors,omitempty"`
	AuthIndex             string                                   `json:"auth-index,omitempty"`
}

func (h *Handler) liveAuthIndexByID() map[string]string {
	out := map[string]string{}
	if h == nil {
		return out
	}
	h.mu.Lock()
	manager := h.authManager
	h.mu.Unlock()
	if manager == nil {
		return out
	}
	// authManager.List() returns clones, so EnsureIndex only affects these copies.
	for _, auth := range manager.List() {
		if auth == nil {
			continue
		}
		id := strings.TrimSpace(auth.ID)
		if id == "" {
			continue
		}
		idx := strings.TrimSpace(auth.Index)
		if idx == "" {
			idx = auth.EnsureIndex()
		}
		if idx == "" {
			continue
		}
		out[id] = idx
	}
	return out
}

// buildKeysWithAuthIndex resolves the live auth index for each entry and
// returns the wrapped entries. keyArgs returns the stable-ID seed arguments,
// or nil when the entry carries no key material (in which case no auth index
// is attached).
func buildKeysWithAuthIndex[T any, W any](entries []T, idKind string, liveIndexByID map[string]string, keyArgs func(T) []string, wrap func(T, string) W) []W {
	idGen := synthesizer.NewStableIDGenerator()
	out := make([]W, len(entries))
	for i := range entries {
		entry := entries[i]
		authIndex := ""
		if args := keyArgs(entry); args != nil {
			id, _ := idGen.Next(idKind, args...)
			authIndex = liveIndexByID[id]
		}
		out[i] = wrap(entry, authIndex)
	}
	return out
}

// keyArgsWithAuthIndex returns the stable-ID seed arguments for an entry
// (key, base URL, proxy URL, prefix, sorted headers), or nil when the entry
// carries no key material.
func keyArgsWithAuthIndex(key string, base string, proxyURL string, prefix string, headers map[string]string) []string {
	key = strings.TrimSpace(key)
	base = strings.TrimSpace(base)
	if key == "" && base == "" {
		return nil
	}
	return []string{key, base, strings.TrimSpace(proxyURL), strings.TrimSpace(prefix), config.FormatSortedHeaders(headers)}
}

func (h *Handler) geminiKeysWithAuthIndex() []geminiKeyWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}
	return buildKeysWithAuthIndex(h.cfg.GeminiKey, "gemini:apikey", liveIndexByID,
		func(entry config.GeminiKey) []string {
			return keyArgsWithAuthIndex(entry.APIKey, entry.BaseURL, entry.ProxyURL, entry.Prefix, entry.Headers)
		},
		func(entry config.GeminiKey, authIndex string) geminiKeyWithAuthIndex {
			return geminiKeyWithAuthIndex{GeminiKey: entry, AuthIndex: authIndex}
		})
}

func (h *Handler) interactionsKeysWithAuthIndex() []geminiKeyWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}
	return buildKeysWithAuthIndex(h.cfg.InteractionsKey, "gemini-interactions:apikey", liveIndexByID,
		func(entry config.GeminiKey) []string {
			return keyArgsWithAuthIndex(entry.APIKey, entry.BaseURL, entry.ProxyURL, entry.Prefix, entry.Headers)
		},
		func(entry config.GeminiKey, authIndex string) geminiKeyWithAuthIndex {
			return geminiKeyWithAuthIndex{GeminiKey: entry, AuthIndex: authIndex}
		})
}

func (h *Handler) claudeKeysWithAuthIndex() []claudeKeyWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}
	return buildKeysWithAuthIndex(h.cfg.ClaudeKey, "claude:apikey", liveIndexByID,
		func(entry config.ClaudeKey) []string {
			return keyArgsWithAuthIndex(entry.APIKey, entry.BaseURL, entry.ProxyURL, entry.Prefix, entry.Headers)
		},
		func(entry config.ClaudeKey, authIndex string) claudeKeyWithAuthIndex {
			return claudeKeyWithAuthIndex{ClaudeKey: entry, AuthIndex: authIndex}
		})
}

func (h *Handler) codexKeysWithAuthIndex() []codexKeyWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}
	return buildKeysWithAuthIndex(h.cfg.CodexKey, "codex:apikey", liveIndexByID,
		func(entry config.CodexKey) []string {
			return keyArgsWithAuthIndex(entry.APIKey, entry.BaseURL, entry.ProxyURL, entry.Prefix, entry.Headers)
		},
		func(entry config.CodexKey, authIndex string) codexKeyWithAuthIndex {
			return codexKeyWithAuthIndex{CodexKey: entry, AuthIndex: authIndex}
		})
}

func (h *Handler) xaiKeysWithAuthIndex() []xaiKeyWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}
	return buildKeysWithAuthIndex(h.cfg.XAIKey, "xai:apikey", liveIndexByID,
		func(entry config.CodexKey) []string {
			return keyArgsWithAuthIndex(entry.APIKey, entry.BaseURL, entry.ProxyURL, entry.Prefix, entry.Headers)
		},
		func(entry config.CodexKey, authIndex string) xaiKeyWithAuthIndex {
			return xaiKeyWithAuthIndex{XAIKey: entry, AuthIndex: authIndex}
		})
}

func (h *Handler) vertexCompatKeysWithAuthIndex() []vertexCompatKeyWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}

	idGen := synthesizer.NewStableIDGenerator()
	out := make([]vertexCompatKeyWithAuthIndex, len(h.cfg.VertexCompatAPIKey))
	for i := range h.cfg.VertexCompatAPIKey {
		entry := h.cfg.VertexCompatAPIKey[i]
		id, _ := idGen.Next("vertex:apikey", entry.APIKey, entry.BaseURL, entry.ProxyURL)
		authIndex := liveIndexByID[id]
		out[i] = vertexCompatKeyWithAuthIndex{
			VertexCompatKey: entry,
			AuthIndex:       authIndex,
		}
	}
	return out
}

func (h *Handler) openAICompatibilityWithAuthIndex() []openAICompatibilityWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}

	normalized := normalizedOpenAICompatibilityEntries(h.cfg.OpenAICompatibility)
	out := make([]openAICompatibilityWithAuthIndex, len(normalized))
	idGen := synthesizer.NewStableIDGenerator()
	for i := range normalized {
		entry := normalized[i]
		providerName := strings.ToLower(strings.TrimSpace(entry.Name))
		if providerName == "" {
			providerName = "openai-compatibility"
		}
		idKind := fmt.Sprintf("openai-compatibility:%s", providerName)

		response := openAICompatibilityWithAuthIndex{
			Name:                  entry.Name,
			Priority:              entry.Priority,
			Disabled:              entry.Disabled,
			Prefix:                entry.Prefix,
			BaseURL:               entry.BaseURL,
			Models:                entry.Models,
			Headers:               entry.Headers,
			SupportPromptCacheKey: entry.SupportPromptCacheKey,
			DisableCooling:        entry.DisableCooling,
			RequestRetry:          entry.RequestRetry,
			RequestScopedErrors:   entry.RequestScopedErrors,
			AuthIndex:             "",
		}
		if len(entry.APIKeyEntries) == 0 {
			id, _ := idGen.Next(idKind, entry.BaseURL)
			response.AuthIndex = liveIndexByID[id]
		} else {
			response.APIKeyEntries = make([]openAICompatibilityAPIKeyWithAuthIndex, len(entry.APIKeyEntries))
			for j := range entry.APIKeyEntries {
				apiKeyEntry := entry.APIKeyEntries[j]
				id, _ := idGen.Next(idKind, apiKeyEntry.APIKey, entry.BaseURL, apiKeyEntry.ProxyURL)
				response.APIKeyEntries[j] = openAICompatibilityAPIKeyWithAuthIndex{
					OpenAICompatibilityAPIKey: apiKeyEntry,
					AuthIndex:                 liveIndexByID[id],
				}
			}
		}
		out[i] = response
	}
	return out
}
