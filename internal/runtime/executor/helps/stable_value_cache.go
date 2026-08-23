package helps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	homekv "github.com/router-for-me/CLIProxyAPI/v7/internal/home"
)

// stableValueKV is the Home KV surface a stable value cache needs: get, set-if-
// absent, and refresh-expiry.
type stableValueKV interface {
	KVGet(ctx context.Context, key string) ([]byte, bool, error)
	KVSetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error)
	KVExpire(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

// claudeIDKVClient is kept as an alias for existing callers and tests.
type claudeIDKVClient = stableValueKV

// currentClaudeIDKVClient returns the current Home KV client. It is a package
// var so tests can inject a fake.
var currentClaudeIDKVClient = func() (stableValueKV, bool, error) {
	return homekv.CurrentKVClient()
}

// stableValueCacheEntry is one cached value with its expiry.
type stableValueCacheEntry struct {
	value  string
	expire time.Time
}

// stableValueCacheConfig configures one get-or-generate stable value cache.
type stableValueCacheConfig struct {
	// ttl is the per-value TTL, refreshed on each hit.
	ttl time.Duration
	// cleanupPeriod is how often the in-memory purge loop runs.
	cleanupPeriod time.Duration
	// kvClient resolves the Home KV client for the persistent tier.
	kvClient func() (stableValueKV, bool, error)
	// kvKey builds the Home KV key for an apiKey.
	kvKey func(apiKey string) string
	// localKey builds the in-memory cache key for an apiKey.
	localKey func(apiKey string) string
	// validate reports whether a stored value is acceptable to return.
	validate func(value string) bool
	// generate produces a fresh value for an apiKey.
	generate func(ctx context.Context, apiKey string) (string, error)
	// missingAfter is returned when a value vanishes between set and read-back.
	missingAfter error
	// entries and mu are the in-memory tier storage.
	entries *map[string]stableValueCacheEntry
	mu      *sync.RWMutex
}

// stableValueCache returns a stable value per apiKey, persisting to Home KV when
// available and falling back to an in-memory map with a background purge.
type stableValueCache struct {
	stableValueCacheConfig
	cleanupOnce sync.Once
}

func newStableValueCache(cfg stableValueCacheConfig) *stableValueCache {
	return &stableValueCache{stableValueCacheConfig: cfg}
}

// Get returns the stable value for apiKey, generating it on first use.
func (c *stableValueCache) Get(ctx context.Context, apiKey string) (string, error) {
	if apiKey == "" {
		return c.generate(ctx, apiKey)
	}
	client, homeMode, errClient := c.kvClient()
	if homeMode {
		if errClient != nil {
			return "", errClient
		}
		key := c.kvKey(apiKey)
		raw, found, errGet := client.KVGet(ctx, key)
		if errGet != nil {
			return "", errGet
		}
		if found && c.validate(strings.TrimSpace(string(raw))) {
			if _, errExpire := client.KVExpire(ctx, key, c.ttl); errExpire != nil {
				return "", errExpire
			}
			return strings.TrimSpace(string(raw)), nil
		}
		value, errGenerate := c.generate(ctx, apiKey)
		if errGenerate != nil {
			return "", errGenerate
		}
		if _, errSet := client.KVSetNX(ctx, key, []byte(value), c.ttl); errSet != nil {
			return "", errSet
		}
		raw, found, errGet = client.KVGet(ctx, key)
		if errGet != nil {
			return "", errGet
		}
		if found && c.validate(strings.TrimSpace(string(raw))) {
			return strings.TrimSpace(string(raw)), nil
		}
		return "", c.missingAfter
	}

	startStableCacheCleanup(&c.cleanupOnce, c.cleanupPeriod, c.purge)
	return c.getLocal(ctx, apiKey)
}

// getLocal resolves a value from the in-memory tier, refreshing the TTL on hit.
func (c *stableValueCache) getLocal(ctx context.Context, apiKey string) (string, error) {
	key := c.localKey(apiKey)
	now := time.Now()

	c.mu.RLock()
	entry, ok := (*c.entries)[key]
	valid := ok && entry.value != "" && entry.expire.After(now) && c.validate(entry.value)
	c.mu.RUnlock()
	if valid {
		c.mu.Lock()
		entry = (*c.entries)[key]
		if entry.value != "" && entry.expire.After(now) && c.validate(entry.value) {
			entry.expire = now.Add(c.ttl)
			(*c.entries)[key] = entry
			c.mu.Unlock()
			return entry.value, nil
		}
		c.mu.Unlock()
	}

	value, errGenerate := c.generate(ctx, apiKey)
	if errGenerate != nil {
		return "", errGenerate
	}

	c.mu.Lock()
	entry, ok = (*c.entries)[key]
	if !ok || entry.value == "" || !entry.expire.After(now) || !c.validate(entry.value) {
		entry.value = value
	}
	entry.expire = now.Add(c.ttl)
	(*c.entries)[key] = entry
	c.mu.Unlock()
	return entry.value, nil
}

// purge removes expired entries from the in-memory tier.
func (c *stableValueCache) purge() {
	now := time.Now()
	c.mu.Lock()
	for key, entry := range *c.entries {
		if !entry.expire.After(now) {
			delete(*c.entries, key)
		}
	}
	c.mu.Unlock()
}

// stableValueLocalKey is the default in-memory cache key for an apiKey.
func stableValueLocalKey(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(sum[:])
}

// startStableCacheCleanup runs one background purge loop per cache.
func startStableCacheCleanup(once *sync.Once, period time.Duration, purge func()) {
	once.Do(func() {
		go func() {
			ticker := time.NewTicker(period)
			defer ticker.Stop()
			for range ticker.C {
				purge()
			}
		}()
	})
}
