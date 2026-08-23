package helps

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	homekv "github.com/router-for-me/CLIProxyAPI/v7/internal/home"
)

// sessionIDTTL is the TTL for a cached stable session ID.
const sessionIDTTL = time.Hour

// sessionIDCacheCleanupPeriod is how often expired session IDs are purged.
const sessionIDCacheCleanupPeriod = 15 * time.Minute

// sessionIDCacheEntry is kept as an alias so tests can keep injecting state.
type sessionIDCacheEntry = stableValueCacheEntry

var (
	sessionIDCache   = make(map[string]sessionIDCacheEntry)
	sessionIDCacheMu sync.RWMutex
)

var sessionIDCacheEngine = newStableValueCache(stableValueCacheConfig{
	ttl:           sessionIDTTL,
	cleanupPeriod: sessionIDCacheCleanupPeriod,
	kvClient: func() (stableValueKV, bool, error) {
		return currentClaudeIDKVClient()
	},
	kvKey:    claudeSessionIDKVKey,
	localKey: stableValueLocalKey,
	validate: func(value string) bool { return value != "" },
	generate: func(context.Context, string) (string, error) {
		return uuid.New().String(), nil
	},
	missingAfter: errors.New("home kv session id missing after set"),
	entries:      &sessionIDCache,
	mu:           &sessionIDCacheMu,
})

// CachedSessionID returns a stable session UUID per apiKey, refreshing the TTL on each access.
func CachedSessionID(apiKey string) string {
	value, errValue := CachedSessionIDRequired(context.Background(), apiKey)
	if errValue == nil && value != "" {
		return value
	}
	return uuid.New().String()
}

// CachedSessionIDRequired returns a stable session UUID per apiKey for request-time paths.
func CachedSessionIDRequired(ctx context.Context, apiKey string) (string, error) {
	return sessionIDCacheEngine.Get(ctx, apiKey)
}

func claudeSessionIDKVKey(apiKey string) string {
	return "cpa:claude:session-id:" + homekv.HashKeyPart(apiKey)
}
