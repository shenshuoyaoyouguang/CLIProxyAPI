package cache

import (
	"bytes"
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
	// ClaudeThinkingReplayCacheTTL limits how long signed assistant turns stay replayable.
	ClaudeThinkingReplayCacheTTL = 1 * time.Hour

	// ClaudeThinkingReplayCacheMaxEntries bounds process memory used by Claude replay continuity.
	ClaudeThinkingReplayCacheMaxEntries = 10240

	// ClaudeThinkingReplayCacheEvictBatchSize leaves headroom after reaching capacity.
	ClaudeThinkingReplayCacheEvictBatchSize = 128

	// ClaudeThinkingReplayCacheMaxBytesPerSession bounds all cached assistant turns for one session.
	ClaudeThinkingReplayCacheMaxBytesPerSession = 8 << 20

	// ClaudeThinkingReplayCacheMaxTurnsPerSession bounds the number of assistant turns per session.
	ClaudeThinkingReplayCacheMaxTurnsPerSession = 64

	// ClaudeThinkingReplayCacheMaxBlocksPerTurn prevents pathological content arrays.
	ClaudeThinkingReplayCacheMaxBlocksPerTurn = 512

	// ClaudeThinkingReplayCacheMaxTotalBytes bounds aggregate in-process Claude replay content.
	ClaudeThinkingReplayCacheMaxTotalBytes = 256 << 20

	claudeThinkingReplayCacheMaxSerializedBytes = ClaudeThinkingReplayCacheMaxBytesPerSession + 1024
)

// ClaudeThinkingReplaySnapshot identifies the exact replay generation read for one request.
type ClaudeThinkingReplaySnapshot = KimiThinkingReplaySnapshot

type claudeThinkingReplayHomeValue struct {
	Generation string            `json:"generation"`
	Deleted    bool              `json:"deleted,omitempty"`
	Contents   []json.RawMessage `json:"contents,omitempty"`
}

var (
	claudeThinkingReplayMu         sync.Mutex
	claudeThinkingReplayEntries    = make(map[string]fencedReplayEntry[[][]byte])
	claudeThinkingReplayTotalBytes int
)

var currentClaudeThinkingReplayKVClient = func() (kimiThinkingReplayKVClient, bool, error) {
	return homekv.CurrentKVClient()
}

func claudeThinkingReplayLimits() fencedReplayLimits {
	return fencedReplayLimits{
		TTL:           ClaudeThinkingReplayCacheTTL,
		MaxEntries:    ClaudeThinkingReplayCacheMaxEntries,
		EvictBatch:    ClaudeThinkingReplayCacheEvictBatchSize,
		MaxTotalBytes: ClaudeThinkingReplayCacheMaxTotalBytes,
	}
}

// CacheClaudeThinkingReplayBestEffort stores one complete signed assistant content array.
func CacheClaudeThinkingReplayBestEffort(ctx context.Context, modelFamily, sessionKey string, content []byte) bool {
	key := claudeThinkingReplayCacheKey(modelFamily, sessionKey)
	if key == "" || !validClaudeThinkingReplayContent(content) {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	contents := [][]byte{append([]byte(nil), content...)}
	generation := uuid.NewString()
	if client, homeMode, errClient := currentClaudeThinkingReplayKVClient(); homeMode {
		if errClient != nil {
			log.Errorf("home kv best-effort Claude thinking replay set failed: %v", errClient)
			return false
		}
		raw, errMarshal := marshalClaudeThinkingReplayHomeValue(generation, false, contents)
		if errMarshal != nil {
			log.Errorf("home kv best-effort Claude thinking replay set failed: %v", errMarshal)
			return false
		}
		written, errSet := client.KVSet(ctx, claudeThinkingReplayKVKey(modelFamily, sessionKey), raw, homekv.KVSetOptions{EX: ClaudeThinkingReplayCacheTTL})
		if errSet != nil {
			log.Errorf("home kv best-effort Claude thinking replay set failed: %v", errSet)
			return false
		}
		return written
	}

	cacheCleanupOnce.Do(startCacheCleanup)
	storeFencedReplayLocal(&claudeThinkingReplayMu, claudeThinkingReplayEntries, &claudeThinkingReplayTotalBytes, claudeThinkingReplayLimits(), claudeThinkingReplayEntryBytes, key, contents, generation, false, time.Now())
	return true
}

// GetClaudeThinkingReplayRequired retrieves all cached assistant turns for request-time replay.
func GetClaudeThinkingReplayRequired(ctx context.Context, modelFamily, sessionKey string) ([][]byte, bool, error) {
	contents, _, found, errGet := GetClaudeThinkingReplayWithSnapshotRequired(ctx, modelFamily, sessionKey)
	return contents, found, errGet
}

// GetClaudeThinkingReplayWithSnapshotRequired retrieves replay content and the exact cache state read.
func GetClaudeThinkingReplayWithSnapshotRequired(ctx context.Context, modelFamily, sessionKey string) ([][]byte, ClaudeThinkingReplaySnapshot, bool, error) {
	key := claudeThinkingReplayCacheKey(modelFamily, sessionKey)
	if key == "" {
		return nil, ClaudeThinkingReplaySnapshot{}, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	client, homeMode, errClient := currentClaudeThinkingReplayKVClient()
	if homeMode {
		if errClient != nil {
			return nil, ClaudeThinkingReplaySnapshot{loaded: true}, false, errClient
		}
		kvKey := claudeThinkingReplayKVKey(modelFamily, sessionKey)
		tombstone, errMarshal := marshalClaudeThinkingReplayHomeValue(uuid.NewString(), true, nil)
		if errMarshal != nil {
			return nil, ClaudeThinkingReplaySnapshot{loaded: true}, false, errMarshal
		}
		raw, errRead := readOrReserveFencedReplayHome(ctx, client, kvKey, ClaudeThinkingReplayCacheTTL, claudeThinkingReplayCacheMaxSerializedBytes, "Claude", tombstone)
		if errRead != nil {
			return nil, ClaudeThinkingReplaySnapshot{loaded: true}, false, errRead
		}
		snapshot := ClaudeThinkingReplaySnapshot{raw: append([]byte(nil), raw...), loaded: true, found: true}
		contents, generation, deleted, okDecode := decodeClaudeThinkingReplayHomeValue(raw)
		if !okDecode {
			return nil, snapshot, false, fmt.Errorf("invalid Claude thinking replay content")
		}
		snapshot.generation = generation
		if _, errExpire := client.KVExpire(ctx, kvKey, ClaudeThinkingReplayCacheTTL); errExpire != nil {
			log.Warnf("home kv Claude thinking replay expire failed: %v", errExpire)
		}
		if deleted {
			return nil, snapshot, false, nil
		}
		return cloneClaudeThinkingReplayContents(contents), snapshot, len(contents) > 0, nil
	}

	cacheCleanupOnce.Do(startCacheCleanup)
	entry := lookupFencedReplayLocal(&claudeThinkingReplayMu, claudeThinkingReplayEntries, &claudeThinkingReplayTotalBytes, claudeThinkingReplayLimits(), claudeThinkingReplayEntryBytes, key, time.Now())
	snapshot := ClaudeThinkingReplaySnapshot{generation: entry.Generation, loaded: true, found: true}
	if entry.Deleted {
		return nil, snapshot, false, nil
	}
	return cloneClaudeThinkingReplayContents(entry.Value), snapshot, len(entry.Value) > 0, nil
}

// ReplaceClaudeThinkingReplayIfUnchanged appends a completed assistant turn only if the request snapshot is current.
func ReplaceClaudeThinkingReplayIfUnchanged(ctx context.Context, modelFamily, sessionKey string, snapshot ClaudeThinkingReplaySnapshot, content []byte) (bool, error) {
	key := claudeThinkingReplayCacheKey(modelFamily, sessionKey)
	if key == "" || !validClaudeThinkingReplayContent(content) {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !snapshot.loaded {
		return CacheClaudeThinkingReplayBestEffort(ctx, modelFamily, sessionKey, content), nil
	}
	client, homeMode, errClient := currentClaudeThinkingReplayKVClient()
	if homeMode {
		if errClient != nil {
			return false, errClient
		}
		contents, _, deleted, okDecode := decodeClaudeThinkingReplayHomeValue(snapshot.raw)
		if !okDecode {
			return false, fmt.Errorf("invalid Claude thinking replay snapshot")
		}
		if deleted {
			contents = nil
		}
		contents = appendClaudeThinkingReplayContent(contents, content)
		raw, errMarshal := marshalClaudeThinkingReplayHomeValue(uuid.NewString(), false, contents)
		if errMarshal != nil {
			return false, errMarshal
		}
		return client.KVCompareAndSwap(ctx, claudeThinkingReplayKVKey(modelFamily, sessionKey), snapshot.raw, snapshot.found, raw, ClaudeThinkingReplayCacheTTL)
	}

	claudeThinkingReplayMu.Lock()
	defer claudeThinkingReplayMu.Unlock()
	entry, current := fencedReplaySnapshotCurrentLocked(claudeThinkingReplayEntries, key, snapshot.found, snapshot.generation)
	if !current {
		return false, nil
	}
	contents := appendClaudeThinkingReplayContent(entry.Value, content)
	commitFencedReplayReplaceLocked(claudeThinkingReplayEntries, &claudeThinkingReplayTotalBytes, claudeThinkingReplayLimits(), claudeThinkingReplayEntryBytes, key, contents, time.Now())
	return true, nil
}

// DeleteClaudeThinkingReplayIfUnchanged clears replay state only if the request snapshot is current.
func DeleteClaudeThinkingReplayIfUnchanged(ctx context.Context, modelFamily, sessionKey string, snapshot ClaudeThinkingReplaySnapshot) (bool, error) {
	key := claudeThinkingReplayCacheKey(modelFamily, sessionKey)
	if key == "" {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !snapshot.loaded {
		return true, DeleteClaudeThinkingReplayRequired(ctx, modelFamily, sessionKey)
	}
	generation := uuid.NewString()
	client, homeMode, errClient := currentClaudeThinkingReplayKVClient()
	if homeMode {
		if errClient != nil {
			return false, errClient
		}
		tombstone, errMarshal := marshalClaudeThinkingReplayHomeValue(generation, true, nil)
		if errMarshal != nil {
			return false, errMarshal
		}
		return client.KVCompareAndSwap(ctx, claudeThinkingReplayKVKey(modelFamily, sessionKey), snapshot.raw, snapshot.found, tombstone, ClaudeThinkingReplayCacheTTL)
	}

	return tombstoneFencedReplayIfUnchanged(&claudeThinkingReplayMu, claudeThinkingReplayEntries, &claudeThinkingReplayTotalBytes, claudeThinkingReplayEntryBytes, key, snapshot.found, snapshot.generation, time.Now()), nil
}

// DeleteClaudeThinkingReplayRequired removes stale replay state unconditionally.
func DeleteClaudeThinkingReplayRequired(ctx context.Context, modelFamily, sessionKey string) error {
	key := claudeThinkingReplayCacheKey(modelFamily, sessionKey)
	if key == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	client, homeMode, errClient := currentClaudeThinkingReplayKVClient()
	if homeMode {
		if errClient != nil {
			return errClient
		}
		_, errDelete := client.KVDel(ctx, claudeThinkingReplayKVKey(modelFamily, sessionKey))
		return errDelete
	}
	removeFencedReplayLocal(&claudeThinkingReplayMu, claudeThinkingReplayEntries, &claudeThinkingReplayTotalBytes, claudeThinkingReplayEntryBytes, key)
	return nil
}

// ClearClaudeThinkingReplayCache clears only Claude replay state.
func ClearClaudeThinkingReplayCache() {
	clearFencedReplayLocal(&claudeThinkingReplayMu, claudeThinkingReplayEntries, &claudeThinkingReplayTotalBytes)
}

func marshalClaudeThinkingReplayHomeValue(generation string, deleted bool, contents [][]byte) ([]byte, error) {
	value := claudeThinkingReplayHomeValue{Generation: generation, Deleted: deleted}
	if !deleted {
		value.Contents = make([]json.RawMessage, 0, len(contents))
		for _, content := range contents {
			value.Contents = append(value.Contents, json.RawMessage(append([]byte(nil), content...)))
		}
	}
	return json.Marshal(value)
}

func decodeClaudeThinkingReplayHomeValue(raw []byte) ([][]byte, string, bool, bool) {
	if len(raw) == 0 || len(raw) > claudeThinkingReplayCacheMaxSerializedBytes || !gjson.ValidBytes(raw) {
		return nil, "", false, false
	}
	var value claudeThinkingReplayHomeValue
	if errUnmarshal := json.Unmarshal(raw, &value); errUnmarshal != nil || strings.TrimSpace(value.Generation) == "" {
		return nil, "", false, false
	}
	if value.Deleted {
		return nil, value.Generation, true, true
	}
	contents := make([][]byte, 0, len(value.Contents))
	for _, content := range value.Contents {
		if !validClaudeThinkingReplayContent(content) {
			return nil, "", false, false
		}
		contents = append(contents, append([]byte(nil), content...))
	}
	if len(contents) == 0 {
		return nil, "", false, false
	}
	return contents, value.Generation, false, true
}

func appendClaudeThinkingReplayContent(contents [][]byte, content []byte) [][]byte {
	cloned := cloneClaudeThinkingReplayContents(contents)
	for _, existing := range cloned {
		if claudeThinkingReplayJSONEqual(existing, content) {
			return cloned
		}
	}
	cloned = append(cloned, append([]byte(nil), content...))
	for len(cloned) > ClaudeThinkingReplayCacheMaxTurnsPerSession || claudeThinkingReplayEntryBytes(cloned) > ClaudeThinkingReplayCacheMaxBytesPerSession {
		if len(cloned) == 0 {
			break
		}
		cloned = cloned[1:]
	}
	return cloned
}

func cloneClaudeThinkingReplayContents(contents [][]byte) [][]byte {
	cloned := make([][]byte, 0, len(contents))
	for _, content := range contents {
		cloned = append(cloned, append([]byte(nil), content...))
	}
	return cloned
}

func claudeThinkingReplayEntryBytes(contents [][]byte) int {
	total := 0
	for _, content := range contents {
		total += len(content)
	}
	return total
}

func claudeThinkingReplayCacheKey(modelFamily, sessionKey string) string {
	modelFamily = strings.TrimSpace(modelFamily)
	sessionKey = strings.TrimSpace(sessionKey)
	if modelFamily == "" || sessionKey == "" {
		return ""
	}
	return strings.Join([]string{"claude-thinking-replay", modelFamily, sessionKey}, "\x00")
}

func claudeThinkingReplayKVKey(modelFamily, sessionKey string) string {
	return "cpa:claude:thinking-replay:" + homekv.HashKeyPart(strings.TrimSpace(modelFamily)) + ":" + homekv.HashKeyPart(strings.TrimSpace(sessionKey))
}

func validClaudeThinkingReplayContent(content []byte) bool {
	if len(content) == 0 || len(content) > ClaudeThinkingReplayCacheMaxBytesPerSession || !gjson.ValidBytes(content) {
		return false
	}
	root := gjson.ParseBytes(content)
	return root.IsArray() && len(root.Array()) > 0 && len(root.Array()) <= ClaudeThinkingReplayCacheMaxBlocksPerTurn
}

func claudeThinkingReplayJSONEqual(left, right []byte) bool {
	leftCanonical, leftOK := claudeThinkingReplayCanonicalJSON(left)
	rightCanonical, rightOK := claudeThinkingReplayCanonicalJSON(right)
	return leftOK && rightOK && bytes.Equal(leftCanonical, rightCanonical)
}

func claudeThinkingReplayCanonicalJSON(raw []byte) ([]byte, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if errDecode := decoder.Decode(&value); errDecode != nil {
		return nil, false
	}
	canonical, errMarshal := json.Marshal(value)
	return canonical, errMarshal == nil
}

func purgeExpiredClaudeThinkingReplayCache(now time.Time) {
	purgeExpiredFencedReplay(&claudeThinkingReplayMu, claudeThinkingReplayEntries, &claudeThinkingReplayTotalBytes, claudeThinkingReplayLimits(), claudeThinkingReplayEntryBytes, now)
}
