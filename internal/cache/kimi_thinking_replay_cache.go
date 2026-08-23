package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	homekv "github.com/router-for-me/CLIProxyAPI/v7/internal/home"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

const (
	// KimiThinkingReplayCacheTTL limits how long signed assistant content stays replayable.
	KimiThinkingReplayCacheTTL = 1 * time.Hour

	// KimiThinkingReplayCacheMaxEntries bounds process memory used for replay continuity.
	KimiThinkingReplayCacheMaxEntries = 10240

	// KimiThinkingReplayCacheEvictBatchSize leaves headroom after reaching capacity.
	KimiThinkingReplayCacheEvictBatchSize = 128

	// KimiThinkingReplayCacheMaxBytesPerEntry bounds one complete assistant content array.
	KimiThinkingReplayCacheMaxBytesPerEntry = 8 << 20

	// KimiThinkingReplayCacheMaxBlocksPerEntry prevents pathological content arrays.
	KimiThinkingReplayCacheMaxBlocksPerEntry = 512

	// KimiThinkingReplayCacheMaxTotalBytes bounds aggregate in-process replay content.
	KimiThinkingReplayCacheMaxTotalBytes = 256 << 20

	kimiThinkingReplayCacheMaxSerializedBytes = KimiThinkingReplayCacheMaxBytesPerEntry + 1024
)

// KimiThinkingReplaySnapshot identifies the exact replay generation read for one request.
type KimiThinkingReplaySnapshot struct {
	raw        []byte
	generation string
	loaded     bool
	found      bool
}

type kimiThinkingReplayHomeValue struct {
	Generation string          `json:"generation"`
	Deleted    bool            `json:"deleted,omitempty"`
	Content    json.RawMessage `json:"content,omitempty"`
}

var (
	kimiThinkingReplayMu         sync.Mutex
	kimiThinkingReplayEntries    = make(map[string]fencedReplayEntry[[]byte])
	kimiThinkingReplayTotalBytes int
)

type kimiThinkingReplayKVClient interface {
	KVGet(ctx context.Context, key string) ([]byte, bool, error)
	KVSet(ctx context.Context, key string, value []byte, opts homekv.KVSetOptions) (bool, error)
	KVDel(ctx context.Context, keys ...string) (int64, error)
	KVCompareAndSwap(ctx context.Context, key string, expected []byte, expectedExists bool, value []byte, ttl time.Duration) (bool, error)
	KVExpire(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

var currentKimiThinkingReplayKVClient = func() (kimiThinkingReplayKVClient, bool, error) {
	return homekv.CurrentKVClient()
}

func kimiThinkingReplayLimits() fencedReplayLimits {
	return fencedReplayLimits{
		TTL:           KimiThinkingReplayCacheTTL,
		MaxEntries:    KimiThinkingReplayCacheMaxEntries,
		EvictBatch:    KimiThinkingReplayCacheEvictBatchSize,
		MaxTotalBytes: KimiThinkingReplayCacheMaxTotalBytes,
	}
}

// CacheKimiThinkingReplayBestEffort stores one complete signed assistant content array.
func CacheKimiThinkingReplayBestEffort(ctx context.Context, modelFamily, sessionKey string, content []byte) bool {
	key := kimiThinkingReplayCacheKey(modelFamily, sessionKey)
	if key == "" || !validKimiThinkingReplayContent(content) {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cloned := append([]byte(nil), content...)
	generation := uuid.NewString()
	if client, homeMode, errClient := currentKimiThinkingReplayKVClient(); homeMode {
		if errClient != nil {
			log.Errorf("home kv best-effort kimi thinking replay set failed prefix=cpa:kimi:*: %v", errClient)
			return false
		}
		raw, errMarshal := marshalKimiThinkingReplayHomeValue(generation, false, cloned)
		if errMarshal != nil {
			log.Errorf("home kv best-effort kimi thinking replay set failed prefix=cpa:kimi:*: %v", errMarshal)
			return false
		}
		written, errSet := client.KVSet(ctx, kimiThinkingReplayKVKey(modelFamily, sessionKey), raw, homekv.KVSetOptions{EX: KimiThinkingReplayCacheTTL})
		if errSet != nil {
			log.Errorf("home kv best-effort kimi thinking replay set failed prefix=cpa:kimi:*: %v", errSet)
			return false
		}
		return written
	}

	cacheCleanupOnce.Do(startCacheCleanup)
	storeFencedReplayLocal(&kimiThinkingReplayMu, kimiThinkingReplayEntries, &kimiThinkingReplayTotalBytes, kimiThinkingReplayLimits(), kimiThinkingReplayContentSize, key, cloned, generation, false, time.Now())
	return true
}

// GetKimiThinkingReplayRequired retrieves complete assistant content for request-time replay.
func GetKimiThinkingReplayRequired(ctx context.Context, modelFamily, sessionKey string) ([]byte, bool, error) {
	content, _, found, errGet := GetKimiThinkingReplayWithSnapshotRequired(ctx, modelFamily, sessionKey)
	return content, found, errGet
}

// GetKimiThinkingReplayWithSnapshotRequired retrieves replay content and the exact cache state read.
func GetKimiThinkingReplayWithSnapshotRequired(ctx context.Context, modelFamily, sessionKey string) ([]byte, KimiThinkingReplaySnapshot, bool, error) {
	key := kimiThinkingReplayCacheKey(modelFamily, sessionKey)
	if key == "" {
		return nil, KimiThinkingReplaySnapshot{}, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	client, homeMode, errClient := currentKimiThinkingReplayKVClient()
	if homeMode {
		if errClient != nil {
			return nil, KimiThinkingReplaySnapshot{loaded: true}, false, errClient
		}
		kvKey := kimiThinkingReplayKVKey(modelFamily, sessionKey)
		tombstone, errMarshal := marshalKimiThinkingReplayHomeValue(uuid.NewString(), true, nil)
		if errMarshal != nil {
			return nil, KimiThinkingReplaySnapshot{loaded: true}, false, errMarshal
		}
		raw, errRead := readOrReserveFencedReplayHome(ctx, client, kvKey, KimiThinkingReplayCacheTTL, kimiThinkingReplayCacheMaxSerializedBytes, "kimi", tombstone)
		if errRead != nil {
			return nil, KimiThinkingReplaySnapshot{loaded: true}, false, errRead
		}
		snapshot := KimiThinkingReplaySnapshot{raw: append([]byte(nil), raw...), loaded: true, found: true}
		content, generation, deleted, okDecode := decodeKimiThinkingReplayHomeValue(raw)
		if !okDecode {
			return nil, snapshot, false, fmt.Errorf("invalid kimi thinking replay content")
		}
		snapshot.generation = generation
		if _, errExpire := client.KVExpire(ctx, kvKey, KimiThinkingReplayCacheTTL); errExpire != nil {
			log.Warnf("home kv kimi thinking replay expire failed prefix=cpa:kimi:*: %v", errExpire)
		}
		if deleted {
			return nil, snapshot, false, nil
		}
		return content, snapshot, true, nil
	}

	cacheCleanupOnce.Do(startCacheCleanup)
	entry := lookupFencedReplayLocal(&kimiThinkingReplayMu, kimiThinkingReplayEntries, &kimiThinkingReplayTotalBytes, kimiThinkingReplayLimits(), kimiThinkingReplayContentSize, key, time.Now())
	snapshot := KimiThinkingReplaySnapshot{generation: entry.Generation, loaded: true, found: true}
	if entry.Deleted {
		return nil, snapshot, false, nil
	}
	return append([]byte(nil), entry.Value...), snapshot, true, nil
}

// ReplaceKimiThinkingReplayIfUnchanged stores completed content only if the request snapshot is current.
func ReplaceKimiThinkingReplayIfUnchanged(ctx context.Context, modelFamily, sessionKey string, snapshot KimiThinkingReplaySnapshot, content []byte) (bool, error) {
	key := kimiThinkingReplayCacheKey(modelFamily, sessionKey)
	if key == "" || !validKimiThinkingReplayContent(content) {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !snapshot.loaded {
		return CacheKimiThinkingReplayBestEffort(ctx, modelFamily, sessionKey, content), nil
	}
	cloned := append([]byte(nil), content...)
	client, homeMode, errClient := currentKimiThinkingReplayKVClient()
	if homeMode {
		if errClient != nil {
			return false, errClient
		}
		raw, errMarshal := marshalKimiThinkingReplayHomeValue(uuid.NewString(), false, cloned)
		if errMarshal != nil {
			return false, errMarshal
		}
		return client.KVCompareAndSwap(ctx, kimiThinkingReplayKVKey(modelFamily, sessionKey), snapshot.raw, snapshot.found, raw, KimiThinkingReplayCacheTTL)
	}

	cacheCleanupOnce.Do(startCacheCleanup)
	kimiThinkingReplayMu.Lock()
	defer kimiThinkingReplayMu.Unlock()
	if _, current := fencedReplaySnapshotCurrentLocked(kimiThinkingReplayEntries, key, snapshot.found, snapshot.generation); !current {
		return false, nil
	}
	commitFencedReplayReplaceLocked(kimiThinkingReplayEntries, &kimiThinkingReplayTotalBytes, kimiThinkingReplayLimits(), kimiThinkingReplayContentSize, key, cloned, time.Now())
	return true, nil
}

// DeleteKimiThinkingReplayIfUnchanged clears replay state only if the request snapshot is current.
func DeleteKimiThinkingReplayIfUnchanged(ctx context.Context, modelFamily, sessionKey string, snapshot KimiThinkingReplaySnapshot) (bool, error) {
	key := kimiThinkingReplayCacheKey(modelFamily, sessionKey)
	if key == "" {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !snapshot.loaded {
		return true, DeleteKimiThinkingReplayRequired(ctx, modelFamily, sessionKey)
	}
	generation := uuid.NewString()
	client, homeMode, errClient := currentKimiThinkingReplayKVClient()
	if homeMode {
		if errClient != nil {
			return false, errClient
		}
		tombstone, errMarshal := marshalKimiThinkingReplayHomeValue(generation, true, nil)
		if errMarshal != nil {
			return false, errMarshal
		}
		return client.KVCompareAndSwap(ctx, kimiThinkingReplayKVKey(modelFamily, sessionKey), snapshot.raw, snapshot.found, tombstone, KimiThinkingReplayCacheTTL)
	}

	return tombstoneFencedReplayIfUnchanged(&kimiThinkingReplayMu, kimiThinkingReplayEntries, &kimiThinkingReplayTotalBytes, kimiThinkingReplayContentSize, key, snapshot.found, snapshot.generation, time.Now()), nil
}

// DeleteKimiThinkingReplayRequired removes stale replay state unconditionally.
func DeleteKimiThinkingReplayRequired(ctx context.Context, modelFamily, sessionKey string) error {
	key := kimiThinkingReplayCacheKey(modelFamily, sessionKey)
	if key == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	client, homeMode, errClient := currentKimiThinkingReplayKVClient()
	if homeMode {
		if errClient != nil {
			return errClient
		}
		_, errDelete := client.KVDel(ctx, kimiThinkingReplayKVKey(modelFamily, sessionKey))
		return errDelete
	}
	removeFencedReplayLocal(&kimiThinkingReplayMu, kimiThinkingReplayEntries, &kimiThinkingReplayTotalBytes, kimiThinkingReplayContentSize, key)
	return nil
}

// ClearKimiThinkingReplayCache clears all in-process Kimi replay state.
func ClearKimiThinkingReplayCache() {
	clearFencedReplayLocal(&kimiThinkingReplayMu, kimiThinkingReplayEntries, &kimiThinkingReplayTotalBytes)
}

func kimiThinkingReplayContentSize(content []byte) int {
	return len(content)
}

func marshalKimiThinkingReplayHomeValue(generation string, deleted bool, content []byte) ([]byte, error) {
	value := kimiThinkingReplayHomeValue{Generation: generation, Deleted: deleted}
	if !deleted {
		value.Content = append(json.RawMessage(nil), content...)
	}
	return json.Marshal(value)
}

func decodeKimiThinkingReplayHomeValue(raw []byte) ([]byte, string, bool, bool) {
	if len(raw) == 0 || len(raw) > kimiThinkingReplayCacheMaxSerializedBytes || !gjson.ValidBytes(raw) {
		return nil, "", false, false
	}
	root := gjson.ParseBytes(raw)
	if root.IsArray() {
		if !validKimiThinkingReplayContent(raw) {
			return nil, "", false, false
		}
		return append([]byte(nil), raw...), "legacy", false, true
	}
	var value kimiThinkingReplayHomeValue
	if errUnmarshal := json.Unmarshal(raw, &value); errUnmarshal != nil || strings.TrimSpace(value.Generation) == "" {
		return nil, "", false, false
	}
	if value.Deleted {
		return nil, value.Generation, true, true
	}
	if !validKimiThinkingReplayContent(value.Content) {
		return nil, "", false, false
	}
	return append([]byte(nil), value.Content...), value.Generation, false, true
}

func kimiThinkingReplayCacheKey(modelFamily, sessionKey string) string {
	modelFamily = strings.TrimSpace(modelFamily)
	sessionKey = strings.TrimSpace(sessionKey)
	if modelFamily == "" || sessionKey == "" {
		return ""
	}
	return strings.Join([]string{"kimi-thinking-replay", modelFamily, sessionKey}, "\x00")
}

func kimiThinkingReplayKVKey(modelFamily, sessionKey string) string {
	return "cpa:kimi:thinking-replay:" + homekv.HashKeyPart(strings.TrimSpace(modelFamily)) + ":" + homekv.HashKeyPart(strings.TrimSpace(sessionKey))
}

func validKimiThinkingReplayContent(content []byte) bool {
	if len(content) == 0 || len(content) > KimiThinkingReplayCacheMaxBytesPerEntry || !gjson.ValidBytes(content) {
		return false
	}
	root := gjson.ParseBytes(content)
	return root.IsArray() && len(root.Array()) > 0 && len(root.Array()) <= KimiThinkingReplayCacheMaxBlocksPerEntry
}

func purgeExpiredKimiThinkingReplayCache(now time.Time) {
	purgeExpiredFencedReplay(&kimiThinkingReplayMu, kimiThinkingReplayEntries, &kimiThinkingReplayTotalBytes, kimiThinkingReplayLimits(), kimiThinkingReplayContentSize, now)
}
