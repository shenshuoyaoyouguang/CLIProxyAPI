package helps

import (
	"context"
	"errors"
	"sync"
	"time"

	homekv "github.com/router-for-me/CLIProxyAPI/v7/internal/home"
)

// userIDTTL is the TTL for a cached stable fake user ID.
const userIDTTL = time.Hour

// userIDCacheCleanupPeriod is how often expired user IDs are purged.
const userIDCacheCleanupPeriod = 15 * time.Minute

// userIDCacheEntry is kept as an alias so tests can keep injecting state.
type userIDCacheEntry = stableValueCacheEntry

var (
	userIDCache   = make(map[string]userIDCacheEntry)
	userIDCacheMu sync.RWMutex
)

var userIDCacheEngine = newStableValueCache(stableValueCacheConfig{
	ttl:           userIDTTL,
	cleanupPeriod: userIDCacheCleanupPeriod,
	kvClient: func() (stableValueKV, bool, error) {
		return currentClaudeIDKVClient()
	},
	kvKey:    claudeUserIDKVKey,
	localKey: stableValueLocalKey,
	validate: isValidUserID,
	generate: func(ctx context.Context, apiKey string) (string, error) {
		sessionID, errSessionID := CachedSessionIDRequired(ctx, apiKey)
		if errSessionID != nil {
			return "", errSessionID
		}
		return generateFakeUserIDWithSessionID(sessionID), nil
	},
	missingAfter: errors.New("home kv user id missing after set"),
	entries:      &userIDCache,
	mu:           &userIDCacheMu,
})

// CachedUserID returns a stable fake user ID per apiKey, refreshing the TTL on each access.
func CachedUserID(apiKey string) string {
	value, errValue := CachedUserIDRequired(context.Background(), apiKey)
	if errValue == nil && value != "" {
		return value
	}
	return generateFakeUserID()
}

// CachedUserIDRequired returns a stable fake user ID per apiKey for request-time paths.
func CachedUserIDRequired(ctx context.Context, apiKey string) (string, error) {
	return userIDCacheEngine.Get(ctx, apiKey)
}

// userIDCacheKey is kept for tests that inject expired state directly.
func userIDCacheKey(apiKey string) string {
	return stableValueLocalKey(apiKey)
}

func claudeUserIDKVKey(apiKey string) string {
	return "cpa:claude:user-id:" + homekv.HashKeyPart(apiKey)
}
