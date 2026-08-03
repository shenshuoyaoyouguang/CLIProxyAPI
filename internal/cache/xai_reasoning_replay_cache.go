package cache

import (
	"context"
	"strings"
	"time"

	homekv "github.com/router-for-me/CLIProxyAPI/v7/internal/home"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/signature"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	// XAIReasoningReplayCacheTTL limits how long encrypted reasoning replay
	// items stay in process memory.
	XAIReasoningReplayCacheTTL = 1 * time.Hour

	// XAIReasoningReplayCacheMaxEntries bounds process memory for replay
	// continuity. Oldest entries are evicted first.
	XAIReasoningReplayCacheMaxEntries = 10240

	// XAIReasoningReplayCacheEvictBatchSize leaves headroom after the cache
	// reaches capacity so high write volume does not rescan the map every turn.
	XAIReasoningReplayCacheEvictBatchSize = 128
)

// xaiReasoningReplayKVClient is kept as a named alias so tests can inject a
// fake client through currentXAIReasoningReplayKVClient.
type xaiReasoningReplayKVClient = reasoningReplayKVClient

var currentXAIReasoningReplayKVClient = func() (xaiReasoningReplayKVClient, bool, error) {
	return homekv.CurrentKVClient()
}

var xaiStore = newReasoningReplayStore(reasoningReplayStoreConfig{
	memoryKeyPrefix:    "xai-reasoning-replay\x00",
	kvKeyPrefix:        "cpa:xai:reasoning-replay:",
	ttl:                XAIReasoningReplayCacheTTL,
	maxEntries:         XAIReasoningReplayCacheMaxEntries,
	evictBatchSize:     XAIReasoningReplayCacheEvictBatchSize,
	logLabel:           "xai",
	expireFailureFatal: false,
	normalize:          normalizeXAIReasoningReplayItems,
	kvClient: func() (reasoningReplayKVClient, bool, error) {
		return currentXAIReasoningReplayKVClient()
	},
})

// CacheXAIReasoningReplayItem stores a final Grok reasoning item for stateless
// replay. The stored item is normalized to the minimal shape accepted by
// Responses input replay.
func CacheXAIReasoningReplayItem(modelName, sessionKey string, item []byte) bool {
	return CacheXAIReasoningReplayItems(modelName, sessionKey, [][]byte{item})
}

// CacheXAIReasoningReplayItems stores the final Grok assistant output items
// needed to replay a stateless next turn.
func CacheXAIReasoningReplayItems(modelName, sessionKey string, items [][]byte) bool {
	return CacheXAIReasoningReplayItemsBestEffort(context.Background(), modelName, sessionKey, items)
}

// XAIReasoningReplayStoreStatus reports why a completed-turn cache write
// succeeded or failed so callers can decide whether to keep prior entries.
type XAIReasoningReplayStoreStatus = reasoningReplayStoreStatus

const (
	// XAIReasoningReplayStoreInvalidArgs means model/session were empty.
	XAIReasoningReplayStoreInvalidArgs = reasoningReplayStoreInvalidArgs
	// XAIReasoningReplayStored means a valid reasoning batch was written.
	XAIReasoningReplayStored = reasoningReplayStoreStored
	// XAIReasoningReplayNoReplayableState means the completed output had no
	// cacheable reasoning batch (for example reasoning disabled).
	XAIReasoningReplayNoReplayableState = reasoningReplayStoreNoReplayableState
	// XAIReasoningReplayStoreBackendError means normalize succeeded but the
	// storage backend failed. The completed-turn cache writer
	// (cacheXAIReasoningReplayFromCompleted) intentionally deletes any prior
	// entry under this status so a stale encrypted reasoning block from an
	// earlier turn is not injected into the now-advanced conversation; callers
	// that prefer "retain on backend error" semantics must implement their own
	// retry / fallback rather than relying on the cache layer to preserve state.
	XAIReasoningReplayStoreBackendError = reasoningReplayStoreBackendError
)

// CacheXAIReasoningReplayItemsBestEffort stores replay items for completed response paths.
func CacheXAIReasoningReplayItemsBestEffort(ctx context.Context, modelName, sessionKey string, items [][]byte) bool {
	return StoreXAIReasoningReplayItems(ctx, modelName, sessionKey, items) == XAIReasoningReplayStored
}

// StoreXAIReasoningReplayItems stores replay items and distinguishes empty
// completed state from backend failures.
func StoreXAIReasoningReplayItems(ctx context.Context, modelName, sessionKey string, items [][]byte) XAIReasoningReplayStoreStatus {
	return xaiStore.set(ctx, modelName, sessionKey, items)
}

// GetXAIReasoningReplayItem retrieves a normalized reasoning replay item.
func GetXAIReasoningReplayItem(modelName, sessionKey string) ([]byte, bool) {
	items, ok := GetXAIReasoningReplayItems(modelName, sessionKey)
	if !ok || len(items) == 0 {
		return nil, false
	}
	return items[0], true
}

// GetXAIReasoningReplayItems retrieves normalized assistant output items.
func GetXAIReasoningReplayItems(modelName, sessionKey string) ([][]byte, bool) {
	items, ok, err := GetXAIReasoningReplayItemsRequired(context.Background(), modelName, sessionKey)
	if err == nil {
		return items, ok
	}
	return nil, false
}

// GetXAIReasoningReplayItemsRequired retrieves replay items for request-time paths.
func GetXAIReasoningReplayItemsRequired(ctx context.Context, modelName, sessionKey string) ([][]byte, bool, error) {
	return xaiStore.get(ctx, modelName, sessionKey)
}

// DeleteXAIReasoningReplayItem removes one replay item after upstream rejects
// it or the caller otherwise knows it is stale.
func DeleteXAIReasoningReplayItem(modelName, sessionKey string) {
	if errDelete := DeleteXAIReasoningReplayItemRequired(context.Background(), modelName, sessionKey); errDelete != nil {
		return
	}
}

// DeleteXAIReasoningReplayItemRequired removes one replay item for request-time paths.
func DeleteXAIReasoningReplayItemRequired(ctx context.Context, modelName, sessionKey string) error {
	return xaiStore.delete(ctx, modelName, sessionKey)
}

// ClearXAIReasoningReplayCache clears all xAI reasoning replay state.
func ClearXAIReasoningReplayCache() {
	xaiStore.clear()
}

func xaiReasoningReplayKVKey(modelName, sessionKey string) string {
	return reasoningReplayKVKey("cpa:xai:reasoning-replay:", modelName, sessionKey)
}

func normalizeXAIReasoningReplayItems(items [][]byte) ([][]byte, bool) {
	normalized := make([][]byte, 0, len(items))
	hasReplayAnchor := false
	for _, item := range items {
		normalizedItem, ok := normalizeXAIReasoningReplayItem(item)
		if ok {
			normalized = append(normalized, normalizedItem)
			switch strings.TrimSpace(gjson.GetBytes(normalizedItem, "type").String()) {
			case "reasoning", "function_call", "custom_tool_call":
				hasReplayAnchor = true
			}
		}
	}
	return normalized, hasReplayAnchor
}

func normalizeXAIReasoningReplayItem(item []byte) ([]byte, bool) {
	itemResult := gjson.ParseBytes(item)
	switch strings.TrimSpace(itemResult.Get("type").String()) {
	case "reasoning":
		return normalizeXAIReasoningReplayReasoningItem(itemResult)
	case "message":
		return normalizeXAIReasoningReplayMessageItem(itemResult)
	case "function_call":
		return normalizeXAIReasoningReplayFunctionCallItem(itemResult)
	case "custom_tool_call":
		return normalizeXAIReasoningReplayCustomToolCallItem(itemResult)
	default:
		return nil, false
	}
}

func normalizeXAIReasoningReplayReasoningItem(itemResult gjson.Result) ([]byte, bool) {
	encryptedContentResult := itemResult.Get("encrypted_content")
	if encryptedContentResult.Type != gjson.String {
		return nil, false
	}
	encryptedContent := encryptedContentResult.String()
	if encryptedContent != strings.TrimSpace(encryptedContent) {
		return nil, false
	}
	if _, err := signature.InspectGrokEncryptedContent(encryptedContent); err != nil {
		return nil, false
	}

	normalized := []byte(`{"type":"reasoning","summary":[],"content":null}`)
	normalized, _ = sjson.SetBytes(normalized, "encrypted_content", encryptedContent)
	return normalized, true
}

func normalizeXAIReasoningReplayMessageItem(itemResult gjson.Result) ([]byte, bool) {
	if !strings.EqualFold(strings.TrimSpace(itemResult.Get("role").String()), "assistant") {
		return nil, false
	}
	content := itemResult.Get("content")
	if !content.IsArray() || len(content.Array()) == 0 {
		return nil, false
	}

	normalized := []byte(`{"type":"message","role":"assistant","content":[]}`)
	for _, part := range content.Array() {
		partType := strings.TrimSpace(part.Get("type").String())
		var nextPart []byte
		switch partType {
		case "output_text":
			textValue := part.Get("text")
			if textValue.Type != gjson.String {
				continue
			}
			nextPart = []byte(`{"type":"output_text","text":""}`)
			nextPart, _ = sjson.SetBytes(nextPart, "text", textValue.String())
		case "refusal":
			// Responses API refusal parts use the "refusal" field, not "text".
			refusalValue := part.Get("refusal")
			if refusalValue.Type != gjson.String {
				continue
			}
			nextPart = []byte(`{"type":"refusal","refusal":""}`)
			nextPart, _ = sjson.SetBytes(nextPart, "refusal", refusalValue.String())
		default:
			continue
		}
		updated, errSet := sjson.SetRawBytes(normalized, "content.-1", nextPart)
		if errSet != nil {
			return nil, false
		}
		normalized = updated
	}
	if len(gjson.GetBytes(normalized, "content").Array()) == 0 {
		return nil, false
	}
	return normalized, true
}

func normalizeXAIReasoningReplayFunctionCallItem(itemResult gjson.Result) ([]byte, bool) {
	callID := strings.TrimSpace(itemResult.Get("call_id").String())
	name := strings.TrimSpace(itemResult.Get("name").String())
	arguments := itemResult.Get("arguments")
	if callID == "" || name == "" || arguments.Type != gjson.String {
		return nil, false
	}

	normalized := []byte(`{"type":"function_call"}`)
	normalized, _ = sjson.SetBytes(normalized, "call_id", callID)
	normalized, _ = sjson.SetBytes(normalized, "name", name)
	normalized, _ = sjson.SetBytes(normalized, "arguments", arguments.String())
	return normalized, true
}

func normalizeXAIReasoningReplayCustomToolCallItem(itemResult gjson.Result) ([]byte, bool) {
	callID := strings.TrimSpace(itemResult.Get("call_id").String())
	name := strings.TrimSpace(itemResult.Get("name").String())
	input := itemResult.Get("input")
	if callID == "" || name == "" || !input.Exists() {
		return nil, false
	}

	normalized := []byte(`{"type":"custom_tool_call","status":"completed"}`)
	if status := strings.TrimSpace(itemResult.Get("status").String()); status != "" {
		normalized, _ = sjson.SetBytes(normalized, "status", status)
	}
	normalized, _ = sjson.SetBytes(normalized, "call_id", callID)
	normalized, _ = sjson.SetBytes(normalized, "name", name)
	if input.Type == gjson.String {
		normalized, _ = sjson.SetBytes(normalized, "input", input.String())
	} else {
		normalized, _ = sjson.SetRawBytes(normalized, "input", []byte(input.Raw))
	}
	return normalized, true
}

func purgeExpiredXAIReasoningReplayCache(now time.Time) {
	xaiStore.purgeExpired(now)
}
