package helps

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// callerNamespaceHashBytes is the number of SHA-256 digest bytes used for the
// caller namespace (16 bytes → 128 bits → 32 hex chars). This keeps birthday
// collision risk in the same class as SignatureTextHashLen=64 (full 32-byte
// digest). The previous sum[:8] (64-bit) truncation reintroduced the risk the
// signature-cache fix had just removed (issue #11).
const callerNamespaceHashBytes = 16

// degradedCallerSalt is a process-local salt used when userApiKey is missing.
// IsolateClientControlledSessionKey no longer returns empty (full disable) for
// client-controlled sessions without a caller API key. Instead it falls back to
// a process-level namespace:
//   - distinct processes stay isolated (in-memory caches are process-local)
//   - anonymous callers in the same process are further split by client IP, so
//     distinct clients cannot share replay state by reusing an identical
//     sessionKey (review S1)
//   - same-IP anonymous callers may still share a sessionKey, but session keys
//     are typically client-generated UUIDs so collisions are rare
//
// This keeps Codex / xAI / Antigravity reasoning replay usable without a
// userApiKey while avoiding bare global client keys (issue #8).
var (
	degradedCallerSaltOnce sync.Once
	degradedCallerSalt     string
)

func loadDegradedCallerSalt() string {
	degradedCallerSaltOnce.Do(func() {
		var buf [32]byte
		if _, err := rand.Read(buf[:]); err != nil {
			// rand.Read failure is extremely rare; fall back to a sha256-derived
			// seed mixed with process-local entropy so the namespace is still
			// unique per process (issue: zero-value buf alone would be
			// deterministic across processes, breaking the isolation comment).
			log.Warnf("session isolate: rand.Read for degraded salt failed: %v; falling back to sha256 seed", err)
			seed := append([]byte("cpa-degraded-salt:"), buf[:]...)
			seed = append(seed, strconv.FormatInt(time.Now().UnixNano(), 10)...)
			seed = append(seed, strconv.Itoa(os.Getpid())...)
			sum := sha256.Sum256(seed)
			degradedCallerSalt = hex.EncodeToString(sum[:callerNamespaceHashBytes])
			return
		}
		degradedCallerSalt = hex.EncodeToString(buf[:callerNamespaceHashBytes])
	})
	return degradedCallerSalt
}

// IsolateClientControlledSessionKey namespaces client-controlled session keys by
// the downstream CPA API key so two callers cannot share encrypted reasoning,
// thought signatures, or assistant state by reusing prompt_cache_key / window /
// session headers / sessionId values.
//
// Trusted execution session keys (prefix "execution:") keep their existing form.
//
// Client-controlled sessions without a userApiKey are no longer fully disabled:
// they receive a "degraded:<salt>:" process-level namespace so replay still works
// (issue #8). When the request carries a client IP, the degraded namespace is
// further split by an IP hash so anonymous callers from distinct clients cannot
// share state (review S1). The caller namespace width is 128 bits to avoid
// birthday collisions (issue #11).
func IsolateClientControlledSessionKey(ctx context.Context, sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	if strings.HasPrefix(sessionKey, "execution:") {
		return sessionKey
	}
	apiKey := strings.TrimSpace(APIKeyFromContext(ctx))
	if apiKey == "" {
		// Degraded namespace: process-local random salt. Distinct processes stay
		// isolated; within a process the sessionKey itself still provides
		// isolation, and the client IP hash separates anonymous callers.
		if clientIP := strings.TrimSpace(clientIPFromContext(ctx)); clientIP != "" {
			sum := sha256.Sum256([]byte(clientIP))
			return "degraded:" + loadDegradedCallerSalt() + ":" + hex.EncodeToString(sum[:callerNamespaceHashBytes]) + ":" + sessionKey
		}
		return "degraded:" + loadDegradedCallerSalt() + ":" + sessionKey
	}
	sum := sha256.Sum256([]byte(apiKey))
	return "caller:" + hex.EncodeToString(sum[:callerNamespaceHashBytes]) + ":" + sessionKey
}

// clientIPFromContext returns the peer IP from the gin context, if any.
//
// It deliberately uses the unspoofable peer address (RemoteAddr) instead of
// gin's ClientIP(): the server never configures SetTrustedProxies, so gin
// trusts client-supplied X-Forwarded-For by default, and an anonymous caller
// could otherwise join another client's degraded namespace with a forged
// header (review S1).
func clientIPFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	ginCtx, ok := ctx.Value("gin").(*gin.Context)
	if !ok || ginCtx == nil || ginCtx.Request == nil {
		return ""
	}
	remote := strings.TrimSpace(ginCtx.Request.RemoteAddr)
	if remote == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		// No port present; treat the whole value as the peer address.
		return remote
	}
	return host
}
