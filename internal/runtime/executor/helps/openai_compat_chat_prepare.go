package helps

import (
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
)

// OpenAICompatChatPrepareInput is the contract for outbound OpenAI-compat chat
// body preparation after TranslateRequest.
//
// Model dual identity (must not collapse into one field):
//   - ModelForApply: full client model (may include thinking suffix); feeds ApplyThinking only.
//   - BaseModel: ParseSuffix result; feeds thinking route, passback, and typically PayloadConfig Middle.
type OpenAICompatChatPrepareInput struct {
	Body []byte

	// ModelForApply may include a thinking suffix, e.g. "deepseek-v4-flash(high)".
	ModelForApply string
	// BaseModel is the suffix-stripped model id used for route + passback.
	BaseModel string

	FromFormat   string
	WireToFormat string
	ProviderKey  string

	// SkipPassback is true for responses/compact (no messages[] passback).
	SkipPassback bool
	// Middle runs after ApplyThinking and before passback (nil = identity).
	// Execute/Stream pass ApplyPayloadConfig here; CountTokens omits it (documented residual).
	Middle func([]byte) []byte
}

// openAICompatThinkingRoute selects ApplyThinking toFormat/providerKey.
// DeepSeek passback models must use the deepseek applier (thinking.type +
// reasoning_effort) even though the wire format remains OpenAI chat completions.
func openAICompatThinkingRoute(baseModel, toFormat, providerKey string) (thinkingTo, thinkingProvider string) {
	thinkingTo = toFormat
	thinkingProvider = providerKey
	if thinking.RequiresDeepSeekReasoningPassback(baseModel) {
		thinkingTo = "deepseek"
		thinkingProvider = "deepseek"
	}
	return thinkingTo, thinkingProvider
}

// PrepareOpenAICompatChatBody runs:
//  1. thinking route + ApplyThinking
//  2. Middle (optional; PayloadConfig on Execute/Stream)
//  3. multi-turn reasoning passback unless SkipPassback
//
// It does not Translate, set stream_options, sanitize encrypted content, or HTTP.
func PrepareOpenAICompatChatBody(in OpenAICompatChatPrepareInput) ([]byte, error) {
	body := in.Body
	thinkingTo, thinkingProvider := openAICompatThinkingRoute(in.BaseModel, in.WireToFormat, in.ProviderKey)
	var err error
	body, err = thinking.ApplyThinking(body, in.ModelForApply, in.FromFormat, thinkingTo, thinkingProvider)
	if err != nil {
		return nil, err
	}
	if in.Middle != nil {
		body = in.Middle(body)
	}
	if !in.SkipPassback {
		body = thinking.EnsureMultiTurnReasoningPassback(body, in.BaseModel)
	}
	return body, nil
}
