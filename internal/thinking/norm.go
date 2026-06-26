package thinking

import "strings"

// NormalizeEffort clamps internal thinking levels to the set accepted by
// the target provider's API. This prevents sending unsupported values like
// "max" or "xhigh" to upstream APIs that reject them.
//
// The normalization is applied at translation boundaries (translators) and
// at the applier layer for user-defined models, where the target model's
// ThinkingSupport is unknown.
//
// Provider-specific rules:
//   - "openai", "codex", "xai": standard levels (low/medium/high/minimal).
//     "max"/"xhigh" → "high" (they cause 400 errors or context overflow).
//   - "deepseek": accepts high/max, no clamping needed.
//   - All others: passthrough (appliers handle per-model clamping via ValidateConfig).
func NormalizeEffort(effort string, provider string) string {
	effort = strings.ToLower(strings.TrimSpace(effort))
	switch provider {
	case "openai", "codex", "xai", "openai-response":
		switch effort {
		case "max", "xhigh":
			return string(LevelHigh)
		}
	}
	return effort
}

// MapXHighToMax maps the internal "xhigh" level to "max" for providers that
// accept "max" but not "xhigh".
//
// When ConvertBudgetToLevel returns "xhigh" (budget > ThresholdHigh = 32768),
// providers that support "max" should use this function to convert the level
// before sending to the upstream API.
//
// Usage:
//
//	effort = thinking.MapXHighToMax(effort)
func MapXHighToMax(effort string) string {
	if effort == string(LevelXHigh) {
		return string(LevelMax)
	}
	return effort
}
