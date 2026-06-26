package thinking

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

// levelToBudgetMap defines the standard Level → Budget mapping.
// All keys are lowercase; lookups should use strings.ToLower.
// levelToBudgetMap defines the standard Level → Budget mapping.
//
// minimal, low, and medium are aliases for auto (-1). The level system is
// simplified to three tiers: high (24576), xhigh (32768), and max (65536).
// Lower levels are collapsed into auto, meaning "let the model decide".
//
// Note: This is a one-way simplification. ConvertBudgetToLevel (Budget → Level)
// still returns minimal/low/medium labels for backward compatibility with
// models that declare support for these levels in their registry metadata.
// Feeding those labels back through ConvertLevelToBudget yields -1 (auto),
// which is the intended behavior.
var levelToBudgetMap = map[string]int{
	"none":    0,
	"auto":    -1,
	"minimal": -1,
	"low":     -1,
	"medium":  -1,
	"high":    24576,
	"xhigh":   32768,
	// "max" is used by Claude adaptive thinking effort. We map it to a large budget
	// and rely on per-model clamping when converting to budget-only providers.
	"max": 65536,
}

// ConvertLevelToBudget converts a thinking level to a budget value.
//
// This is a semantic conversion that maps discrete levels to numeric budgets.
// Level matching is case-insensitive.
//
// Level → Budget mapping:
//   - none    → 0
//   - auto    → -1
//   - minimal → -1 (auto)
//   - low     → -1 (auto)
//   - medium  → -1 (auto)
//   - high    → 24576
//   - xhigh   → 32768
//   - max     → 65536
//
// Returns:
//   - budget: The converted budget value
//   - ok: true if level is valid, false otherwise
func ConvertLevelToBudget(level string) (int, bool) {
	budget, ok := levelToBudgetMap[strings.ToLower(level)]
	return budget, ok
}

// BudgetThreshold constants define the upper bounds for each thinking level.
// These are used by ConvertBudgetToLevel for range-based mapping.
const (
	// ThresholdMinimal is the upper bound for "minimal" level (1-1024)
	ThresholdMinimal = 1024
	// ThresholdLow is the upper bound for "low" level (1025-2048)
	ThresholdLow = 2048
	// ThresholdMedium is the upper bound for "medium" level (2049-8192)
	ThresholdMedium = 8192
	// ThresholdHigh is the upper bound for "high" level (8193-32768)
	ThresholdHigh = 32768
)

// ConvertBudgetToLevel converts a budget value to the nearest thinking level.
//
// This is a semantic conversion that maps numeric budgets to discrete levels.
// Uses threshold-based mapping for range conversion.
//
// Note: The returned level labels for budgets ≤ ThresholdMedium (minimal, low,
// medium) are for backward compatibility with model registry metadata. These
// levels are aliases for auto (-1) in the Level → Budget direction — see
// levelToBudgetMap for details.
//
// Budget → Level thresholds:
//   - -1         → auto
//   - 0          → none
//   - 1-1024     → minimal
//   - 1025-2048  → low
//   - 2049-8192  → medium
//   - 8193-32768 → high
//   - 32769+     → xhigh
//
// Returns:
//   - level: The converted thinking level string
//   - ok: true if budget is valid, false for invalid negatives (< -1)
func ConvertBudgetToLevel(budget int) (string, bool) {
	switch {
	case budget < -1:
		// Invalid negative values
		return "", false
	case budget == -1:
		return string(LevelAuto), true
	case budget == 0:
		return string(LevelNone), true
	case budget <= ThresholdMinimal:
		return string(LevelMinimal), true
	case budget <= ThresholdLow:
		return string(LevelLow), true
	case budget <= ThresholdMedium:
		return string(LevelMedium), true
	case budget <= ThresholdHigh:
		return string(LevelHigh), true
	default:
		return string(LevelXHigh), true
	}
}

// HasLevel reports whether the given target level exists in the levels slice.
// Matching is case-insensitive with leading/trailing whitespace trimmed.
func HasLevel(levels []string, target string) bool {
	for _, level := range levels {
		if strings.EqualFold(strings.TrimSpace(level), target) {
			return true
		}
	}
	return false
}

// MapToClaudeEffort maps a generic thinking level string to a Claude adaptive
// thinking effort value (low/medium/high/max).
//
// supportsMax indicates whether the target model supports "max" effort.
// Returns the mapped effort and true if the level is valid, or ("", false) otherwise.
func MapToClaudeEffort(level string, supportsMax bool) (string, bool) {
	level = strings.ToLower(strings.TrimSpace(level))
	switch level {
	case "":
		return "", false
	case "minimal":
		return "low", true
	case "low", "medium", "high":
		return level, true
	case "xhigh", "max":
		if supportsMax {
			return "max", true
		}
		return "high", true
	case "auto":
		return "high", true
	default:
		return "", false
	}
}

// ModelCapability describes the thinking format support of a model.
type ModelCapability int

const (
	// CapabilityUnknown indicates modelInfo is nil (passthrough behavior, internal use).
	CapabilityUnknown ModelCapability = iota - 1
	// CapabilityNone indicates model doesn't support thinking (Thinking is nil).
	CapabilityNone
	// CapabilityBudgetOnly indicates the model supports numeric budgets only.
	CapabilityBudgetOnly
	// CapabilityLevelOnly indicates the model supports discrete levels only.
	CapabilityLevelOnly
	// CapabilityHybrid indicates the model supports both budgets and levels.
	CapabilityHybrid
)

// detectModelCapability determines the thinking format capability of a model.
//
// This is an internal function used by validation and conversion helpers.
// It analyzes the model's ThinkingSupport configuration to classify the model:
//   - CapabilityNone: modelInfo.Thinking is nil (model doesn't support thinking)
//   - CapabilityBudgetOnly: Has Min/Max but no Levels (Claude, Gemini 2.5)
//   - CapabilityLevelOnly: Has Levels but no Min/Max (OpenAI, Codex, Kimi)
//   - CapabilityHybrid: Has both Min/Max and Levels (Gemini 3)
//
// Note: Returns a special sentinel value when modelInfo itself is nil (unknown model).
func detectModelCapability(modelInfo *registry.ModelInfo) ModelCapability {
	if modelInfo == nil {
		return CapabilityUnknown // sentinel for "passthrough" behavior
	}
	if modelInfo.Thinking == nil {
		return CapabilityNone
	}
	support := modelInfo.Thinking
	hasBudget := support.Min > 0 || support.Max > 0
	hasLevels := len(support.Levels) > 0

	switch {
	case hasBudget && hasLevels:
		return CapabilityHybrid
	case hasBudget:
		return CapabilityBudgetOnly
	case hasLevels:
		return CapabilityLevelOnly
	default:
		return CapabilityNone
	}
}
