package cache

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cespare/xxhash/v2"
	homekv "github.com/router-for-me/CLIProxyAPI/v7/internal/home"
	log "github.com/sirupsen/logrus"
)

// SignatureEntry holds a cached thinking signature with timestamp
type SignatureEntry struct {
	Signature string
	Timestamp time.Time
}

const (
	// SignatureCacheTTL is how long signatures are valid
	SignatureCacheTTL = 3 * time.Hour

	// SignatureTextHashLen is the length of the hash key (16 hex chars = 64-bit key space).
	// The length is kept at 16 purely for wire/key-layout compatibility with the previous
	// SHA-256 prefix; it is NOT a cryptographic-strength indicator. Because the key is a
	// 64-bit non-cryptographic xxhash digest, the collision probability is far higher than
	// a full SHA-256 (birthday-bound to roughly 50% at ~2^32 distinct keys). A collision in
	// this cache only ever yields a wrong-value cache hit or a redundant signature
	// recomputation for a different text; it cannot corrupt data or leak secrets, so the
	// weaker, much faster hash is an acceptable trade-off on the hot path.
	SignatureTextHashLen = 16

	// MinValidSignatureLen is the minimum length for a signature to be considered valid
	MinValidSignatureLen = 50

	// CacheCleanupInterval controls how often stale entries are purged
	CacheCleanupInterval = 10 * time.Minute

	// maxSignatureEntriesPerGroup caps in-memory entries per model group to
	// prevent unbounded growth under high-cardinality text keys.
	maxSignatureEntriesPerGroup = 10000
)

// signatureCache stores signatures by model group -> textHash -> SignatureEntry
var signatureCache sync.Map

// cacheCleanupOnce ensures the background cleanup goroutine starts only once
var cacheCleanupOnce sync.Once

type signatureKVClient interface {
	KVGet(ctx context.Context, key string) ([]byte, bool, error)
	KVSet(ctx context.Context, key string, value []byte, opts homekv.KVSetOptions) (bool, error)
	KVDel(ctx context.Context, keys ...string) (int64, error)
	KVExpire(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

var currentSignatureKVClient = func() (signatureKVClient, bool, error) {
	return homekv.CurrentKVClient()
}

// groupCache is the inner per-model-group bucket. The entries map is a
// small LRU keyed by text hash; signature_cache.go only touches the
// outer sync.Map of groupCaches, so the inner LRU guards itself.
type groupCache struct {
	entries *LRUCache[string, SignatureEntry]
}

// hashText creates a stable, Unicode-safe key from text content.
// It uses xxhash/v2 (non-cryptographic, 64-bit) on the hot path instead of
// SHA-256 for speed. The digest is formatted as %016x so the key length (16 hex
// chars) and cache-key layout match the previous SHA-256 prefix exactly, keeping
// existing key namespaces and tooling compatible.
//
// Collision semantics: unlike SHA-256, a 64-bit digest collides with meaningful
// probability at scale (birthday-bound to ~50% near 2^32 distinct keys). For this
// cache that consequence is benign — a collision can only produce a cache hit on a
// different text's signature (triggering a recompute) or a redundant store; it
// never corrupts stored data or exposes secrets. Hot signatures stay resident via
// the sliding TTL (SignatureCacheTTL), so transient collisions do not accumulate
// into correctness issues.
func hashText(text string) string {
	return fmt.Sprintf("%016x", xxhash.Sum64String(text))
}

// getOrCreateGroupCache gets or creates a cache bucket for a model group
func getOrCreateGroupCache(groupKey string) *groupCache {
	// Start background cleanup on first access
	cacheCleanupOnce.Do(startCacheCleanup)

	if val, ok := signatureCache.Load(groupKey); ok {
		return val.(*groupCache)
	}
	entries := NewLRUCache[string, SignatureEntry](maxSignatureEntriesPerGroup, SignatureCacheTTL, nil)
	// Sliding TTL: every Get refreshes the entry age, matching the previous
	// "oldest last-touched" eviction behavior of the signature cache and the
	// three reasoning replay caches. Hot signatures therefore stay alive as
	// long as they are being read, avoiding thundering-herd re-validation on
	// frequently accessed model groups.
	entries.SetSliding(true)
	sc := &groupCache{entries: entries}
	actual, _ := signatureCache.LoadOrStore(groupKey, sc)
	return actual.(*groupCache)
}

// startCacheCleanup launches a background goroutine that periodically
// removes caches where all entries have expired.
func startCacheCleanup() {
	go func() {
		ticker := time.NewTicker(CacheCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			purgeExpiredCaches()
		}
	}()
}

// purgeExpiredCaches removes caches with no valid (non-expired) entries.
// It is responsible only for the signature cache's own buckets; other caches
// register their purge functions via registerCacheCleanup and run through
// runCacheCleanupCallbacks, so this file no longer reaches into their state.
func purgeExpiredCaches() {
	now := time.Now()
	// Two-phase purge: collect empty buckets during Range, then delete them
	// outside the callback. Deleting from a sync.Map inside its Range callback
	// is undefined behavior, so signatureCache.Delete must not run here.
	var emptyBuckets []any
	signatureCache.Range(func(key, value any) bool {
		sc := value.(*groupCache)
		// Remove expired entries from the inner LRU (internally locked).
		sc.entries.PurgeExpired(now)
		if sc.entries.Len() == 0 {
			emptyBuckets = append(emptyBuckets, key)
		}
		return true
	})
	for _, key := range emptyBuckets {
		signatureCache.Delete(key)
	}
	runCacheCleanupCallbacks(now)
}

// CacheSignature stores a thinking signature for a given model group and text.
// Used for Claude models that require signed thinking blocks in multi-turn conversations.
func CacheSignature(modelName, text, signature string) {
	CacheSignatureBestEffort(context.Background(), modelName, text, signature)
}

// CacheSignatureBestEffort stores a thinking signature for completed response paths.
func CacheSignatureBestEffort(ctx context.Context, modelName, text, signature string) bool {
	if text == "" || signature == "" {
		return false
	}
	if len(signature) < MinValidSignatureLen {
		return false
	}

	if client, homeMode, errClient := currentSignatureKVClient(); homeMode {
		if errClient != nil {
			log.Errorf("home kv best-effort signature set failed prefix=cpa:signature:*: %v", errClient)
			return false
		}
		written, errSet := client.KVSet(ctx, signatureKVKey(modelName, text), []byte(signature), homekv.KVSetOptions{EX: SignatureCacheTTL})
		if errSet != nil {
			log.Errorf("home kv best-effort signature set failed prefix=cpa:signature:*: %v", errSet)
			return false
		}
		return written
	}

	groupKey := GetModelGroup(modelName)
	textHash := hashText(text)
	sc := getOrCreateGroupCache(groupKey)
	sc.entries.Set(textHash, SignatureEntry{
		Signature: signature,
		Timestamp: time.Now(),
	})
	return true
}

// GetCachedSignature retrieves a cached signature for a given model group and text.
// Returns empty string if not found or expired.
func GetCachedSignature(modelName, text string) string {
	signature, errSignature := GetCachedSignatureRequired(context.Background(), modelName, text)
	if errSignature != nil {
		return ""
	}
	return signature
}

// GetCachedSignatureRequired retrieves a cached signature for request-time paths.
func GetCachedSignatureRequired(ctx context.Context, modelName, text string) (string, error) {
	groupKey := GetModelGroup(modelName)

	if text == "" {
		if groupKey == "gemini" {
			return "skip_thought_signature_validator", nil
		}
		return "", nil
	}

	if client, homeMode, errClient := currentSignatureKVClient(); homeMode {
		if errClient != nil {
			return "", errClient
		}
		key := signatureKVKey(modelName, text)
		raw, found, errGet := client.KVGet(ctx, key)
		if errGet != nil {
			return "", errGet
		}
		if !found {
			if groupKey == "gemini" {
				return "skip_thought_signature_validator", nil
			}
			return "", nil
		}
		if _, errExpire := client.KVExpire(ctx, key, SignatureCacheTTL); errExpire != nil {
			return "", errExpire
		}
		return string(raw), nil
	}

	val, ok := signatureCache.Load(groupKey)
	if !ok {
		if groupKey == "gemini" {
			return "skip_thought_signature_validator", nil
		}
		return "", nil
	}
	sc := val.(*groupCache)

	textHash := hashText(text)

	// LRU.Get returns false for both missing and expired entries, which is
	// exactly the branch that returns the Gemini sentinel below. With sliding
	// TTL enabled in getOrCreateGroupCache, a successful Get also refreshes
	// the entry's age so hot signatures stay alive across repeated reads.
	entry, ok := sc.entries.Get(textHash)
	if !ok {
		if groupKey == "gemini" {
			return "skip_thought_signature_validator", nil
		}
		return "", nil
	}
	return entry.Signature, nil
}

// ClearSignatureCache clears signature cache for a specific model group or all groups.
func ClearSignatureCache(modelName string) {
	if modelName == "" {
		// Two-phase clear: collect keys during Range, then delete outside the
		// callback. Deleting from a sync.Map inside its Range is undefined
		// behavior.
		var keys []any
		signatureCache.Range(func(key, _ any) bool {
			keys = append(keys, key)
			return true
		})
		for _, key := range keys {
			signatureCache.Delete(key)
		}
		return
	}
	groupKey := GetModelGroup(modelName)
	signatureCache.Delete(groupKey)
}

// DeleteCachedSignatureRequired removes one exact cached signature.
func DeleteCachedSignatureRequired(ctx context.Context, modelName, text string) error {
	if text == "" {
		return nil
	}
	if client, homeMode, errClient := currentSignatureKVClient(); homeMode {
		if errClient != nil {
			return errClient
		}
		_, errDel := client.KVDel(ctx, signatureKVKey(modelName, text))
		return errDel
	}
	groupKey := GetModelGroup(modelName)
	textHash := hashText(text)
	val, ok := signatureCache.Load(groupKey)
	if !ok {
		return nil
	}
	sc := val.(*groupCache)
	sc.entries.Delete(textHash)
	// Empty buckets are left for periodic cleanup. Deleting the outer bucket here
	// can drop a concurrent setter that loaded this group before the delete.
	return nil
}

// HasValidSignature checks if a signature is valid (non-empty and long enough)
func HasValidSignature(modelName, signature string) bool {
	return (signature != "" && len(signature) >= MinValidSignatureLen) || (signature == "skip_thought_signature_validator" && GetModelGroup(modelName) == "gemini")
}

// GetModelGroup maps a model name to a signature-cache bucket.
// Brand tokens are matched on path/separator boundaries to avoid substring
// false positives such as "engpt-helper" matching "gpt".
func GetModelGroup(modelName string) string {
	lower := strings.ToLower(strings.TrimSpace(modelName))
	parts := strings.FieldsFunc(lower, func(r rune) bool {
		return r == '/' || r == '-' || r == '_' || r == '.' || r == ' '
	})
	for _, p := range parts {
		switch {
		case strings.HasPrefix(p, "gpt"):
			return "gpt"
		case strings.HasPrefix(p, "claude"):
			return "claude"
		case strings.HasPrefix(p, "gemini"):
			return "gemini"
		}
	}
	return modelName
}

func signatureKVKey(modelName, text string) string {
	return fmt.Sprintf("cpa:signature:%s:%s", GetModelGroup(modelName), homekv.HashKeyPart(text))
}

var signatureCacheEnabled atomic.Bool
var signatureBypassStrictMode atomic.Bool

func init() {
	signatureCacheEnabled.Store(true)
	signatureBypassStrictMode.Store(false)
}

// SetSignatureCacheEnabled switches Antigravity signature handling between cache mode and bypass mode.
func SetSignatureCacheEnabled(enabled bool) {
	previous := signatureCacheEnabled.Swap(enabled)
	if previous == enabled {
		return
	}
	if !enabled {
		log.Info("antigravity signature cache DISABLED - bypass mode active, cached signatures will not be used for request translation")
	}
}

// SignatureCacheEnabled returns whether signature cache validation is enabled.
func SignatureCacheEnabled() bool {
	return signatureCacheEnabled.Load()
}

// SetSignatureBypassStrictMode controls whether bypass mode uses strict protobuf-tree validation.
func SetSignatureBypassStrictMode(strict bool) {
	previous := signatureBypassStrictMode.Swap(strict)
	if previous == strict {
		return
	}
	if strict {
		log.Debug("antigravity bypass signature validation: strict mode (protobuf tree)")
	} else {
		log.Debug("antigravity bypass signature validation: basic mode (R/E + 0x12)")
	}
}

// SignatureBypassStrictMode returns whether bypass mode uses strict protobuf-tree validation.
func SignatureBypassStrictMode() bool {
	return signatureBypassStrictMode.Load()
}
