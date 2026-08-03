// Package kimi implements thinking configuration for Kimi (Moonshot AI) models.
//
// Kimi models use a native thinking object for both enabled and disabled thinking.
// The top-level reasoning_effort field is accepted only as a legacy input by the
// unified extraction layer and is removed from the final Kimi payload.
package kimi

import (
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Applier implements thinking.ProviderApplier for Kimi models.
//
// Kimi-specific behavior:
//   - Enabled thinking: thinking.type="enabled" + thinking.effort=<level>
//   - Disabled thinking: thinking.type="disabled"
//   - Supports budget-to-level conversion
//   - Preserves existing thinking.keep when enabling or changing effort
type Applier struct{}

var _ thinking.ProviderApplier = (*Applier)(nil)

// NewApplier creates a new Kimi thinking applier.
func NewApplier() *Applier {
	return &Applier{}
}

func init() {
	thinking.RegisterProvider("kimi", NewApplier())
}

// Apply applies thinking configuration to Kimi request body.
//
// Expected output format (enabled):
//
//	{
//	  "thinking": {
//	    "type": "enabled",
//	    "effort": "high"
//	  }
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
		return applyCompatibleKimi(body, config)
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
		effort = string(config.Level)
	case thinking.ModeNone:
		// Respect clamped fallback level for models that cannot disable thinking.
		if config.Level != "" && config.Level != thinking.LevelNone {
			effort = string(config.Level)
			break
		}
		// Kimi requires explicit disabled thinking object.
		return applyDisabledThinking(body)
	case thinking.ModeBudget:
		// Convert budget to level using threshold mapping
		level, ok := thinking.ConvertBudgetToLevel(config.Budget)
		if !ok {
			return body, nil
		}
		effort = level
	case thinking.ModeAuto:
		// Auto mode maps to "auto" effort
		effort = string(thinking.LevelAuto)
	default:
		return body, nil
	}

	if effort == "" {
		return body, nil
	}
	return applyEnabledThinking(body, effort)
}

// applyCompatibleKimi applies thinking config for user-defined Kimi models.
func applyCompatibleKimi(body []byte, config thinking.ThinkingConfig) ([]byte, error) {
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
		// User-defined models skip validation, so a clamped fallback level can
		// never legitimately reach here: ModeNone must always mean disabled.
		// Silently enabling thinking for a disable request would be a surprise.
		return applyDisabledThinking(body)
	case thinking.ModeAuto:
		effort = string(thinking.LevelAuto)
	case thinking.ModeBudget:
		// Convert budget to level
		level, ok := thinking.ConvertBudgetToLevel(config.Budget)
		if !ok {
			return body, nil
		}
		effort = level
	default:
		return body, nil
	}

	return applyEnabledThinking(body, effort)
}

func applyEnabledThinking(body []byte, effort string) ([]byte, error) {
	result, err := sjson.DeleteBytes(body, "reasoning_effort")
	if err != nil {
		return body, fmt.Errorf("kimi thinking: failed to clear reasoning_effort: %w", err)
	}

	thRaw, writable := thinkingRaw(result)
	if !writable {
		// Array-valued thinking cannot be written via sjson; keep the
		// reasoning_effort deletion made above so the returned body reflects
		// the mutation that did succeed.
		return result, fmt.Errorf("kimi thinking: failed to set thinking.type: thinking is an array and cannot be written")
	}

	// Batch thinking.type + thinking.effort into a single full-body re-insert
	// instead of two independent re-serializations of the whole payload.
	thRaw, err = sjson.SetBytes(thRaw, "type", "enabled")
	if err != nil {
		return body, fmt.Errorf("kimi thinking: failed to set thinking.type: %w", err)
	}
	thRaw, err = sjson.SetBytes(thRaw, "effort", effort)
	if err != nil {
		return body, fmt.Errorf("kimi thinking: failed to set thinking.effort: %w", err)
	}
	return setThinkingRaw(result, thRaw), nil
}

// thinkingRaw returns the raw JSON of the thinking object, or an empty object
// when thinking is absent, null, or a scalar (sjson promotes those to an object
// when a child key is set). writable is false only for array-valued thinking,
// which sjson rejects; the caller must then leave thinking untouched.
func thinkingRaw(body []byte) (raw []byte, writable bool) {
	return thinking.ObjectSubtreeRaw(body, "thinking")
}

// setThinkingRaw replaces (or creates) the thinking object in a single pass.
func setThinkingRaw(body, thRaw []byte) []byte {
	return thinking.SetObjectSubtreeRaw(body, "thinking", thRaw)
}

func applyDisabledThinking(body []byte) ([]byte, error) {
	result, errDeleteThinking := sjson.DeleteBytes(body, "thinking")
	if errDeleteThinking != nil {
		return body, fmt.Errorf("kimi thinking: failed to clear thinking object: %w", errDeleteThinking)
	}
	result, errDeleteEffort := sjson.DeleteBytes(result, "reasoning_effort")
	if errDeleteEffort != nil {
		return body, fmt.Errorf("kimi thinking: failed to clear reasoning_effort: %w", errDeleteEffort)
	}
	result, errSetType := sjson.SetBytes(result, "thinking.type", "disabled")
	if errSetType != nil {
		return body, fmt.Errorf("kimi thinking: failed to set thinking.type: %w", errSetType)
	}
	return result, nil
}
