package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	homekv "github.com/router-for-me/CLIProxyAPI/v7/internal/home"
	log "github.com/sirupsen/logrus"
)

// One engine module housing both reasoning-replay cache families behind a
// deduplicated core:
//
//   - the best-effort item-list caches (Codex, xAI): last-write-wins
//     home-KV plumbing with TTL expiry and oldest-first batch eviction;
//   - the generation-fenced caches (kimi, Claude): CAS tombstone
//     reservations, snapshot fencing, and aggregate byte accounting.
//
// The two protocols are deliberately NOT unified further: fencing an
// unfenced cache would change concurrency semantics, and parameterizing the
// fence policy would collapse into a callback framework. What they genuinely
// share — mutex-guarded maps, timestamp-based eviction/purge, TTL refresh —
// lives in the shared core below. Provider packages keep their own state
// variables (tests reach into them), their wire formats, and their
// provider-specific merge semantics.

// Shared core

// replayStamper lets the shared eviction and purge cores read an entry's
// last-used timestamp without knowing the entry layout.
type replayStamper interface {
	stamp() time.Time
}

// evictOldestReplayEntriesLocked removes the count oldest entries by
// timestamp, invoking onEvict for each removed entry when non-nil (used for
// byte accounting); the caller holds the cache mutex.
func evictOldestReplayEntriesLocked[V replayStamper](entries map[string]V, count int, onEvict func(V)) {
	if count <= 0 || len(entries) == 0 {
		return
	}
	type candidate struct {
		key       string
		timestamp time.Time
	}
	candidates := make([]candidate, 0, len(entries))
	for key, entry := range entries {
		candidates = append(candidates, candidate{key: key, timestamp: entry.stamp()})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].timestamp.Before(candidates[j].timestamp)
	})
	if count > len(candidates) {
		count = len(candidates)
	}
	for i := 0; i < count; i++ {
		if onEvict != nil {
			onEvict(entries[candidates[i].key])
		}
		delete(entries, candidates[i].key)
	}
}

// purgeExpiredReplayEntries drops every entry past its TTL under one lock
// hold, invoking onEvict for each dropped entry when non-nil.
func purgeExpiredReplayEntries[V replayStamper](mu *sync.Mutex, entries map[string]V, ttl time.Duration, now time.Time, onEvict func(V)) {
	mu.Lock()
	for key, entry := range entries {
		if now.Sub(entry.stamp()) > ttl {
			if onEvict != nil {
				onEvict(entry)
			}
			delete(entries, key)
		}
	}
	mu.Unlock()
}

// Item-list engine (Codex, xAI)

// itemReplayEntry is one session's cached item list.
type itemReplayEntry struct {
	Items     [][]byte
	Timestamp time.Time
}

func (e itemReplayEntry) stamp() time.Time { return e.Timestamp }

// itemReplayKVClient is the KV subset the item-list replay caches need.
type itemReplayKVClient interface {
	KVGet(ctx context.Context, key string) ([]byte, bool, error)
	KVSet(ctx context.Context, key string, value []byte, opts homekv.KVSetOptions) (bool, error)
	KVDel(ctx context.Context, keys ...string) (int64, error)
	KVExpire(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

// cloneItemReplayItems deep-copies an item list.
func cloneItemReplayItems(items [][]byte) [][]byte {
	cloned := make([][]byte, 0, len(items))
	for _, item := range items {
		cloned = append(cloned, append([]byte(nil), item...))
	}
	return cloned
}

// storeItemReplayLocal overwrites key with items and evicts the oldest
// entries when the cache exceeds maxEntries.
func storeItemReplayLocal(mu *sync.Mutex, entries map[string]itemReplayEntry, maxEntries, evictBatch int, key string, items [][]byte, now time.Time) {
	mu.Lock()
	defer mu.Unlock()
	entries[key] = itemReplayEntry{Items: items, Timestamp: now}
	if len(entries) > maxEntries {
		evictOldestReplayEntriesLocked(entries, evictBatch, nil)
	}
}

// lookupItemReplayLocal returns a cloned item list, dropping entries past
// their TTL and refreshing the timestamp of live ones.
func lookupItemReplayLocal(mu *sync.Mutex, entries map[string]itemReplayEntry, ttl time.Duration, key string, now time.Time) ([][]byte, bool) {
	mu.Lock()
	defer mu.Unlock()
	entry, ok := entries[key]
	if !ok {
		return nil, false
	}
	if now.Sub(entry.Timestamp) > ttl {
		delete(entries, key)
		return nil, false
	}
	entry.Timestamp = now
	entries[key] = entry
	return cloneItemReplayItems(entry.Items), true
}

// updateItemReplayLocal applies merge to the stored items under one lock
// hold, mirroring the original read-modify-write append: expired entries are
// reset before merging, and the timestamp refresh plus eviction happen in the
// same critical section.
func updateItemReplayLocal(mu *sync.Mutex, entries map[string]itemReplayEntry, ttl time.Duration, maxEntries, evictBatch int, key string, merge func(existing [][]byte) [][]byte, now time.Time) {
	mu.Lock()
	defer mu.Unlock()
	entry := entries[key]
	if now.Sub(entry.Timestamp) > ttl {
		entry.Items = nil
	}
	entry.Items = merge(entry.Items)
	entry.Timestamp = now
	entries[key] = entry
	if len(entries) > maxEntries {
		evictOldestReplayEntriesLocked(entries, evictBatch, nil)
	}
}

// removeItemReplayLocal deletes one key from the local cache.
func removeItemReplayLocal(mu *sync.Mutex, entries map[string]itemReplayEntry, key string) {
	mu.Lock()
	delete(entries, key)
	mu.Unlock()
}

// purgeExpiredItemReplay drops every item-list entry past its TTL.
func purgeExpiredItemReplay(mu *sync.Mutex, entries map[string]itemReplayEntry, ttl time.Duration, now time.Time) {
	purgeExpiredReplayEntries(mu, entries, ttl, now, nil)
}

// itemReplayHomeOptions carries the provider-specific bits of the home-KV
// flows: the hashed key, the log wording, and whether a failed TTL refresh
// aborts the read (Codex) or only warns (xAI).
type itemReplayHomeOptions struct {
	KVKey          string
	TTL            time.Duration
	Label          string // provider word in warning text, e.g. "xai"
	LogPrefix      string // e.g. "cpa:xai:*"
	ExpireErrFatal bool
}

// putItemReplayHome marshals items and writes them as a bare JSON array.
func putItemReplayHome(ctx context.Context, client itemReplayKVClient, opts itemReplayHomeOptions, items [][]byte) (bool, error) {
	raw, errMarshal := json.Marshal(items)
	if errMarshal != nil {
		return false, errMarshal
	}
	return client.KVSet(ctx, opts.KVKey, raw, homekv.KVSetOptions{EX: opts.TTL})
}

// getItemReplayHome reads and decodes the home value, then refreshes its TTL.
func getItemReplayHome(ctx context.Context, client itemReplayKVClient, opts itemReplayHomeOptions) ([][]byte, bool, error) {
	raw, found, errGet := client.KVGet(ctx, opts.KVKey)
	if errGet != nil || !found {
		return nil, false, errGet
	}
	var homeItems [][]byte
	if errUnmarshal := json.Unmarshal(raw, &homeItems); errUnmarshal != nil {
		return nil, false, errUnmarshal
	}
	if _, errExpire := client.KVExpire(ctx, opts.KVKey, opts.TTL); errExpire != nil {
		if opts.ExpireErrFatal {
			return nil, false, errExpire
		}
		log.Warnf("home kv %s reasoning replay expire failed prefix=%s: %v", opts.Label, opts.LogPrefix, errExpire)
	}
	return cloneItemReplayItems(homeItems), true, nil
}

// Generation-fenced engine (kimi, Claude)

// fencedReplayLimits configures one family of generation-fenced caches.
type fencedReplayLimits struct {
	TTL           time.Duration
	MaxEntries    int
	EvictBatch    int
	MaxTotalBytes int
}

// fencedReplayEntry is one session's fenced replay state.
type fencedReplayEntry[V any] struct {
	Value      V
	Timestamp  time.Time
	Generation string
	Deleted    bool
}

func (e fencedReplayEntry[V]) stamp() time.Time { return e.Timestamp }

// reserveFencedReplayLocalLocked plants a local tombstone reservation for an
// absent or expired key; the caller holds mu.
func reserveFencedReplayLocalLocked[V any](entries map[string]fencedReplayEntry[V], totalBytes *int, limits fencedReplayLimits, sizeOf func(V) int, key string, now time.Time) fencedReplayEntry[V] {
	entry := fencedReplayEntry[V]{Timestamp: now, Generation: uuid.NewString(), Deleted: true}
	entries[key] = entry
	enforceFencedReplayLimitsLocked(entries, totalBytes, limits, sizeOf)
	return entry
}

// storeFencedReplayLocal overwrites key with accounted bytes and enforces limits.
func storeFencedReplayLocal[V any](mu *sync.Mutex, entries map[string]fencedReplayEntry[V], totalBytes *int, limits fencedReplayLimits, sizeOf func(V) int, key string, value V, generation string, deleted bool, now time.Time) {
	mu.Lock()
	defer mu.Unlock()
	if previous, found := entries[key]; found {
		*totalBytes -= sizeOf(previous.Value)
	}
	*totalBytes += sizeOf(value)
	entries[key] = fencedReplayEntry[V]{Value: value, Timestamp: now, Generation: generation, Deleted: deleted}
	enforceFencedReplayLimitsLocked(entries, totalBytes, limits, sizeOf)
}

// lookupFencedReplayLocal returns the current entry for key. Absent or
// expired keys get a fresh local tombstone reservation; live entries get
// their timestamp refreshed.
func lookupFencedReplayLocal[V any](mu *sync.Mutex, entries map[string]fencedReplayEntry[V], totalBytes *int, limits fencedReplayLimits, sizeOf func(V) int, key string, now time.Time) fencedReplayEntry[V] {
	mu.Lock()
	defer mu.Unlock()
	entry, ok := entries[key]
	if !ok || now.Sub(entry.Timestamp) > limits.TTL {
		if ok {
			*totalBytes -= sizeOf(entry.Value)
			delete(entries, key)
		}
		return reserveFencedReplayLocalLocked(entries, totalBytes, limits, sizeOf, key, now)
	}
	entry.Timestamp = now
	entries[key] = entry
	return entry
}

// fencedReplaySnapshotCurrentLocked reports whether the stored state still
// matches the request snapshot; the caller holds mu.
func fencedReplaySnapshotCurrentLocked[V any](entries map[string]fencedReplayEntry[V], key string, found bool, generation string) (fencedReplayEntry[V], bool) {
	entry, entryFound := entries[key]
	if entryFound != found || (entryFound && entry.Generation != generation) {
		return entry, false
	}
	return entry, true
}

// commitFencedReplayReplaceLocked stores value under a fresh generation with
// byte accounting and limit enforcement; the caller holds mu and has already
// verified the snapshot is current.
func commitFencedReplayReplaceLocked[V any](entries map[string]fencedReplayEntry[V], totalBytes *int, limits fencedReplayLimits, sizeOf func(V) int, key string, value V, now time.Time) string {
	generation := uuid.NewString()
	*totalBytes -= sizeOf(entries[key].Value)
	*totalBytes += sizeOf(value)
	entries[key] = fencedReplayEntry[V]{Value: value, Timestamp: now, Generation: generation}
	enforceFencedReplayLimitsLocked(entries, totalBytes, limits, sizeOf)
	return generation
}

// tombstoneFencedReplayIfUnchanged replaces key with a tombstone when the
// snapshot still matches; no limit enforcement, matching both originals.
func tombstoneFencedReplayIfUnchanged[V any](mu *sync.Mutex, entries map[string]fencedReplayEntry[V], totalBytes *int, sizeOf func(V) int, key string, found bool, generation string, now time.Time) bool {
	mu.Lock()
	defer mu.Unlock()
	entry, current := fencedReplaySnapshotCurrentLocked(entries, key, found, generation)
	if !current {
		return false
	}
	*totalBytes -= sizeOf(entry.Value)
	entries[key] = fencedReplayEntry[V]{Timestamp: now, Generation: uuid.NewString(), Deleted: true}
	return true
}

// enforceFencedReplayLimitsLocked evicts oldest entries until count and
// total-bytes caps hold; the caller holds mu.
func enforceFencedReplayLimitsLocked[V any](entries map[string]fencedReplayEntry[V], totalBytes *int, limits fencedReplayLimits, sizeOf func(V) int) {
	for len(entries) > limits.MaxEntries || *totalBytes > limits.MaxTotalBytes {
		if len(entries) == 0 {
			*totalBytes = 0
			return
		}
		evictOldestReplayEntriesLocked(entries, limits.EvictBatch, func(entry fencedReplayEntry[V]) {
			*totalBytes -= sizeOf(entry.Value)
		})
	}
}

// purgeExpiredFencedReplay drops fenced entries past their TTL with byte
// accounting.
func purgeExpiredFencedReplay[V any](mu *sync.Mutex, entries map[string]fencedReplayEntry[V], totalBytes *int, limits fencedReplayLimits, sizeOf func(V) int, now time.Time) {
	purgeExpiredReplayEntries(mu, entries, limits.TTL, now, func(entry fencedReplayEntry[V]) {
		*totalBytes -= sizeOf(entry.Value)
	})
}

// removeFencedReplayLocal deletes one key unconditionally with accounting.
func removeFencedReplayLocal[V any](mu *sync.Mutex, entries map[string]fencedReplayEntry[V], totalBytes *int, sizeOf func(V) int, key string) {
	mu.Lock()
	if entry, found := entries[key]; found {
		*totalBytes -= sizeOf(entry.Value)
		delete(entries, key)
	}
	mu.Unlock()
}

// clearFencedReplayLocal resets all in-process state for one cache.
func clearFencedReplayLocal[V any](mu *sync.Mutex, entries map[string]fencedReplayEntry[V], totalBytes *int) {
	mu.Lock()
	for key := range entries {
		delete(entries, key)
	}
	*totalBytes = 0
	mu.Unlock()
}

// readOrReserveFencedReplayHome returns the current home value, planting a
// CAS tombstone reservation when the key is absent (up to four attempts).
// label appears verbatim in error messages; tombstone must be pre-marshaled
// by the caller in the provider's wire format.
func readOrReserveFencedReplayHome(ctx context.Context, client kimiThinkingReplayKVClient, kvKey string, ttl time.Duration, maxSerializedBytes int, label string, tombstone []byte) ([]byte, error) {
	for attempt := 0; attempt < 4; attempt++ {
		raw, found, errGet := client.KVGet(ctx, kvKey)
		if errGet != nil {
			return nil, errGet
		}
		if found {
			if len(raw) > maxSerializedBytes {
				return nil, fmt.Errorf("%s thinking replay value exceeds size limit", label)
			}
			return raw, nil
		}
		swapped, errReserve := client.KVCompareAndSwap(ctx, kvKey, nil, false, tombstone, ttl)
		if errReserve != nil {
			return nil, errReserve
		}
		if swapped {
			return tombstone, nil
		}
	}
	return nil, fmt.Errorf("could not reserve absent %s thinking replay state", label)
}
