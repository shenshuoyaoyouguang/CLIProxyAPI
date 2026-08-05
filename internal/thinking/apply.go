// Package thinking provides unified thinking configuration processing.
package thinking

import (
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

type pluginProviderApplier struct {
	owner    string
	priority int
	applier  ProviderApplier
}

var providerAppliersMu sync.RWMutex

// nativeProviderAppliers maps built-in provider names to their implementations.
var nativeProviderAppliers = map[string]ProviderApplier{
	"gemini":      nil,
	"claude":      nil,
	"openai":      nil,
	"codex":       nil,
	"antigravity": nil,
	"kimi":        nil,
	"xai":         nil,
	"deepseek":    nil,
	"nvidia":      nil,
}

// pluginProviderAppliers maps plugin-owned provider names to their implementations.
var pluginProviderAppliers = map[string]pluginProviderApplier{}

// GetProviderApplier returns the ProviderApplier for the given provider name.
// Returns nil if the provider is not registered.
func GetProviderApplier(provider string) ProviderApplier {
	provider = normalizedProviderName(provider)
	if provider == "" {
		return nil
	}
	providerAppliersMu.RLock()
	defer providerAppliersMu.RUnlock()
	if nativeApplier, okNative := nativeProviderAppliers[provider]; okNative {
		return nativeApplier
	}
	return pluginProviderAppliers[provider].applier
}

// RegisterProvider registers a provider applier by name.
//
// Native registrations always win over plugin registrations at lookup time
// (see GetProvider). Shadowing a plugin-owned applier is therefore a silent
// behaviour change for that plugin, so it is reported explicitly.
func RegisterProvider(name string, applier ProviderApplier) {
	name = normalizedProviderName(name)
	if name == "" {
		return
	}
	providerAppliersMu.Lock()
	defer providerAppliersMu.Unlock()
	if plugin, shadowed := pluginProviderAppliers[name]; shadowed {
		log.Warnf("thinking: native provider applier %q shadows plugin applier owned by %q; the plugin applier will no longer be used", name, plugin.owner)
	}
	nativeProviderAppliers[name] = applier
}

// RegisterPluginProvider registers a plugin-owned provider applier.
func RegisterPluginProvider(owner string, name string, priority int, applier ProviderApplier) bool {
	owner = strings.TrimSpace(owner)
	name = normalizedProviderName(name)
	if owner == "" || name == "" || applier == nil {
		return false
	}
	providerAppliersMu.Lock()
	defer providerAppliersMu.Unlock()
	if _, native := nativeProviderAppliers[name]; native {
		return false
	}
	current, exists := pluginProviderAppliers[name]
	if exists && (current.priority > priority || (current.priority == priority && current.owner <= owner)) {
		return false
	}
	pluginProviderAppliers[name] = pluginProviderApplier{
		owner:    owner,
		priority: priority,
		applier:  applier,
	}
	return true
}

// UnregisterPluginProviders removes all provider appliers and extractors owned by one plugin.
func UnregisterPluginProviders(owner string) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return
	}
	providerAppliersMu.Lock()
	defer providerAppliersMu.Unlock()
	for provider, record := range pluginProviderAppliers {
		if record.owner == owner {
			delete(pluginProviderAppliers, provider)
		}
	}
	for provider, record := range pluginProviderExtractors {
		if record.owner == owner {
			delete(pluginProviderExtractors, provider)
		}
	}
}

// ClearPluginProviders removes all plugin-owned provider appliers.
func ClearPluginProviders() {
	providerAppliersMu.Lock()
	defer providerAppliersMu.Unlock()
	pluginProviderAppliers = map[string]pluginProviderApplier{}
	pluginProviderExtractors = map[string]pluginProviderExtractor{}
}

type pluginProviderExtractor struct {
	owner     string
	priority  int
	extractor func(body []byte) ThinkingConfig
}

// nativeProviderExtractors maps built-in provider names to their body config extractors.
var nativeProviderExtractors = map[string]func(body []byte) ThinkingConfig{
	"claude":       extractClaudeConfig,
	"gemini":       func(body []byte) ThinkingConfig { return extractGeminiConfig(body, "gemini") },
	"antigravity":  func(body []byte) ThinkingConfig { return extractGeminiConfig(body, "antigravity") },
	"interactions": extractInteractionsConfig,
	"openai":       extractOpenAIConfig,
	"codex":        extractCodexConfig,
	"xai":          extractCodexConfig,
	"deepseek":     extractDeepSeekConfig,
	"kimi":         extractKimiConfig,
	"nvidia":       extractNvidiaConfig,
	"zai":          extractOpenAIConfig,
}

// pluginProviderExtractors maps plugin-owned provider names to their body config extractors.
var pluginProviderExtractors = map[string]pluginProviderExtractor{}

// GetProviderExtractor returns the body config extractor for the given provider name.
// Returns nil if the provider is not registered.
func GetProviderExtractor(provider string) func(body []byte) ThinkingConfig {
	provider = normalizedProviderName(provider)
	if provider == "" {
		return nil
	}
	providerAppliersMu.RLock()
	defer providerAppliersMu.RUnlock()
	if nativeExtractor, okNative := nativeProviderExtractors[provider]; okNative {
		return nativeExtractor
	}
	return pluginProviderExtractors[provider].extractor
}

// RegisterProviderExtractor registers a body config extractor by provider name.
//
// Native registrations always win over plugin registrations at lookup time
// (see GetProviderExtractor). Shadowing a plugin-owned extractor is therefore a
// silent behaviour change for that plugin, so it is reported explicitly.
func RegisterProviderExtractor(name string, extractor func(body []byte) ThinkingConfig) {
	name = normalizedProviderName(name)
	if name == "" || extractor == nil {
		return
	}
	providerAppliersMu.Lock()
	defer providerAppliersMu.Unlock()
	if plugin, shadowed := pluginProviderExtractors[name]; shadowed {
		log.Warnf("thinking: native provider extractor %q shadows plugin extractor owned by %q; the plugin extractor will no longer be used", name, plugin.owner)
	}
	nativeProviderExtractors[name] = extractor
}

// RegisterPluginProviderExtractor registers a plugin-owned body config extractor.
func RegisterPluginProviderExtractor(owner string, name string, priority int, extractor func(body []byte) ThinkingConfig) bool {
	owner = strings.TrimSpace(owner)
	name = normalizedProviderName(name)
	if owner == "" || name == "" || extractor == nil {
		return false
	}
	providerAppliersMu.Lock()
	defer providerAppliersMu.Unlock()
	if _, native := nativeProviderExtractors[name]; native {
		return false
	}
	current, exists := pluginProviderExtractors[name]
	if exists && (current.priority > priority || (current.priority == priority && current.owner <= owner)) {
		return false
	}
	pluginProviderExtractors[name] = pluginProviderExtractor{
		owner:     owner,
		priority:  priority,
		extractor: extractor,
	}
	return true
}

func normalizedProviderName(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

// IsUserDefinedModel reports whether the model is a user-defined model that should
// have thinking configuration passed through without validation.
//
// User-defined models are configured via config file's models[] array
// (e.g., openai-compatibility.*.models[], *-api-key.models[]). These models
// are marked with UserDefined=true at registration time.
//
// User-defined models should have their thinking configuration applied directly,
// letting the upstream service validate the configuration.
func IsUserDefinedModel(modelInfo *registry.ModelInfo) bool {
	if modelInfo == nil {
		return true
	}
	return modelInfo.UserDefined
}

// ApplyThinking applies thinking configuration to a request body.
//
// This is the unified entry point for all providers. It follows the processing
// order defined in FR25: route check → model capability query → config extraction
// → validation → application.
//
// Suffix Priority: When the model name includes a thinking suffix (e.g., "gemini-2.5-pro(8192)"),
// the suffix configuration takes priority over any thinking parameters in the request body.
// This enables users to override thinking settings via the model name without modifying their
// request payload.
//
// Parameters:
//   - body: Original request body JSON
//   - model: Model name, optionally with thinking suffix (e.g., "claude-sonnet-4-5(16384)")
//   - fromFormat: Source request format (e.g., openai, codex, gemini)
//   - toFormat: Target provider format for the request body (gemini, antigravity, claude, openai, codex, kimi, xai)
//   - providerKey: Provider identifier used for registry model lookups (may differ from toFormat, e.g., openrouter -> openai)
//
// Returns:
//   - Modified request body JSON with thinking configuration applied
//   - Error if validation fails (ThinkingError). On error, the original body
//     is returned (not nil) to enable defensive programming patterns.
//
// Passthrough behavior (returns original body without error):
//   - Unknown provider (not in providerAppliers map)
//   - modelInfo.Thinking is nil (model doesn't support thinking)
//
// Note: Unknown models (modelInfo is nil) are treated as user-defined models: we skip
// validation and still apply the thinking config so the upstream can validate it.
//
// Example:
//
//	// With suffix - suffix config takes priority
//	result, err := thinking.ApplyThinking(body, "gemini-2.5-pro(8192)", "gemini", "gemini", "gemini")
//
//	// Without suffix - uses body config
//	result, err := thinking.ApplyThinking(body, "gemini-2.5-pro", "gemini", "gemini", "gemini")
func ApplyThinking(body []byte, model string, fromFormat string, toFormat string, providerKey string) ([]byte, error) {
	summaryConfig := ExtractSummaryConfig(body, toFormat)
	return applyThinking(body, nil, model, fromFormat, toFormat, providerKey, nil, false, summaryConfig)
}

// ApplyThinkingWithSummary applies canonical thinking effort while preserving
// summary visibility extracted from the original source request. Callers that
// translate before applying thinking must pass the source config explicitly:
// a target Claude body can temporarily lack display while disabled thinking is
// being rewritten by a model suffix.
func ApplyThinkingWithSummary(body []byte, model string, fromFormat string, toFormat string, providerKey string, summaryConfig SummaryConfig) ([]byte, error) {
	return applyThinking(body, nil, model, fromFormat, toFormat, providerKey, nil, false, summaryConfig)
}

// ApplyThinkingWithModelInfo applies thinking with the exact configured model
// definition selected for an API-key execution attempt while preserving summary
// visibility from the original source body.
func ApplyThinkingWithModelInfo(body, sourceBody []byte, model string, fromFormat string, toFormat string, providerKey string, modelInfo *registry.ModelInfo) ([]byte, error) {
	summaryConfig := ExtractSummaryConfig(sourceBody, fromFormat)
	// The source body is authoritative, but a summary intent produced by the
	// translation into the target body (e.g. a normalizer that materialized the
	// visibility field) must not be discarded when the source carries none.
	if summaryConfig.Mode == SummaryUnspecified {
		summaryConfig = ExtractSummaryConfig(body, toFormat)
	}
	return ApplyThinkingWithModelInfoAndSummary(body, sourceBody, model, fromFormat, toFormat, providerKey, modelInfo, summaryConfig)
}

// ApplyThinkingWithModelInfoAndSummary applies the exact configured model
// definition with a summary intent already resolved across source translation
// and plugin normalization.
func ApplyThinkingWithModelInfoAndSummary(body, sourceBody []byte, model string, fromFormat string, toFormat string, providerKey string, modelInfo *registry.ModelInfo, summaryConfig SummaryConfig) ([]byte, error) {
	return applyThinking(body, sourceBody, model, fromFormat, toFormat, providerKey, modelInfo, true, summaryConfig)
}

func applyThinking(body, sourceBody []byte, model string, fromFormat string, toFormat string, providerKey string, resolvedModelInfo *registry.ModelInfo, modelInfoResolved bool, summaryConfig SummaryConfig) ([]byte, error) {
	providerFormat := strings.ToLower(strings.TrimSpace(toFormat))
	if modelInfoResolved && providerFormat == "openai-response" {
		providerFormat = "codex"
	}
	providerKey = strings.ToLower(strings.TrimSpace(providerKey))
	if providerKey == "" {
		providerKey = providerFormat
	}
	fromFormat = strings.ToLower(strings.TrimSpace(fromFormat))
	if fromFormat == "" {
		fromFormat = providerFormat
	}
	// Summary visibility is orthogonal to thinking effort. Keep the original
	// source intent before a suffix-specific applier rewrites provider fields,
	// then restore it after the canonical effort has been applied.
	// 1. Route check: Get provider applier
	applier := GetProviderApplier(providerFormat)
	if applier == nil {
		log.WithFields(log.Fields{
			"provider": providerFormat,
			"model":    model,
		}).Debug("thinking: unknown provider, passthrough |")
		return body, nil
	}

	// 2. Parse suffix and get modelInfo
	suffixResult := ParseSuffix(model)
	baseModel := suffixResult.ModelName
	// Use provider-specific lookup to handle capability differences across providers.
	modelInfo := resolvedModelInfo
	if !modelInfoResolved {
		modelInfo = registry.LookupModelInfo(baseModel, providerKey)
	}

	// 3. Model capability check
	// Unknown models are treated as user-defined so thinking config can still be applied.
	// The upstream service is responsible for validating the configuration.
	if IsUserDefinedModel(modelInfo) {
		return applyUserDefinedModel(body, modelInfo, fromFormat, providerFormat, providerKey, suffixResult, summaryConfig)
	}
	if modelInfo.Thinking == nil {
		config := extractThinkingConfig(body, providerFormat)
		if hasThinkingConfig(config) {
			// Registry has no thinking block for this model, but the request
			// carries explicit thinking effort (reasoning_effort, thinking.*,
			// enable_thinking, etc). Don't unilaterally strip — pass through
			// to the upstream provider for裁决 (mirrors user-defined model
			// behavior, where the upstream is authoritative).
			log.WithFields(log.Fields{
				"model":    baseModel,
				"provider": providerFormat,
			}).Debug("thinking: registry missing thinking block, passing user intent through |")
			return applyUserDefinedModel(body, modelInfo, fromFormat, providerFormat, providerKey, suffixResult, summaryConfig)
		}
		// Pure summary-visibility control (no thinking effort). Strip so a
		// non-thinking upstream doesn't reject an includeThoughts / display
		// field it never asked for.
		if summaryConfig.Mode != SummaryUnspecified {
			log.WithFields(log.Fields{
				"model":    baseModel,
				"provider": providerFormat,
			}).Debug("thinking: model does not support thinking, stripping summary-only config |")
			return StripThinkingConfig(body, providerFormat), nil
		}
		log.WithFields(log.Fields{
			"provider": providerFormat,
			"model":    baseModel,
		}).Debug("thinking: model does not support thinking, passthrough |")
		return body, nil
	}

	// 4. Get config: suffix priority over body
	var config ThinkingConfig
	if suffixResult.HasSuffix {
		config = parseSuffixToConfig(suffixResult.RawSuffix, providerFormat, model)
		log.WithFields(log.Fields{
			"provider": providerFormat,
			"model":    model,
			"mode":     config.Mode,
			"budget":   config.Budget,
			"level":    config.Level,
		}).Debug("thinking: config from model suffix |")
	} else {
		if modelInfoResolved && len(sourceBody) > 0 {
			config = extractSourceThinkingConfig(sourceBody, fromFormat)
		}
		if !hasThinkingConfig(config) {
			config = extractThinkingConfig(body, providerFormat)
		}
		if hasThinkingConfig(config) {
			log.WithFields(log.Fields{
				"provider": providerFormat,
				"model":    modelInfo.ID,
				"mode":     config.Mode,
				"budget":   config.Budget,
				"level":    config.Level,
			}).Debug("thinking: original config from request |")
		}
	}

	if !hasThinkingConfig(config) {
		log.WithFields(log.Fields{
			"provider": providerFormat,
			"model":    modelInfo.ID,
		}).Debug("thinking: no config found, passthrough |")
		if modelInfoResolved && providerFormat == "claude" && fromFormat != providerFormat && ExtractSummaryConfig(sourceBody, fromFormat).Mode == SummaryEnabled {
			// Registry translation can only see aggregate model capabilities. For a
			// cross-protocol summary-only request it may have activated adaptive
			// thinking solely to make display valid. The selected API-key model is
			// authoritative at execution time, so discard that inferred activation
			// when the exact model supports only manual extended thinking. Use the
			// source intent here even if a target normalizer removed display; in that
			// case the inferred amount must disappear with it. Explicit native Claude
			// thinking never reaches this cross-protocol branch.
			body = stripInferredClaudeSummaryActivation(body, modelInfo)
		}
		return applySummaryConfigForProvider(body, providerFormat, baseModel, providerKey, modelInfo, summaryConfig), nil
	}
	if modelInfoResolved && config.Mode == ModeLevel && modelInfo != nil && modelInfo.Thinking != nil && shouldMapConfiguredHighIntent(fromFormat, providerFormat, modelInfo) {
		config.Level = mapConfiguredHighIntent(config.Level, modelInfo)
	}

	// 5. Validate and normalize configuration
	validated, err := ValidateConfig(config, modelInfo, fromFormat, providerFormat, suffixResult.HasSuffix)
	if err != nil {
		log.WithFields(log.Fields{
			"provider": providerFormat,
			"model":    modelInfo.ID,
			"error":    err.Error(),
		}).Warn("thinking: validation failed |")
		// Return original body on validation failure (defensive programming).
		// This ensures callers who ignore the error won't receive nil body.
		// The upstream service will decide how to handle the unmodified request.
		return body, err
	}

	// Defensive check: ValidateConfig should never return (nil, nil)
	if validated == nil {
		log.WithFields(log.Fields{
			"provider": providerFormat,
			"model":    modelInfo.ID,
		}).Warn("thinking: ValidateConfig returned nil config without error, passthrough |")
		return body, nil
	}

	log.WithFields(log.Fields{
		"provider": providerFormat,
		"model":    modelInfo.ID,
		"mode":     validated.Mode,
		"budget":   validated.Budget,
		"level":    validated.Level,
	}).Debug("thinking: processed config to apply |")

	// 6. Apply configuration using provider-specific applier, then restore the
	// target summary intent that was explicit before suffix processing.
	applied, err := applier.Apply(body, *validated, modelInfo)
	if err != nil {
		return applied, err
	}
	// A fully disabled amount takes precedence over visibility. Re-applying a
	// summary-only field can recreate an otherwise removed provider config and
	// make a default-on model think again.
	if thinkingIsFullyDisabled(*validated) {
		return applied, nil
	}
	return applySummaryConfigForProvider(applied, providerFormat, baseModel, providerKey, modelInfo, summaryConfig), nil
}

func thinkingIsFullyDisabled(config ThinkingConfig) bool {
	return config.Mode == ModeNone && config.Budget == 0 && config.Level == ""
}

func shouldMapConfiguredHighIntent(fromFormat, toFormat string, modelInfo *registry.ModelInfo) bool {
	fromFormat = strings.ToLower(strings.TrimSpace(fromFormat))
	toFormat = strings.ToLower(strings.TrimSpace(toFormat))
	// Different wire formats (e.g. openai-response vs codex) carry different
	// effort vocabularies, so high intent maps to the closest supported level
	// even when both belong to the same provider family. Identical wire formats
	// keep the explicit level authoritative unless the model family differs.
	if fromFormat != toFormat {
		return true
	}
	// A model whose family differs from the wire format (e.g. a non-Claude model
	// served over the Claude wire) maps high intent to the closest supported level.
	// providerFamily falls back to the wire format when the model type is unknown,
	// in which case the families match and no mapping is needed.
	return providerFamily(modelInfo, toFormat) != normalizeProviderFamily(toFormat)
}

func mapConfiguredHighIntent(level ThinkingLevel, modelInfo *registry.ModelInfo) ThinkingLevel {
	if modelInfo == nil || modelInfo.Thinking == nil || len(modelInfo.Thinking.Levels) == 0 {
		return level
	}
	level = ThinkingLevel(strings.ToLower(strings.TrimSpace(string(level))))
	var candidates []ThinkingLevel
	switch level {
	case LevelXHigh:
		candidates = []ThinkingLevel{LevelXHigh, LevelMax, LevelHigh}
	case LevelMax:
		candidates = []ThinkingLevel{LevelMax, LevelXHigh, LevelHigh}
	default:
		return level
	}
	for _, candidate := range candidates {
		if isLevelSupported(string(candidate), modelInfo.Thinking.Levels) {
			return candidate
		}
	}
	return level
}

func extractSourceThinkingConfig(body []byte, provider string) ThinkingConfig {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "openai-response" {
		return extractCodexConfig(body)
	}
	return extractThinkingConfig(body, provider)
}

// parseSuffixToConfig converts a raw suffix string to ThinkingConfig.
//
// Parsing priority:
//  1. Special values: "none" → ModeNone, "auto"/"-1" → ModeAuto
//  2. Level names: "minimal", "low", "medium", "high", "xhigh" → ModeLevel
//  3. Numeric values: positive integers → ModeBudget, 0 → ModeNone
//
// If none of the above match, returns empty ThinkingConfig (treated as no config).
func parseSuffixToConfig(rawSuffix, provider, model string) ThinkingConfig {
	// 1. Try special values first (none, auto, -1)
	if mode, ok := ParseSpecialSuffix(rawSuffix); ok {
		switch mode {
		case ModeNone:
			return ThinkingConfig{Mode: ModeNone, Budget: 0}
		case ModeAuto:
			return ThinkingConfig{Mode: ModeAuto, Budget: -1}
		}
	}

	// 2. Try level parsing (minimal, low, medium, high, xhigh)
	if level, ok := ParseLevelSuffix(rawSuffix); ok {
		return ThinkingConfig{Mode: ModeLevel, Level: level}
	}

	// 3. Try numeric parsing
	if budget, ok := ParseNumericSuffix(rawSuffix); ok {
		if budget == 0 {
			return ThinkingConfig{Mode: ModeNone, Budget: 0}
		}
		return ThinkingConfig{Mode: ModeBudget, Budget: budget}
	}

	// Unknown suffix format - return empty config
	log.WithFields(log.Fields{
		"provider":   provider,
		"model":      model,
		"raw_suffix": rawSuffix,
	}).Debug("thinking: unknown suffix format, treating as no config |")
	return ThinkingConfig{}
}

// applyUserDefinedModel applies thinking configuration for user-defined models
// without ThinkingSupport validation.
func applyUserDefinedModel(body []byte, modelInfo *registry.ModelInfo, fromFormat, toFormat, providerKey string, suffixResult SuffixResult, summaryConfig SummaryConfig) ([]byte, error) {
	// Get model ID for logging
	modelID := ""
	if modelInfo != nil {
		modelID = modelInfo.ID
	} else {
		modelID = suffixResult.ModelName
	}

	// Get config: suffix priority over body
	var config ThinkingConfig
	if suffixResult.HasSuffix {
		config = parseSuffixToConfig(suffixResult.RawSuffix, toFormat, modelID)
		log.WithFields(log.Fields{
			"provider": toFormat,
			"model":    modelID,
			"mode":     config.Mode,
			"budget":   config.Budget,
			"level":    config.Level,
		}).Debug("thinking: config from model suffix |")
	} else {
		config = extractThinkingConfig(body, fromFormat)
		if !hasThinkingConfig(config) && fromFormat != toFormat {
			config = extractThinkingConfig(body, toFormat)
		}
		if hasThinkingConfig(config) {
			log.WithFields(log.Fields{
				"provider": toFormat,
				"model":    modelID,
				"mode":     config.Mode,
				"budget":   config.Budget,
				"level":    config.Level,
			}).Debug("thinking: original config from request |")
		}
	}

	if !hasThinkingConfig(config) {
		log.WithFields(log.Fields{
			"model":    modelID,
			"provider": toFormat,
		}).Debug("thinking: user-defined model, passthrough (no config) |")
		return applySummaryConfigForProvider(body, toFormat, modelID, providerKey, modelInfo, summaryConfig), nil
	}

	applier := GetProviderApplier(toFormat)
	if applier == nil {
		log.WithFields(log.Fields{
			"model":    modelID,
			"provider": toFormat,
		}).Debug("thinking: user-defined model, passthrough (unknown provider) |")
		return body, nil
	}

	// The provider appliers route ModeLevel to their level format natively (e.g.
	// Gemini 3.x thinkingLevel), so a request's explicit level must not be
	// coerced to a budget here: a user-defined Gemini 3 model would otherwise
	// receive thinkingBudget and be rejected or ignored by level-only upstreams.
	// ModeBudget requests are left untouched as well.
	log.WithFields(log.Fields{
		"provider": toFormat,
		"model":    modelID,
		"mode":     config.Mode,
		"budget":   config.Budget,
		"level":    config.Level,
	}).Debug("thinking: processed config to apply |")
	applied, err := applier.Apply(body, config, modelInfo)
	if err != nil {
		return applied, err
	}
	if thinkingIsFullyDisabled(config) {
		return applied, nil
	}
	return applySummaryConfigForProvider(applied, toFormat, modelID, providerKey, modelInfo, summaryConfig), nil
}

// extractThinkingConfig extracts provider-specific thinking config from request body.
func extractThinkingConfig(body []byte, provider string) ThinkingConfig {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ThinkingConfig{}
	}
	extractor := GetProviderExtractor(provider)
	if extractor == nil {
		return ThinkingConfig{}
	}
	return extractor(body)
}

// hasThinkingConfig reports whether the extracted config carries any thinking
// intent. The zero ThinkingConfig is {Mode: ModeBudget, Budget: 0, Level: ""}
// because ModeBudget is the iota zero value, so "no config found" is exactly
// that triple; anything else means at least one field was populated.
func hasThinkingConfig(config ThinkingConfig) bool {
	isZeroValue := config.Mode == ModeBudget && config.Budget == 0 && config.Level == ""
	return !isZeroValue
}

// ExtractReasoningEffort returns the request's thinking setting as a canonical
// reasoning_effort label for usage logging. Model suffixes have the same
// priority as ApplyThinking: a valid suffix overrides body fields.
func ExtractReasoningEffort(body []byte, provider, model string) string {
	if effort := reasoningEffortFromSuffix(ParseSuffix(model)); effort != "" {
		return effort
	}

	provider = strings.ToLower(strings.TrimSpace(provider))
	config := extractThinkingConfig(body, provider)
	if !hasThinkingConfig(config) {
		switch provider {
		case "openai-response":
			config = extractCodexConfig(body)
		case "openai":
			config = extractCodexConfig(body)
		case "deepseek":
			config = extractOpenAIConfig(body)
		}
	}
	return reasoningEffortFromConfig(config)
}

// ExtractTranslatedReasoningEffort returns the final provider payload's thinking
// setting as a canonical reasoning_effort label for usage logging.
func ExtractTranslatedReasoningEffort(body []byte, provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	config := extractThinkingConfig(body, provider)
	if !hasThinkingConfig(config) {
		switch provider {
		case "openai-response":
			// No native extractor is registered for "openai-response", so probe
			// both payload shapes: Responses API (reasoning.effort) first, then
			// Chat Completions (reasoning_effort).
			config = extractCodexConfig(body)
			if !hasThinkingConfig(config) {
				config = extractOpenAIConfig(body)
			}
		case "openai":
			// extractThinkingConfig already ran extractOpenAIConfig for this
			// provider and it is deterministic, so only the Responses-style
			// reasoning.effort shape is still worth probing.
			config = extractCodexConfig(body)
		case "deepseek":
			config = extractOpenAIConfig(body)
		}
	}
	return reasoningEffortFromConfig(config)
}

func reasoningEffortFromSuffix(suffix SuffixResult) string {
	if !suffix.HasSuffix {
		return ""
	}
	return reasoningEffortFromConfig(parseSuffixToConfig(suffix.RawSuffix, "", suffix.ModelName))
}

func reasoningEffortFromConfig(config ThinkingConfig) string {
	if !hasThinkingConfig(config) {
		return ""
	}
	switch config.Mode {
	case ModeNone:
		return string(LevelNone)
	case ModeAuto:
		return string(LevelAuto)
	case ModeLevel:
		return strings.ToLower(strings.TrimSpace(string(config.Level)))
	case ModeBudget:
		level, ok := ConvertBudgetToLevel(config.Budget)
		if !ok {
			return ""
		}
		return level
	default:
		return ""
	}
}

// fieldNonNil reports whether the gjson Result represents a present,
// non-null JSON value. gjson.Result.Exists() returns true for JSON null,
// which causes callers that then read .Int()/.String() to misinterpret a
// null field as 0/"none"/"" and apply an unintended thinking mode (e.g.
// "enabled" silently becoming disabled, or a 400 rejection). Guard every
// optional-field presence check with fieldNonNil.
func fieldNonNil(r gjson.Result) bool {
	return r.Exists() && r.Type != gjson.Null
}

// extractClaudeConfig extracts thinking configuration from Claude format request body.
//
// Claude API format:
//   - thinking.type: "enabled" or "disabled"
//   - thinking.budget_tokens: integer (-1=auto, 0=disabled, >0=budget)
//
// Priority: thinking.type="disabled" takes precedence over budget_tokens.
// When type="enabled" without budget_tokens, returns ModeAuto to indicate
// the user wants thinking enabled but didn't specify a budget.
func extractClaudeConfig(body []byte) ThinkingConfig {
	thinkingType := gjson.GetBytes(body, "thinking.type").String()
	if thinkingType == "disabled" {
		return ThinkingConfig{Mode: ModeNone, Budget: 0}
	}
	if thinkingType == "adaptive" || thinkingType == "auto" {
		// Claude adaptive thinking uses output_config.effort (low/medium/high/max).
		// We only treat it as a thinking config when effort is explicitly present;
		// otherwise we passthrough and let upstream defaults apply.
		if effort := gjson.GetBytes(body, "output_config.effort"); effort.Exists() && effort.Type == gjson.String {
			value := strings.ToLower(strings.TrimSpace(effort.String()))
			if value == "" {
				return ThinkingConfig{}
			}
			switch value {
			case "none":
				return ThinkingConfig{Mode: ModeNone, Budget: 0}
			case "auto":
				return ThinkingConfig{Mode: ModeAuto, Budget: -1}
			default:
				return ThinkingConfig{Mode: ModeLevel, Level: ThinkingLevel(value)}
			}
		}
		return ThinkingConfig{}
	}

	// Check budget_tokens
	if budget := gjson.GetBytes(body, "thinking.budget_tokens"); fieldNonNil(budget) {
		value := int(budget.Int())
		switch value {
		case 0:
			return ThinkingConfig{Mode: ModeNone, Budget: 0}
		case -1:
			return ThinkingConfig{Mode: ModeAuto, Budget: -1}
		default:
			return ThinkingConfig{Mode: ModeBudget, Budget: value}
		}
	}

	// If type="enabled" but no budget_tokens, treat as auto (user wants thinking but no budget specified)
	if thinkingType == "enabled" {
		return ThinkingConfig{Mode: ModeAuto, Budget: -1}
	}

	return ThinkingConfig{}
}

// extractGeminiConfig extracts thinking configuration from Gemini format request body.
//
// Gemini API format:
//   - generationConfig.thinkingConfig.thinkingLevel: "none", "auto", or level name (Gemini 3)
//   - generationConfig.thinkingConfig.thinkingBudget: integer (Gemini 2.5)
//
// For antigravity providers, the path is prefixed with "request.".
//
// Priority: thinkingLevel is checked first (Gemini 3 format), then thinkingBudget (Gemini 2.5 format).
// This allows newer Gemini 3 level-based configs to take precedence.
func extractGeminiConfig(body []byte, provider string) ThinkingConfig {
	prefix := "generationConfig.thinkingConfig"
	if provider == "antigravity" {
		prefix = "request.generationConfig.thinkingConfig"
	}

	// Check thinkingLevel first (Gemini 3 format takes precedence)
	level := gjson.GetBytes(body, prefix+".thinkingLevel")
	if !level.Exists() {
		// Google official Gemini Python SDK sends snake_case field names
		level = gjson.GetBytes(body, prefix+".thinking_level")
	}
	if fieldNonNil(level) {
		value := level.String()
		switch value {
		case "none":
			return ThinkingConfig{Mode: ModeNone, Budget: 0}
		case "auto":
			return ThinkingConfig{Mode: ModeAuto, Budget: -1}
		default:
			return ThinkingConfig{Mode: ModeLevel, Level: ThinkingLevel(value)}
		}
	}

	// Check thinkingBudget (Gemini 2.5 format)
	budget := gjson.GetBytes(body, prefix+".thinkingBudget")
	if !budget.Exists() {
		// Google official Gemini Python SDK sends snake_case field names
		budget = gjson.GetBytes(body, prefix+".thinking_budget")
	}
	if fieldNonNil(budget) {
		value := int(budget.Int())
		switch value {
		case 0:
			return ThinkingConfig{Mode: ModeNone, Budget: 0}
		case -1:
			return ThinkingConfig{Mode: ModeAuto, Budget: -1}
		default:
			return ThinkingConfig{Mode: ModeBudget, Budget: value}
		}
	}

	return ThinkingConfig{}
}

func extractInteractionsConfig(body []byte) ThinkingConfig {
	for _, path := range []string{
		"generation_config.thinking_level",
		"generation_config.thinkingLevel",
		"generation_config.thinking_config.thinking_level",
		"generation_config.thinking_config.thinkingLevel",
		"generation_config.thinkingConfig.thinking_level",
		"generation_config.thinkingConfig.thinkingLevel",
	} {
		level := gjson.GetBytes(body, path)
		if !level.Exists() {
			continue
		}
		value := strings.ToLower(strings.TrimSpace(level.String()))
		switch value {
		case "none":
			return ThinkingConfig{Mode: ModeNone, Budget: 0}
		case "auto":
			return ThinkingConfig{Mode: ModeAuto, Budget: -1}
		default:
			return ThinkingConfig{Mode: ModeLevel, Level: ThinkingLevel(value)}
		}
	}

	for _, path := range []string{
		"generation_config.thinking_budget",
		"generation_config.thinkingBudget",
		"generation_config.thinking_config.thinking_budget",
		"generation_config.thinking_config.thinkingBudget",
		"generation_config.thinkingConfig.thinking_budget",
		"generation_config.thinkingConfig.thinkingBudget",
	} {
		budget := gjson.GetBytes(body, path)
		if !budget.Exists() {
			continue
		}
		value := int(budget.Int())
		switch value {
		case 0:
			return ThinkingConfig{Mode: ModeNone, Budget: 0}
		case -1:
			return ThinkingConfig{Mode: ModeAuto, Budget: -1}
		default:
			return ThinkingConfig{Mode: ModeBudget, Budget: value}
		}
	}

	return ThinkingConfig{}
}

// extractOpenAIConfig extracts thinking configuration from OpenAI format request body.
//
// OpenAI API format:
//   - reasoning_effort: "none", "low", "medium", "high" (discrete levels)
//
// OpenAI uses level-based thinking configuration only, no numeric budget support.
// The "none" value is treated specially to return ModeNone.
func extractOpenAIConfig(body []byte) ThinkingConfig {
	// Check reasoning_effort (OpenAI Chat Completions format)
	if effort := gjson.GetBytes(body, "reasoning_effort"); fieldNonNil(effort) {
		value := effort.String()
		if value == "none" {
			return ThinkingConfig{Mode: ModeNone, Budget: 0}
		}
		return ThinkingConfig{Mode: ModeLevel, Level: ThinkingLevel(value)}
	}

	return ThinkingConfig{}
}

// extractKimiConfig extracts Kimi's native thinking object while retaining
// reasoning_effort as a legacy input fallback.
//
// Native fields take precedence over reasoning_effort. In particular,
// thinking.type="enabled" without an explicit effort means "use the upstream
// default" and therefore returns an empty config so ApplyThinking preserves the
// request unchanged instead of interpreting it as CPA's ModeAuto.
func extractKimiConfig(body []byte) ThinkingConfig {
	thinkingType := gjson.GetBytes(body, "thinking.type")
	if thinkingType.Exists() {
		switch strings.ToLower(strings.TrimSpace(thinkingType.String())) {
		case "disabled":
			return ThinkingConfig{Mode: ModeNone, Budget: 0}
		case "enabled":
			if !gjson.GetBytes(body, "thinking.effort").Exists() {
				return ThinkingConfig{}
			}
		}
	}

	if effort := gjson.GetBytes(body, "thinking.effort"); effort.Exists() {
		value := strings.ToLower(strings.TrimSpace(effort.String()))
		switch value {
		case "":
			return ThinkingConfig{}
		case "none":
			return ThinkingConfig{Mode: ModeNone, Budget: 0}
		case "auto":
			return ThinkingConfig{Mode: ModeAuto, Budget: -1}
		default:
			return ThinkingConfig{Mode: ModeLevel, Level: ThinkingLevel(value)}
		}
	}

	// An explicit native thinking object without an effort should be left for
	// the Kimi upstream to interpret and must not be overridden by the legacy
	// field.
	if thinkingType.Exists() {
		return ThinkingConfig{}
	}

	return extractOpenAIConfig(body)
}

// extractCodexConfig extracts thinking configuration from Codex format request body.
//
// Codex API format (OpenAI Responses API):
//   - reasoning.effort: "none", "low", "medium", "high"
//
// This is similar to OpenAI but uses nested field "reasoning.effort" instead of "reasoning_effort".
func extractCodexConfig(body []byte) ThinkingConfig {
	// Check reasoning.effort (Codex / OpenAI Responses API format)
	if effort := gjson.GetBytes(body, "reasoning.effort"); fieldNonNil(effort) {
		value := effort.String()
		if value == "none" {
			return ThinkingConfig{Mode: ModeNone, Budget: 0}
		}
		return ThinkingConfig{Mode: ModeLevel, Level: ThinkingLevel(value)}
	}

	return ThinkingConfig{}
}

// extractDeepSeekConfig extracts thinking configuration from DeepSeek format request body.
//
// DeepSeek API format:
//   - thinking.type: "enabled"/"disabled" (thought toggle)
//   - reasoning_effort: "none", "low", "medium", "high", "xhigh", "max", "auto" (discrete levels)
//
// thinking.type takes precedence. When thinking.type="disabled", any reasoning_effort is ignored.
// When thinking.type="enabled" without reasoning_effort, returns empty config to let the
// upstream use its default effort.
//
// When thinking.type="enabled" and reasoning_effort="none", thinking must stay enabled.
// Returning ModeNone would call applyDisabledThinking (thinking.type=disabled), contradicting
// the explicit enabled flag. Returning ModeLevel+LevelNone is also unsafe because ValidateConfig
// normalizes it back to ModeNone. We therefore return an empty config so the body passes through
// unchanged: the upstream receives thinking.type=enabled + reasoning_effort=none exactly as the
// client intended.
func extractDeepSeekConfig(body []byte) ThinkingConfig {
	thinkingType := gjson.GetBytes(body, "thinking.type")
	thinkingEnabled := false
	if thinkingType.Exists() {
		switch strings.ToLower(strings.TrimSpace(thinkingType.String())) {
		case "disabled":
			return ThinkingConfig{Mode: ModeNone, Budget: 0}
		case "enabled":
			thinkingEnabled = true
			// A JSON null reasoning_effort is treated exactly like a missing
			// field (gjson.Exists() reports true for null, so check the type):
			// the upstream then receives thinking.type=enabled without effort.
			if effort := gjson.GetBytes(body, "reasoning_effort"); !effort.Exists() || effort.Type == gjson.Null {
				return ThinkingConfig{}
			}
		}
	}

	// Check reasoning_effort (OpenAI-compatible top-level field)
	if effort := gjson.GetBytes(body, "reasoning_effort"); effort.Exists() && effort.Type != gjson.Null {
		value := strings.ToLower(strings.TrimSpace(effort.String()))
		switch value {
		case "":
			return ThinkingConfig{}
		case "none":
			// When thinking.type=enabled, "none" must not disable thinking.
			// Passthrough so the upstream sees thinking.type=enabled + effort=none.
			if thinkingEnabled {
				return ThinkingConfig{}
			}
			return ThinkingConfig{Mode: ModeNone, Budget: 0}
		case "auto":
			return ThinkingConfig{Mode: ModeAuto, Budget: -1}
		default:
			return ThinkingConfig{Mode: ModeLevel, Level: ThinkingLevel(value)}
		}
	}

	return ThinkingConfig{}
}

// extractNvidiaConfig extracts thinking configuration from NVIDIA format request body.
//
// NVIDIA Nemotron API format:
//   - enable_thinking: true/false (primary toggle)
//   - reasoning_budget: integer (optional token budget)
//   - medium_effort: true/false (reduced thinking depth)
//
// Falls back to reasoning_effort for cross-compatibility with OpenAI format clients.
func extractNvidiaConfig(body []byte) ThinkingConfig {
	// Check enable_thinking first (NVIDIA-native)
	enabled := gjson.GetBytes(body, "enable_thinking")
	if enabled.Exists() {
		if !enabled.Bool() {
			return ThinkingConfig{Mode: ModeNone, Budget: 0}
		}

		// Check medium_effort for level mapping
		if effort := gjson.GetBytes(body, "medium_effort"); effort.Exists() && effort.Bool() {
			return ThinkingConfig{Mode: ModeLevel, Level: LevelLow}
		}

		// Check reasoning_budget
		if budget := gjson.GetBytes(body, "reasoning_budget"); budget.Exists() {
			value := int(budget.Int())
			if value > 0 {
				return ThinkingConfig{Mode: ModeBudget, Budget: value}
			}
		}

		// Thinking enabled without specific config - treat as auto
		return ThinkingConfig{Mode: ModeAuto, Budget: -1}
	}

	// Fall back to reasoning_effort (OpenAI-compat client compatibility)
	return extractOpenAIConfig(body)
}
