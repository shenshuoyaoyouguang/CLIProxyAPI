package management

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// redactSecretValue masks a secret for management list/overview responses.
// Empty values stay empty; short values become "***"; longer values keep a
// small prefix/suffix so operators can still identify which key is configured
// without exposing the full credential.
func redactSecretValue(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	if len(secret) <= 8 {
		return "***"
	}
	return secret[:4] + "..." + secret[len(secret)-4:]
}

// redactProxyURL removes userinfo (username/password) from a proxy URL while
// keeping scheme/host/port/path so operators can still see which proxy is set.
// Non-URL strings fall back to redactSecretValue.
func redactProxyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		// Opaque or malformed values may still embed credentials.
		return redactSecretValue(raw)
	}
	if parsed.User != nil {
		parsed.User = nil
	}
	return parsed.String()
}

// isSensitiveHeaderName reports whether a header name typically carries secrets.
func isSensitiveHeaderName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "authorization", "proxy-authorization", "cookie", "set-cookie",
		"x-api-key", "x-auth-token", "x-access-token", "api-key", "apikey":
		return true
	default:
		return strings.Contains(name, "token") ||
			strings.Contains(name, "secret") ||
			strings.Contains(name, "password") ||
			strings.Contains(name, "auth") ||
			strings.Contains(name, "cookie")
	}
}

// looksLikePEMPrivateKey detects PEM private-key material that may appear under
// generic field names such as tls.key.
func looksLikePEMPrivateKey(value string) bool {
	upper := strings.ToUpper(value)
	return strings.Contains(upper, "PRIVATE KEY") ||
		strings.Contains(upper, "BEGIN RSA PRIVATE") ||
		strings.Contains(upper, "BEGIN EC PRIVATE") ||
		strings.Contains(upper, "BEGIN OPENSSH PRIVATE")
}

// marshalConfigForManagementJSON returns a JSON snapshot of cfg with provider
// and proxy client secrets redacted. Dedicated key-management endpoints remain
// the only paths that return full secret values.
func marshalConfigForManagementJSON(cfg *config.Config) ([]byte, error) {
	if cfg == nil {
		return []byte("{}"), nil
	}
	// Encode then decode so we do not mutate the live *config.Config and do not
	// need a full deep-copy of every nested map/slice type.
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var doc any
	if err = json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	redactJSONSecrets(doc)
	return json.Marshal(doc)
}

func redactJSONSecrets(node any) {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			switch strings.ToLower(key) {
			case "api-key", "api_key", "apikey",
				"password", "secret", "token",
				"access-token", "access_token",
				"refresh-token", "refresh_token",
				"client-secret", "client_secret",
				"private-key", "private_key":
				if secret, ok := child.(string); ok {
					value[key] = redactSecretValue(secret)
					continue
				}
			case "api-keys", "api_keys":
				if list, ok := child.([]any); ok {
					for i, item := range list {
						if secret, okItem := item.(string); okItem {
							list[i] = redactSecretValue(secret)
						} else {
							redactJSONSecrets(item)
						}
					}
					continue
				}
			case "api-key-entries", "api_key_entries":
				redactJSONSecrets(child)
				continue
			case "proxy-url", "proxy_url":
				if proxy, ok := child.(string); ok {
					value[key] = redactProxyURL(proxy)
					continue
				}
			case "headers":
				if headers, ok := child.(map[string]any); ok {
					redactHeaderMap(headers)
					continue
				}
			case "key":
				// TLS and similar configs use a generic "key" field for private material.
				if secret, ok := child.(string); ok && looksLikePEMPrivateKey(secret) {
					value[key] = redactSecretValue(secret)
					continue
				}
			}
			redactJSONSecrets(child)
		}
	case []any:
		for _, item := range value {
			redactJSONSecrets(item)
		}
	}
}

func redactHeaderMap(headers map[string]any) {
	for name, raw := range headers {
		secret, ok := raw.(string)
		if !ok {
			redactJSONSecrets(raw)
			continue
		}
		if isSensitiveHeaderName(name) {
			headers[name] = redactSecretValue(secret)
		}
	}
}
