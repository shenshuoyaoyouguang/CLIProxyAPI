package helps

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

var errTestStableValueMissing = errors.New("test: missing after set")

type testStableValueCache struct {
	*stableValueCache
	generateCalls int
}

func newLocalTestStableValueCache() *testStableValueCache {
	c := &testStableValueCache{}
	c.stableValueCache = newStableValueCache(stableValueCacheConfig{
		ttl:           time.Hour,
		cleanupPeriod: time.Hour,
		kvClient:      func() (stableValueKV, bool, error) { return nil, false, nil },
		localKey:      stableValueLocalKey,
		validate:      func(value string) bool { return value != "" },
		generate: func(context.Context, string) (string, error) {
			c.generateCalls++
			return uuid.New().String(), nil
		},
		entries: &map[string]stableValueCacheEntry{},
		mu:      &sync.RWMutex{},
	})
	return c
}

func newHomeTestStableValueCache(client *fakeClaudeIDKVClient) *testStableValueCache {
	c := &testStableValueCache{}
	c.stableValueCache = newStableValueCache(stableValueCacheConfig{
		ttl:           time.Hour,
		cleanupPeriod: time.Hour,
		kvClient:      func() (stableValueKV, bool, error) { return client, true, nil },
		kvKey:         claudeSessionIDKVKey,
		localKey:      stableValueLocalKey,
		validate:      func(value string) bool { return value != "" },
		generate: func(context.Context, string) (string, error) {
			c.generateCalls++
			return uuid.New().String(), nil
		},
		missingAfter: errTestStableValueMissing,
		entries:      &map[string]stableValueCacheEntry{},
		mu:           &sync.RWMutex{},
	})
	return c
}

func TestStableValueCacheCachesPerKey(t *testing.T) {
	cache := newLocalTestStableValueCache()

	first, errFirst := cache.Get(context.Background(), "api-key-1")
	if errFirst != nil {
		t.Fatalf("Get() first error = %v", errFirst)
	}
	second, errSecond := cache.Get(context.Background(), "api-key-1")
	if errSecond != nil {
		t.Fatalf("Get() second error = %v", errSecond)
	}
	if first != second {
		t.Fatalf("Get() = %q then %q, want same cached value", first, second)
	}
	if cache.generateCalls != 1 {
		t.Fatalf("generate calls = %d, want 1 (cached after first)", cache.generateCalls)
	}
}

func TestStableValueCacheRegeneratesAfterExpiry(t *testing.T) {
	cache := newLocalTestStableValueCache()
	const key = "api-key-expired"

	first, errFirst := cache.Get(context.Background(), key)
	if errFirst != nil {
		t.Fatalf("Get() first error = %v", errFirst)
	}

	cache.mu.Lock()
	(*cache.entries)[cache.localKey(key)] = stableValueCacheEntry{
		value:  first,
		expire: time.Now().Add(-time.Minute),
	}
	cache.mu.Unlock()

	second, errSecond := cache.Get(context.Background(), key)
	if errSecond != nil {
		t.Fatalf("Get() second error = %v", errSecond)
	}
	if second == first {
		t.Fatalf("Get() = %q, want regenerated value after expiry", second)
	}
	if cache.generateCalls != 2 {
		t.Fatalf("generate calls = %d, want 2", cache.generateCalls)
	}
}

func TestStableValueCacheRenewsTTLOnHit(t *testing.T) {
	cache := newLocalTestStableValueCache()
	const key = "api-key-renew"

	id, errGet := cache.Get(context.Background(), key)
	if errGet != nil {
		t.Fatalf("Get() error = %v", errGet)
	}

	soon := time.Now()
	cache.mu.Lock()
	(*cache.entries)[cache.localKey(key)] = stableValueCacheEntry{
		value:  id,
		expire: soon.Add(2 * time.Second),
	}
	cache.mu.Unlock()

	refreshed, errRefreshed := cache.Get(context.Background(), key)
	if errRefreshed != nil {
		t.Fatalf("Get() refreshed error = %v", errRefreshed)
	}
	if refreshed != id {
		t.Fatalf("Get() = %q, want same value before expiry", refreshed)
	}

	cache.mu.RLock()
	entry := (*cache.entries)[cache.localKey(key)]
	cache.mu.RUnlock()
	if entry.expire.Sub(soon) < 30*time.Minute {
		t.Fatalf("TTL not renewed: remaining %v", entry.expire.Sub(soon))
	}
}

func TestStableValueCacheHomePersistsAcrossLocalReset(t *testing.T) {
	client := newFakeClaudeIDKVClient()
	cache := newHomeTestStableValueCache(client)

	first, errFirst := cache.Get(context.Background(), "api-key-1")
	if errFirst != nil {
		t.Fatalf("Get() first error = %v", errFirst)
	}

	// Clear the in-memory tier; the Home KV value must still win.
	cache.mu.Lock()
	*cache.entries = make(map[string]stableValueCacheEntry)
	cache.mu.Unlock()

	second, errSecond := cache.Get(context.Background(), "api-key-1")
	if errSecond != nil {
		t.Fatalf("Get() second error = %v", errSecond)
	}
	if first != second {
		t.Fatalf("Get() = %q then %q, want same Home KV value", first, second)
	}
	if client.setCount != 1 {
		t.Fatalf("KVSetNX count = %d, want 1", client.setCount)
	}
}

func TestStableValueCacheHomeMissingAfterSet(t *testing.T) {
	client := newFakeClaudeIDKVClient()
	client.setNoPersist = true
	cache := newHomeTestStableValueCache(client)

	if _, errValue := cache.Get(context.Background(), "api-key-1"); errValue == nil {
		t.Fatalf("Get() error = nil, want missing-after-set error")
	}
}

func TestStableValueCacheEmptyKeyGeneratesWithoutKV(t *testing.T) {
	client := newFakeClaudeIDKVClient()
	cache := newHomeTestStableValueCache(client)

	value, errValue := cache.Get(context.Background(), "")
	if errValue != nil {
		t.Fatalf("Get() empty error = %v", errValue)
	}
	if value == "" {
		t.Fatal("Get() empty returned empty value")
	}
	if client.getCount != 0 || client.setCount != 0 || client.expireCount != 0 {
		t.Fatalf("KV calls = get %d set %d expire %d, want all zero", client.getCount, client.setCount, client.expireCount)
	}
}
