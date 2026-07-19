package management

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"gopkg.in/yaml.v3"
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

// restoreRedactedSecrets reverses redactSecretValue/redactProxyURL redaction
// in a YAML management PUT body by substituting original secret values from
// the live config before persistence. Without this, a round-trip through
// GET /v0/management/config -> PUT /v0/management/config.yaml would write
// masked placeholders (e.g., "sk-a...wxyz" or "***") to disk and break every
// provider auth.
//
// The function operates on the YAML AST so comments, ordering, and scalar
// styles are preserved. Only values that match the redacted form of a current
// secret are touched; everything else (including intentionally-new keys) is
// left alone. When disambiguation is impossible (e.g., two providers share
// the same redacted prefix/suffix), the redacted value is left in place
// rather than risk restoring the wrong credential.
func restoreRedactedSecrets(body []byte, current *config.Config) ([]byte, error) {
	if current == nil || len(body) == 0 {
		return body, nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	if root.Kind == 0 || root.Kind != yaml.DocumentNode {
		// Empty document or non-document node: nothing to restore.
		return body, nil
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return body, nil
	}
	restoreSecretsInMapping(root.Content[0], current)
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// restoreSecretsInMapping walks the top-level config mapping and dispatches
// each known secret-bearing key to a specialized restorer.
func restoreSecretsInMapping(rootMap *yaml.Node, current *config.Config) {
	for i := 0; i+1 < len(rootMap.Content); i += 2 {
		key := rootMap.Content[i]
		val := rootMap.Content[i+1]
		if key.Kind != yaml.ScalarNode {
			continue
		}
		switch key.Value {
		case "api-keys":
			restoreStringListSecrets(val, current.APIKeys)
		case "proxy-url":
			restoreProxyURLScalar(val, current.ProxyURL, []string{current.ProxyURL})
		case "tls":
			restoreTLSMapping(val, current.TLS)
		case "openai-compatibility":
			restoreOpenAICompatibilityList(val, current.OpenAICompatibility)
		case "gemini-api-key":
			restoreProviderAPIKeyList(val, extractGeminiSecrets(current.GeminiKey))
		case "interactions-api-key":
			restoreProviderAPIKeyList(val, extractGeminiSecrets(current.InteractionsKey))
		case "codex-api-key":
			restoreProviderAPIKeyList(val, extractCodexSecrets(current.CodexKey))
		case "xai-api-key":
			restoreProviderAPIKeyList(val, extractCodexSecrets(current.XAIKey))
		case "claude-api-key":
			restoreProviderAPIKeyList(val, extractClaudeSecrets(current.ClaudeKey))
		case "vertex-api-key":
			restoreProviderAPIKeyList(val, extractVertexSecrets(current.VertexCompatAPIKey))
		}
	}
}

// providerSecretBundle is the per-entry secret view shared by every provider
// list type so the same restore logic applies uniformly.
type providerSecretBundle struct {
	apiKey   string
	proxyURL string
	headers  map[string]string
}

func extractGeminiSecrets(in []config.GeminiKey) []providerSecretBundle {
	out := make([]providerSecretBundle, 0, len(in))
	for _, k := range in {
		out = append(out, providerSecretBundle{apiKey: k.APIKey, proxyURL: k.ProxyURL, headers: k.Headers})
	}
	return out
}

func extractCodexSecrets(in []config.CodexKey) []providerSecretBundle {
	out := make([]providerSecretBundle, 0, len(in))
	for _, k := range in {
		out = append(out, providerSecretBundle{apiKey: k.APIKey, proxyURL: k.ProxyURL, headers: k.Headers})
	}
	return out
}

func extractClaudeSecrets(in []config.ClaudeKey) []providerSecretBundle {
	out := make([]providerSecretBundle, 0, len(in))
	for _, k := range in {
		out = append(out, providerSecretBundle{apiKey: k.APIKey, proxyURL: k.ProxyURL, headers: k.Headers})
	}
	return out
}

func extractVertexSecrets(in []config.VertexCompatKey) []providerSecretBundle {
	out := make([]providerSecretBundle, 0, len(in))
	for _, k := range in {
		out = append(out, providerSecretBundle{apiKey: k.APIKey, proxyURL: k.ProxyURL, headers: k.Headers})
	}
	return out
}

// restoreProviderAPIKeyList restores api-key/proxy-url/headers for each entry
// of a provider list (claude-api-key, codex-api-key, etc.). Matching is by
// index with a fallback scan when the redacted prefix/suffix uniquely
// identifies a single current secret.
func restoreProviderAPIKeyList(seqNode *yaml.Node, current []providerSecretBundle) {
	if seqNode == nil || seqNode.Kind != yaml.SequenceNode {
		return
	}
	allAPIKeys := make([]string, 0, len(current))
	allProxyURLs := make([]string, 0, len(current))
	for _, c := range current {
		allAPIKeys = append(allAPIKeys, c.apiKey)
		allProxyURLs = append(allProxyURLs, c.proxyURL)
	}
	for i, item := range seqNode.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		var idxAPIKey, idxProxy string
		if i < len(current) {
			idxAPIKey = current[i].apiKey
			idxProxy = current[i].proxyURL
		}
		var idxHeaders map[string]string
		if i < len(current) {
			idxHeaders = current[i].headers
		}
		for j := 0; j+1 < len(item.Content); j += 2 {
			key := item.Content[j]
			val := item.Content[j+1]
			if key.Kind != yaml.ScalarNode {
				continue
			}
			switch key.Value {
			case "api-key":
				restoreSecretScalar(val, idxAPIKey, allAPIKeys)
			case "proxy-url":
				restoreProxyURLScalar(val, idxProxy, allProxyURLs)
			case "headers":
				restoreHeadersMapping(val, idxHeaders)
			}
		}
	}
}

// restoreOpenAICompatibilityList restores api-key-entries[*].api-key,
// proxy-url, and headers for each OpenAI-compat provider. Matching is by the
// provider's `name` field so reordering is safe.
func restoreOpenAICompatibilityList(seqNode *yaml.Node, current []config.OpenAICompatibility) {
	if seqNode == nil || seqNode.Kind != yaml.SequenceNode {
		return
	}
	byName := make(map[string]int, len(current))
	for i, p := range current {
		if p.Name != "" {
			byName[strings.ToLower(p.Name)] = i
		}
	}
	for _, item := range seqNode.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		name := findMappingScalar(item, "name")
		if name == "" {
			continue
		}
		idx, ok := byName[strings.ToLower(name)]
		if !ok {
			continue
		}
		cur := current[idx]
		for j := 0; j+1 < len(item.Content); j += 2 {
			key := item.Content[j]
			val := item.Content[j+1]
			if key.Kind != yaml.ScalarNode {
				continue
			}
			switch key.Value {
			case "api-key-entries":
				bundles := make([]providerSecretBundle, 0, len(cur.APIKeyEntries))
				for _, e := range cur.APIKeyEntries {
					bundles = append(bundles, providerSecretBundle{apiKey: e.APIKey, proxyURL: e.ProxyURL})
				}
				restoreProviderAPIKeyList(val, bundles)
			case "headers":
				restoreHeadersMapping(val, cur.Headers)
			}
		}
	}
}

// restoreStringListSecrets restores a top-level string list (e.g., api-keys)
// where every item is redacted via redactSecretValue.
func restoreStringListSecrets(seqNode *yaml.Node, current []string) {
	if seqNode == nil || seqNode.Kind != yaml.SequenceNode {
		return
	}
	for i, item := range seqNode.Content {
		if item.Kind != yaml.ScalarNode {
			continue
		}
		idxVal := ""
		if i < len(current) {
			idxVal = current[i]
		}
		restoreSecretScalar(item, idxVal, current)
	}
}

// restoreTLSMapping restores the TLS private key (yaml field "key") when it
// was redacted via redactSecretValue because it looked like PEM material.
func restoreTLSMapping(tlsNode *yaml.Node, current config.TLSConfig) {
	if tlsNode == nil || tlsNode.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(tlsNode.Content); i += 2 {
		key := tlsNode.Content[i]
		val := tlsNode.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Value != "key" {
			continue
		}
		if val.Kind != yaml.ScalarNode {
			continue
		}
		if current.Key == "" || !looksLikePEMPrivateKey(current.Key) {
			continue
		}
		restoreSecretScalar(val, current.Key, []string{current.Key})
	}
}

// restoreSecretScalar detects redacted placeholders in a scalar node and
// restores the original secret. Restoration requires exactly one candidate
// whose redaction matches the incoming value — when multiple candidates share
// the same redaction we cannot safely disambiguate (the body may have been
// reordered) so the redacted value is left in place. The index-aligned value
// is preferred when it satisfies the unique match.
func restoreSecretScalar(valNode *yaml.Node, indexValue string, allCandidates []string) {
	if valNode == nil || valNode.Kind != yaml.ScalarNode {
		return
	}
	incoming := valNode.Value
	if !isRedactedPlaceholder(incoming) {
		return
	}
	matchCount := 0
	for _, cand := range allCandidates {
		if cand == "" {
			continue
		}
		if redactSecretValue(cand) == incoming {
			matchCount++
		}
	}
	if matchCount != 1 {
		return
	}
	if indexValue != "" && redactSecretValue(indexValue) == incoming {
		setScalarString(valNode, indexValue)
		return
	}
	for _, cand := range allCandidates {
		if cand != "" && redactSecretValue(cand) == incoming {
			setScalarString(valNode, cand)
			return
		}
	}
}

// restoreProxyURLScalar restores proxy URLs that were redacted via
// redactProxyURL (userinfo stripped). As with restoreSecretScalar, we require
// a unique candidate to avoid restoring the wrong credential when the body
// has been reordered.
func restoreProxyURLScalar(valNode *yaml.Node, indexValue string, allCandidates []string) {
	if valNode == nil || valNode.Kind != yaml.ScalarNode {
		return
	}
	incoming := valNode.Value
	if incoming == "" {
		return
	}
	matchCount := 0
	for _, cand := range allCandidates {
		if cand == "" || cand == incoming {
			continue
		}
		if redactProxyURL(cand) == incoming {
			matchCount++
		}
	}
	if matchCount != 1 {
		return
	}
	if indexValue != "" && indexValue != incoming && redactProxyURL(indexValue) == incoming {
		setScalarString(valNode, indexValue)
		return
	}
	for _, cand := range allCandidates {
		if cand == "" || cand == incoming {
			continue
		}
		if redactProxyURL(cand) == incoming {
			setScalarString(valNode, cand)
			return
		}
	}
}

// restoreHeadersMapping restores sensitive header values that were redacted
// via redactSecretValue. Non-sensitive header names are left untouched.
func restoreHeadersMapping(headersNode *yaml.Node, currentHeaders map[string]string) {
	if headersNode == nil || headersNode.Kind != yaml.MappingNode {
		return
	}
	if len(currentHeaders) == 0 {
		return
	}
	for i := 0; i+1 < len(headersNode.Content); i += 2 {
		keyNode := headersNode.Content[i]
		valNode := headersNode.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode || valNode.Kind != yaml.ScalarNode {
			continue
		}
		if !isSensitiveHeaderName(keyNode.Value) {
			continue
		}
		incoming := valNode.Value
		if !isRedactedPlaceholder(incoming) {
			continue
		}
		currentVal, ok := currentHeaders[keyNode.Value]
		if !ok {
			for k, v := range currentHeaders {
				if strings.EqualFold(k, keyNode.Value) {
					currentVal = v
					ok = true
					break
				}
			}
		}
		if !ok || currentVal == "" {
			continue
		}
		if redactSecretValue(currentVal) == incoming {
			setScalarString(valNode, currentVal)
		}
	}
}

// findMappingScalar returns the scalar value associated with a key in a
// mapping node, or empty string if not found.
func findMappingScalar(mappingNode *yaml.Node, fieldName string) string {
	if mappingNode == nil || mappingNode.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(mappingNode.Content); i += 2 {
		key := mappingNode.Content[i]
		val := mappingNode.Content[i+1]
		if key.Kind == yaml.ScalarNode && key.Value == fieldName && val.Kind == yaml.ScalarNode {
			return val.Value
		}
	}
	return ""
}

// isRedactedPlaceholder reports whether s matches the redaction shape
// produced by redactSecretValue: either "***" (for secrets <=8 chars) or a
// 11-byte value of the form "xxxx...yyyy" (for longer secrets).
func isRedactedPlaceholder(s string) bool {
	if s == "***" {
		return true
	}
	return len(s) == 11 && s[4:7] == "..."
}

// setScalarString overwrites a scalar node's value while choosing a style
// that round-trips safely: plain for simple ASCII tokens, literal block style
// for multi-line material (e.g., PEM), and double-quoted for anything else.
func setScalarString(node *yaml.Node, value string) {
	if node == nil {
		return
	}
	node.Value = value
	node.Tag = "!!str"
	switch {
	case isPlainSafeScalar(value):
		node.Style = 0
	case strings.Contains(value, "\n"):
		node.Style = yaml.LiteralStyle
	default:
		node.Style = yaml.DoubleQuotedStyle
	}
}

// isPlainSafeScalar reports whether value can be safely emitted as a plain
// (unquoted) YAML scalar. Conservative: only [A-Za-z0-9._-]+ and not starting
// with a YAML indicator.
func isPlainSafeScalar(s string) bool {
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, "\n\t ") {
		return false
	}
	first := s[0]
	switch first {
	case '!', '&', '*', '-', '?', '|', '>', '\'', '"', '%', '@', '`', '#',
		',', '[', ']', '{', '}', ':':
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '.', c == '_', c == '-':
			// allowed
		default:
			return false
		}
	}
	return true
}
