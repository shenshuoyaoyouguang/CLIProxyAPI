package cache

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	homekv "github.com/router-for-me/CLIProxyAPI/v7/internal/home"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/signature"
	log "github.com/sirupsen/logrus"
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

type codexReasoningReplayEntry struct {
	Items     [][]byte
	Timestamp time.Time
}

var codexReasoningReplayCache = NewLRUCache[string, codexReasoningReplayEntry](CodexReasoningReplayCacheMaxEntries, CodexReasoningReplayCacheTTL, nil)

func init() {
	// Sliding TTL: every Get refreshes the entry age, matching the
	// previous "oldest last-touched" eviction behavior.
	codexReasoningReplayCache.SetSliding(true)
	registerCacheCleanup(purgeExpiredCodexReasoningReplayCache)
}

type codexReasoningReplayKVClient interface {
	KVGet(ctx context.Context, key string) ([]byte, bool, error)
	KVSet(ctx context.Context, key string, value []byte, opts homekv.KVSetOptions) (bool, error)
	KVCompareAndSwap(ctx context.Context, key string, expected []byte, expectedExists bool, value []byte, ttl time.Duration) (bool, error)
	KVDel(ctx context.Context, keys ...string) (int64, error)
	KVExpire(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

var currentCodexReasoningReplayKVClient = func() (codexReasoningReplayKVClient, bool, error) {
	return homekv.CurrentKVClient()
}

// CacheCodexReasoningReplayItem stores a final GPT/Codex reasoning item for
// stateless replay. The stored item is normalized to the minimal shape accepted
// by Responses input replay.
func CacheCodexReasoningReplayItem(modelName, sessionKey string, item []byte) bool {
	return CacheCodexReasoningReplayItems(modelName, sessionKey, [][]byte{item})
}

// CacheCodexReasoningReplayItems stores the final GPT/Codex assistant output
// items needed to replay a stateless next turn.
func CacheCodexReasoningReplayItems(modelName, sessionKey string, items [][]byte) bool {
	return CacheCodexReasoningReplayItemsBestEffort(context.Background(), modelName, sessionKey, items)
}

// CacheCodexReasoningReplayItemsBestEffort stores replay items for completed response paths.
func CacheCodexReasoningReplayItemsBestEffort(ctx context.Context, modelName, sessionKey string, items [][]byte) bool {
	key := codexReasoningReplayCacheKey(modelName, sessionKey)
	if key == "" {
		return false
	}
	normalized, ok := normalizeCodexReasoningReplayItems(items)
	if !ok {
		return false
	}
	if client, homeMode, errClient := currentCodexReasoningReplayKVClient(); homeMode {
		if errClient != nil {
			log.Errorf("home kv best-effort codex reasoning replay set failed prefix=cpa:codex:*: %v", errClient)
			return false
		}
		raw, errMarshal := json.Marshal(normalized)
		if errMarshal != nil {
			log.Errorf("home kv best-effort codex reasoning replay set failed prefix=cpa:codex:*: %v", errMarshal)
			return false
		}
		written, errSet := client.KVSet(ctx, codexReasoningReplayKVKey(modelName, sessionKey), raw, homekv.KVSetOptions{EX: CodexReasoningReplayCacheTTL})
		if errSet != nil {
			log.Errorf("home kv best-effort codex reasoning replay set failed prefix=cpa:codex:*: %v", errSet)
			return false
		}
		return written
	}

	cacheCleanupOnce.Do(startCacheCleanup)
	codexReasoningReplayCache.Set(key, codexReasoningReplayEntry{
		Items:     normalized,
		Timestamp: time.Now(),
	})
	return true
}

// AppendCodexReasoningReplayItemsBestEffort appends one completed turn to existing replay state.
func AppendCodexReasoningReplayItemsBestEffort(ctx context.Context, modelName, sessionKey string, items [][]byte) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	key := codexReasoningReplayCacheKey(modelName, sessionKey)
	if key == "" {
		return false
	}
	normalized, ok := normalizeCodexReasoningReplayItems(items)
	if !ok {
		return false
	}
	if client, homeMode, errClient := currentCodexReasoningReplayKVClient(); homeMode {
		if errClient != nil {
			log.Errorf("home kv best-effort codex reasoning replay append failed prefix=cpa:codex:*: %v", errClient)
			return false
		}
		kvKey := codexReasoningReplayKVKey(modelName, sessionKey)
		const maxCASAttempts = 32
		for attempt := 0; attempt < maxCASAttempts; attempt++ {
			if errContext := ctx.Err(); errContext != nil {
				return false
			}
			existingRaw, found, errGet := client.KVGet(ctx, kvKey)
			if errGet != nil {
				log.Errorf("home kv best-effort codex reasoning replay append failed prefix=cpa:codex:*: %v", errGet)
				return false
			}
			var existing [][]byte
			if found {
				if errUnmarshal := json.Unmarshal(existingRaw, &existing); errUnmarshal != nil {
					log.Errorf("home kv best-effort codex reasoning replay append failed prefix=cpa:codex:*: %v", errUnmarshal)
					return false
				}
			}
			combined := appendCodexReasoningReplayTurn(existing, normalized)
			raw, errMarshal := json.Marshal(combined)
			if errMarshal != nil {
				log.Errorf("home kv best-effort codex reasoning replay append failed prefix=cpa:codex:*: %v", errMarshal)
				return false
			}
			written, errCAS := client.KVCompareAndSwap(ctx, kvKey, existingRaw, found, raw, CodexReasoningReplayCacheTTL)
			if errCAS != nil {
				log.Errorf("home kv best-effort codex reasoning replay append failed prefix=cpa:codex:*: %v", errCAS)
				return false
			}
			if written {
				return true
			}
		}
		log.Warn("home kv best-effort codex reasoning replay append exhausted compare-and-swap attempts")
		return false
	}

	cacheCleanupOnce.Do(startCacheCleanup)
	// Atomic read-modify-write: the LRU's internal lock protects the full
	// Get → append → Set sequence, preventing lost updates between concurrent
	// Append calls for the same session key.
	now := time.Now()
	codexReasoningReplayCache.Update(key, func(old codexReasoningReplayEntry, exists bool) (codexReasoningReplayEntry, bool) {
		if !exists {
			old = codexReasoningReplayEntry{Items: nil, Timestamp: now}
		}
		old.Items = appendCodexReasoningReplayTurn(old.Items, normalized)
		old.Timestamp = now
		return old, true
	})
	return true
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
				return trimCodexReasoningReplayItems(cloneCodexReasoningReplayItems(existing))
			}
		}
	}
	combined := make([][]byte, 0, len(existing)+len(turn))
	combined = append(combined, cloneCodexReasoningReplayItems(existing)...)
	combined = append(combined, cloneCodexReasoningReplayItems(turn)...)
	return trimCodexReasoningReplayItems(combined)
}

func trimCodexReasoningReplayItems(items [][]byte) [][]byte {
	for {
		turnStarts := []int{0}
		totalBytes := 0
		for index, item := range items {
			totalBytes += len(item)
			if index > 0 && strings.TrimSpace(gjson.GetBytes(item, "type").String()) == CodexReasoningReplayTurnType {
				turnStarts = append(turnStarts, index)
			}
		}
		if len(turnStarts) <= CodexReasoningReplayCacheMaxTurnsPerEntry && totalBytes <= CodexReasoningReplayCacheMaxBytesPerEntry {
			return items
		}
		if len(turnStarts) <= 1 {
			return nil
		}
		items = items[turnStarts[1]:]
	}
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
	key := codexReasoningReplayCacheKey(modelName, sessionKey)
	if key == "" {
		return nil, false, nil
	}
	client, homeMode, errClient := currentCodexReasoningReplayKVClient()
	if homeMode {
		if errClient != nil {
			return nil, false, errClient
		}
		raw, found, errGet := client.KVGet(ctx, codexReasoningReplayKVKey(modelName, sessionKey))
		if errGet != nil || !found {
			return nil, false, errGet
		}
		var homeItems [][]byte
		if errUnmarshal := json.Unmarshal(raw, &homeItems); errUnmarshal != nil {
			return nil, false, errUnmarshal
		}
		if _, errExpire := client.KVExpire(ctx, codexReasoningReplayKVKey(modelName, sessionKey), CodexReasoningReplayCacheTTL); errExpire != nil {
			return nil, false, errExpire
		}
		return cloneCodexReasoningReplayItems(homeItems), true, nil
	}

	cacheCleanupOnce.Do(startCacheCleanup)
	entry, ok := codexReasoningReplayCache.Get(key)
	if !ok {
		return nil, false, nil
	}
	return cloneCodexReasoningReplayItems(entry.Items), true, nil
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
	key := codexReasoningReplayCacheKey(modelName, sessionKey)
	if key == "" {
		return nil
	}
	client, homeMode, errClient := currentCodexReasoningReplayKVClient()
	if homeMode {
		if errClient != nil {
			return errClient
		}
		_, errDel := client.KVDel(ctx, codexReasoningReplayKVKey(modelName, sessionKey))
		return errDel
	}
	codexReasoningReplayCache.Delete(key)
	return nil
}

// ClearCodexReasoningReplayCache clears all Codex reasoning replay state.
func ClearCodexReasoningReplayCache() {
	codexReasoningReplayCache.Clear()
}

func codexReasoningReplayCacheKey(modelName, sessionKey string) string {
	modelName = strings.TrimSpace(modelName)
	sessionKey = strings.TrimSpace(sessionKey)
	if modelName == "" || sessionKey == "" {
		return ""
	}
	// The session key is the continuity boundary. Keep this independent from
	// the selected upstream Codex credential so auth failover can preserve replay.
	return strings.Join([]string{"codex-reasoning-replay", modelName, sessionKey}, "\x00")
}

func codexReasoningReplayKVKey(modelName, sessionKey string) string {
	return "cpa:codex:reasoning-replay:" + homekv.HashKeyPart(strings.TrimSpace(modelName)) + ":" + homekv.HashKeyPart(strings.TrimSpace(sessionKey))
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

func cloneCodexReasoningReplayItems(items [][]byte) [][]byte {
	cloned := make([][]byte, 0, len(items))
	for _, item := range items {
		cloned = append(cloned, append([]byte(nil), item...))
	}
	return cloned
}

func purgeExpiredCodexReasoningReplayCache(now time.Time) {
	codexReasoningReplayCache.PurgeExpired(now)
}
