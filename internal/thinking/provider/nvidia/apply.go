// Package nvidia implements thinking configuration for NVIDIA models.
//
// NVIDIA Nemotron models use enable_thinking + reasoning_budget + medium_effort
// for thinking control, served through an OpenAI-compatible API.
//
// NVIDIA-specific behavior:
//   - enable_thinking (bool): primary toggle for reasoning mode
//   - reasoning_budget (int, optional): cap thinking token count
//   - medium_effort (bool, optional): reduce thinking depth
//   - Also strips reasoning_effort since NVIDIA uses native parameters
//
// See: https://build.nvidia.com/nvidia/nemotron-3-ultra-550b-a55b
package nvidia

import (
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Applier implements thinking.ProviderApplier for NVIDIA models.
//
// NVIDIA Nemotron thinking modes:
//   - Disabled: enable_thinking=false
//   - Medium effort: enable_thinking=true, medium_effort=true
//   - Full thinking: enable_thinking=true
//   - With budget: enable_thinking=true, reasoning_budget=<N>
type Applier struct{}

var _ thinking.ProviderApplier = (*Applier)(nil)

// NewApplier creates a new NVIDIA thinking applier.
func NewApplier() *Applier {
	return &Applier{}
}

func init() {
	thinking.RegisterProvider("nvidia", NewApplier())
}

// Apply applies thinking configuration to NVIDIA request body.
//
// NVIDIA Nemotron uses its own thinking parameters alongside the
// OpenAI-compatible format. We strip the standard reasoning_effort
// and use NVIDIA-native fields instead.
//
// Expected output format (disabled):
//
//	{
//	  "enable_thinking": false
//	}
//
// Expected output format (medium effort):
//
//	{
//	  "enable_thinking": true,
//	  "medium_effort": true
//	}
//
// Expected output format (full thinking):
//
//	{
//	  "enable_thinking": true
//	}
//
// Expected output format (with budget):
//
//	{
//	  "enable_thinking": true,
//	  "reasoning_budget": 4096
//	}
func (a *Applier) Apply(body []byte, config thinking.ThinkingConfig, modelInfo *registry.ModelInfo) ([]byte, error) {
	if thinking.IsUserDefinedModel(modelInfo) {
		return applyCompatibleNvidia(body, config)
	}
	if modelInfo.Thinking == nil {
		return body, nil
	}

	// Only handle ModeLevel, ModeNone, and ModeAuto; other modes pass through unchanged.
	if config.Mode != thinking.ModeLevel && config.Mode != thinking.ModeNone && config.Mode != thinking.ModeAuto {
		return body, nil
	}

	if len(body) == 0 || !gjson.ValidBytes(body) {
		body = []byte(`{}`)
	}

	switch config.Mode {
	case thinking.ModeLevel:
		if config.Level == "" {
			return body, nil
		}
		return applyEnabledLevel(body, string(config.Level))

	case thinking.ModeNone:
		// Respect clamped fallback level for models that cannot disable thinking.
		if config.Level != "" && config.Level != thinking.LevelNone {
			return applyEnabledLevel(body, string(config.Level))
		}
		return applyDisabledThinking(body)

	case thinking.ModeAuto:
		return applyEnabledAuto(body)
	}

	return body, nil
}

// applyCompatibleNvidia applies thinking config for user-defined NVIDIA models.
func applyCompatibleNvidia(body []byte, config thinking.ThinkingConfig) ([]byte, error) {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		body = []byte(`{}`)
	}

	switch config.Mode {
	case thinking.ModeLevel:
		if config.Level == "" {
			return body, nil
		}
		return applyEnabledLevel(body, string(config.Level))

	case thinking.ModeNone:
		if config.Level == "" || config.Level == thinking.LevelNone {
			return applyDisabledThinking(body)
		}
		if config.Level != "" {
			return applyEnabledLevel(body, string(config.Level))
		}

	case thinking.ModeAuto:
		return applyEnabledAuto(body)

	case thinking.ModeBudget:
		if config.Budget == 0 {
			return applyDisabledThinking(body)
		}
		return applyBudget(body, config.Budget)
	}

	return body, nil
}

// applyEnabledLevel sets enable_thinking and optionally medium_effort.
func applyEnabledLevel(body []byte, level string) ([]byte, error) {
	result, err := sjson.SetBytes(body, "enable_thinking", true)
	if err != nil {
		return body, fmt.Errorf("nvidia thinking: failed to set enable_thinking: %w", err)
	}

	// Strip reasoning_effort to avoid conflicts
	result, err = sjson.DeleteBytes(result, "reasoning_effort")
	if err != nil {
		return body, fmt.Errorf("nvidia thinking: failed to clear reasoning_effort: %w", err)
	}

	switch level {
	case string(thinking.LevelLow):
		// reasoning_budget and medium_effort are mutually exclusive upstream;
		// a budget request clamped to low must not leave the original budget
		// field behind (mirrors applyBudget clearing medium_effort).
		result, err = sjson.DeleteBytes(result, "reasoning_budget")
		if err != nil {
			return body, fmt.Errorf("nvidia thinking: failed to clear reasoning_budget: %w", err)
		}
		result, err = sjson.SetBytes(result, "medium_effort", true)
		if err != nil {
			return body, fmt.Errorf("nvidia thinking: failed to set medium_effort: %w", err)
		}
	default:
		result, err = sjson.DeleteBytes(result, "medium_effort")
		if err != nil {
			return body, fmt.Errorf("nvidia thinking: failed to clear medium_effort: %w", err)
		}
		// reasoning_budget and medium_effort are mutually exclusive upstream. A
		// level request must not leave a previously supplied reasoning_budget
		// behind, mirroring the low branch above.
		result, err = sjson.DeleteBytes(result, "reasoning_budget")
		if err != nil {
			return body, fmt.Errorf("nvidia thinking: failed to clear reasoning_budget: %w", err)
		}
	}

	return result, nil
}

// applyDisabledThinking sets enable_thinking to false and cleans up related fields.
func applyDisabledThinking(body []byte) ([]byte, error) {
	result, err := sjson.SetBytes(body, "enable_thinking", false)
	if err != nil {
		return body, fmt.Errorf("nvidia thinking: failed to set enable_thinking: %w", err)
	}

	result, err = sjson.DeleteBytes(result, "reasoning_effort")
	if err != nil {
		return body, fmt.Errorf("nvidia thinking: failed to clear reasoning_effort: %w", err)
	}

	result, err = sjson.DeleteBytes(result, "medium_effort")
	if err != nil {
		return body, fmt.Errorf("nvidia thinking: failed to clear medium_effort: %w", err)
	}

	result, err = sjson.DeleteBytes(result, "reasoning_budget")
	if err != nil {
		return body, fmt.Errorf("nvidia thinking: failed to clear reasoning_budget: %w", err)
	}

	return result, nil
}

// applyEnabledAuto enables thinking without explicit budget or effort.
func applyEnabledAuto(body []byte) ([]byte, error) {
	result, err := sjson.SetBytes(body, "enable_thinking", true)
	if err != nil {
		return body, fmt.Errorf("nvidia thinking: failed to set enable_thinking: %w", err)
	}

	result, err = sjson.DeleteBytes(result, "reasoning_effort")
	if err != nil {
		return body, fmt.Errorf("nvidia thinking: failed to clear reasoning_effort: %w", err)
	}

	result, err = sjson.DeleteBytes(result, "medium_effort")
	if err != nil {
		return body, fmt.Errorf("nvidia thinking: failed to clear medium_effort: %w", err)
	}

	result, err = sjson.DeleteBytes(result, "reasoning_budget")
	if err != nil {
		return body, fmt.Errorf("nvidia thinking: failed to clear reasoning_budget: %w", err)
	}

	return result, nil
}

// applyBudget enables thinking with a specific reasoning_budget.
func applyBudget(body []byte, budget int) ([]byte, error) {
	result, err := sjson.SetBytes(body, "enable_thinking", true)
	if err != nil {
		return body, fmt.Errorf("nvidia thinking: failed to set enable_thinking: %w", err)
	}

	result, err = sjson.DeleteBytes(result, "reasoning_effort")
	if err != nil {
		return body, fmt.Errorf("nvidia thinking: failed to clear reasoning_effort: %w", err)
	}

	result, err = sjson.DeleteBytes(result, "medium_effort")
	if err != nil {
		return body, fmt.Errorf("nvidia thinking: failed to clear medium_effort: %w", err)
	}

	result, err = sjson.SetBytes(result, "reasoning_budget", budget)
	if err != nil {
		return body, fmt.Errorf("nvidia thinking: failed to set reasoning_budget: %w", err)
	}

	return result, nil
}
