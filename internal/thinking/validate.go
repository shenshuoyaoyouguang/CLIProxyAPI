// Package thinking provides unified thinking configuration processing logic.
package thinking

import (
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	log "github.com/sirupsen/logrus"
)

// ValidateConfig validates a thinking configuration against model capabilities.
//
// This function performs comprehensive validation:
//   - Checks if the model supports thinking
//   - Auto-converts between Budget and Level formats based on model capability
//   - Validates that requested level is in the model's supported levels list
//   - Clamps budget values to model's allowed range
//   - When converting Budget -> Level for level-only models, clamps the derived standard level to the nearest supported level
//     (special values none/auto are preserved)
//   - When config comes from a model suffix, strict budget validation is disabled (we clamp instead of error)
//
// Parameters:
//   - config: The thinking configuration to validate
//   - support: Model's ThinkingSupport properties (nil means no thinking support)
//   - fromFormat: Source provider format (used to determine strict validation rules)
//   - toFormat: Target provider format
//   - fromSuffix: Whether config was sourced from model suffix
//
// Returns:
//   - Normalized ThinkingConfig with clamped values
//   - ThinkingError if validation fails (ErrThinkingNotSupported, ErrLevelNotSupported, etc.)
//
// Auto-conversion behavior:
//   - Budget-only model + Level config → Level converted to Budget
//   - Level-only model + Budget config → Budget converted to Level
//   - Hybrid model → preserve original format
//
// validateContext bundles the inputs and intermediate computation state for a
// single ValidateConfig invocation. Its methods perform each validation/conversion
// step so that ValidateConfig stays a thin orchestrator. The struct holds no
// externally observable state and is never reused across invocations.
type validateContext struct {
	config     ThinkingConfig
	modelInfo  *registry.ModelInfo
	fromFormat string
	toFormat   string
	fromSuffix bool

	model                  string
	support                *registry.ThinkingSupport
	toCapability           ModelCapability
	toHasLevelSupport      bool
	modelFamilyMismatch    bool
	allowClampUnsupported  bool
	strictBudget           bool
	budgetDerivedFromLevel bool
}

// ValidateConfig validates a thinking configuration against model capabilities.
//
// This function performs comprehensive validation:
//   - Checks if the model supports thinking
//   - Auto-converts between Budget and Level formats based on model capability
//   - Validates that requested level is in the model's supported levels list
//   - Clamps budget values to model's allowed range
//   - When converting Budget -> Level for level-only models, clamps the derived standard level to the nearest supported level
//     (special values none/auto are preserved)
//   - When config comes from a model suffix, strict budget validation is disabled (we clamp instead of error)
//
// Parameters:
//   - config: The thinking configuration to validate
//   - support: Model's ThinkingSupport properties (nil means no thinking support)
//   - fromFormat: Source provider format (used to determine strict validation rules)
//   - toFormat: Target provider format
//   - fromSuffix: Whether config was sourced from model suffix
//
// Returns:
//   - Normalized ThinkingConfig with clamped values
//   - ThinkingError if validation fails (ErrThinkingNotSupported, ErrLevelNotSupported, etc.)
//
// Auto-conversion behavior:
//   - Budget-only model + Level config → Level converted to Budget
//   - Level-only model + Budget config → Budget converted to Level
//   - Hybrid model → preserve original format
func ValidateConfig(config ThinkingConfig, modelInfo *registry.ModelInfo, fromFormat, toFormat string, fromSuffix bool) (*ThinkingConfig, error) {
	fromFormat, toFormat = strings.ToLower(strings.TrimSpace(fromFormat)), strings.ToLower(strings.TrimSpace(toFormat))

	c := &validateContext{
		config:     config,
		modelInfo:  modelInfo,
		fromFormat: fromFormat,
		toFormat:   toFormat,
		fromSuffix: fromSuffix,
	}
	c.resolveSupport()
	if c.support == nil {
		if c.config.Mode != ModeNone {
			return nil, NewThinkingErrorWithModel(ErrThinkingNotSupported, "thinking not supported for this model", c.model)
		}
		return &c.config, nil
	}

	c.resolveCapabilityFlags()

	if err := c.convertCapabilityFormat(); err != nil {
		return nil, err
	}
	c.normalizeModeAliases()
	if err := c.validateLevelSupport(); err != nil {
		return nil, err
	}
	if err := c.validateBudgetRange(); err != nil {
		return nil, err
	}

	// Convert ModeAuto to mid-range if dynamic not allowed.
	if c.config.Mode == ModeAuto && !c.support.DynamicAllowed {
		c.config = convertAutoToMidRange(c.config, c.support, c.modelInfo, c.toFormat, c.model)
		// The canonical mid-range level may not be present in a model's discrete
		// level subset (for example, Levels=[low, high]). Clamp the generated
		// fallback just like a budget-derived level so providers never receive an
		// unsupported value.
		if c.config.Mode == ModeLevel && len(c.support.Levels) > 0 && !isLevelSupported(string(c.config.Level), c.support.Levels) {
			c.config.Level = clampLevel(c.config.Level, c.modelInfo, c.toFormat)
		}
	}

	c.finalizeModeNone()

	return &c.config, nil
}

// resolveSupport populates the model identity and thinking support from modelInfo.
func (c *validateContext) resolveSupport() {
	c.model = "unknown"
	c.support = nil
	if c.modelInfo == nil {
		return
	}
	if c.modelInfo.ID != "" {
		c.model = c.modelInfo.ID
	}
	c.support = c.modelInfo.Thinking
}

// resolveCapabilityFlags computes the capability-based validation switches
// (level clamp allowance, strict budget enforcement, family mismatch detection).
func (c *validateContext) resolveCapabilityFlags() {
	c.toCapability = detectModelCapability(c.modelInfo)
	c.toHasLevelSupport = c.toCapability == CapabilityLevelOnly || c.toCapability == CapabilityHybrid
	c.modelFamilyMismatch = false
	if c.modelInfo != nil {
		modelType := strings.ToLower(strings.TrimSpace(c.modelInfo.Type))
		if modelType != "" {
			if (c.fromFormat != "" && !isSameProviderFamily(c.fromFormat, modelType)) ||
				(c.toFormat != "" && !isSameProviderFamily(c.toFormat, modelType)) {
				c.modelFamilyMismatch = true
			}
		}
	}
	// allowClampUnsupported determines whether to clamp unsupported levels instead of returning an error.
	// This applies when crossing provider families (e.g., openai→gemini, claude→gemini) and the target
	// model supports discrete levels. Same-family conversions require strict validation.
	//
	// modelFamilyMismatch covers providers that reuse another protocol on the wire
	// (e.g. Kimi serving Claude-compatible /v1/messages). In that path fromFormat and
	// toFormat both look like "claude", but the model itself is not Claude-family, so
	// unsupported levels such as "max" should clamp to the nearest supported level
	// (typically "high") instead of failing validation.
	c.allowClampUnsupported = c.toHasLevelSupport && (!isSameProviderFamily(c.fromFormat, c.toFormat) || c.modelFamilyMismatch)

	// strictBudget determines whether to enforce strict budget range validation.
	// This applies when: (1) config comes from request body (not suffix), (2) source format is known,
	// and (3) source and target are in the same provider family. Cross-family or suffix-based configs
	// are clamped instead of rejected to improve interoperability.
	c.strictBudget = !c.fromSuffix && c.fromFormat != "" && isSameProviderFamily(c.fromFormat, c.toFormat) && !c.modelFamilyMismatch
	c.budgetDerivedFromLevel = false
}

// convertCapabilityFormat converts the config between level and budget
// representations based on the model's capability. It returns a ThinkingError
// when an explicit level cannot be mapped to a valid budget (or vice versa).
func (c *validateContext) convertCapabilityFormat() error {
	switch c.toCapability {
	case CapabilityBudgetOnly:
		if c.config.Mode == ModeLevel {
			if c.config.Level == LevelAuto {
				break
			}
			budget, ok := ConvertLevelToBudget(string(c.config.Level))
			if !ok {
				return NewThinkingError(ErrUnknownLevel, fmt.Sprintf("unknown level: %s", c.config.Level))
			}
			c.config.Mode = ModeBudget
			c.config.Budget = budget
			c.config.Level = ""
			c.budgetDerivedFromLevel = true
		}
	case CapabilityLevelOnly:
		if c.config.Mode == ModeBudget {
			level, ok := ConvertBudgetToLevel(c.config.Budget)
			if !ok {
				return NewThinkingError(ErrUnknownLevel, fmt.Sprintf("budget %d cannot be converted to a valid level", c.config.Budget))
			}
			// When converting Budget -> Level for level-only models, clamp the derived standard level
			// to the nearest supported level. Special values (none/auto) are preserved.
			c.config.Mode = ModeLevel
			c.config.Level = clampLevel(ThinkingLevel(level), c.modelInfo, c.toFormat)
			c.config.Budget = 0
		}
	case CapabilityHybrid:
	}
	return nil
}

// normalizeModeAliases folds special level values (none/auto) and a zero budget
// into their canonical Mode representation.
func (c *validateContext) normalizeModeAliases() {
	if c.config.Mode == ModeLevel && c.config.Level == LevelNone {
		c.config.Mode = ModeNone
		c.config.Budget = 0
		c.config.Level = ""
	}
	if c.config.Mode == ModeLevel && c.config.Level == LevelAuto {
		c.config.Mode = ModeAuto
		c.config.Budget = -1
		c.config.Level = ""
	}
	if c.config.Mode == ModeBudget && c.config.Budget == 0 {
		c.config.Mode = ModeNone
		c.config.Level = ""
	}
}

// validateLevelSupport ensures the requested level is within the model's
// supported level set, clamping (when allowed) or returning ErrLevelNotSupported.
func (c *validateContext) validateLevelSupport() error {
	if len(c.support.Levels) > 0 && c.config.Mode == ModeLevel {
		if !isLevelSupported(string(c.config.Level), c.support.Levels) {
			if c.allowClampUnsupported {
				c.config.Level = clampLevel(c.config.Level, c.modelInfo, c.toFormat)
			}
			if !isLevelSupported(string(c.config.Level), c.support.Levels) {
				// User explicitly specified an unsupported level - return error
				// (budget-derived levels may be clamped based on source format)
				validLevels := normalizeLevels(c.support.Levels)
				message := fmt.Sprintf("level %q not supported, valid levels: %s", strings.ToLower(string(c.config.Level)), strings.Join(validLevels, ", "))
				return NewThinkingError(ErrLevelNotSupported, message)
			}
		}
	}
	return nil
}

// validateBudgetRange enforces the model's numeric budget bounds when strict
// validation is active.
func (c *validateContext) validateBudgetRange() error {
	if c.strictBudget && c.config.Mode == ModeBudget && !c.budgetDerivedFromLevel {
		min, max := c.support.Min, c.support.Max
		if min != 0 || max != 0 {
			if c.config.Budget < min || c.config.Budget > max || (c.config.Budget == 0 && !c.support.ZeroAllowed) {
				message := fmt.Sprintf("budget %d out of range [%d,%d]", c.config.Budget, min, max)
				return NewThinkingError(ErrBudgetOutOfRange, message)
			}
		}
	}
	return nil
}

// finalizeModeNone applies the final ModeNone handling: providers with a native
// disabled mode keep thinking fully off, while other models fall back to the
// lowest supported level when disabling is not possible.
func (c *validateContext) finalizeModeNone() {
	if c.config.Mode == ModeNone && isDisabledCapableProvider(c.toFormat) {
		// Providers with an explicit disabled mode (claude/deepseek/kimi) keep
		// thinking fully off. Keep Budget=0 and Level="" so the applier emits
		// the native "disabled" marker instead of a clamped level that would
		// otherwise re-enable thinking for models whose Levels list omits "none".
		c.config.Budget = 0
		c.config.Level = ""
		return
	}

	switch c.config.Mode {
	case ModeBudget, ModeAuto, ModeNone:
		c.config.Budget = clampBudget(c.config.Budget, c.modelInfo, c.toFormat)
	}

	// ModeNone for a model that cannot be disabled falls back to the lowest
	// supported level. Budget-capable models reach this path with Budget > 0;
	// level-only models need the capability flags checked explicitly because
	// their Min/Max range is zero.
	cannotDisableLevelModel := !c.support.ZeroAllowed && !isLevelSupported(string(LevelNone), c.support.Levels)
	if c.config.Mode == ModeNone && len(c.support.Levels) > 0 && (c.config.Budget > 0 || cannotDisableLevelModel) {
		c.config.Level = ThinkingLevel(c.support.Levels[0])
	}
}

// convertAutoToMidRange converts ModeAuto to a mid-range value when dynamic is not allowed.
//
// This function handles the case where a model does not support dynamic/auto thinking.
// The auto mode is silently converted to a fixed value based on model capability:
//   - Level-only models: convert to ModeLevel with LevelMedium
//   - Budget models: convert to ModeBudget with mid = (Min + Max) / 2
//
// Logging:
//   - Debug level when conversion occurs
//   - Fields: original_mode, clamped_to, reason
func convertAutoToMidRange(config ThinkingConfig, support *registry.ThinkingSupport, modelInfo *registry.ModelInfo, provider, model string) ThinkingConfig {
	// For level-only models (has Levels but no Min/Max range), use ModeLevel with the
	// level nearest to medium that the model actually supports.
	if len(support.Levels) > 0 && support.Min == 0 && support.Max == 0 {
		config.Mode = ModeLevel
		config.Level = clampLevel(LevelMedium, modelInfo, provider)
		config.Budget = 0
		log.WithFields(log.Fields{
			"provider":      provider,
			"model":         model,
			"original_mode": "auto",
			"clamped_to":    string(LevelMedium),
		}).Debug("thinking: mode converted, dynamic not allowed, using medium level |")
		return config
	}

	// For budget models, use mid-range budget
	mid := (support.Min + support.Max) / 2
	if mid <= 0 {
		// When the mid budget is non-positive, prefer preserving dynamic
		// thinking if the model supports it, rather than forcing a fixed value.
		if support.DynamicAllowed {
			config.Mode = ModeAuto
			config.Budget = -1
		} else if support.ZeroAllowed {
			config.Mode = ModeNone
			config.Budget = 0
		} else {
			config.Mode = ModeBudget
			config.Budget = support.Min
		}
	} else {
		config.Mode = ModeBudget
		config.Budget = mid
	}
	log.WithFields(log.Fields{
		"provider":      provider,
		"model":         model,
		"original_mode": "auto",
		"clamped_to":    config.Budget,
	}).Debug("thinking: mode converted, dynamic not allowed |")
	return config
}

// standardLevelOrder defines the canonical ordering of thinking levels from lowest to highest.
var standardLevelOrder = []ThinkingLevel{LevelMinimal, LevelLow, LevelMedium, LevelHigh, LevelXHigh, LevelMax}

// clampLevel clamps the given level to the nearest supported level.
// On tie, prefers the lower level.
func clampLevel(level ThinkingLevel, modelInfo *registry.ModelInfo, provider string) ThinkingLevel {
	model := "unknown"
	var supported []string
	if modelInfo != nil {
		if modelInfo.ID != "" {
			model = modelInfo.ID
		}
		if modelInfo.Thinking != nil {
			supported = modelInfo.Thinking.Levels
		}
	}

	if len(supported) == 0 || isLevelSupported(string(level), supported) {
		return level
	}

	pos := levelIndex(string(level))
	if pos == -1 {
		return level
	}
	bestIdx, bestDist := -1, len(standardLevelOrder)+1

	for _, s := range supported {
		if idx := levelIndex(strings.TrimSpace(s)); idx != -1 {
			if dist := abs(pos - idx); dist < bestDist || (dist == bestDist && idx < bestIdx) {
				bestIdx, bestDist = idx, dist
			}
		}
	}

	if bestIdx >= 0 {
		clamped := standardLevelOrder[bestIdx]
		log.WithFields(log.Fields{
			"provider":       provider,
			"model":          model,
			"original_value": string(level),
			"clamped_to":     string(clamped),
		}).Debug("thinking: level clamped |")
		return clamped
	}
	return level
}

// clampBudget clamps a budget value to the model's supported range.
func clampBudget(value int, modelInfo *registry.ModelInfo, provider string) int {
	model := "unknown"
	support := (*registry.ThinkingSupport)(nil)
	if modelInfo != nil {
		if modelInfo.ID != "" {
			model = modelInfo.ID
		}
		support = modelInfo.Thinking
	}
	if support == nil {
		return value
	}

	// Auto value (-1) passes through without clamping.
	if value == -1 {
		return value
	}

	min, max := support.Min, support.Max
	if value == 0 && !support.ZeroAllowed {
		log.WithFields(log.Fields{
			"provider":       provider,
			"model":          model,
			"original_value": value,
			"clamped_to":     min,
			"min":            min,
			"max":            max,
		}).Warn("thinking: budget zero not allowed |")
		return min
	}

	// Some models are level-only and do not define numeric budget ranges.
	if min == 0 && max == 0 {
		return value
	}

	if value < min {
		if value == 0 && support.ZeroAllowed {
			return 0
		}
		logClamp(provider, model, value, min, min, max)
		return min
	}
	if value > max {
		logClamp(provider, model, value, max, min, max)
		return max
	}
	return value
}

// isLevelSupported reports whether level exists in supported.
// It delegates to HasLevel (convert.go) to keep a single implementation.
func isLevelSupported(level string, supported []string) bool {
	return HasLevel(supported, level)
}

func levelIndex(level string) int {
	for i, l := range standardLevelOrder {
		if strings.EqualFold(level, string(l)) {
			return i
		}
	}
	return -1
}

func normalizeLevels(levels []string) []string {
	out := make([]string, len(levels))
	for i, l := range levels {
		out[i] = strings.ToLower(strings.TrimSpace(l))
	}
	return out
}

// isBudgetCapableProvider returns true if the provider supports budget-based thinking.
// These providers may also support level-based thinking (hybrid models).
func isBudgetCapableProvider(provider string) bool {
	switch provider {
	case "gemini", "antigravity", "claude":
		return true
	default:
		return false
	}
}

func isGeminiFamily(provider string) bool {
	switch provider {
	case "gemini", "antigravity":
		return true
	default:
		return false
	}
}

func isOpenAIFamily(provider string) bool {
	switch provider {
	case "openai", "openai-response", "codex":
		return true
	default:
		return false
	}
}

func isSameProviderFamily(from, to string) bool {
	if from == to {
		return true
	}
	return (isGeminiFamily(from) && isGeminiFamily(to)) ||
		(isOpenAIFamily(from) && isOpenAIFamily(to))
}

// isDisabledCapableProvider reports whether the provider has a native explicit
// disable mode (thinking.type="disabled") for thinking. For these providers a
// ModeNone request must stay fully disabled (Budget=0, Level="") rather than
// falling back to the lowest supported level, which would re-enable thinking.
//
// This is data-driven: it consults the provider's registered applier via
// SupportsNativeDisabled instead of a hardcoded provider-name list. The applier is
// the single source of truth for the disabled marker, so adding a new
// disabled-capable provider requires no change here — implement the marker in that
// provider's applier and the registry lookup picks it up automatically.
func isDisabledCapableProvider(provider string) bool {
	if applier := GetProviderApplier(provider); applier != nil {
		return applier.SupportsNativeDisabled()
	}
	return false
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func logClamp(provider, model string, original, clampedTo, min, max int) {
	log.WithFields(log.Fields{
		"provider":       provider,
		"model":          model,
		"original_value": original,
		"min":            min,
		"max":            max,
		"clamped_to":     clampedTo,
	}).Debug("thinking: budget clamped |")
}
