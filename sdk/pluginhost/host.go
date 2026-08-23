// Package pluginhost exposes the public plugin SDK surface for embedders:
// shared types, conversion helpers between internal and public shapes, and a
// minimal Host facade for the operations that require SDK-side adaptation
// (config translation and model-result conversion). The remaining host
// lifecycle/auth/scheduler operations live on the internal host directly.
package pluginhost

import (
	"context"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	internalpluginhost "github.com/router-for-me/CLIProxyAPI/v7/internal/pluginhost"
	internalregistry "github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

// ModelInfo describes a plugin-provided model using public plugin SDK types.
type ModelInfo = pluginapi.ModelInfo

// ThinkingSupport describes plugin-provided thinking controls.
type ThinkingSupport = pluginapi.ThinkingSupport

// OAuthModelAlias defines a model ID alias for OAuth/file-backed auth channels.
type OAuthModelAlias struct {
	Name  string
	Alias string
	Fork  bool
}

// RuntimeConfig is the public plugin host configuration used by embedders.
type RuntimeConfig struct {
	Enabled             bool
	Dir                 string
	AuthDir             string
	ProxyURL            string
	ForceModelPrefix    bool
	OAuthModelAlias     map[string][]OAuthModelAlias
	OAuthExcludedModels map[string][]string
	Configs             map[string]PluginInstanceConfig
}

// PluginInstanceConfig stores host-owned plugin settings and the original plugin YAML subtree.
type PluginInstanceConfig struct {
	Enabled  *bool
	Priority int
	Raw      yaml.Node
}

// AuthModelResult is the public result for per-auth model discovery.
type AuthModelResult struct {
	Provider string
	Models   []ModelInfo
	Auth     *coreauth.Auth
	Handled  bool
	Err      error
}

// RegisteredPluginInfo describes a plugin active in the current host snapshot.
type RegisteredPluginInfo = internalpluginhost.RegisteredPluginInfo

// RegisteredPluginMenu describes a plugin-owned resource menu entry.
type RegisteredPluginMenu = internalpluginhost.RegisteredPluginMenu

// Host wraps the internal plugin host behind a public SDK surface for the
// operations whose results need SDK-side conversion. Lifecycle, auth, and
// scheduler pass-through wrappers were removed as pure delegation.
type Host struct {
	inner *internalpluginhost.Host
}

// New creates a plugin host.
func New() *Host {
	return &Host{inner: internalpluginhost.New()}
}

// ApplyConfig applies plugin runtime configuration.
func (h *Host) ApplyConfig(ctx context.Context, cfg RuntimeConfig) {
	if h == nil || h.inner == nil {
		return
	}
	internalCfg := runtimeConfigToInternalConfig(cfg)
	h.inner.ApplyConfig(ctx, internalCfg)
}

// ModelsForAuth lets plugin model providers discover auth-bound models.
func (h *Host) ModelsForAuth(ctx context.Context, auth *coreauth.Auth) AuthModelResult {
	if h == nil || h.inner == nil {
		return AuthModelResult{}
	}
	result := h.inner.ModelsForAuth(ctx, auth)
	return AuthModelResult{
		Provider: result.Provider,
		Models:   registryModelsToPluginModels(result.Models),
		Auth:     result.Auth,
		Handled:  result.Handled,
		Err:      result.Err,
	}
}

// ModelsForProvider returns static models registered for a provider by plugins.
func (h *Host) ModelsForProvider(provider string) []ModelInfo {
	if h == nil || h.inner == nil {
		return nil
	}
	return registryModelsToPluginModels(h.inner.ModelsForProvider(provider))
}

func runtimeConfigToInternalConfig(cfg RuntimeConfig) *internalconfig.Config {
	out := &internalconfig.Config{
		SDKConfig: internalconfig.SDKConfig{
			ProxyURL:         cfg.ProxyURL,
			ForceModelPrefix: cfg.ForceModelPrefix,
		},
		AuthDir:             cfg.AuthDir,
		OAuthExcludedModels: cloneStringSliceMap(cfg.OAuthExcludedModels),
		OAuthModelAlias:     oauthModelAliasToInternal(cfg.OAuthModelAlias),
		Plugins: internalconfig.PluginsConfig{
			Enabled: cfg.Enabled,
			Dir:     cfg.Dir,
			Configs: pluginConfigsToInternal(cfg.Configs),
		},
	}
	out.NormalizePluginsConfig()
	out.SanitizeOAuthModelAlias()
	return out
}

func pluginConfigsToInternal(in map[string]PluginInstanceConfig) map[string]internalconfig.PluginInstanceConfig {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]internalconfig.PluginInstanceConfig, len(in))
	for id, item := range in {
		out[id] = internalconfig.PluginInstanceConfig{
			Enabled:  item.Enabled,
			Priority: item.Priority,
			Raw:      *deepCopyYAMLNode(&item.Raw),
		}
	}
	return out
}

func oauthModelAliasToInternal(in map[string][]OAuthModelAlias) map[string][]internalconfig.OAuthModelAlias {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]internalconfig.OAuthModelAlias, len(in))
	for provider, aliases := range in {
		if len(aliases) == 0 {
			continue
		}
		items := make([]internalconfig.OAuthModelAlias, 0, len(aliases))
		for _, alias := range aliases {
			items = append(items, internalconfig.OAuthModelAlias{
				Name:  alias.Name,
				Alias: alias.Alias,
				Fork:  alias.Fork,
			})
		}
		out[provider] = items
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func registryModelsToPluginModels(models []*internalregistry.ModelInfo) []ModelInfo {
	if len(models) == 0 {
		return nil
	}
	out := make([]ModelInfo, 0, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}
		out = append(out, registryModelToPluginModel(model))
	}
	return out
}

func registryModelToPluginModel(model *internalregistry.ModelInfo) ModelInfo {
	if model == nil {
		return ModelInfo{}
	}
	return ModelInfo{
		ID:                         model.ID,
		Object:                     model.Object,
		Created:                    model.Created,
		OwnedBy:                    model.OwnedBy,
		Type:                       model.Type,
		DisplayName:                model.DisplayName,
		Name:                       model.Name,
		Version:                    model.Version,
		Description:                model.Description,
		InputTokenLimit:            int64(model.InputTokenLimit),
		OutputTokenLimit:           int64(model.OutputTokenLimit),
		SupportedGenerationMethods: cloneStringSlice(model.SupportedGenerationMethods),
		ContextLength:              int64(model.ContextLength),
		MaxCompletionTokens:        int64(model.MaxCompletionTokens),
		SupportedParameters:        cloneStringSlice(model.SupportedParameters),
		SupportedInputModalities:   cloneStringSlice(model.SupportedInputModalities),
		SupportedOutputModalities:  cloneStringSlice(model.SupportedOutputModalities),
		Thinking:                   thinkingSupportToPlugin(model.Thinking),
		UserDefined:                model.UserDefined,
	}
}

func thinkingSupportToPlugin(thinking *internalregistry.ThinkingSupport) *ThinkingSupport {
	if thinking == nil {
		return nil
	}
	return &ThinkingSupport{
		Min:            thinking.Min,
		Max:            thinking.Max,
		ZeroAllowed:    thinking.ZeroAllowed,
		DynamicAllowed: thinking.DynamicAllowed,
		Levels:         cloneStringSlice(thinking.Levels),
	}
}

func cloneStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	return append([]string(nil), in...)
}

func cloneStringSliceMap(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for key, values := range in {
		out[key] = cloneStringSlice(values)
	}
	return out
}

func deepCopyYAMLNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return &yaml.Node{}
	}
	copyNode := *node
	if len(node.Content) > 0 {
		copyNode.Content = make([]*yaml.Node, 0, len(node.Content))
		for _, child := range node.Content {
			copyNode.Content = append(copyNode.Content, deepCopyYAMLNode(child))
		}
	}
	return &copyNode
}
