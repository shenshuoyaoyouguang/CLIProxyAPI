package cache

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	homekv "github.com/router-for-me/CLIProxyAPI/v7/internal/home"
	log "github.com/sirupsen/logrus"
)

// Shared implementation for the best-effort item-list reasoning replay
// caches (Codex and xAI): a mutex-guarded entry map with TTL expiry,
// oldest-first batch eviction, and last-write-wins home-KV plumbing.
// Provider packages keep their own state variables (tests reach into them)
// and their provider-specific normalization; everything mechanical lives here.

// itemReplayEntry is one session's cached item list.
type itemReplayEntry struct {
	Items     [][]byte
	Timestamp time.Time
}

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
		evictOldestItemReplayEntriesLocked(entries, evictBatch)
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
		evictOldestItemReplayEntriesLocked(entries, evictBatch)
	}
}

// removeItemReplayLocal deletes one key from the local cache.
func removeItemReplayLocal(mu *sync.Mutex, entries map[string]itemReplayEntry, key string) {
	mu.Lock()
	delete(entries, key)
	mu.Unlock()
}

// evictOldestItemReplayEntriesLocked removes the count oldest entries by
// timestamp; the caller holds mu.
func evictOldestItemReplayEntriesLocked(entries map[string]itemReplayEntry, count int) {
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
		delete(entries, candidates[i].key)
	}
}

// purgeExpiredItemReplay drops every entry past its TTL.
func purgeExpiredItemReplay(mu *sync.Mutex, entries map[string]itemReplayEntry, ttl time.Duration, now time.Time) {
	mu.Lock()
	for key, entry := range entries {
		if now.Sub(entry.Timestamp) > ttl {
			delete(entries, key)
		}
	}
	mu.Unlock()
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
