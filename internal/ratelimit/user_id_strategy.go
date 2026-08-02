package ratelimit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
)

// UserIDResolver resolves a stable DeepSeek "user" value for governance.
type UserIDResolver struct {
	strategy      UserIDStrategy
	headerName    string
	fixedValue    string
	prefix        string
	credentialMap sync.Map // credentialID -> userID
}

// UserIDStrategy selects how user ids are derived.
type UserIDStrategy string

const (
	StrategyAutoHash      UserIDStrategy = "auto_hash"
	StrategyHeader        UserIDStrategy = "header"
	StrategyFixed         UserIDStrategy = "fixed"
	StrategyPerCredential UserIDStrategy = "per_credential"
)

// ParseUserIDStrategy maps a config string to a strategy (default per_credential).
func ParseUserIDStrategy(s string) UserIDStrategy {
	switch UserIDStrategy(strings.ToLower(strings.TrimSpace(s))) {
	case StrategyAutoHash:
		return StrategyAutoHash
	case StrategyHeader:
		return StrategyHeader
	case StrategyFixed:
		return StrategyFixed
	case StrategyPerCredential:
		return StrategyPerCredential
	default:
		return StrategyPerCredential
	}
}

// NewUserIDResolver creates a resolver.
func NewUserIDResolver(strategy UserIDStrategy, headerName, fixedValue, prefix string) *UserIDResolver {
	if headerName == "" {
		headerName = "X-DeepSeek-User-ID"
	}
	if prefix == "" {
		prefix = "cliproxy"
	}
	return &UserIDResolver{
		strategy:   strategy,
		headerName: headerName,
		fixedValue: fixedValue,
		prefix:     prefix,
	}
}

// Resolve derives a user id from headers and credential identity.
func (r *UserIDResolver) Resolve(ctx context.Context, req *http.Request, header http.Header, credentialID, apiKey string) string {
	_ = ctx
	if r == nil {
		return ""
	}
	switch r.strategy {
	case StrategyHeader:
		if v := headerValue(req, header, r.headerName); v != "" {
			return r.sanitize(v)
		}
		fallthrough // empty header falls back to auto_hash
	case StrategyAutoHash:
		return r.hashBased(apiKey)
	case StrategyFixed:
		if r.fixedValue != "" {
			return r.sanitize(r.fixedValue)
		}
		return r.hashBased(apiKey)
	case StrategyPerCredential:
		if credentialID != "" {
			if v, ok := r.credentialMap.Load(credentialID); ok {
				return v.(string)
			}
			userID := r.prefix + "-" + shortHash(credentialID)
			r.credentialMap.Store(credentialID, userID)
			return userID
		}
		return r.hashBased(apiKey)
	default:
		return r.hashBased(apiKey)
	}
}

func headerValue(req *http.Request, header http.Header, name string) string {
	if name == "" {
		return ""
	}
	if header != nil {
		if v := strings.TrimSpace(header.Get(name)); v != "" {
			return v
		}
	}
	if req != nil {
		return strings.TrimSpace(req.Header.Get(name))
	}
	return ""
}

func shortHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:12]
}

func (r *UserIDResolver) hashBased(key string) string {
	if key == "" {
		return r.prefix + "-anonymous"
	}
	return r.prefix + "-" + shortHash(key)
}

func (r *UserIDResolver) sanitize(v string) string {
	v = strings.TrimSpace(v)
	var b strings.Builder
	for _, c := range v {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			b.WriteRune(c)
		}
	}
	result := b.String()
	if result == "" {
		return r.prefix + "-invalid"
	}
	if len(result) > 512 {
		result = result[:512]
	}
	return result
}

// SetCredentialMapping manually maps credentialID -> userID.
func (r *UserIDResolver) SetCredentialMapping(credentialID, userID string) {
	if r == nil || credentialID == "" {
		return
	}
	r.credentialMap.Store(credentialID, r.sanitize(userID))
}

// GetCredentialMapping returns a manual mapping if present.
func (r *UserIDResolver) GetCredentialMapping(credentialID string) (string, bool) {
	if r == nil {
		return "", false
	}
	v, ok := r.credentialMap.Load(credentialID)
	if !ok {
		return "", false
	}
	return v.(string), true
}
