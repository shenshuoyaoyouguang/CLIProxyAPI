// Package claude implements thinking configuration scaffolding for Claude models.
//
// Claude models support two thinking control styles:
//   - Manual thinking: thinking.type="enabled" with thinking.budget_tokens (token budget)
//   - Adaptive thinking (Claude 4.6): thinking.type="adaptive" with output_config.effort (low/medium/high/max)
//
// Some Claude models support ZeroAllowed (sonnet-4-5, opus-4-5), while older models do not.
// See: _bmad-output/planning-artifacts/architecture.md#Epic-6
package claude

import (
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Applier implements thinking.ProviderApplier for Claude models.
// This applier is stateless and holds no configuration.
type Applier struct{}

// NewApplier creates a new Claude thinking applier.
func NewApplier() *Applier {
	return &Applier{}
}

func init() {
	thinking.RegisterProvider("claude", NewApplier())
}

// Apply applies thinking configuration to Claude request body.
//
// IMPORTANT: This method expects config to be pre-validated by thinking.ValidateConfig.
// ValidateConfig handles:
//   - Mode conversion (Level→Budget, Auto→Budget)
//   - Budget clamping to model range
//   - ZeroAllowed constraint enforcement
//
// Apply processes:
//   - ModeBudget: manual thinking budget_tokens
//   - ModeLevel: adaptive thinking effort (Claude 4.6)
//   - ModeAuto: provider default adaptive/manual behavior
//   - ModeNone: disabled
//
// Expected output format when enabled:
//
//	{
//	  "thinking": {
//	    "type": "enabled",
//	    "budget_tokens": 16384
//	  }
//	}
//
// Expected output format for adaptive:
//
//	{
//	  "thinking": {
//	    "type": "adaptive"
//	  },
//	  "output_config": {
//	    "effort": "high"
//	  }
//	}
//
// Expected output format when disabled:
//
//	{
//	  "thinking": {
//	    "type": "disabled"
//	  }
//	}
func (a *Applier) Apply(body []byte, config thinking.ThinkingConfig, modelInfo *registry.ModelInfo) ([]byte, error) {
	if thinking.IsUserDefinedModel(modelInfo) {
		return applyCompatibleClaude(body, config)
	}
	if modelInfo.Thinking == nil {
		return body, nil
	}

	if len(body) == 0 || !gjson.ValidBytes(body) {
		body = []byte(`{}`)
	}

	supportsAdaptive := modelInfo != nil && modelInfo.Thinking != nil && len(modelInfo.Thinking.Levels) > 0

	switch config.Mode {
	case thinking.ModeNone:
		return applyClaudeDisabled(body, true), nil

	case thinking.ModeLevel:
		// Adaptive thinking effort is only valid when the model advertises discrete levels.
		// (Claude 4.6 uses output_config.effort.)
		if supportsAdaptive && config.Level != "" {
			thRaw, writable := thinkingRaw(body)
			if writable {
				thRaw, _ = sjson.SetBytes(thRaw, "type", "adaptive")
				thRaw, _ = sjson.DeleteBytes(thRaw, "budget_tokens")
				body = setThinkingRaw(body, thRaw)
			}
			return setOutputConfigEffort(body, string(config.Level)), nil
		}

		// Fallback for non-adaptive Claude models: convert level to budget_tokens.
		if budget, ok := thinking.ConvertLevelToBudget(string(config.Level)); ok {
			config.Mode = thinking.ModeBudget
			config.Budget = budget
			config.Level = ""
		} else {
			return body, nil
		}
		fallthrough

	case thinking.ModeBudget:
		// Budget is expected to be pre-validated by ValidateConfig (clamped, ZeroAllowed enforced).
		// Decide enabled/disabled based on budget value.
		if config.Budget == 0 {
			return applyClaudeDisabled(body, false), nil
		}
		return a.applyClaudeBudget(body, config.Budget, modelInfo), nil

	case thinking.ModeAuto:
		// For Claude 4.6 models, auto maps to adaptive thinking with upstream defaults.
		if supportsAdaptive {
			thRaw, writable := thinkingRaw(body)
			if writable {
				thRaw, _ = sjson.SetBytes(thRaw, "type", "adaptive")
				thRaw, _ = sjson.DeleteBytes(thRaw, "budget_tokens")
				body = setThinkingRaw(body, thRaw)
			}
			return deleteOutputConfigEffort(body), nil
		}

		// Legacy fallback for manual-thinking Claude models: "auto" maps to the
		// model's minimum budget. Anthropic rejects thinking.type:"enabled"
		// without budget_tokens (HTTP 400), so a bare "enabled" here would fail
		// upstream. Without capability info, leave the request untouched so the
		// client's own thinking configuration stands.
		minBudget := 0
		if modelInfo != nil && modelInfo.Thinking != nil {
			minBudget = modelInfo.Thinking.Min
		}
		if minBudget <= 0 {
			return body, nil
		}
		thRaw, writable := thinkingRaw(body)
		if writable {
			thRaw, _ = sjson.SetBytes(thRaw, "type", "enabled")
			thRaw, _ = sjson.SetBytes(thRaw, "budget_tokens", minBudget)
			body = setThinkingRaw(body, thRaw)
		}
		return deleteOutputConfigEffort(body), nil

	default:
		return body, nil
	}
}

// applyClaudeBudget enables manual thinking with the given budget, enforcing the
// Anthropic constraint that max_tokens must exceed budget_tokens. The thinking
// mutations and any budget adjustment are batched into a single re-insert; only
// the optional max_tokens write-back triggers a second full-body pass.
func (a *Applier) applyClaudeBudget(body []byte, budgetTokens int, modelInfo *registry.ModelInfo) []byte {
	effectiveMax, setDefaultMax := a.effectiveMaxTokens(body, modelInfo)

	// Compute the budget we would apply after enforcing budget_tokens < max_tokens.
	adjustedBudget := budgetTokens
	if effectiveMax > 0 && adjustedBudget >= effectiveMax {
		adjustedBudget = effectiveMax - 1
	}

	minBudget := 0
	if modelInfo != nil && modelInfo.Thinking != nil {
		minBudget = modelInfo.Thinking.Min
	}
	if minBudget > 0 && adjustedBudget > 0 && adjustedBudget < minBudget {
		// The two constraints cannot both be satisfied: honouring
		// budget_tokens < max_tokens would push the budget below the model
		// minimum. Leaving the request untouched would forward the original
		// budget_tokens (>= max_tokens), which Anthropic rejects with a 400.
		// Disable thinking instead so the request stays valid, and leave a trace
		// because this is a silent capability downgrade.
		log.Warnf("claude thinking: budget %d cannot satisfy both max_tokens=%d and model minimum=%d; disabling thinking for this request", budgetTokens, effectiveMax, minBudget)
		body = applyClaudeDisabled(body, false)
		if setDefaultMax && effectiveMax > 0 {
			body, _ = sjson.SetBytes(body, "max_tokens", effectiveMax)
		}
		return body
	}

	// Batch thinking.type + budget_tokens into a single re-insert.
	thRaw, writable := thinkingRaw(body)
	if writable {
		thRaw, _ = sjson.SetBytes(thRaw, "type", "enabled")
		thRaw, _ = sjson.SetBytes(thRaw, "budget_tokens", adjustedBudget)
		body = setThinkingRaw(body, thRaw)
	}
	body = deleteOutputConfigEffort(body)

	// If max_tokens came from the model default, write it back so the request
	// carries the value the budget was clamped against. The legacy
	// normalizeClaudeBudget early-returned for budgetTokens <= 0 before this
	// write-back, so keep that boundary (the thinking mutations above still
	// apply, mirroring the original Apply-then-normalize order).
	if setDefaultMax && effectiveMax > 0 && budgetTokens > 0 {
		body, _ = sjson.SetBytes(body, "max_tokens", effectiveMax)
	}
	return body
}

// effectiveMaxTokens returns the max tokens to cap thinking:
// prefer request-provided max_tokens; otherwise fall back to model default.
// The boolean indicates whether the value came from the model default (and thus should be written back).
func (a *Applier) effectiveMaxTokens(body []byte, modelInfo *registry.ModelInfo) (max int, fromModel bool) {
	if maxTok := gjson.GetBytes(body, "max_tokens"); maxTok.Exists() && maxTok.Int() > 0 {
		return int(maxTok.Int()), false
	}
	if modelInfo != nil && modelInfo.MaxCompletionTokens > 0 {
		return modelInfo.MaxCompletionTokens, true
	}
	return 0, false
}

func applyCompatibleClaude(body []byte, config thinking.ThinkingConfig) ([]byte, error) {
	if config.Mode != thinking.ModeBudget && config.Mode != thinking.ModeNone && config.Mode != thinking.ModeAuto && config.Mode != thinking.ModeLevel {
		return body, nil
	}

	if len(body) == 0 || !gjson.ValidBytes(body) {
		body = []byte(`{}`)
	}

	switch config.Mode {
	case thinking.ModeNone:
		return applyClaudeDisabled(body, true), nil
	case thinking.ModeAuto:
		// For user-defined models there is no capability data to derive a valid
		// "auto" from: Anthropic rejects thinking.type:"enabled" without
		// budget_tokens, so leave the request untouched rather than risk a 400.
		return body, nil
	case thinking.ModeLevel:
		// For user-defined models, interpret ModeLevel as Claude adaptive thinking effort.
		// Upstream is responsible for validating whether the target model supports it.
		if config.Level == "" {
			return body, nil
		}
		thRaw, writable := thinkingRaw(body)
		if writable {
			thRaw, _ = sjson.SetBytes(thRaw, "type", "adaptive")
			thRaw, _ = sjson.DeleteBytes(thRaw, "budget_tokens")
			body = setThinkingRaw(body, thRaw)
		}
		return setOutputConfigEffort(body, string(config.Level)), nil
	default:
		thRaw, writable := thinkingRaw(body)
		if writable {
			thRaw, _ = sjson.SetBytes(thRaw, "type", "enabled")
			thRaw, _ = sjson.SetBytes(thRaw, "budget_tokens", config.Budget)
			body = setThinkingRaw(body, thRaw)
		}
		return deleteOutputConfigEffort(body), nil
	}
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

// deleteOutputConfigEffort removes output_config.effort and drops output_config
// entirely when it would end up an empty object. Non-object output_config
// (absent, array, null, scalar) is left untouched, matching sjson's no-op
// behaviour on those value shapes.
func deleteOutputConfigEffort(body []byte) []byte {
	oc := gjson.GetBytes(body, "output_config")
	if !oc.IsObject() {
		return body
	}
	ocRaw, _ := sjson.DeleteBytes([]byte(oc.Raw), "effort")
	if parsed := gjson.ParseBytes(ocRaw); parsed.IsObject() && len(parsed.Map()) == 0 {
		out, _ := sjson.DeleteBytes(body, "output_config")
		return out
	}
	out, _ := sjson.SetRawBytes(body, "output_config", ocRaw)
	return out
}

// setOutputConfigEffort writes output_config.effort, creating output_config if
// absent and promoting null/scalar values to an object (matching sjson).
// Array-valued output_config is left untouched because sjson rejects writes
// into arrays.
func setOutputConfigEffort(body []byte, effort string) []byte {
	oc := gjson.GetBytes(body, "output_config")
	if oc.IsArray() {
		return body
	}
	ocRaw := []byte(`{}`)
	if oc.IsObject() {
		ocRaw = []byte(oc.Raw)
	}
	ocRaw, _ = sjson.SetBytes(ocRaw, "effort", effort)
	out, _ := sjson.SetRawBytes(body, "output_config", ocRaw)
	return out
}

// applyClaudeDisabled disables thinking, removing the budget and adaptive-effort
// fields, and drops output_config when it would end up empty. dropDisplay also
// removes thinking.display, which only the ModeNone paths do; a zero-budget
// ModeBudget request keeps display so a summary remains visible.
func applyClaudeDisabled(body []byte, dropDisplay bool) []byte {
	thRaw, writable := thinkingRaw(body)
	if writable {
		thRaw, _ = sjson.SetBytes(thRaw, "type", "disabled")
		thRaw, _ = sjson.DeleteBytes(thRaw, "budget_tokens")
		if dropDisplay {
			thRaw, _ = sjson.DeleteBytes(thRaw, "display")
		}
		body = setThinkingRaw(body, thRaw)
	}
	return deleteOutputConfigEffort(body)
}
