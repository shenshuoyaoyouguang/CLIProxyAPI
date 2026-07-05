package helps

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
)

type idempotencyKeyType struct{}

var idempotencyKey = idempotencyKeyType{}

// IdempotencyKey is a unique identifier for a request that persists across retries.
type IdempotencyKey string

// ContextWithIdempotencyKey stores an idempotency key in the context.
func ContextWithIdempotencyKey(ctx context.Context, key IdempotencyKey) context.Context {
	return context.WithValue(ctx, idempotencyKey, key)
}

// IdempotencyKeyFromContext retrieves the idempotency key from the context.
func IdempotencyKeyFromContext(ctx context.Context) IdempotencyKey {
	if v, ok := ctx.Value(idempotencyKey).(IdempotencyKey); ok {
		return v
	}
	return ""
}

// GenerateIdempotencyKey creates a new idempotency key with a "cpa-" prefix.
// If an existing key is in the context, it is reused.
func GenerateIdempotencyKey(ctx context.Context) IdempotencyKey {
	if existing := IdempotencyKeyFromContext(ctx); existing != "" {
		return existing
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return IdempotencyKey(fmt.Sprintf("cpa-%x", b))
}

// ApplyIdempotencyKey sets the X-Idempotency-Key header on an HTTP request.
func ApplyIdempotencyKey(req *http.Request, key IdempotencyKey) {
	if key != "" && req != nil {
		req.Header.Set("X-Idempotency-Key", string(key))
	}
}

// IdempotencyKeyFromRequest reads the X-Idempotency-Key header from an HTTP request.
func IdempotencyKeyFromRequest(req *http.Request) IdempotencyKey {
	if req == nil {
		return ""
	}
	return IdempotencyKey(req.Header.Get("X-Idempotency-Key"))
}
