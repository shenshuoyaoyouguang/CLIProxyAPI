package cache

import (
	"container/list"
	"sync"
	"time"
)

// LRUEntry wraps a cached value with its insertion timestamp.
type lruEntry[K comparable, V any] struct {
	key       K
	value     V
	timestamp time.Time
}

// LRUCache is a generic, thread-safe LRU cache with an absolute TTL and a
// maximum entry count.
//
// Eviction happens in two situations:
//   - Capacity: when Set pushes the cache past maxEntries, the least-recently-used
//     entry is evicted (one per Set, to avoid rescanning under write storms).
//   - Expiry: PurgeExpired removes entries older than the TTL relative to the
//     supplied time. Get also drops an expired entry on read.
//
// TTL is absolute by default: Get does not refresh an entry's age, so hot entries
// still expire ttl after insertion. Set Sliding(true) to instead refresh the age
// on every Get (and move the entry to the front), which makes eviction behave
// like "oldest last-touched".
//
// Because the backing store is a plain map guarded by an internal mutex, the
// cache never needs the two-phase delete dance required by sync.Map.Range.
//
// IMPORTANT: onEvict is invoked while the internal mutex is held. The callback
// MUST NOT call any LRUCache method (Set, Delete, Get, Len, PurgeExpired, etc.)
// because Go's sync.Mutex is not reentrant and will deadlock.
type LRUCache[K comparable, V any] struct {
	mu         sync.Mutex
	ll         *list.List // front = most recently used
	items      map[K]*list.Element
	maxEntries int
	ttl        time.Duration
	sliding    bool
	onEvict    func(K, V)
}

// NewLRUCache creates an LRU cache. maxEntries <= 0 is treated as 1.
// onEvict, if non-nil, is invoked for every evicted entry.
func NewLRUCache[K comparable, V any](maxEntries int, ttl time.Duration, onEvict func(K, V)) *LRUCache[K, V] {
	if maxEntries <= 0 {
		maxEntries = 1
	}
	return &LRUCache[K, V]{
		ll:         list.New(),
		items:      make(map[K]*list.Element),
		maxEntries: maxEntries,
		ttl:        ttl,
		onEvict:    onEvict,
	}
}

// SetSliding toggles sliding-window TTL behavior (see LRUCache docs).
func (c *LRUCache[K, V]) SetSliding(enable bool) {
	c.mu.Lock()
	c.sliding = enable
	c.mu.Unlock()
}

// Get returns the value for key. It returns (zero, false) when the key is
// missing or expired. On a hit with sliding enabled, the entry's age is
// refreshed and it is moved to the front.
func (c *LRUCache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.items[key]
	if !ok {
		var zero V
		return zero, false
	}
	entry := elem.Value.(*lruEntry[K, V])
	if c.ttl > 0 && time.Since(entry.timestamp) > c.ttl {
		c.removeElement(elem)
		var zero V
		return zero, false
	}
	if c.sliding {
		entry.timestamp = time.Now()
		c.ll.MoveToFront(elem)
	}
	return entry.value, true
}

// Set stores value under key, refreshing its timestamp. If the key already
// exists, its previous value is passed to onEvict (if set) before replacement.
// When over capacity, the least-recently-used entry is evicted.
func (c *LRUCache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		entry := elem.Value.(*lruEntry[K, V])
		if c.onEvict != nil {
			c.onEvict(entry.key, entry.value)
		}
		entry.value = value
		entry.timestamp = time.Now()
		c.ll.MoveToFront(elem)
		return
	}
	entry := &lruEntry[K, V]{key: key, value: value, timestamp: time.Now()}
	elem := c.ll.PushFront(entry)
	c.items[key] = elem
	if c.maxEntries > 0 && c.ll.Len() > c.maxEntries {
		c.removeOldest()
	}
}

// Update atomically reads, modifies, and writes the value for key under a single
// lock acquisition, eliminating the lost-update window between separate Get/Set
// calls. The update callback receives the current value and whether the key
// existed (and was not expired); it returns the new value to store and whether
// to perform the write. The entry's timestamp is refreshed on write, and sliding
// reads also refresh the timestamp of an existing entry.
func (c *LRUCache[K, V]) Update(key K, update func(old V, exists bool) (new V, write bool)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var old V
	exists := false
	if elem, ok := c.items[key]; ok {
		entry := elem.Value.(*lruEntry[K, V])
		if c.ttl > 0 && time.Since(entry.timestamp) > c.ttl {
			// Expired entry: remove and treat as missing.
			c.removeElement(elem)
		} else {
			old = entry.value
			exists = true
			if c.sliding {
				entry.timestamp = time.Now()
				c.ll.MoveToFront(elem)
			}
		}
	}
	newVal, write := update(old, exists)
	if !write {
		return
	}
	if elem, ok := c.items[key]; ok {
		entry := elem.Value.(*lruEntry[K, V])
		if c.onEvict != nil {
			c.onEvict(entry.key, entry.value)
		}
		entry.value = newVal
		entry.timestamp = time.Now()
		c.ll.MoveToFront(elem)
		return
	}
	entry := &lruEntry[K, V]{key: key, value: newVal, timestamp: time.Now()}
	elem := c.ll.PushFront(entry)
	c.items[key] = elem
	if c.maxEntries > 0 && c.ll.Len() > c.maxEntries {
		c.removeOldest()
	}
}

// Delete removes key if present, invoking onEvict for the removed entry.
func (c *LRUCache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		c.removeElement(elem)
	}
}

// Len returns the number of live entries.
func (c *LRUCache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

// Clear removes all entries.
func (c *LRUCache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ll.Init()
	c.items = make(map[K]*list.Element)
}

// PurgeExpired removes every entry whose age exceeds the TTL relative to now,
// returning the number of entries removed. With ttl <= 0 it is a no-op.
func (c *LRUCache[K, V]) PurgeExpired(now time.Time) int {
	if c.ttl <= 0 {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := 0
	for elem := c.ll.Front(); elem != nil; {
		next := elem.Next()
		entry := elem.Value.(*lruEntry[K, V])
		if now.Sub(entry.timestamp) > c.ttl {
			c.removeElement(elem)
			removed++
		}
		elem = next
	}
	return removed
}

// removeElement detaches elem from the list and map, firing onEvict.
// Caller must hold c.mu.
func (c *LRUCache[K, V]) removeElement(elem *list.Element) {
	entry := elem.Value.(*lruEntry[K, V])
	if c.onEvict != nil {
		c.onEvict(entry.key, entry.value)
	}
	c.ll.Remove(elem)
	delete(c.items, entry.key)
}

// removeOldest evicts the least-recently-used entry (list back).
// Caller must hold c.mu.
func (c *LRUCache[K, V]) removeOldest() {
	elem := c.ll.Back()
	if elem != nil {
		c.removeElement(elem)
	}
}

// cacheCleanupCallbacks holds purge functions registered by individual caches so
// that no single cache file needs to reach into another's cleanup logic.
var (
	cacheCleanupCallbacksMu sync.Mutex
	cacheCleanupCallbacks   []func(time.Time)
)

// registerCacheCleanup adds fn to the set invoked by the shared cleanup ticker.
func registerCacheCleanup(fn func(time.Time)) {
	cacheCleanupCallbacksMu.Lock()
	defer cacheCleanupCallbacksMu.Unlock()
	cacheCleanupCallbacks = append(cacheCleanupCallbacks, fn)
}

// runCacheCleanupCallbacks invokes every registered purge function with now.
func runCacheCleanupCallbacks(now time.Time) {
	cacheCleanupCallbacksMu.Lock()
	cbs := make([]func(time.Time), 0, len(cacheCleanupCallbacks))
	cbs = append(cbs, cacheCleanupCallbacks...)
	cacheCleanupCallbacksMu.Unlock()
	for _, cb := range cbs {
		cb(now)
	}
}
