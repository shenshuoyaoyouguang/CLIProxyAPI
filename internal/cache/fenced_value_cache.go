package cache

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Shared implementation for the generation-fenced thinking replay caches
// (kimi + claude): a mutex-guarded entry map with per-entry generation
// strings, tombstone reservations, aggregate byte accounting, and
// read-or-reserve home-KV fencing. Provider packages keep their own state
// variables (tests reach into them), their wire-format encode/decode, and
// their provider-specific merge semantics; the fencing machinery lives here.

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
		evictOldestFencedReplayEntriesLocked(entries, totalBytes, sizeOf, limits.EvictBatch)
	}
}

// evictOldestFencedReplayEntriesLocked removes the count oldest entries by
// timestamp; the caller holds mu.
func evictOldestFencedReplayEntriesLocked[V any](entries map[string]fencedReplayEntry[V], totalBytes *int, sizeOf func(V) int, count int) {
	if count <= 0 || len(entries) == 0 {
		return
	}
	type candidate struct {
		key       string
		timestamp time.Time
	}
	candidates := make([]candidate, 0, len(entries))
	for key, entry := range entries {
		candidates = append(candidates, candidate{key: key, timestamp: entry.Timestamp})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].timestamp.Before(candidates[j].timestamp)
	})
	if count > len(candidates) {
		count = len(candidates)
	}
	for i := 0; i < count; i++ {
		entry := entries[candidates[i].key]
		*totalBytes -= sizeOf(entry.Value)
		delete(entries, candidates[i].key)
	}
}

// purgeExpiredFencedReplay drops entries past their TTL with byte accounting.
func purgeExpiredFencedReplay[V any](mu *sync.Mutex, entries map[string]fencedReplayEntry[V], totalBytes *int, limits fencedReplayLimits, sizeOf func(V) int, now time.Time) {
	mu.Lock()
	for key, entry := range entries {
		if now.Sub(entry.Timestamp) > limits.TTL {
			*totalBytes -= sizeOf(entry.Value)
			delete(entries, key)
		}
	}
	mu.Unlock()
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
