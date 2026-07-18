package thinking

import (
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// buildCompatibleEffort translates a canonical ThinkingConfig into a provider
// request body for user-defined (compatible) models. It is a pure mode switch
// and does not consult model capability, matching the original OpenAI/Codex
// applyCompatible* behavior. field is the provider effort key
// (e.g. "reasoning_effort" or "reasoning.effort").
func BuildCompatibleEffort(body []byte, config ThinkingConfig, field, budgetErrMsg string) ([]byte, error) {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		body = []byte(`{}`)
	}

	var effort string
	switch config.Mode {
	case ModeLevel:
		if config.Level == "" {
			return body, nil
		}
		effort = string(config.Level)
	case ModeNone:
		effort = string(LevelNone)
		if config.Level != "" {
			effort = string(config.Level)
		}
	case ModeAuto:
		// Auto mode for user-defined models: pass through as "auto"
		effort = string(LevelAuto)
	case ModeBudget:
		// Budget mode: convert budget to level using threshold mapping
		level, ok := ConvertBudgetToLevel(config.Budget)
		if !ok {
			return body, NewThinkingError(ErrBudgetOutOfRange, budgetErrMsg)
		}
		effort = level
	default:
		return body, nil
	}

	result, _ := sjson.SetBytes(body, field, effort)
	return result, nil
}

// buildRegisteredEffort translates a canonical ThinkingConfig into a provider
// request body for registered models, consulting model capability (support).
// It matches the original OpenAI/Codex Apply behavior.
func BuildRegisteredEffort(body []byte, config ThinkingConfig, support *registry.ThinkingSupport, field string) ([]byte, error) {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		body = []byte(`{}`)
	}

	if config.Mode == ModeLevel {
		result, _ := sjson.SetBytes(body, field, string(config.Level))
		return result, nil
	}

	effort := ""
	if config.Budget == 0 {
		if support.ZeroAllowed || HasLevel(support.Levels, string(LevelNone)) {
			effort = string(LevelNone)
		}
	}
	if effort == "" && config.Level != "" {
		effort = string(config.Level)
	}
	if effort == "" && len(support.Levels) > 0 {
		effort = support.Levels[0]
	}
	if effort == "" {
		return body, nil
	}

	result, _ := sjson.SetBytes(body, field, effort)
	return result, nil
}
