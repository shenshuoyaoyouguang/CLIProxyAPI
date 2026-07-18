package management

import (
	"encoding/json"
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
			case "api-key", "api_key", "apikey":
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
			}
			redactJSONSecrets(child)
		}
	case []any:
		for _, item := range value {
			redactJSONSecrets(item)
		}
	}
}
