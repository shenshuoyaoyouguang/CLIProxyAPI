// Package gemini implements thinking configuration for Gemini models.
//
// Gemini models have two formats:
//   - Gemini 2.5: Uses thinkingBudget (numeric)
//   - Gemini 3.x: Uses thinkingLevel (string: minimal/low/medium/high)
//     or thinkingBudget=-1 for auto/dynamic mode
//
// Output format is determined by ThinkingConfig.Mode and ThinkingSupport.Levels:
//   - ModeAuto: Always uses thinkingBudget=-1 (both Gemini 2.5 and 3.x)
//   - len(Levels) > 0: Uses thinkingLevel (Gemini 3.x discrete levels)
//   - len(Levels) == 0: Uses thinkingBudget (Gemini 2.5)
package gemini

import (
	"bytes"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Applier applies thinking configuration for Gemini models.
//
// Gemini-specific behavior:
//   - Gemini 2.5: thinkingBudget format, flash series supports ZeroAllowed
//   - Gemini 3.x: thinkingLevel format, disable by removing thinkingConfig when zero is allowed
//   - Use ThinkingSupport.Levels to decide output format
type Applier struct{}

// NewApplier creates a new Gemini thinking applier.
func NewApplier() *Applier {
	return &Applier{}
}

func init() {
	thinking.RegisterProvider("gemini", NewApplier())
}

// Apply applies thinking configuration to Gemini request body.
//
// Expected output format (Gemini 2.5):
//
//	{
//	  "generationConfig": {
//	    "thinkingConfig": {
//	      "thinkingBudget": 8192,
//	      "includeThoughts": true
//	    }
//	  }
//	}
//
// Expected output format (Gemini 3.x):
//
//	{
//	  "generationConfig": {
//	    "thinkingConfig": {
//	      "thinkingLevel": "high",
//	      "includeThoughts": true
//	    }
//	  }
//	}
func (a *Applier) Apply(body []byte, config thinking.ThinkingConfig, modelInfo *registry.ModelInfo) ([]byte, error) {
	if thinking.IsUserDefinedModel(modelInfo) {
		return a.applyCompatible(body, config)
	}
	if modelInfo.Thinking == nil {
		return body, nil
	}

	if config.Mode != thinking.ModeBudget && config.Mode != thinking.ModeLevel && config.Mode != thinking.ModeNone && config.Mode != thinking.ModeAuto {
		return body, nil
	}

	if len(body) == 0 || !gjson.ValidBytes(body) {
		body = []byte(`{}`)
	}

	// Choose format based on config.Mode and model capabilities:
	// - ModeLevel: use Level format (validation will reject unsupported levels)
	// - ModeNone: use Level format if model has Levels, else Budget format
	// - ModeBudget/ModeAuto: use Budget format
	switch config.Mode {
	case thinking.ModeLevel:
		return a.applyLevelFormat(body, config)
	case thinking.ModeNone:
		// ModeNone: route based on model capability (has Levels or not)
		if len(modelInfo.Thinking.Levels) > 0 {
			return a.applyLevelFormat(body, config)
		}
		return a.applyBudgetFormat(body, config)
	default:
		return a.applyBudgetFormat(body, config)
	}
}

func (a *Applier) applyCompatible(body []byte, config thinking.ThinkingConfig) ([]byte, error) {
	if config.Mode != thinking.ModeBudget && config.Mode != thinking.ModeLevel && config.Mode != thinking.ModeNone && config.Mode != thinking.ModeAuto {
		return body, nil
	}

	if len(body) == 0 || !gjson.ValidBytes(body) {
		body = []byte(`{}`)
	}

	if config.Mode == thinking.ModeAuto {
		return a.applyBudgetFormat(body, config)
	}

	if config.Mode == thinking.ModeLevel || (config.Mode == thinking.ModeNone && config.Level != "") {
		return a.applyLevelFormat(body, config)
	}

	return a.applyBudgetFormat(body, config)
}

func (a *Applier) applyLevelFormat(body []byte, config thinking.ThinkingConfig) ([]byte, error) {
	// ModeNone semantics:
	//   - ModeNone + Budget=0: remove the thinking amount configuration.
	//   - ModeNone + Budget>0: clamp to the model's lowest supported amount.
	// Summary visibility remains independent and is restored only when explicitly set.

	// ModeNone + fully disabled removes the whole thinkingConfig, which works for
	// any existing value shape (including arrays), so handle it before the
	// subtree writability check.
	if config.Mode == thinking.ModeNone && config.Budget == 0 && config.Level == "" {
		// With the amount fully disabled, visibility is irrelevant. Restoring
		// includeThoughts alone would recreate thinkingConfig and let a
		// default-on model think again.
		result, _ := sjson.DeleteBytes(body, "generationConfig.thinkingConfig")
		return result, nil
	}

	tcRaw, writable := thinkingConfigRaw(body)
	if !writable {
		return body, nil
	}
	original := append([]byte(nil), tcRaw...)

	// Remove conflicting fields to avoid both thinkingLevel and thinkingBudget in output.
	tcRaw, _ = sjson.DeleteBytes(tcRaw, "thinkingBudget")
	tcRaw, _ = sjson.DeleteBytes(tcRaw, "thinking_budget")
	tcRaw, _ = sjson.DeleteBytes(tcRaw, "thinking_level")
	// Normalize includeThoughts field name and retain only documented booleans.
	tcRaw, _ = sjson.DeleteBytes(tcRaw, "includeThoughts")
	tcRaw, _ = sjson.DeleteBytes(tcRaw, "include_thoughts")

	if config.Mode == thinking.ModeNone {
		if config.Level != "" {
			tcRaw, _ = sjson.SetBytes(tcRaw, "thinkingLevel", string(config.Level))
		}
		tcRaw = applyGeminiIncludeThoughts(tcRaw, body)
		return setGeminiThinkingConfigIfChanged(body, original, tcRaw), nil
	}

	// Only handle ModeLevel - budget conversion should be done by upper layer
	if config.Mode != thinking.ModeLevel {
		return body, nil
	}

	tcRaw, _ = sjson.SetBytes(tcRaw, "thinkingLevel", string(config.Level))
	tcRaw = applyGeminiIncludeThoughts(tcRaw, body)
	return setGeminiThinkingConfigIfChanged(body, original, tcRaw), nil
}

func (a *Applier) applyBudgetFormat(body []byte, config thinking.ThinkingConfig) ([]byte, error) {
	tcRaw, writable := thinkingConfigRaw(body)
	if !writable {
		return body, nil
	}
	original := append([]byte(nil), tcRaw...)

	// Remove conflicting fields to avoid both thinkingLevel and thinkingBudget in output.
	tcRaw, _ = sjson.DeleteBytes(tcRaw, "thinkingLevel")
	tcRaw, _ = sjson.DeleteBytes(tcRaw, "thinking_level")
	tcRaw, _ = sjson.DeleteBytes(tcRaw, "thinking_budget")
	// Normalize includeThoughts field name and retain only documented booleans.
	tcRaw, _ = sjson.DeleteBytes(tcRaw, "includeThoughts")
	tcRaw, _ = sjson.DeleteBytes(tcRaw, "include_thoughts")

	tcRaw, _ = sjson.SetBytes(tcRaw, "thinkingBudget", config.Budget)
	tcRaw = applyGeminiIncludeThoughts(tcRaw, body)
	return setGeminiThinkingConfigIfChanged(body, original, tcRaw), nil
}

// thinkingConfigRaw returns the raw JSON of generationConfig.thinkingConfig, or
// an empty object when absent, null, or scalar (sjson promotes those to an
// object when a child key is set). writable is false only for array-valued
// thinkingConfig, which sjson rejects; the caller must then leave the body
// untouched to match sjson's error-on-array behavior.
func thinkingConfigRaw(body []byte) (raw []byte, writable bool) {
	tc := gjson.GetBytes(body, "generationConfig.thinkingConfig")
	switch {
	case tc.IsObject():
		return []byte(tc.Raw), true
	case tc.IsArray():
		return nil, false
	default:
		return []byte(`{}`), true
	}
}

// setGeminiThinkingConfig replaces (or creates) generationConfig.thinkingConfig
// in a single full-body pass.
func setGeminiThinkingConfig(body, tcRaw []byte) []byte {
	out, _ := sjson.SetRawBytes(body, "generationConfig.thinkingConfig", tcRaw)
	return out
}

// setGeminiThinkingConfigIfChanged replaces generationConfig.thinkingConfig only
// when the subtree actually changed. When nothing was written (e.g. ModeNone
// with no level and no includeThoughts), the original body is returned untouched
// so an absent thinkingConfig is not created and null/scalar values are not
// promoted, matching the legacy per-path sjson behaviour.
func setGeminiThinkingConfigIfChanged(body, original, tcRaw []byte) []byte {
	if bytes.Equal(original, tcRaw) {
		return body
	}
	return setGeminiThinkingConfig(body, tcRaw)
}

// applyGeminiIncludeThoughts restores an explicit includeThoughts boolean from
// the original body onto the thinkingConfig subtree doc. The field name is
// normalized to camelCase and only documented boolean values are retained.
func applyGeminiIncludeThoughts(tcRaw, original []byte) []byte {
	for _, path := range []string{
		"generationConfig.thinkingConfig.includeThoughts",
		"generationConfig.thinkingConfig.include_thoughts",
	} {
		switch value := gjson.GetBytes(original, path); value.Type {
		case gjson.True:
			tcRaw, _ = sjson.SetBytes(tcRaw, "includeThoughts", true)
			return tcRaw
		case gjson.False:
			tcRaw, _ = sjson.SetBytes(tcRaw, "includeThoughts", false)
			return tcRaw
		}
	}
	return tcRaw
}
