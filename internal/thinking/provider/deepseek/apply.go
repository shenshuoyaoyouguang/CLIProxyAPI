// Package deepseek implements thinking configuration for DeepSeek models.
//
// DeepSeek models use the reasoning_effort format with discrete levels
// (low/medium/high/xhigh/max), compatible with the OpenAI API format.
// See: https://api-docs.deepseek.com/guides/thinking_mode
//
// DeepSeek-specific behavior:
//   - Output format: reasoning_effort (string: low/medium/high/xhigh/max)
//   - Level-only mode: no native numeric budget support (converted via thresholds)
//   - Server-side mapping: low/medium → high, xhigh → max
//   - Supports thinking.type: "enabled"/"disabled" for explicit control
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
//   - Output format: reasoning_effort (string, top-level) + thinking.type
//   - Level-only mode: no native numeric budget support
//   - Enabled thinking: sets reasoning_effort and thinking.type="enabled"
//   - Disabled thinking: removes reasoning_effort and sets thinking.type="disabled"
type Applier struct{}

var _ thinking.ProviderApplier = (*Applier)(nil)

// NewApplier creates a new DeepSeek thinking applier.
func NewApplier() *Applier {
	return &Applier{}
}

func init() {
	thinking.RegisterProvider("deepseek", NewApplier())
}

// Apply applies thinking configuration to DeepSeek request body.
//
// DeepSeek uses both reasoning_effort and thinking.type for thinking control.
// The thinking.type defaults to "enabled" when reasoning_effort is set.
// We explicitly set both for robustness.
//
// Expected output format (enabled):
//
//	{
//	  "thinking": { "type": "enabled" },
//	  "reasoning_effort": "high"
//	}
//
// Expected output format (disabled):
//
//	{
//	  "thinking": { "type": "disabled" }
//	}
//
// Expected output format (auto):
//
//	{
//	  "thinking": { "type": "enabled" },
//	  "reasoning_effort": "auto"
//	}
func (a *Applier) Apply(body []byte, config thinking.ThinkingConfig, modelInfo *registry.ModelInfo) ([]byte, error) {
	if thinking.IsUserDefinedModel(modelInfo) {
		return applyCompatibleDeepSeek(body, config)
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
		return applyEnabledThinking(body, string(config.Level))

	case thinking.ModeNone:
		// Respect clamped fallback level for models that cannot disable thinking.
		if config.Level != "" && config.Level != thinking.LevelNone {
			return applyEnabledThinking(body, string(config.Level))
		}
		return applyDisabledThinking(body)

	case thinking.ModeAuto:
		return applyEnabledThinking(body, "auto")
	}

	return body, nil
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
		effort = string(config.Level)
	case thinking.ModeNone:
		if config.Level == "" || config.Level == thinking.LevelNone {
			return applyDisabledThinking(body)
		}
		if config.Level != "" {
			effort = string(config.Level)
		}
	case thinking.ModeAuto:
		effort = "auto"
	case thinking.ModeBudget:
		// Convert budget to level using threshold mapping
		level, ok := thinking.ConvertBudgetToLevel(config.Budget)
		if !ok {
			return body, nil
		}
		effort = level
	default:
		return body, nil
	}

	if effort == "" {
		return body, nil
	}

	return applyEnabledThinking(body, effort)
}

// applyEnabledThinking sets thinking.type="enabled" and reasoning_effort to the given level.
func applyEnabledThinking(body []byte, effort string) ([]byte, error) {
	result, err := sjson.SetBytes(body, "thinking.type", "enabled")
	if err != nil {
		return body, fmt.Errorf("deepseek thinking: failed to set thinking.type: %w", err)
	}
	result, err = sjson.SetBytes(result, "reasoning_effort", effort)
	if err != nil {
		return body, fmt.Errorf("deepseek thinking: failed to set reasoning_effort: %w", err)
	}
	return result, nil
}

// applyDisabledThinking sets thinking.type to "disabled" and removes reasoning_effort.
func applyDisabledThinking(body []byte) ([]byte, error) {
	result, err := sjson.DeleteBytes(body, "reasoning_effort")
	if err != nil {
		return body, fmt.Errorf("deepseek thinking: failed to clear reasoning_effort: %w", err)
	}
	result, err = sjson.SetBytes(result, "thinking.type", "disabled")
	if err != nil {
		return body, fmt.Errorf("deepseek thinking: failed to set thinking.type: %w", err)
	}
	return result, nil
}
