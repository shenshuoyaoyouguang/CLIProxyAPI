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
	// CodexReasoningReplayTurnType identifies an internal turn-boundary marker.
	CodexReasoningReplayTurnType = "cpa_codex_replay_turn"

	// CodexReasoningReplayCacheTTL limits how long encrypted reasoning replay
	// items stay in process memory.
	CodexReasoningReplayCacheTTL = 1 * time.Hour

	// CodexReasoningReplayCacheMaxEntries bounds process memory for replay
	// continuity. Oldest entries are evicted first.
	CodexReasoningReplayCacheMaxEntries = 10240

	// CodexReasoningReplayCacheMaxTurnsPerEntry bounds cumulative state for one agent.
	CodexReasoningReplayCacheMaxTurnsPerEntry = 256

	// CodexReasoningReplayCacheMaxBytesPerEntry bounds cumulative serialized items for one agent.
	CodexReasoningReplayCacheMaxBytesPerEntry = 16 << 20

	// CodexReasoningReplayCacheEvictBatchSize leaves headroom after the cache
	// reaches capacity so high write volume does not rescan the map every turn.
	CodexReasoningReplayCacheEvictBatchSize = 128
)

// codexReasoningReplayKVClient is kept as a named alias so tests can inject a
// fake client through currentCodexReasoningReplayKVClient.
type codexReasoningReplayKVClient = reasoningReplayKVClient

var currentCodexReasoningReplayKVClient = func() (codexReasoningReplayKVClient, bool, error) {
	return homekv.CurrentKVClient()
}

var codexStore = newReasoningReplayStore(reasoningReplayStoreConfig{
	memoryKeyPrefix: "codex-reasoning-replay\x00",
	kvKeyPrefix:     "cpa:codex:reasoning-replay:",
	ttl:             CodexReasoningReplayCacheTTL,
	maxEntries:      CodexReasoningReplayCacheMaxEntries,
	evictBatchSize:  CodexReasoningReplayCacheEvictBatchSize,
	logLabel:        "codex",
	// A TTL refresh failure must not fail the read path: the items were already
	// fetched successfully and the request can proceed with them.
	expireFailureFatal: false,
	normalize:          normalizeCodexReasoningReplayItems,
	appendTurn:         appendCodexReasoningReplayTurn,
	kvClient: func() (reasoningReplayKVClient, bool, error) {
		return currentCodexReasoningReplayKVClient()
	},
})

// CacheCodexReasoningReplayItem stores a final GPT/Codex reasoning item for
// stateless replay. The stored item is normalized to the minimal shape accepted
// by Responses input replay.
//
// WARNING: This REPLACES the entire cache entry (single-slot write). Multi-turn
// production paths must use AppendCodexReasoningReplayItemsBestEffort so prior
// turns remain available for cumulative replay. Prefer Append in new code.
func CacheCodexReasoningReplayItem(modelName, sessionKey string, item []byte) bool {
	return CacheCodexReasoningReplayItems(modelName, sessionKey, [][]byte{item})
}

// CacheCodexReasoningReplayItems stores the final GPT/Codex assistant output
// items needed to replay a stateless next turn.
//
// WARNING: Whole-entry replace, not append. See CacheCodexReasoningReplayItem.
func CacheCodexReasoningReplayItems(modelName, sessionKey string, items [][]byte) bool {
	return CacheCodexReasoningReplayItemsBestEffort(context.Background(), modelName, sessionKey, items)
}

// CacheCodexReasoningReplayItemsBestEffort stores replay items for completed
// response paths by replacing the full entry. Production multi-turn Codex
// completion handling uses AppendCodexReasoningReplayItemsBestEffort instead.
func CacheCodexReasoningReplayItemsBestEffort(ctx context.Context, modelName, sessionKey string, items [][]byte) bool {
	return codexStore.set(ctx, modelName, sessionKey, items) == reasoningReplayStoreStored
}

// AppendCodexReasoningReplayItemsBestEffort appends one completed turn to existing replay state.
func AppendCodexReasoningReplayItemsBestEffort(ctx context.Context, modelName, sessionKey string, items [][]byte) bool {
	return codexStore.append(ctx, modelName, sessionKey, items)
}

func appendCodexReasoningReplayTurn(existing, turn [][]byte) [][]byte {
	if len(existing) > 0 && strings.TrimSpace(gjson.GetBytes(existing[0], "type").String()) != CodexReasoningReplayTurnType {
		existing = nil
	}
	turnID := ""
	if len(turn) > 0 && strings.TrimSpace(gjson.GetBytes(turn[0], "type").String()) == CodexReasoningReplayTurnType {
		turnID = strings.TrimSpace(gjson.GetBytes(turn[0], "id").String())
	}
	if turnID != "" {
		for _, item := range existing {
			if strings.TrimSpace(gjson.GetBytes(item, "type").String()) == CodexReasoningReplayTurnType &&
				strings.TrimSpace(gjson.GetBytes(item, "id").String()) == turnID {
				return trimCodexReasoningReplayItems(cloneReasoningReplayItems(existing))
			}
		}
	}
	combined := make([][]byte, 0, len(existing)+len(turn))
	combined = append(combined, cloneReasoningReplayItems(existing)...)
	combined = append(combined, cloneReasoningReplayItems(turn)...)
	return trimCodexReasoningReplayItems(combined)
}

func trimCodexReasoningReplayItems(items [][]byte) [][]byte {
	return trimReasoningReplayItems(items, CodexReasoningReplayTurnType, CodexReasoningReplayCacheMaxTurnsPerEntry, CodexReasoningReplayCacheMaxBytesPerEntry)
}

// GetCodexReasoningReplayItem retrieves the first normalized upstream replay item.
func GetCodexReasoningReplayItem(modelName, sessionKey string) ([]byte, bool) {
	items, ok := GetCodexReasoningReplayItems(modelName, sessionKey)
	if !ok {
		return nil, false
	}
	for _, item := range items {
		if strings.TrimSpace(gjson.GetBytes(item, "type").String()) != CodexReasoningReplayTurnType {
			return item, true
		}
	}
	return nil, false
}

// GetCodexReasoningReplayItems retrieves normalized assistant output items.
func GetCodexReasoningReplayItems(modelName, sessionKey string) ([][]byte, bool) {
	items, ok, err := GetCodexReasoningReplayItemsRequired(context.Background(), modelName, sessionKey)
	if err == nil {
		return items, ok
	}
	return nil, false
}

// GetCodexReasoningReplayItemsRequired retrieves replay items for request-time paths.
func GetCodexReasoningReplayItemsRequired(ctx context.Context, modelName, sessionKey string) ([][]byte, bool, error) {
	return codexStore.get(ctx, modelName, sessionKey)
}

// DeleteCodexReasoningReplayItem removes one replay item after upstream rejects
// it or the caller otherwise knows it is stale.
func DeleteCodexReasoningReplayItem(modelName, sessionKey string) {
	if errDelete := DeleteCodexReasoningReplayItemRequired(context.Background(), modelName, sessionKey); errDelete != nil {
		return
	}
}

// DeleteCodexReasoningReplayItemRequired removes one replay item for request-time paths.
func DeleteCodexReasoningReplayItemRequired(ctx context.Context, modelName, sessionKey string) error {
	return codexStore.delete(ctx, modelName, sessionKey)
}

// ClearCodexReasoningReplayCache clears all Codex reasoning replay state.
func ClearCodexReasoningReplayCache() {
	codexStore.clear()
}

func codexReasoningReplayKVKey(modelName, sessionKey string) string {
	return reasoningReplayKVKey("cpa:codex:reasoning-replay:", modelName, sessionKey)
}

func normalizeCodexReasoningReplayItems(items [][]byte) ([][]byte, bool) {
	normalized := make([][]byte, 0, len(items))
	for _, item := range items {
		normalizedItem, ok := normalizeCodexReasoningReplayItem(item)
		if ok {
			normalized = append(normalized, normalizedItem)
		}
	}
	normalized = trimCodexReasoningReplayItems(normalized)
	return normalized, len(normalized) > 0
}

func normalizeCodexReasoningReplayItem(item []byte) ([]byte, bool) {
	itemResult := gjson.ParseBytes(item)
	switch strings.TrimSpace(itemResult.Get("type").String()) {
	case CodexReasoningReplayTurnType:
		return normalizeCodexReasoningReplayTurn(itemResult)
	case "reasoning":
		return normalizeCodexReasoningReplayReasoningItem(itemResult)
	case "function_call":
		return normalizeCodexReasoningReplayFunctionCallItem(itemResult)
	case "custom_tool_call":
		return normalizeCodexReasoningReplayCustomToolCallItem(itemResult)
	default:
		return nil, false
	}
}

func normalizeCodexReasoningReplayTurn(itemResult gjson.Result) ([]byte, bool) {
	turnID := strings.TrimSpace(itemResult.Get("id").String())
	if turnID == "" {
		return nil, false
	}
	normalized := []byte(`{"type":"` + CodexReasoningReplayTurnType + `"}`)
	normalized, _ = sjson.SetBytes(normalized, "id", turnID)
	if fingerprint := strings.TrimSpace(itemResult.Get("assistant_fingerprint").String()); fingerprint != "" {
		normalized, _ = sjson.SetBytes(normalized, "assistant_fingerprint", fingerprint)
	}
	if fingerprint := strings.TrimSpace(itemResult.Get("request_fingerprint").String()); fingerprint != "" {
		normalized, _ = sjson.SetBytes(normalized, "request_fingerprint", fingerprint)
	}
	callIDs := itemResult.Get("call_ids")
	if callIDs.IsArray() {
		for _, callIDResult := range callIDs.Array() {
			if callID := strings.TrimSpace(callIDResult.String()); callID != "" {
				normalized, _ = sjson.SetBytes(normalized, "call_ids.-1", callID)
			}
		}
	}
	return normalized, true
}

func normalizeCodexReasoningReplayReasoningItem(itemResult gjson.Result) ([]byte, bool) {
	encryptedContentResult := itemResult.Get("encrypted_content")
	if encryptedContentResult.Type != gjson.String {
		return nil, false
	}
	encryptedContent := encryptedContentResult.String()
	if encryptedContent != strings.TrimSpace(encryptedContent) {
		return nil, false
	}
	if _, err := signature.InspectGPTReasoningSignature(encryptedContent); err != nil {
		return nil, false
	}

	normalized := []byte(`{"type":"reasoning","summary":[],"content":null}`)
	normalized, _ = sjson.SetBytes(normalized, "encrypted_content", encryptedContent)
	return normalized, true
}

func normalizeCodexReasoningReplayFunctionCallItem(itemResult gjson.Result) ([]byte, bool) {
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

func normalizeCodexReasoningReplayCustomToolCallItem(itemResult gjson.Result) ([]byte, bool) {
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

func purgeExpiredCodexReasoningReplayCache(now time.Time) {
	codexStore.purgeExpired(now)
}
