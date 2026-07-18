// Package deepseek implements thinking configuration for DeepSeek models.
//
// DeepSeek uses the OpenAI-compatible reasoning_effort format for enabled thinking
// levels, but relies on thinking.type=disabled when thinking is explicitly turned off.
package deepseek

import (
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Applier implements thinking.ProviderApplier for DeepSeek models.
//
// DeepSeek-specific behavior:
//   - Enabled thinking: reasoning_effort (string levels)
//   - Disabled thinking: thinking.type="disabled"
//   - Supports budget-to-level conversion
//   - "xhigh" level is mapped to "max" (DeepSeek does not accept "xhigh")
type Applier struct{}

var _ thinking.ProviderApplier = (*Applier)(nil)

// NewApplier creates a new DeepSeek thinking applier.
func NewApplier() *Applier { return &Applier{} }

func init() {
	thinking.RegisterProvider("deepseek", NewApplier())
}

// Apply applies thinking configuration to DeepSeek request body.
//
// Expected output format (enabled):
//
//	{
//	  "reasoning_effort": "high"
//	}
//
// Expected output format (disabled):
//
//	{
//	  "thinking": {
//	    "type": "disabled"
//	  }
//	}
func (a *Applier) Apply(body []byte, config thinking.ThinkingConfig, modelInfo *registry.ModelInfo) ([]byte, error) {
	if thinking.IsUserDefinedModel(modelInfo) {
		return applyCompatibleDeepSeek(body, config)
	}
	if modelInfo.Thinking == nil {
		return body, nil
	}

	if len(body) == 0 || !gjson.ValidBytes(body) {
		body = []byte(`{}`)
	}

	var effort string
	switch config.Mode {
	case thinking.ModeLevel:
		if config.Level == "" {
			return body, nil
		}
		effort = normalizeDeepSeekEffort(string(config.Level))
	case thinking.ModeNone:
		// Respect clamped fallback level for models that cannot disable thinking.
		if config.Level != "" && config.Level != thinking.LevelNone {
			effort = normalizeDeepSeekEffort(string(config.Level))
			break
		}
		return applyDisabledThinking(body)
	case thinking.ModeBudget:
		// Convert budget to level using threshold mapping.
		level, ok := thinking.ConvertBudgetToLevel(config.Budget)
		if !ok {
			return body, nil
		}
		effort = normalizeDeepSeekEffort(level)
	case thinking.ModeAuto:
		return applyDefaultThinking(body)
	default:
		return body, nil
	}

	if effort == "" {
		return body, nil
	}
	return applyReasoningEffort(body, effort)
}

// applyCompatibleDeepSeek applies thinking config for user-defined DeepSeek models.
func applyCompatibleDeepSeek(body []byte, config thinking.ThinkingConfig) ([]byte, error) {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		body = []byte(`{}`)
	}

	var effort string
	switch config.Mode {
	case thinking.ModeLevel:
		if config.Level == "" {
			return body, nil
		}
		effort = normalizeDeepSeekEffort(string(config.Level))
	case thinking.ModeNone:
		if config.Level != "" && config.Level != thinking.LevelNone {
			effort = normalizeDeepSeekEffort(string(config.Level))
			break
		}
		return applyDisabledThinking(body)
	case thinking.ModeAuto:
		return applyDefaultThinking(body)
	case thinking.ModeBudget:
		// Convert budget to level.
		level, ok := thinking.ConvertBudgetToLevel(config.Budget)
		if !ok {
			return body, nil
		}
		effort = normalizeDeepSeekEffort(level)
	default:
		return body, nil
	}

	return applyReasoningEffort(body, effort)
}

func applyDefaultThinking(body []byte) ([]byte, error) {
	result, errDeleteThinking := sjson.DeleteBytes(body, "thinking")
	if errDeleteThinking != nil {
		return body, fmt.Errorf("deepseek thinking: failed to clear thinking object: %w", errDeleteThinking)
	}
	result, errDeleteEffort := sjson.DeleteBytes(result, "reasoning_effort")
	if errDeleteEffort != nil {
		return body, fmt.Errorf("deepseek thinking: failed to clear reasoning_effort: %w", errDeleteEffort)
	}
	return result, nil
}

// normalizeDeepSeekEffort maps internal thinking levels to DeepSeek-accepted values.
// DeepSeek accepts "high" and "max", but not "xhigh". When budget > 24576,
// ConvertBudgetToLevel returns "xhigh", which must be mapped to "max" for DeepSeek.
func normalizeDeepSeekEffort(effort string) string {
	switch effort {
	case "xhigh":
		return "max"
	default:
		return effort
	}
}

func applyReasoningEffort(body []byte, effort string) ([]byte, error) {
	result, errDeleteThinking := sjson.DeleteBytes(body, "thinking")
	if errDeleteThinking != nil {
		return body, fmt.Errorf("deepseek thinking: failed to clear thinking object: %w", errDeleteThinking)
	}
	result, errSetEffort := sjson.SetBytes(result, "reasoning_effort", effort)
	if errSetEffort != nil {
		return body, fmt.Errorf("deepseek thinking: failed to set reasoning_effort: %w", errSetEffort)
	}
	return result, nil
}

func applyDisabledThinking(body []byte) ([]byte, error) {
	result, errDeleteThinking := sjson.DeleteBytes(body, "thinking")
	if errDeleteThinking != nil {
		return body, fmt.Errorf("deepseek thinking: failed to clear thinking object: %w", errDeleteThinking)
	}
	result, errDeleteEffort := sjson.DeleteBytes(result, "reasoning_effort")
	if errDeleteEffort != nil {
		return body, fmt.Errorf("deepseek thinking: failed to clear reasoning_effort: %w", errDeleteEffort)
	}
	result, errSetType := sjson.SetBytes(result, "thinking.type", "disabled")
	if errSetType != nil {
		return body, fmt.Errorf("deepseek thinking: failed to set thinking.type: %w", errSetType)
	}
	return result, nil
}
