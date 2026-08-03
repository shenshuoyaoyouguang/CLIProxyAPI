package cache

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	homekv "github.com/router-for-me/CLIProxyAPI/v7/internal/home"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// reasoningReplayEntry is the in-memory shape of a reasoning replay cache slot.
type reasoningReplayEntry struct {
	Items     [][]byte
	Timestamp time.Time
}

// reasoningReplayKVClient is the subset of the home KV client used by
// reasoning replay caches. It includes KVCompareAndSwap for the append path.
type reasoningReplayKVClient interface {
	KVGet(ctx context.Context, key string) ([]byte, bool, error)
	KVSet(ctx context.Context, key string, value []byte, opts homekv.KVSetOptions) (bool, error)
	KVCompareAndSwap(ctx context.Context, key string, expected []byte, expectedExists bool, value []byte, ttl time.Duration) (bool, error)
	KVDel(ctx context.Context, keys ...string) (int64, error)
	KVExpire(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

// reasoningReplayStoreStatus reports why a cache write succeeded or failed so
// callers can decide whether to keep prior entries.
type reasoningReplayStoreStatus int

const (
	// reasoningReplayStoreInvalidArgs means model/session were empty.
	reasoningReplayStoreInvalidArgs reasoningReplayStoreStatus = iota
	// reasoningReplayStoreStored means a valid reasoning batch was written.
	reasoningReplayStoreStored
	// reasoningReplayStoreNoReplayableState means the completed output had no
	// cacheable reasoning batch (for example reasoning disabled).
	reasoningReplayStoreNoReplayableState
	// reasoningReplayStoreBackendError means normalize succeeded but the
	// storage backend failed.
	reasoningReplayStoreBackendError
)

// reasoningReplayStoreConfig carries the per-provider knobs for a
// reasoningReplayStore. Behavior that differs between providers (normalize,
// appendTurn) is injected as callbacks; key prefixes, TTL and limits are
// plain fields.
type reasoningReplayStoreConfig struct {
	memoryKeyPrefix    string
	kvKeyPrefix        string
	ttl                time.Duration
	maxEntries         int
	evictBatchSize     int
	logLabel           string
	expireFailureFatal bool
	normalize          func(items [][]byte) ([][]byte, bool)
	appendTurn         func(existing, turn [][]byte) [][]byte
	kvClient           func() (reasoningReplayKVClient, bool, error)
}

// reasoningReplayStore is the shared home-KV/in-memory cache backend used by
// the Codex and XAI reasoning replay caches. All methods are safe for
// concurrent use.
type reasoningReplayStore struct {
	memoryKeyPrefix    string
	kvKeyPrefix        string
	ttl                time.Duration
	maxEntries         int
	evictBatchSize     int
	logLabel           string
	expireFailureFatal bool
	normalize          func(items [][]byte) ([][]byte, bool)
	appendTurn         func(existing, turn [][]byte) [][]byte
	kvClient           func() (reasoningReplayKVClient, bool, error)

	mu      sync.Mutex
	entries map[string]reasoningReplayEntry
}

func newReasoningReplayStore(config reasoningReplayStoreConfig) *reasoningReplayStore {
	return &reasoningReplayStore{
		memoryKeyPrefix:    config.memoryKeyPrefix,
		kvKeyPrefix:        config.kvKeyPrefix,
		ttl:                config.ttl,
		maxEntries:         config.maxEntries,
		evictBatchSize:     config.evictBatchSize,
		logLabel:           config.logLabel,
		expireFailureFatal: config.expireFailureFatal,
		normalize:          config.normalize,
		appendTurn:         config.appendTurn,
		kvClient:           config.kvClient,
		entries:            make(map[string]reasoningReplayEntry),
	}
}

// set stores a normalized reasoning batch, replacing the whole entry.
func (s *reasoningReplayStore) set(ctx context.Context, modelName, sessionKey string, items [][]byte) reasoningReplayStoreStatus {
	key := reasoningReplayCacheKey(s.memoryKeyPrefix, modelName, sessionKey)
	if key == "" {
		return reasoningReplayStoreInvalidArgs
	}
	normalized, ok := s.normalize(items)
	if !ok {
		return reasoningReplayStoreNoReplayableState
	}
	if client, homeMode, errClient := s.kvClient(); homeMode {
		if errClient != nil {
			log.Errorf("home kv best-effort %s reasoning replay set failed prefix=cpa:%s:*: %v", s.logLabel, s.logLabel, errClient)
			return reasoningReplayStoreBackendError
		}
		raw, errMarshal := json.Marshal(normalized)
		if errMarshal != nil {
			log.Errorf("home kv best-effort %s reasoning replay set failed prefix=cpa:%s:*: %v", s.logLabel, s.logLabel, errMarshal)
			return reasoningReplayStoreBackendError
		}
		written, errSet := client.KVSet(ctx, reasoningReplayKVKey(s.kvKeyPrefix, modelName, sessionKey), raw, homekv.KVSetOptions{EX: s.ttl})
		if errSet != nil {
			log.Errorf("home kv best-effort %s reasoning replay set failed prefix=cpa:%s:*: %v", s.logLabel, s.logLabel, errSet)
			return reasoningReplayStoreBackendError
		}
		if !written {
			return reasoningReplayStoreBackendError
		}
		return reasoningReplayStoreStored
	}

	cacheCleanupOnce.Do(startCacheCleanup)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = reasoningReplayEntry{
		Items:     normalized,
		Timestamp: now,
	}
	if len(s.entries) > s.maxEntries {
		s.evictOldestLocked(s.evictBatchSize)
	}
	return reasoningReplayStoreStored
}

// append appends one completed turn to existing replay state. It requires the
// config appendTurn callback; stores without one report failure.
func (s *reasoningReplayStore) append(ctx context.Context, modelName, sessionKey string, items [][]byte) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	key := reasoningReplayCacheKey(s.memoryKeyPrefix, modelName, sessionKey)
	if key == "" {
		return false
	}
	normalized, ok := s.normalize(items)
	if !ok {
		return false
	}
	if s.appendTurn == nil {
		return false
	}
	if client, homeMode, errClient := s.kvClient(); homeMode {
		if errClient != nil {
			log.Errorf("home kv best-effort %s reasoning replay append failed prefix=cpa:%s:*: %v", s.logLabel, s.logLabel, errClient)
			return false
		}
		kvKey := reasoningReplayKVKey(s.kvKeyPrefix, modelName, sessionKey)
		const maxCASAttempts = 32
		for attempt := 0; attempt < maxCASAttempts; attempt++ {
			if errContext := ctx.Err(); errContext != nil {
				return false
			}
			existingRaw, found, errGet := client.KVGet(ctx, kvKey)
			if errGet != nil {
				log.Errorf("home kv best-effort %s reasoning replay append failed prefix=cpa:%s:*: %v", s.logLabel, s.logLabel, errGet)
				return false
			}
			var existing [][]byte
			if found {
				if errUnmarshal := json.Unmarshal(existingRaw, &existing); errUnmarshal != nil {
					log.Errorf("home kv best-effort %s reasoning replay append failed prefix=cpa:%s:*: %v", s.logLabel, s.logLabel, errUnmarshal)
					return false
				}
			}
			combined := s.appendTurn(existing, normalized)
			raw, errMarshal := json.Marshal(combined)
			if errMarshal != nil {
				log.Errorf("home kv best-effort %s reasoning replay append failed prefix=cpa:%s:*: %v", s.logLabel, s.logLabel, errMarshal)
				return false
			}
			written, errCAS := client.KVCompareAndSwap(ctx, kvKey, existingRaw, found, raw, s.ttl)
			if errCAS != nil {
				log.Errorf("home kv best-effort %s reasoning replay append failed prefix=cpa:%s:*: %v", s.logLabel, s.logLabel, errCAS)
				return false
			}
			if written {
				return true
			}
		}
		log.Warn("home kv best-effort " + s.logLabel + " reasoning replay append exhausted compare-and-swap attempts")
		return false
	}

	cacheCleanupOnce.Do(startCacheCleanup)
	now := time.Now()
	s.mu.Lock()
	entry := s.entries[key]
	if now.Sub(entry.Timestamp) > s.ttl {
		entry.Items = nil
	}
	entry.Items = s.appendTurn(entry.Items, normalized)
	entry.Timestamp = now
	s.entries[key] = entry
	if len(s.entries) > s.maxEntries {
		s.evictOldestLocked(s.evictBatchSize)
	}
	s.mu.Unlock()
	return true
}

// get retrieves replay items for request-time paths.
func (s *reasoningReplayStore) get(ctx context.Context, modelName, sessionKey string) ([][]byte, bool, error) {
	key := reasoningReplayCacheKey(s.memoryKeyPrefix, modelName, sessionKey)
	if key == "" {
		return nil, false, nil
	}
	client, homeMode, errClient := s.kvClient()
	if homeMode {
		if errClient != nil {
			return nil, false, errClient
		}
		kvKey := reasoningReplayKVKey(s.kvKeyPrefix, modelName, sessionKey)
		raw, found, errGet := client.KVGet(ctx, kvKey)
		if errGet != nil || !found {
			return nil, false, errGet
		}
		var homeItems [][]byte
		if errUnmarshal := json.Unmarshal(raw, &homeItems); errUnmarshal != nil {
			return nil, false, errUnmarshal
		}
		if _, errExpire := client.KVExpire(ctx, kvKey, s.ttl); errExpire != nil {
			if s.expireFailureFatal {
				return nil, false, errExpire
			}
			log.Warnf("home kv %s reasoning replay expire failed prefix=cpa:%s:*: %v", s.logLabel, s.logLabel, errExpire)
		}
		return cloneReasoningReplayItems(homeItems), true, nil
	}

	cacheCleanupOnce.Do(startCacheCleanup)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if !ok {
		return nil, false, nil
	}
	if now.Sub(entry.Timestamp) > s.ttl {
		delete(s.entries, key)
		return nil, false, nil
	}
	entry.Timestamp = now
	s.entries[key] = entry
	return cloneReasoningReplayItems(entry.Items), true, nil
}

// delete removes one replay item after upstream rejects it or the caller
// otherwise knows it is stale.
func (s *reasoningReplayStore) delete(ctx context.Context, modelName, sessionKey string) error {
	key := reasoningReplayCacheKey(s.memoryKeyPrefix, modelName, sessionKey)
	if key == "" {
		return nil
	}
	client, homeMode, errClient := s.kvClient()
	if homeMode {
		if errClient != nil {
			return errClient
		}
		_, errDel := client.KVDel(ctx, reasoningReplayKVKey(s.kvKeyPrefix, modelName, sessionKey))
		return errDel
	}
	s.mu.Lock()
	delete(s.entries, key)
	s.mu.Unlock()
	return nil
}

// clear drops all in-memory replay state for this store.
func (s *reasoningReplayStore) clear() {
	s.mu.Lock()
	s.entries = make(map[string]reasoningReplayEntry)
	s.mu.Unlock()
}

// purgeExpired removes in-memory entries whose TTL has elapsed. Called by the
// shared background cleanup goroutine.
func (s *reasoningReplayStore) purgeExpired(now time.Time) {
	s.mu.Lock()
	for key, entry := range s.entries {
		if now.Sub(entry.Timestamp) > s.ttl {
			delete(s.entries, key)
		}
	}
	s.mu.Unlock()
}

// evictOldestLocked deletes the oldest entries until count have been removed.
// Caller must hold s.mu.
func (s *reasoningReplayStore) evictOldestLocked(count int) {
	if count <= 0 || len(s.entries) == 0 {
		return
	}
	type candidate struct {
		key       string
		timestamp time.Time
	}
	candidates := make([]candidate, 0, len(s.entries))
	for key, entry := range s.entries {
		candidates = append(candidates, candidate{key: key, timestamp: entry.Timestamp})
	}
	// Entries written within the same clock tick share a timestamp, so break
	// ties on the key. Otherwise randomized map iteration plus an unstable sort
	// makes the eviction victim nondeterministic.
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].timestamp.Equal(candidates[j].timestamp) {
			return candidates[i].timestamp.Before(candidates[j].timestamp)
		}
		return candidates[i].key < candidates[j].key
	})
	if count > len(candidates) {
		count = len(candidates)
	}
	for i := 0; i < count; i++ {
		delete(s.entries, candidates[i].key)
	}
}

func reasoningReplayCacheKey(prefix, modelName, sessionKey string) string {
	modelName = strings.TrimSpace(modelName)
	sessionKey = strings.TrimSpace(sessionKey)
	if modelName == "" || sessionKey == "" {
		return ""
	}
	// The session key is the continuity boundary. Keep this independent from
	// the selected upstream credential so auth failover can preserve replay.
	// Length-prefix the session key so distinct (model, session) tuples cannot
	// collide when the session key contains a \x00 (JSON allows \u0000).
	// Model names come from the registry (no \x00) so they do not need the
	// same treatment.
	return prefix + modelName + "\x00" + strconv.Itoa(len(sessionKey)) + ":" + sessionKey
}

func reasoningReplayKVKey(kvPrefix, modelName, sessionKey string) string {
	return kvPrefix + homekv.HashKeyPart(strings.TrimSpace(modelName)) + ":" + homekv.HashKeyPart(strings.TrimSpace(sessionKey))
}

func cloneReasoningReplayItems(items [][]byte) [][]byte {
	cloned := make([][]byte, 0, len(items))
	for _, item := range items {
		cloned = append(cloned, append([]byte(nil), item...))
	}
	return cloned
}

// trimReasoningReplayItems drops oldest whole turns until the remaining items
// respect the per-entry turn and byte budgets.
func trimReasoningReplayItems(items [][]byte, turnType string, maxTurns, maxBytes int) [][]byte {
	// Each pass strips at least one item, so the trim is bounded by the input
	// length. Capture the initial length: the bound must not shrink with items,
	// or byte-heavy trims would exit early and drop the whole entry.
	bound := len(items)
	for pass := 0; pass < bound; pass++ {
		turnStarts := []int{0}
		totalBytes := 0
		for index, item := range items {
			totalBytes += len(item)
			if index > 0 && strings.TrimSpace(gjson.GetBytes(item, "type").String()) == turnType {
				turnStarts = append(turnStarts, index)
			}
		}
		if len(turnStarts) <= maxTurns && totalBytes <= maxBytes {
			return items
		}
		if len(turnStarts) <= 1 {
			return nil
		}
		items = items[turnStarts[1]:]
	}
	// Unreachable: each pass removes at least one item, so the input hits the
	// len(turnStarts) <= 1 guard before the bound is exhausted.
	return nil
}
