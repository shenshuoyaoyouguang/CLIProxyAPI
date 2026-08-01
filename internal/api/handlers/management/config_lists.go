package management

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// Generic helpers for list[string]
func (h *Handler) putStringList(c *gin.Context, set func([]string), after func()) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var arr []string
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []string `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil || len(obj.Items) == 0 {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	set(arr)
	if after != nil {
		after()
	}
	h.persistLocked(c)
}

func (h *Handler) patchStringList(c *gin.Context, target *[]string, after func()) {
	var body struct {
		Old   *string `json:"old"`
		New   *string `json:"new"`
		Index *int    `json:"index"`
		Value *string `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if body.Index != nil && body.Value != nil && *body.Index >= 0 && *body.Index < len(*target) {
		(*target)[*body.Index] = *body.Value
		if after != nil {
			after()
		}
		h.persistLocked(c)
		return
	}
	if body.Old != nil && body.New != nil {
		for i := range *target {
			if (*target)[i] == *body.Old {
				(*target)[i] = *body.New
				if after != nil {
					after()
				}
				h.persistLocked(c)
				return
			}
		}
		*target = append(*target, *body.New)
		if after != nil {
			after()
		}
		h.persistLocked(c)
		return
	}
	c.JSON(400, gin.H{"error": "missing fields"})
}

func (h *Handler) deleteFromStringList(c *gin.Context, target *[]string, after func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		_, err := fmt.Sscanf(idxStr, "%d", &idx)
		if err == nil && idx >= 0 && idx < len(*target) {
			*target = append((*target)[:idx], (*target)[idx+1:]...)
			if after != nil {
				after()
			}
			h.persistLocked(c)
			return
		}
	}
	// The raw query value is compared, not a trimmed copy: list values are
	// credentials and must match exactly. Trimming the query while comparing
	// exactly is asymmetric - a request to delete "abc " would silently delete the
	// stored "abc" instead, and a stored value with surrounding whitespace could
	// never be deleted by value at all.
	if val := c.Query("value"); strings.TrimSpace(val) != "" {
		out := make([]string, 0, len(*target))
		for _, v := range *target {
			if v != val {
				out = append(out, v)
			}
		}
		*target = out
		if after != nil {
			after()
		}
		h.persistLocked(c)
		return
	}
	c.JSON(400, gin.H{"error": "missing index or value"})
}

// keyPatchRequest is the shared PATCH envelope for key lists: locate the entry
// by index or by a match key ("match" for api-key lists, "name" for named
// providers), then apply the value.
type keyPatchRequest[V any] struct {
	Index *int    `json:"index"`
	Match *string `json:"match"`
	Name  *string `json:"name"`
	Value *V      `json:"value"`
}

// keyPatchSpec configures patchKeyEntry for one key-list module.
type keyPatchSpec[T, V any] struct {
	Items *[]T
	KeyOf func(T) string // match key of an entry (API key or provider name)

	// NameKeyed locates entries via body.name instead of body.match.
	NameKeyed bool
	// APIKeyOf and BaseURLOf expose the patch value's api-key/base-url fields so
	// the empty-value deletion policy is enforced centrally instead of per module.
	APIKeyOf             func(*V) *string
	BaseURLOf            func(*V) *string
	DeleteOnEmptyAPIKey  bool
	DeleteOnEmptyBaseURL bool

	// Apply writes module-specific fields; Sanitize runs after every change.
	Apply    func(*T, *V)
	Sanitize func()
}

// patchKeyEntry implements the shared PATCH flow for key lists: bind the
// envelope, locate the entry (index or match key), remove it when the
// delete-on-empty api-key/base-url policy fires, otherwise apply per-module
// fields, then sanitize and persist.
func patchKeyEntry[T, V any](h *Handler, c *gin.Context, spec keyPatchSpec[T, V]) {
	var body keyPatchRequest[V]
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	matchKey := ""
	if spec.NameKeyed {
		if body.Name != nil {
			matchKey = strings.TrimSpace(*body.Name)
		}
	} else if body.Match != nil {
		matchKey = strings.TrimSpace(*body.Match)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	targetIndex := -1
	if body.Index != nil && *body.Index >= 0 && *body.Index < len(*spec.Items) {
		targetIndex = *body.Index
	}
	if targetIndex == -1 && matchKey != "" {
		for i := range *spec.Items {
			if spec.KeyOf((*spec.Items)[i]) == matchKey {
				targetIndex = i
				break
			}
		}
	}
	if targetIndex == -1 {
		c.JSON(404, gin.H{"error": "item not found"})
		return
	}

	deleted := false
	if spec.DeleteOnEmptyAPIKey && spec.APIKeyOf != nil {
		if k := spec.APIKeyOf(body.Value); k != nil && strings.TrimSpace(*k) == "" {
			deleted = true
		}
	}
	if !deleted && spec.DeleteOnEmptyBaseURL && spec.BaseURLOf != nil {
		if u := spec.BaseURLOf(body.Value); u != nil && strings.TrimSpace(*u) == "" {
			deleted = true
		}
	}
	if deleted {
		*spec.Items = append((*spec.Items)[:targetIndex], (*spec.Items)[targetIndex+1:]...)
	} else {
		entry := (*spec.Items)[targetIndex]
		spec.Apply(&entry, body.Value)
		(*spec.Items)[targetIndex] = entry
	}
	spec.Sanitize()
	h.persistLocked(c)
}

// putKeyList replaces a config list from a raw-body array or {"items": [...]}
// payload. normalize runs per entry (returning an error rejects the whole
// payload with a 400), keep drops entries returning false, and apply receives
// the final list while h.mu is held.
func putKeyList[T any](h *Handler, c *gin.Context, normalize func(int, *T) error, keep func(*T) bool, apply func([]T)) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var arr []T
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []T `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil || len(obj.Items) == 0 {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}
	if normalize != nil {
		for i := range arr {
			if err := normalize(i, &arr[i]); err != nil {
				c.JSON(400, gin.H{"error": err.Error()})
				return
			}
		}
	}
	if keep != nil {
		filtered := arr[:0]
		for i := range arr {
			if keep(&arr[i]) {
				filtered = append(filtered, arr[i])
			}
		}
		arr = filtered
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	apply(arr)
	h.persistLocked(c)
}

// deleteKeyEntry implements the shared DELETE flow for api-key lists:
// ?api-key[&base-url] (removing every matching pair), then ?index.
// With strict404, requests that match nothing return 404 instead of persisting
// an unchanged list.
func deleteKeyEntry[T any](h *Handler, c *gin.Context, items *[]T, apiKeyOf, baseURLOf func(T) string, strict404 bool, sanitize func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if val := strings.TrimSpace(c.Query("api-key")); val != "" {
		if baseRaw, okBase := c.GetQuery("base-url"); okBase {
			base := strings.TrimSpace(baseRaw)
			out := make([]T, 0, len(*items))
			for _, v := range *items {
				if strings.TrimSpace(apiKeyOf(v)) == val && strings.TrimSpace(baseURLOf(v)) == base {
					continue
				}
				out = append(out, v)
			}
			if !strict404 || len(out) != len(*items) {
				*items = out
				sanitize()
				h.persistLocked(c)
				return
			}
			c.JSON(404, gin.H{"error": "item not found"})
			return
		}

		matchIndex := -1
		matchCount := 0
		for i := range *items {
			if strings.TrimSpace(apiKeyOf((*items)[i])) == val {
				matchCount++
				if matchIndex == -1 {
					matchIndex = i
				}
			}
		}
		if matchCount == 0 {
			if strict404 {
				c.JSON(404, gin.H{"error": "item not found"})
				return
			}
			sanitize()
			h.persistLocked(c)
			return
		}
		if matchCount > 1 {
			c.JSON(400, gin.H{"error": "multiple items match api-key; base-url is required"})
			return
		}
		*items = append((*items)[:matchIndex], (*items)[matchIndex+1:]...)
		sanitize()
		h.persistLocked(c)
		return
	}
	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		if _, err := fmt.Sscanf(idxStr, "%d", &idx); err == nil && idx >= 0 && idx < len(*items) {
			*items = append((*items)[:idx], (*items)[idx+1:]...)
			sanitize()
			h.persistLocked(c)
			return
		}
	}
	c.JSON(400, gin.H{"error": "missing api-key or index"})
}

// deleteNamedEntry implements DELETE for lists keyed by name (?name or ?index),
// removing every entry whose name matches.
func deleteNamedEntry[T any](h *Handler, c *gin.Context, items *[]T, nameOf func(T) string, sanitize func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if name := c.Query("name"); name != "" {
		out := make([]T, 0, len(*items))
		for _, v := range *items {
			if nameOf(v) != name {
				out = append(out, v)
			}
		}
		*items = out
		sanitize()
		h.persistLocked(c)
		return
	}
	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		if _, err := fmt.Sscanf(idxStr, "%d", &idx); err == nil && idx >= 0 && idx < len(*items) {
			*items = append((*items)[:idx], (*items)[idx+1:]...)
			sanitize()
			h.persistLocked(c)
			return
		}
	}
	c.JSON(400, gin.H{"error": "missing name or index"})
}

// api-keys
func (h *Handler) GetAPIKeys(c *gin.Context) {
	// Snapshot under the same lock used by the write paths (Put/Patch/Delete).
	// Concurrent slice read while a writer appends/reallocates is a data race
	// and can crash the process when the underlying array is shared.
	h.mu.Lock()
	keys := append([]string(nil), h.cfg.APIKeys...)
	h.mu.Unlock()
	c.JSON(200, gin.H{"api-keys": keys})
}
func (h *Handler) PutAPIKeys(c *gin.Context) {
	h.putStringList(c, func(v []string) {
		h.cfg.APIKeys = append([]string(nil), v...)
	}, nil)
}
func (h *Handler) PatchAPIKeys(c *gin.Context) {
	h.patchStringList(c, &h.cfg.APIKeys, func() {})
}
func (h *Handler) DeleteAPIKeys(c *gin.Context) {
	h.deleteFromStringList(c, &h.cfg.APIKeys, func() {})
}

// gemini-api-key: []GeminiKey
func (h *Handler) GetGeminiKeys(c *gin.Context) {
	c.JSON(200, gin.H{"gemini-api-key": h.geminiKeysWithAuthIndex()})
}
func (h *Handler) PutGeminiKeys(c *gin.Context) {
	putKeyList(h, c, nil, nil, func(v []config.GeminiKey) {
		h.cfg.GeminiKey = v
		h.cfg.SanitizeGeminiKeys()
	})
}
func (h *Handler) PatchGeminiKey(c *gin.Context) {
	type geminiKeyPatch struct {
		APIKey         *string            `json:"api-key"`
		Prefix         *string            `json:"prefix"`
		BaseURL        *string            `json:"base-url"`
		ProxyURL       *string            `json:"proxy-url"`
		Headers        *map[string]string `json:"headers"`
		ExcludedModels *[]string          `json:"excluded-models"`
	}
	patchKeyEntry(h, c, keyPatchSpec[config.GeminiKey, geminiKeyPatch]{
		Items: &h.cfg.GeminiKey,
		KeyOf: func(k config.GeminiKey) string { return k.APIKey },
		APIKeyOf: func(v *geminiKeyPatch) *string {
			return v.APIKey
		},
		BaseURLOf: func(v *geminiKeyPatch) *string {
			return v.BaseURL
		},
		DeleteOnEmptyAPIKey: true,
		Apply: func(entry *config.GeminiKey, v *geminiKeyPatch) {
			if v.APIKey != nil {
				entry.APIKey = strings.TrimSpace(*v.APIKey)
			}
			if v.Prefix != nil {
				entry.Prefix = strings.TrimSpace(*v.Prefix)
			}
			if v.BaseURL != nil {
				entry.BaseURL = strings.TrimSpace(*v.BaseURL)
			}
			if v.ProxyURL != nil {
				entry.ProxyURL = strings.TrimSpace(*v.ProxyURL)
			}
			if v.Headers != nil {
				entry.Headers = config.NormalizeHeaders(*v.Headers)
			}
			if v.ExcludedModels != nil {
				entry.ExcludedModels = config.NormalizeExcludedModels(*v.ExcludedModels)
			}
		},
		Sanitize: h.cfg.SanitizeGeminiKeys,
	})
}
func (h *Handler) DeleteGeminiKey(c *gin.Context) {
	deleteKeyEntry(h, c, &h.cfg.GeminiKey,
		func(k config.GeminiKey) string { return k.APIKey },
		func(k config.GeminiKey) string { return k.BaseURL },
		true, h.cfg.SanitizeGeminiKeys)
}

// interactions-api-key: []GeminiKey
func (h *Handler) GetInteractionsKeys(c *gin.Context) {
	c.JSON(200, gin.H{"interactions-api-key": h.interactionsKeysWithAuthIndex()})
}
func (h *Handler) PutInteractionsKeys(c *gin.Context) {
	putKeyList(h, c, nil, nil, func(v []config.GeminiKey) {
		h.cfg.InteractionsKey = v
		h.cfg.SanitizeInteractionsKeys()
	})
}
func (h *Handler) PatchInteractionsKey(c *gin.Context) {
	type interactionsKeyPatch struct {
		APIKey         *string            `json:"api-key"`
		Prefix         *string            `json:"prefix"`
		BaseURL        *string            `json:"base-url"`
		ProxyURL       *string            `json:"proxy-url"`
		Headers        *map[string]string `json:"headers"`
		ExcludedModels *[]string          `json:"excluded-models"`
	}
	patchKeyEntry(h, c, keyPatchSpec[config.GeminiKey, interactionsKeyPatch]{
		Items: &h.cfg.InteractionsKey,
		KeyOf: func(k config.GeminiKey) string { return k.APIKey },
		APIKeyOf: func(v *interactionsKeyPatch) *string {
			return v.APIKey
		},
		BaseURLOf: func(v *interactionsKeyPatch) *string {
			return v.BaseURL
		},
		DeleteOnEmptyAPIKey: true,
		Apply: func(entry *config.GeminiKey, v *interactionsKeyPatch) {
			if v.APIKey != nil {
				entry.APIKey = strings.TrimSpace(*v.APIKey)
			}
			if v.Prefix != nil {
				entry.Prefix = strings.TrimSpace(*v.Prefix)
			}
			if v.BaseURL != nil {
				entry.BaseURL = strings.TrimSpace(*v.BaseURL)
			}
			if v.ProxyURL != nil {
				entry.ProxyURL = strings.TrimSpace(*v.ProxyURL)
			}
			if v.Headers != nil {
				entry.Headers = config.NormalizeHeaders(*v.Headers)
			}
			if v.ExcludedModels != nil {
				entry.ExcludedModels = config.NormalizeExcludedModels(*v.ExcludedModels)
			}
		},
		Sanitize: h.cfg.SanitizeInteractionsKeys,
	})
}
func (h *Handler) DeleteInteractionsKey(c *gin.Context) {
	deleteKeyEntry(h, c, &h.cfg.InteractionsKey,
		func(k config.GeminiKey) string { return k.APIKey },
		func(k config.GeminiKey) string { return k.BaseURL },
		true, h.cfg.SanitizeInteractionsKeys)
}

// claude-api-key: []ClaudeKey
func (h *Handler) GetClaudeKeys(c *gin.Context) {
	c.JSON(200, gin.H{"claude-api-key": h.claudeKeysWithAuthIndex()})
}
func (h *Handler) PutClaudeKeys(c *gin.Context) {
	putKeyList(h, c, func(_ int, e *config.ClaudeKey) error {
		normalizeClaudeKey(e)
		return nil
	}, nil, func(v []config.ClaudeKey) {
		h.cfg.ClaudeKey = v
		h.cfg.SanitizeClaudeKeys()
	})
}
func (h *Handler) PatchClaudeKey(c *gin.Context) {
	type claudeKeyPatch struct {
		APIKey                  *string               `json:"api-key"`
		Prefix                  *string               `json:"prefix"`
		BaseURL                 *string               `json:"base-url"`
		ProxyURL                *string               `json:"proxy-url"`
		Models                  *[]config.ClaudeModel `json:"models"`
		Headers                 *map[string]string    `json:"headers"`
		ExcludedModels          *[]string             `json:"excluded-models"`
		RebuildMidSystemMessage *bool                 `json:"rebuild-mid-system-message"`
	}
	patchKeyEntry(h, c, keyPatchSpec[config.ClaudeKey, claudeKeyPatch]{
		Items: &h.cfg.ClaudeKey,
		KeyOf: func(k config.ClaudeKey) string { return k.APIKey },
		APIKeyOf: func(v *claudeKeyPatch) *string {
			return v.APIKey
		},
		BaseURLOf: func(v *claudeKeyPatch) *string {
			return v.BaseURL
		},
		Apply: func(entry *config.ClaudeKey, v *claudeKeyPatch) {
			if v.APIKey != nil {
				entry.APIKey = strings.TrimSpace(*v.APIKey)
			}
			if v.Prefix != nil {
				entry.Prefix = strings.TrimSpace(*v.Prefix)
			}
			if v.BaseURL != nil {
				entry.BaseURL = strings.TrimSpace(*v.BaseURL)
			}
			if v.ProxyURL != nil {
				entry.ProxyURL = strings.TrimSpace(*v.ProxyURL)
			}
			if v.Models != nil {
				entry.Models = append([]config.ClaudeModel(nil), (*v.Models)...)
			}
			if v.Headers != nil {
				entry.Headers = config.NormalizeHeaders(*v.Headers)
			}
			if v.ExcludedModels != nil {
				entry.ExcludedModels = config.NormalizeExcludedModels(*v.ExcludedModels)
			}
			if v.RebuildMidSystemMessage != nil {
				entry.RebuildMidSystemMessage = *v.RebuildMidSystemMessage
			}
			normalizeClaudeKey(entry)
		},
		Sanitize: h.cfg.SanitizeClaudeKeys,
	})
}
func (h *Handler) DeleteClaudeKey(c *gin.Context) {
	deleteKeyEntry(h, c, &h.cfg.ClaudeKey,
		func(k config.ClaudeKey) string { return k.APIKey },
		func(k config.ClaudeKey) string { return k.BaseURL },
		false, h.cfg.SanitizeClaudeKeys)
}

// openai-compatibility: []OpenAICompatibility
func (h *Handler) GetOpenAICompat(c *gin.Context) {
	c.JSON(200, gin.H{"openai-compatibility": h.openAICompatibilityWithAuthIndex()})
}
func (h *Handler) PutOpenAICompat(c *gin.Context) {
	putKeyList(h, c, func(_ int, e *config.OpenAICompatibility) error {
		normalizeOpenAICompatibilityEntry(e)
		return nil
	}, func(e *config.OpenAICompatibility) bool {
		return strings.TrimSpace(e.BaseURL) != ""
	}, func(v []config.OpenAICompatibility) {
		h.cfg.OpenAICompatibility = v
		h.cfg.SanitizeOpenAICompatibility()
	})
}
func (h *Handler) PatchOpenAICompat(c *gin.Context) {
	type openAICompatPatch struct {
		Name           *string                             `json:"name"`
		Prefix         *string                             `json:"prefix"`
		Disabled       *bool                               `json:"disabled"`
		DisableCooling *bool                               `json:"disable-cooling"`
		BaseURL        *string                             `json:"base-url"`
		APIKeyEntries  *[]config.OpenAICompatibilityAPIKey `json:"api-key-entries"`
		Models         *[]config.OpenAICompatibilityModel  `json:"models"`
		Headers        *map[string]string                  `json:"headers"`
	}
	patchKeyEntry(h, c, keyPatchSpec[config.OpenAICompatibility, openAICompatPatch]{
		Items:     &h.cfg.OpenAICompatibility,
		KeyOf:     func(k config.OpenAICompatibility) string { return k.Name },
		NameKeyed: true,
		BaseURLOf: func(v *openAICompatPatch) *string {
			return v.BaseURL
		},
		DeleteOnEmptyBaseURL: true,
		Apply: func(entry *config.OpenAICompatibility, v *openAICompatPatch) {
			if v.Name != nil {
				entry.Name = strings.TrimSpace(*v.Name)
			}
			if v.Prefix != nil {
				entry.Prefix = strings.TrimSpace(*v.Prefix)
			}
			if v.Disabled != nil {
				entry.Disabled = *v.Disabled
			}
			if v.DisableCooling != nil {
				entry.DisableCooling = *v.DisableCooling
			}
			if v.BaseURL != nil {
				entry.BaseURL = strings.TrimSpace(*v.BaseURL)
			}
			if v.APIKeyEntries != nil {
				entry.APIKeyEntries = append([]config.OpenAICompatibilityAPIKey(nil), (*v.APIKeyEntries)...)
			}
			if v.Models != nil {
				entry.Models = append([]config.OpenAICompatibilityModel(nil), (*v.Models)...)
			}
			if v.Headers != nil {
				entry.Headers = config.NormalizeHeaders(*v.Headers)
			}
			normalizeOpenAICompatibilityEntry(entry)
		},
		Sanitize: h.cfg.SanitizeOpenAICompatibility,
	})
}
func (h *Handler) DeleteOpenAICompat(c *gin.Context) {
	deleteNamedEntry(h, c, &h.cfg.OpenAICompatibility,
		func(k config.OpenAICompatibility) string { return k.Name },
		h.cfg.SanitizeOpenAICompatibility)
}

// vertex-api-key: []VertexCompatKey
func (h *Handler) GetVertexCompatKeys(c *gin.Context) {
	c.JSON(200, gin.H{"vertex-api-key": h.vertexCompatKeysWithAuthIndex()})
}
func (h *Handler) PutVertexCompatKeys(c *gin.Context) {
	putKeyList(h, c, func(i int, e *config.VertexCompatKey) error {
		normalizeVertexCompatKey(e)
		if e.APIKey == "" {
			return fmt.Errorf("vertex-api-key[%d].api-key is required", i)
		}
		return nil
	}, nil, func(v []config.VertexCompatKey) {
		h.cfg.VertexCompatAPIKey = v
		h.cfg.SanitizeVertexCompatKeys()
	})
}
func (h *Handler) PatchVertexCompatKey(c *gin.Context) {
	type vertexCompatPatch struct {
		APIKey         *string                     `json:"api-key"`
		Prefix         *string                     `json:"prefix"`
		BaseURL        *string                     `json:"base-url"`
		ProxyURL       *string                     `json:"proxy-url"`
		Headers        *map[string]string          `json:"headers"`
		Models         *[]config.VertexCompatModel `json:"models"`
		ExcludedModels *[]string                   `json:"excluded-models"`
	}
	patchKeyEntry(h, c, keyPatchSpec[config.VertexCompatKey, vertexCompatPatch]{
		Items: &h.cfg.VertexCompatAPIKey,
		KeyOf: func(k config.VertexCompatKey) string { return k.APIKey },
		APIKeyOf: func(v *vertexCompatPatch) *string {
			return v.APIKey
		},
		BaseURLOf: func(v *vertexCompatPatch) *string {
			return v.BaseURL
		},
		DeleteOnEmptyAPIKey:  true,
		DeleteOnEmptyBaseURL: true,
		Apply: func(entry *config.VertexCompatKey, v *vertexCompatPatch) {
			if v.APIKey != nil {
				entry.APIKey = strings.TrimSpace(*v.APIKey)
			}
			if v.Prefix != nil {
				entry.Prefix = strings.TrimSpace(*v.Prefix)
			}
			if v.BaseURL != nil {
				entry.BaseURL = strings.TrimSpace(*v.BaseURL)
			}
			if v.ProxyURL != nil {
				entry.ProxyURL = strings.TrimSpace(*v.ProxyURL)
			}
			if v.Headers != nil {
				entry.Headers = config.NormalizeHeaders(*v.Headers)
			}
			if v.Models != nil {
				entry.Models = append([]config.VertexCompatModel(nil), (*v.Models)...)
			}
			if v.ExcludedModels != nil {
				entry.ExcludedModels = config.NormalizeExcludedModels(*v.ExcludedModels)
			}
			normalizeVertexCompatKey(entry)
		},
		Sanitize: h.cfg.SanitizeVertexCompatKeys,
	})
}
func (h *Handler) DeleteVertexCompatKey(c *gin.Context) {
	deleteKeyEntry(h, c, &h.cfg.VertexCompatAPIKey,
		func(k config.VertexCompatKey) string { return k.APIKey },
		func(k config.VertexCompatKey) string { return k.BaseURL },
		false, h.cfg.SanitizeVertexCompatKeys)
}

// oauth-excluded-models: map[string][]string
func (h *Handler) GetOAuthExcludedModels(c *gin.Context) {
	// NormalizeOAuthExcludedModels iterates h.cfg.OAuthExcludedModels (a map).
	// Reading a map concurrently with a writer (Put/Patch/Delete) triggers a
	// fatal "concurrent map read and map write" that crashes the whole process,
	// so the read must happen under the same lock the writers hold.
	h.mu.Lock()
	normalized := config.NormalizeOAuthExcludedModels(h.cfg.OAuthExcludedModels)
	h.mu.Unlock()
	c.JSON(200, gin.H{"oauth-excluded-models": normalized})
}

func (h *Handler) PutOAuthExcludedModels(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var entries map[string][]string
	if err = json.Unmarshal(data, &entries); err != nil {
		var wrapper struct {
			Items map[string][]string `json:"items"`
		}
		if err2 := json.Unmarshal(data, &wrapper); err2 != nil {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		entries = wrapper.Items
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cfg.OAuthExcludedModels = config.NormalizeOAuthExcludedModels(entries)
	h.persistLocked(c)
}

func (h *Handler) PatchOAuthExcludedModels(c *gin.Context) {
	var body struct {
		Provider *string  `json:"provider"`
		Models   []string `json:"models"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Provider == nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	provider := strings.ToLower(strings.TrimSpace(*body.Provider))
	if provider == "" {
		c.JSON(400, gin.H{"error": "invalid provider"})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	normalized := config.NormalizeExcludedModels(body.Models)
	if len(normalized) == 0 {
		if h.cfg.OAuthExcludedModels == nil {
			c.JSON(404, gin.H{"error": "provider not found"})
			return
		}
		if _, ok := h.cfg.OAuthExcludedModels[provider]; !ok {
			c.JSON(404, gin.H{"error": "provider not found"})
			return
		}
		delete(h.cfg.OAuthExcludedModels, provider)
		if len(h.cfg.OAuthExcludedModels) == 0 {
			h.cfg.OAuthExcludedModels = nil
		}
		h.persistLocked(c)
		return
	}
	if h.cfg.OAuthExcludedModels == nil {
		h.cfg.OAuthExcludedModels = make(map[string][]string)
	}
	h.cfg.OAuthExcludedModels[provider] = normalized
	h.persistLocked(c)
}

func (h *Handler) DeleteOAuthExcludedModels(c *gin.Context) {
	provider := strings.ToLower(strings.TrimSpace(c.Query("provider")))
	if provider == "" {
		c.JSON(400, gin.H{"error": "missing provider"})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg.OAuthExcludedModels == nil {
		c.JSON(404, gin.H{"error": "provider not found"})
		return
	}
	if _, ok := h.cfg.OAuthExcludedModels[provider]; !ok {
		c.JSON(404, gin.H{"error": "provider not found"})
		return
	}
	delete(h.cfg.OAuthExcludedModels, provider)
	if len(h.cfg.OAuthExcludedModels) == 0 {
		h.cfg.OAuthExcludedModels = nil
	}
	h.persistLocked(c)
}

// oauth-model-alias: map[string][]OAuthModelAlias
func (h *Handler) GetOAuthModelAlias(c *gin.Context) {
	// sanitizedOAuthModelAlias iterates h.cfg.OAuthModelAlias (a map). Reading
	// it concurrently with a writer triggers a fatal "concurrent map read and
	// map write"; snapshot under the writers' lock and serialize the copy.
	h.mu.Lock()
	normalized := sanitizedOAuthModelAlias(h.cfg.OAuthModelAlias)
	h.mu.Unlock()
	c.JSON(200, gin.H{"oauth-model-alias": normalized})
}

func (h *Handler) PutOAuthModelAlias(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var entries map[string][]config.OAuthModelAlias
	if err = json.Unmarshal(data, &entries); err != nil {
		var wrapper struct {
			Items map[string][]config.OAuthModelAlias `json:"items"`
		}
		if err2 := json.Unmarshal(data, &wrapper); err2 != nil {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		entries = wrapper.Items
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cfg.OAuthModelAlias = sanitizedOAuthModelAlias(entries)
	h.persistLocked(c)
}

func (h *Handler) PatchOAuthModelAlias(c *gin.Context) {
	var body struct {
		Provider *string                  `json:"provider"`
		Channel  *string                  `json:"channel"`
		Aliases  []config.OAuthModelAlias `json:"aliases"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	channelRaw := ""
	if body.Channel != nil {
		channelRaw = *body.Channel
	} else if body.Provider != nil {
		channelRaw = *body.Provider
	}
	channel := strings.ToLower(strings.TrimSpace(channelRaw))
	if channel == "" {
		c.JSON(400, gin.H{"error": "invalid channel"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	normalizedMap := sanitizedOAuthModelAlias(map[string][]config.OAuthModelAlias{channel: body.Aliases})
	normalized := normalizedMap[channel]
	if len(normalized) == 0 {
		if h.cfg.OAuthModelAlias == nil {
			c.JSON(404, gin.H{"error": "channel not found"})
			return
		}
		if _, ok := h.cfg.OAuthModelAlias[channel]; !ok {
			c.JSON(404, gin.H{"error": "channel not found"})
			return
		}
		delete(h.cfg.OAuthModelAlias, channel)
		if len(h.cfg.OAuthModelAlias) == 0 {
			h.cfg.OAuthModelAlias = nil
		}
		h.persistLocked(c)
		return
	}
	if h.cfg.OAuthModelAlias == nil {
		h.cfg.OAuthModelAlias = make(map[string][]config.OAuthModelAlias)
	}
	h.cfg.OAuthModelAlias[channel] = normalized
	h.persistLocked(c)
}

func (h *Handler) DeleteOAuthModelAlias(c *gin.Context) {
	channel := strings.ToLower(strings.TrimSpace(c.Query("channel")))
	if channel == "" {
		channel = strings.ToLower(strings.TrimSpace(c.Query("provider")))
	}
	if channel == "" {
		c.JSON(400, gin.H{"error": "missing channel"})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg.OAuthModelAlias == nil {
		c.JSON(404, gin.H{"error": "channel not found"})
		return
	}
	if _, ok := h.cfg.OAuthModelAlias[channel]; !ok {
		c.JSON(404, gin.H{"error": "channel not found"})
		return
	}
	delete(h.cfg.OAuthModelAlias, channel)
	if len(h.cfg.OAuthModelAlias) == 0 {
		h.cfg.OAuthModelAlias = nil
	}
	h.persistLocked(c)
}

// codex-api-key: []CodexKey
func (h *Handler) GetCodexKeys(c *gin.Context) {
	c.JSON(200, gin.H{"codex-api-key": h.codexKeysWithAuthIndex()})
}
func (h *Handler) PutCodexKeys(c *gin.Context) {
	putKeyList(h, c, func(_ int, e *config.CodexKey) error {
		normalizeCodexKey(e)
		return nil
	}, func(e *config.CodexKey) bool {
		return e.BaseURL != ""
	}, func(v []config.CodexKey) {
		h.cfg.CodexKey = v
		h.cfg.SanitizeCodexKeys()
	})
}
func (h *Handler) PatchCodexKey(c *gin.Context) {
	type codexKeyPatch struct {
		APIKey         *string              `json:"api-key"`
		Prefix         *string              `json:"prefix"`
		BaseURL        *string              `json:"base-url"`
		ProxyURL       *string              `json:"proxy-url"`
		Models         *[]config.CodexModel `json:"models"`
		Headers        *map[string]string   `json:"headers"`
		ExcludedModels *[]string            `json:"excluded-models"`
	}
	patchKeyEntry(h, c, keyPatchSpec[config.CodexKey, codexKeyPatch]{
		Items: &h.cfg.CodexKey,
		KeyOf: func(k config.CodexKey) string { return k.APIKey },
		APIKeyOf: func(v *codexKeyPatch) *string {
			return v.APIKey
		},
		BaseURLOf: func(v *codexKeyPatch) *string {
			return v.BaseURL
		},
		DeleteOnEmptyBaseURL: true,
		Apply: func(entry *config.CodexKey, v *codexKeyPatch) {
			if v.APIKey != nil {
				entry.APIKey = strings.TrimSpace(*v.APIKey)
			}
			if v.Prefix != nil {
				entry.Prefix = strings.TrimSpace(*v.Prefix)
			}
			if v.BaseURL != nil {
				entry.BaseURL = strings.TrimSpace(*v.BaseURL)
			}
			if v.ProxyURL != nil {
				entry.ProxyURL = strings.TrimSpace(*v.ProxyURL)
			}
			if v.Models != nil {
				entry.Models = append([]config.CodexModel(nil), (*v.Models)...)
			}
			if v.Headers != nil {
				entry.Headers = config.NormalizeHeaders(*v.Headers)
			}
			if v.ExcludedModels != nil {
				entry.ExcludedModels = config.NormalizeExcludedModels(*v.ExcludedModels)
			}
			normalizeCodexKey(entry)
		},
		Sanitize: h.cfg.SanitizeCodexKeys,
	})
}
func (h *Handler) DeleteCodexKey(c *gin.Context) {
	deleteKeyEntry(h, c, &h.cfg.CodexKey,
		func(k config.CodexKey) string { return k.APIKey },
		func(k config.CodexKey) string { return k.BaseURL },
		false, h.cfg.SanitizeCodexKeys)
}

// xai-api-key: []XAIKey
func (h *Handler) GetXAIKeys(c *gin.Context) {
	c.JSON(200, gin.H{"xai-api-key": h.xaiKeysWithAuthIndex()})
}
func (h *Handler) PutXAIKeys(c *gin.Context) {
	putKeyList(h, c, func(_ int, e *config.XAIKey) error {
		normalizeCodexKey(e)
		return nil
	}, func(e *config.XAIKey) bool {
		return e.BaseURL != ""
	}, func(v []config.XAIKey) {
		h.cfg.XAIKey = v
		h.cfg.SanitizeXAIKeys()
	})
}
func (h *Handler) PatchXAIKey(c *gin.Context) {
	type xaiKeyPatch struct {
		APIKey         *string            `json:"api-key"`
		Priority       *int               `json:"priority"`
		Prefix         *string            `json:"prefix"`
		BaseURL        *string            `json:"base-url"`
		Websockets     *bool              `json:"websockets"`
		ProxyURL       *string            `json:"proxy-url"`
		Models         *[]config.XAIModel `json:"models"`
		Headers        *map[string]string `json:"headers"`
		ExcludedModels *[]string          `json:"excluded-models"`
		DisableCooling *bool              `json:"disable-cooling"`
	}
	patchKeyEntry(h, c, keyPatchSpec[config.XAIKey, xaiKeyPatch]{
		Items: &h.cfg.XAIKey,
		KeyOf: func(k config.XAIKey) string { return k.APIKey },
		APIKeyOf: func(v *xaiKeyPatch) *string {
			return v.APIKey
		},
		BaseURLOf: func(v *xaiKeyPatch) *string {
			return v.BaseURL
		},
		DeleteOnEmptyBaseURL: true,
		Apply: func(entry *config.XAIKey, v *xaiKeyPatch) {
			if v.APIKey != nil {
				entry.APIKey = strings.TrimSpace(*v.APIKey)
			}
			if v.Priority != nil {
				entry.Priority = *v.Priority
			}
			if v.Prefix != nil {
				entry.Prefix = strings.TrimSpace(*v.Prefix)
			}
			if v.BaseURL != nil {
				entry.BaseURL = strings.TrimSpace(*v.BaseURL)
			}
			if v.Websockets != nil {
				entry.Websockets = *v.Websockets
			}
			if v.ProxyURL != nil {
				entry.ProxyURL = strings.TrimSpace(*v.ProxyURL)
			}
			if v.Models != nil {
				entry.Models = append([]config.XAIModel(nil), (*v.Models)...)
			}
			if v.Headers != nil {
				entry.Headers = config.NormalizeHeaders(*v.Headers)
			}
			if v.ExcludedModels != nil {
				entry.ExcludedModels = config.NormalizeExcludedModels(*v.ExcludedModels)
			}
			if v.DisableCooling != nil {
				entry.DisableCooling = *v.DisableCooling
			}
			normalizeCodexKey(entry)
		},
		Sanitize: h.cfg.SanitizeXAIKeys,
	})
}
func (h *Handler) DeleteXAIKey(c *gin.Context) {
	deleteKeyEntry(h, c, &h.cfg.XAIKey,
		func(k config.XAIKey) string { return k.APIKey },
		func(k config.XAIKey) string { return k.BaseURL },
		false, h.cfg.SanitizeXAIKeys)
}

// zai-api-key: []ZAIKey (shares the CodexKey structure)
func (h *Handler) GetZAIKeys(c *gin.Context) {
	c.JSON(200, gin.H{"zai-api-key": h.zaiKeysWithAuthIndex()})
}
func (h *Handler) PutZAIKeys(c *gin.Context) {
	putKeyList(h, c, func(_ int, e *config.ZAIKey) error {
		normalizeCodexKey(e)
		return nil
	}, func(e *config.ZAIKey) bool {
		return e.BaseURL != ""
	}, func(v []config.ZAIKey) {
		h.cfg.ZAIKey = v
		h.cfg.SanitizeZAIKeys()
	})
}
func (h *Handler) PatchZAIKey(c *gin.Context) {
	type zaiKeyPatch struct {
		APIKey         *string              `json:"api-key"`
		Priority       *int                 `json:"priority"`
		Prefix         *string              `json:"prefix"`
		BaseURL        *string              `json:"base-url"`
		Websockets     *bool                `json:"websockets"`
		ProxyURL       *string              `json:"proxy-url"`
		Models         *[]config.CodexModel `json:"models"`
		Headers        *map[string]string   `json:"headers"`
		ExcludedModels *[]string            `json:"excluded-models"`
		DisableCooling *bool                `json:"disable-cooling"`
	}
	patchKeyEntry(h, c, keyPatchSpec[config.ZAIKey, zaiKeyPatch]{
		Items: &h.cfg.ZAIKey,
		KeyOf: func(k config.ZAIKey) string { return k.APIKey },
		APIKeyOf: func(v *zaiKeyPatch) *string {
			return v.APIKey
		},
		BaseURLOf: func(v *zaiKeyPatch) *string {
			return v.BaseURL
		},
		DeleteOnEmptyBaseURL: true,
		Apply: func(entry *config.ZAIKey, v *zaiKeyPatch) {
			if v.APIKey != nil {
				entry.APIKey = strings.TrimSpace(*v.APIKey)
			}
			if v.Priority != nil {
				entry.Priority = *v.Priority
			}
			if v.Prefix != nil {
				entry.Prefix = strings.TrimSpace(*v.Prefix)
			}
			if v.BaseURL != nil {
				entry.BaseURL = strings.TrimSpace(*v.BaseURL)
			}
			if v.Websockets != nil {
				entry.Websockets = *v.Websockets
			}
			if v.ProxyURL != nil {
				entry.ProxyURL = strings.TrimSpace(*v.ProxyURL)
			}
			if v.Models != nil {
				entry.Models = append([]config.CodexModel(nil), (*v.Models)...)
			}
			if v.Headers != nil {
				entry.Headers = config.NormalizeHeaders(*v.Headers)
			}
			if v.ExcludedModels != nil {
				entry.ExcludedModels = config.NormalizeExcludedModels(*v.ExcludedModels)
			}
			if v.DisableCooling != nil {
				entry.DisableCooling = *v.DisableCooling
			}
			normalizeCodexKey(entry)
		},
		Sanitize: h.cfg.SanitizeZAIKeys,
	})
}
func (h *Handler) DeleteZAIKey(c *gin.Context) {
	deleteKeyEntry(h, c, &h.cfg.ZAIKey,
		func(k config.ZAIKey) string { return k.APIKey },
		func(k config.ZAIKey) string { return k.BaseURL },
		false, h.cfg.SanitizeZAIKeys)
}

func normalizeOpenAICompatibilityEntry(entry *config.OpenAICompatibility) {
	if entry == nil {
		return
	}
	// Trim base-url; empty base-url indicates provider should be removed by sanitization
	entry.BaseURL = strings.TrimSpace(entry.BaseURL)
	entry.Headers = config.NormalizeHeaders(entry.Headers)
	// Only trimming happens here; deduplication is the sanitizer's job
	// (SanitizeOpenAICompatibility), so no lookup set is built.
	for i := range entry.APIKeyEntries {
		entry.APIKeyEntries[i].APIKey = strings.TrimSpace(entry.APIKeyEntries[i].APIKey)
	}
}

func normalizedOpenAICompatibilityEntries(entries []config.OpenAICompatibility) []config.OpenAICompatibility {
	if len(entries) == 0 {
		return nil
	}
	out := make([]config.OpenAICompatibility, len(entries))
	for i := range entries {
		copyEntry := entries[i]
		if len(copyEntry.APIKeyEntries) > 0 {
			copyEntry.APIKeyEntries = append([]config.OpenAICompatibilityAPIKey(nil), copyEntry.APIKeyEntries...)
		}
		normalizeOpenAICompatibilityEntry(&copyEntry)
		out[i] = copyEntry
	}
	return out
}

func normalizeClaudeKey(entry *config.ClaudeKey) {
	if entry == nil {
		return
	}
	entry.APIKey = strings.TrimSpace(entry.APIKey)
	entry.BaseURL = strings.TrimSpace(entry.BaseURL)
	entry.ProxyURL = strings.TrimSpace(entry.ProxyURL)
	entry.Headers = config.NormalizeHeaders(entry.Headers)
	entry.ExcludedModels = config.NormalizeExcludedModels(entry.ExcludedModels)
	if len(entry.Models) == 0 {
		return
	}
	normalized := make([]config.ClaudeModel, 0, len(entry.Models))
	for i := range entry.Models {
		model := entry.Models[i]
		model.Name = strings.TrimSpace(model.Name)
		model.Alias = strings.TrimSpace(model.Alias)
		if model.Name == "" && model.Alias == "" {
			continue
		}
		normalized = append(normalized, model)
	}
	entry.Models = normalized
}

func normalizeCodexKey(entry *config.CodexKey) {
	if entry == nil {
		return
	}
	entry.APIKey = strings.TrimSpace(entry.APIKey)
	entry.Prefix = strings.TrimSpace(entry.Prefix)
	entry.BaseURL = strings.TrimSpace(entry.BaseURL)
	entry.ProxyURL = strings.TrimSpace(entry.ProxyURL)
	entry.Headers = config.NormalizeHeaders(entry.Headers)
	entry.ExcludedModels = config.NormalizeExcludedModels(entry.ExcludedModels)
	if len(entry.Models) == 0 {
		return
	}
	normalized := make([]config.CodexModel, 0, len(entry.Models))
	for i := range entry.Models {
		model := entry.Models[i]
		model.Name = strings.TrimSpace(model.Name)
		model.Alias = strings.TrimSpace(model.Alias)
		if model.Name == "" && model.Alias == "" {
			continue
		}
		normalized = append(normalized, model)
	}
	entry.Models = normalized
}

func normalizeVertexCompatKey(entry *config.VertexCompatKey) {
	if entry == nil {
		return
	}
	entry.APIKey = strings.TrimSpace(entry.APIKey)
	entry.Prefix = strings.TrimSpace(entry.Prefix)
	entry.BaseURL = strings.TrimSpace(entry.BaseURL)
	entry.ProxyURL = strings.TrimSpace(entry.ProxyURL)
	entry.Headers = config.NormalizeHeaders(entry.Headers)
	entry.ExcludedModels = config.NormalizeExcludedModels(entry.ExcludedModels)
	if len(entry.Models) == 0 {
		return
	}
	normalized := make([]config.VertexCompatModel, 0, len(entry.Models))
	for i := range entry.Models {
		model := entry.Models[i]
		model.Name = strings.TrimSpace(model.Name)
		model.Alias = strings.TrimSpace(model.Alias)
		if model.Name == "" || model.Alias == "" {
			continue
		}
		normalized = append(normalized, model)
	}
	entry.Models = normalized
}

func sanitizedOAuthModelAlias(entries map[string][]config.OAuthModelAlias) map[string][]config.OAuthModelAlias {
	if len(entries) == 0 {
		return nil
	}
	copied := make(map[string][]config.OAuthModelAlias, len(entries))
	for channel, aliases := range entries {
		if len(aliases) == 0 {
			continue
		}
		copied[channel] = append([]config.OAuthModelAlias(nil), aliases...)
	}
	if len(copied) == 0 {
		return nil
	}
	cfg := config.Config{OAuthModelAlias: copied}
	cfg.SanitizeOAuthModelAlias()
	if len(cfg.OAuthModelAlias) == 0 {
		return nil
	}
	return cfg.OAuthModelAlias
}
