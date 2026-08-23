package openai

import (
	codexmodels "github.com/router-for-me/CLIProxyAPI/v7/internal/client/codex/models"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

func (h *OpenAIAPIHandler) codexClientModelsResponse() map[string]any {
	optimizeMultiAgentV2 := h != nil && h.Cfg != nil && h.Cfg.CodexOptimizeMultiAgentV2
	return codexmodels.BuildResponse(h.Models(), registry.GetGlobalRegistry().GetModelProviders, optimizeMultiAgentV2)
}
