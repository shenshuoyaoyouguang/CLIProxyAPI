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
	Name           string                                   `json:"name"`
	Priority       int                                      `json:"priority,omitempty"`
	Disabled       bool                                     `json:"disabled"`
	Prefix         string                                   `json:"prefix,omitempty"`
	BaseURL        string                                   `json:"base-url"`
	APIKeyEntries  []openAICompatibilityAPIKeyWithAuthIndex `json:"api-key-entries,omitempty"`
	Models         []config.OpenAICompatibilityModel        `json:"models,omitempty"`
	Headers        map[string]string                        `json:"headers,omitempty"`
	DisableCooling bool                                     `json:"disable-cooling,omitempty"`
	AuthIndex      string                                   `json:"auth-index,omitempty"`
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

// withAuthIndex iterates entries while holding the handler lock and resolves
// the live auth index for each entry's stable ID. parts returns the ID kind
// followed by the ID parts (nil skips the lookup); build receives the entry,
// its auth index, and a resolver for additional IDs (used for entries with
// nested api-key entries).
func withAuthIndex[E, R any](
	h *Handler,
	getEntries func() []E,
	parts func(E) []string,
	build func(E, string, func(...string) string) R,
) []R {
	if h == nil {
		return nil
	}

	// Snapshot the config first, then resolve live auth indexes. The reverse order
	// makes the index map strictly older than the entries it annotates, so a key
	// added concurrently always renders with an empty auth index. Both sections
	// take h.mu separately because liveAuthIndexByID acquires it as well.
	h.mu.Lock()
	if h.cfg == nil {
		h.mu.Unlock()
		return nil
	}
	source := getEntries()
	entries := make([]E, len(source))
	copy(entries, source)
	h.mu.Unlock()

	liveIndexByID := h.liveAuthIndexByID()

	idGen := synthesizer.NewStableIDGenerator()
	resolve := func(idParts ...string) string {
		if len(idParts) == 0 {
			return ""
		}
		id, _ := idGen.Next(idParts[0], idParts[1:]...)
		return liveIndexByID[id]
	}

	out := make([]R, len(entries))
	for i := range entries {
		entry := entries[i]
		authIndex := ""
		if p := parts(entry); p != nil {
			authIndex = resolve(p...)
		}
		out[i] = build(entry, authIndex, resolve)
	}
	return out
}

func (h *Handler) geminiKeysWithAuthIndex() []geminiKeyWithAuthIndex {
	return withAuthIndex(h,
		func() []config.GeminiKey { return h.cfg.GeminiKey },
		func(e config.GeminiKey) []string {
			if strings.TrimSpace(e.APIKey) == "" {
				return nil
			}
			return []string{"gemini:apikey", e.APIKey, e.BaseURL}
		},
		func(e config.GeminiKey, idx string, _ func(...string) string) geminiKeyWithAuthIndex {
			return geminiKeyWithAuthIndex{GeminiKey: e, AuthIndex: idx}
		})
}

func (h *Handler) interactionsKeysWithAuthIndex() []geminiKeyWithAuthIndex {
	return withAuthIndex(h,
		func() []config.GeminiKey { return h.cfg.InteractionsKey },
		func(e config.GeminiKey) []string {
			if strings.TrimSpace(e.APIKey) == "" {
				return nil
			}
			return []string{"gemini-interactions:apikey", e.APIKey, e.BaseURL}
		},
		func(e config.GeminiKey, idx string, _ func(...string) string) geminiKeyWithAuthIndex {
			return geminiKeyWithAuthIndex{GeminiKey: e, AuthIndex: idx}
		})
}

func (h *Handler) claudeKeysWithAuthIndex() []claudeKeyWithAuthIndex {
	return withAuthIndex(h,
		func() []config.ClaudeKey { return h.cfg.ClaudeKey },
		func(e config.ClaudeKey) []string {
			if strings.TrimSpace(e.APIKey) == "" {
				return nil
			}
			return []string{"claude:apikey", e.APIKey, e.BaseURL}
		},
		func(e config.ClaudeKey, idx string, _ func(...string) string) claudeKeyWithAuthIndex {
			return claudeKeyWithAuthIndex{ClaudeKey: e, AuthIndex: idx}
		})
}

func (h *Handler) codexKeysWithAuthIndex() []codexKeyWithAuthIndex {
	return withAuthIndex(h,
		func() []config.CodexKey { return h.cfg.CodexKey },
		func(e config.CodexKey) []string {
			if strings.TrimSpace(e.APIKey) == "" {
				return nil
			}
			return []string{"codex:apikey", e.APIKey, e.BaseURL}
		},
		func(e config.CodexKey, idx string, _ func(...string) string) codexKeyWithAuthIndex {
			return codexKeyWithAuthIndex{CodexKey: e, AuthIndex: idx}
		})
}

func (h *Handler) xaiKeysWithAuthIndex() []xaiKeyWithAuthIndex {
	return withAuthIndex(h,
		func() []config.CodexKey { return h.cfg.XAIKey },
		func(e config.CodexKey) []string {
			if strings.TrimSpace(e.APIKey) == "" {
				return nil
			}
			return []string{"xai:apikey", e.APIKey, e.BaseURL}
		},
		func(e config.CodexKey, idx string, _ func(...string) string) xaiKeyWithAuthIndex {
			return xaiKeyWithAuthIndex{XAIKey: e, AuthIndex: idx}
		})
}

// zaiKeysWithAuthIndex resolves auth indexes for Z.ai credentials
// (ZAIKey shares the CodexKey structure).
func (h *Handler) zaiKeysWithAuthIndex() []codexKeyWithAuthIndex {
	return withAuthIndex(h,
		func() []config.CodexKey { return h.cfg.ZAIKey },
		func(e config.CodexKey) []string {
			if strings.TrimSpace(e.APIKey) == "" {
				return nil
			}
			return []string{"zai:apikey", e.APIKey, e.BaseURL}
		},
		func(e config.CodexKey, idx string, _ func(...string) string) codexKeyWithAuthIndex {
			return codexKeyWithAuthIndex{CodexKey: e, AuthIndex: idx}
		})
}

func (h *Handler) vertexCompatKeysWithAuthIndex() []vertexCompatKeyWithAuthIndex {
	return withAuthIndex(h,
		func() []config.VertexCompatKey { return h.cfg.VertexCompatAPIKey },
		func(e config.VertexCompatKey) []string {
			return []string{"vertex:apikey", e.APIKey, e.BaseURL, e.ProxyURL}
		},
		func(e config.VertexCompatKey, idx string, _ func(...string) string) vertexCompatKeyWithAuthIndex {
			return vertexCompatKeyWithAuthIndex{VertexCompatKey: e, AuthIndex: idx}
		})
}

// openAICompatKind returns the stable ID kind for an OpenAI-compatibility
// entry, derived from its provider name.
func openAICompatKind(e config.OpenAICompatibility) string {
	providerName := strings.ToLower(strings.TrimSpace(e.Name))
	if providerName == "" {
		providerName = "openai-compatibility"
	}
	return fmt.Sprintf("openai-compatibility:%s", providerName)
}

func (h *Handler) openAICompatibilityWithAuthIndex() []openAICompatibilityWithAuthIndex {
	return withAuthIndex(h,
		func() []config.OpenAICompatibility {
			return normalizedOpenAICompatibilityEntries(h.cfg.OpenAICompatibility)
		},
		func(e config.OpenAICompatibility) []string {
			if len(e.APIKeyEntries) == 0 {
				return []string{openAICompatKind(e), e.BaseURL}
			}
			return nil
		},
		func(e config.OpenAICompatibility, idx string, resolve func(...string) string) openAICompatibilityWithAuthIndex {
			response := openAICompatibilityWithAuthIndex{
				Name:           e.Name,
				Priority:       e.Priority,
				Disabled:       e.Disabled,
				Prefix:         e.Prefix,
				BaseURL:        e.BaseURL,
				Models:         e.Models,
				Headers:        e.Headers,
				DisableCooling: e.DisableCooling,
				AuthIndex:      idx,
			}
			if len(e.APIKeyEntries) > 0 {
				response.APIKeyEntries = make([]openAICompatibilityAPIKeyWithAuthIndex, len(e.APIKeyEntries))
				for j := range e.APIKeyEntries {
					apiKeyEntry := e.APIKeyEntries[j]
					response.APIKeyEntries[j] = openAICompatibilityAPIKeyWithAuthIndex{
						OpenAICompatibilityAPIKey: apiKeyEntry,
						AuthIndex:                 resolve(openAICompatKind(e), apiKeyEntry.APIKey, e.BaseURL, apiKeyEntry.ProxyURL),
					}
				}
			}
			return response
		})
}
