// Package thinking provides unified thinking configuration processing.
package thinking

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

// normalizeProviderFamily returns the canonical family key for a provider or
// model-type name. Providers that reuse the same wire conventions group into a
// single family so cross-family conversions are detected consistently:
//   - "gemini": gemini, antigravity
//   - "openai": openai, openai-response, codex, deepseek, nvidia
//
// Any other name (claude, kimi, xai, ...) is its own family.
func normalizeProviderFamily(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "gemini", "antigravity":
		return "gemini"
	case "openai", "openai-response", "codex", "deepseek", "nvidia":
		return "openai"
	default:
		return name
	}
}

// providerFamily returns the canonical family of a model served on the given wire
// format. The model's registered Type is authoritative when present, so providers
// that reuse another protocol on the wire (e.g. Kimi serving Claude-compatible
// /v1/messages) resolve to their true family instead of the wire format. The wire
// format is used only as a fallback for models with no registered type.
func providerFamily(modelInfo *registry.ModelInfo, wireFormat string) string {
	if modelInfo != nil {
		if t := strings.ToLower(strings.TrimSpace(modelInfo.Type)); t != "" {
			return normalizeProviderFamily(t)
		}
	}
	return normalizeProviderFamily(wireFormat)
}

// isSameProviderFamily reports whether two provider or model-type names belong to
// the same family (see normalizeProviderFamily).
func isSameProviderFamily(from, to string) bool {
	return normalizeProviderFamily(from) == normalizeProviderFamily(to)
}
