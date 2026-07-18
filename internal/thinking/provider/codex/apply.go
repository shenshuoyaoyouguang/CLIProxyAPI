// Package codex implements thinking configuration for Codex (OpenAI Responses API) models.
//
// Codex models use the reasoning.effort format with discrete levels
// (low/medium/high). This is similar to OpenAI but uses nested field
// "reasoning.effort" instead of "reasoning_effort".
package codex

import (
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
)

// Applier implements thinking.ProviderApplier for Codex models.
//
// Codex-specific behavior:
//   - Output format: reasoning.effort (string: low/medium/high/xhigh)
//   - Level-only mode: no numeric budget support
//   - Some models support ZeroAllowed (gpt-5.1, gpt-5.2)
type Applier struct{}

var _ thinking.ProviderApplier = (*Applier)(nil)

// SupportsNativeDisabled reports whether Codex honors an explicit disable marker
// for ModeNone. Codex has no disabled marker; ModeNone clamps to the lowest
// supported reasoning.effort level, so it must not be treated as fully disabled.
func (a *Applier) SupportsNativeDisabled() bool { return false }

// NewApplier creates a new Codex thinking applier.
func NewApplier() *Applier {
	return &Applier{}
}

func init() {
	thinking.RegisterProvider("codex", NewApplier())
}

// Apply applies thinking configuration to Codex request body.
//
// Expected output format:
//
//	{
//	  "reasoning": {
//	    "effort": "high"
//	  }
//	}
func (a *Applier) Apply(body []byte, config thinking.ThinkingConfig, modelInfo *registry.ModelInfo) ([]byte, error) {
	if thinking.IsUserDefinedModel(modelInfo) {
		return applyCompatibleCodex(body, config)
	}
	if modelInfo.Thinking == nil {
		return body, nil
	}

	// Only handle ModeLevel and ModeNone; other modes pass through unchanged.
	if config.Mode != thinking.ModeLevel && config.Mode != thinking.ModeNone {
		return body, nil
	}

	return thinking.BuildRegisteredEffort(body, config, modelInfo.Thinking, "reasoning.effort")
}

func applyCompatibleCodex(body []byte, config thinking.ThinkingConfig) ([]byte, error) {
	return thinking.BuildCompatibleEffort(body, config, "reasoning.effort", "invalid budget for reasoning.effort conversion")
}
