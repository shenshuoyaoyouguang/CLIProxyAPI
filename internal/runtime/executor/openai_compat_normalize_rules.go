package executor

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/modelkind"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type openAICompatProviderNormalizeRequest struct {
	Model       string
	CompatModel *config.OpenAICompatibilityModel
}

type openAICompatProviderNormalizeRule interface {
	NormalizeRequest([]byte, openAICompatProviderNormalizeRequest) []byte
	NormalizeResponse([]byte, openAICompatProviderNormalizeRequest) []byte
}

var openAICompatProviderNormalizeRules = []openAICompatProviderNormalizeRule{
	openAICompatCapabilityNormalizeRule{},
	openAICompatDeepSeekNormalizeRule{},
	openAICompatMimoNormalizeRule{},
}

func applyOpenAICompatProviderNormalizeRules(body []byte, req openAICompatProviderNormalizeRequest) []byte {
	out := body
	for _, rule := range openAICompatProviderNormalizeRules {
		out = rule.NormalizeRequest(out, req)
	}
	return out
}

func applyOpenAICompatProviderNormalizeResponseRules(body []byte, req openAICompatProviderNormalizeRequest) []byte {
	out := body
	for _, rule := range openAICompatProviderNormalizeRules {
		out = rule.NormalizeResponse(out, req)
	}
	return out
}

type openAICompatCapabilityNormalizeRule struct{}

func (openAICompatCapabilityNormalizeRule) NormalizeRequest(body []byte, req openAICompatProviderNormalizeRequest) []byte {
	if req.CompatModel == nil || len(body) == 0 || !gjson.ValidBytes(body) {
		return body
	}
	out := body
	for _, rawPath := range req.CompatModel.UnsupportedParameters {
		path := normalizeOpenAICompatPayloadPath(rawPath)
		if path == "" {
			continue
		}
		next, err := sjson.DeleteBytes(out, path)
		if err != nil {
			log.WithError(err).WithField("path", path).Debug("openai compat executor: failed to delete unsupported parameter")
			continue
		}
		out = next
	}
	for rawPath, value := range req.CompatModel.LockedParameters {
		path := normalizeOpenAICompatPayloadPath(rawPath)
		if path == "" {
			continue
		}
		next, err := sjson.SetBytes(out, path, value)
		if err != nil {
			log.WithError(err).WithField("path", path).Debug("openai compat executor: failed to lock parameter")
			continue
		}
		out = next
	}
	return out
}

func (openAICompatCapabilityNormalizeRule) NormalizeResponse(body []byte, _ openAICompatProviderNormalizeRequest) []byte {
	return body
}

type openAICompatDeepSeekNormalizeRule struct{}

func (openAICompatDeepSeekNormalizeRule) NormalizeRequest(body []byte, req openAICompatProviderNormalizeRequest) []byte {
	if !modelkind.IsDeepSeekModel(openAICompatNormalizeBaseModel(req.Model)) {
		return body
	}
	return normalizeDeepSeekToolMessageReasoning(body)
}

func (openAICompatDeepSeekNormalizeRule) NormalizeResponse(body []byte, req openAICompatProviderNormalizeRequest) []byte {
	if !modelkind.IsDeepSeekModel(openAICompatNormalizeBaseModel(req.Model)) {
		return body
	}
	return helps.NormalizeDeepSeekOpenAIUsage(body)
}

type openAICompatMimoNormalizeRule struct{}

func (openAICompatMimoNormalizeRule) NormalizeRequest(body []byte, req openAICompatProviderNormalizeRequest) []byte {
	if !modelkind.IsMIMOModel(openAICompatNormalizeBaseModel(req.Model)) {
		return body
	}
	out := normalizeMimoToolMessageReasoning(body)
	return mimoLockThinkingParams(out)
}

func (openAICompatMimoNormalizeRule) NormalizeResponse(body []byte, _ openAICompatProviderNormalizeRequest) []byte {
	return body
}

func openAICompatNormalizeBaseModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	parsed := thinking.ParseSuffix(model)
	if strings.TrimSpace(parsed.ModelName) != "" {
		return strings.TrimSpace(parsed.ModelName)
	}
	return model
}

func normalizeOpenAICompatPayloadPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, ".")
	return path
}
