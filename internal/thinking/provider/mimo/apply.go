// Package mimo implements thinking configuration for Xiaomi MiMo models.
//
// MiMo models use thinking.type for both enabled and disabled thinking states.
// When thinking.type is absent, reasoning_effort is accepted as a fallback
// (mapped to MiMo budget ranges).
package mimo

import (
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Applier implements thinking.ProviderApplier for Xiaomi MiMo models.
//
// MiMo-specific behavior:
//   - Enabled thinking: thinking.type="enabled"
//   - Disabled thinking: thinking.type="disabled"
//   - Budget mode boosts max_completion_tokens to at least the budget value
type Applier struct{}

var _ thinking.ProviderApplier = (*Applier)(nil)

// NewApplier creates a new MiMo thinking applier.
func NewApplier() *Applier { return &Applier{} }

func init() {
	thinking.RegisterProvider("mimo", NewApplier())
}

// Apply applies thinking configuration to MiMo request body.
//
// Expected output format (enabled):
//
//	{
//	  "thinking": {
//	    "type": "enabled"
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
		return applyCompatibleMimo(body, config)
	}
	if modelInfo.Thinking == nil {
		return body, nil
	}

	if len(body) == 0 || !gjson.ValidBytes(body) {
		body = []byte(`{}`)
	}

	switch config.Mode {
	case thinking.ModeLevel:
		if config.Level == "" || config.Level == thinking.LevelNone {
			return applyDisabledThinking(body)
		}
		return applyEnabledThinking(body)
	case thinking.ModeNone:
		if config.Level != "" && config.Level != thinking.LevelNone {
			return applyEnabledThinking(body)
		}
		return applyDisabledThinking(body)
	case thinking.ModeBudget:
		if config.Budget == 0 {
			return applyDisabledThinking(body)
		}
		body, err := applyEnabledThinking(body)
		if err != nil {
			return body, err
		}
		return mimoBoostMaxCompletion(body, config.Budget), nil
	case thinking.ModeAuto:
		return body, nil
	default:
		return body, nil
	}
}

// applyCompatibleMimo applies thinking config for user-defined MiMo models.
func applyCompatibleMimo(body []byte, config thinking.ThinkingConfig) ([]byte, error) {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		body = []byte(`{}`)
	}

	switch config.Mode {
	case thinking.ModeLevel:
		if config.Level == "" || config.Level == thinking.LevelNone {
			return applyDisabledThinking(body)
		}
		return applyEnabledThinking(body)
	case thinking.ModeNone:
		return applyDisabledThinking(body)
	case thinking.ModeAuto:
		return body, nil
	case thinking.ModeBudget:
		if config.Budget == 0 {
			return applyDisabledThinking(body)
		}
		body, err := applyEnabledThinking(body)
		if err != nil {
			return body, err
		}
		return mimoBoostMaxCompletion(body, config.Budget), nil
	default:
		return body, nil
	}
}

func applyEnabledThinking(body []byte) ([]byte, error) {
	result, errSetType := sjson.SetBytes(body, "thinking.type", "enabled")
	if errSetType != nil {
		return body, fmt.Errorf("mimo thinking: failed to set thinking.type: %w", errSetType)
	}
	return result, nil
}

// mimoBoostMaxCompletion sets max_completion_tokens to at least the given budget
// when thinking is enabled. This gives the model maximum space for reasoning.
func mimoBoostMaxCompletion(body []byte, budget int) []byte {
	if budget <= 0 {
		return body
	}
	thinkingType := gjson.GetBytes(body, "thinking.type")
	if !thinkingType.Exists() || thinkingType.String() != "enabled" {
		return body
	}
	current := gjson.GetBytes(body, "max_completion_tokens").Int()
	if int64(budget) > current {
		body, _ = sjson.SetBytes(body, "max_completion_tokens", budget)
	}
	return body
}

func applyDisabledThinking(body []byte) ([]byte, error) {
	result, errSetType := sjson.SetBytes(body, "thinking.type", "disabled")
	if errSetType != nil {
		return body, fmt.Errorf("mimo thinking: failed to set thinking.type: %w", errSetType)
	}
	return result, nil
}
