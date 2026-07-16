package util

import (
	"net/http"
	"strings"
)

// ApplyCustomHeadersFromAttrs applies user-defined headers stored in the provided attributes map.
// Custom headers override built-in defaults when conflicts occur.
func ApplyCustomHeadersFromAttrs(r *http.Request, attrs map[string]string) {
	if r == nil {
		return
	}
	applyCustomHeaders(r, extractCustomHeaders(attrs))
}

func extractCustomHeaders(attrs map[string]string) map[string]string {
	if len(attrs) == 0 {
		return nil
	}
	headers := make(map[string]string)
	for k, v := range attrs {
		if !strings.HasPrefix(k, "header:") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(k, "header:"))
		if name == "" {
			continue
		}
		val := strings.TrimSpace(v)
		if val == "" {
			continue
		}
		headers[name] = val
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

// sensitiveHeadersBlacklist 是禁止通过 auth.Attributes 注入的敏感头黑名单。
// 这些头与凭证、身份、来源相关，由 executor 显式控制，不允许被覆盖。
var sensitiveHeadersBlacklist = map[string]bool{
	"Authorization":       true,
	"Host":                true,
	"Cookie":              true,
	"Set-Cookie":          true,
	"X-Forwarded-For":     true,
	"X-Forwarded-Host":    true,
	"X-Forwarded-Proto":   true,
	"X-Real-Ip":           true,
	"Proxy-Authorization": true,
	"Proxy-Connection":    true,
}

func applyCustomHeaders(r *http.Request, headers map[string]string) {
	if r == nil || len(headers) == 0 {
		return
	}
	for k, v := range headers {
		if k == "" || v == "" {
			continue
		}
		canonical := http.CanonicalHeaderKey(k)
		if sensitiveHeadersBlacklist[canonical] {
			// 敏感头不允许通过自定义头注入，跳过
			continue
		}
		r.Header.Set(k, v)
	}
}
