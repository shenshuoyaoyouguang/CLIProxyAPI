// Package executor implements the Xiaomi MiMo API executor.
//
// MiMo uses OpenAI-compatible chat completions API with a custom thinking.type
// parameter for deep thinking control. This executor embeds OpenAICompatExecutor
// and adds MiMo-specific credential handling and parameter adjustments.
package executor

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// MiMo locked parameters when deep thinking (thinking.type="enabled") is active.
// Per MiMo docs, these models do not support custom temperature/top_p in deep
// thinking mode, so we force the documented defaults.
const (
	mimoThinkingTemperature = 1.0
	mimoThinkingTopP        = 0.95
)

// MIMOExecutor implements a stateless executor for Xiaomi MiMo API.
// It embeds OpenAICompatExecutor for the base OpenAI-compatible logic and adds
// MiMo-specific credential injection. MiMo accepts both "Authorization: Bearer"
// (used by the OpenAI SDK) and "api-key" headers; PrepareRequest uses the
// "api-key" header for the HttpRequest path, while Execute/ExecuteStream
// delegate to OpenAICompatExecutor which sets "Authorization: Bearer".
type MIMOExecutor struct {
	OpenAICompatExecutor
	cfg *config.Config
}

// NewMIMOExecutor creates a new MiMo executor.
func NewMIMOExecutor(cfg *config.Config) *MIMOExecutor {
	return &MIMOExecutor{cfg: cfg}
}

// Identifier returns the executor identifier.
func (e *MIMOExecutor) Identifier() string { return "mimo" }

// PrepareRequest injects MiMo credentials into the outgoing HTTP request.
// Uses the "api-key" header for the HttpRequest path. Note: MiMo also accepts
// "Authorization: Bearer" (used by Execute/ExecuteStream via OpenAICompatExecutor).
func (e *MIMOExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	_, apiKey := mimoCreds(auth)
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("api-key", apiKey)
	}
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(req, attrs)
	return nil
}

// HttpRequest injects MiMo credentials into the request and executes it.
func (e *MIMOExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("mimo executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	if err := e.PrepareRequest(httpReq, auth); err != nil {
		return nil, err
	}
	httpClient := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	return httpClient.Do(httpReq)
}

// Execute delegates to OpenAICompatExecutor with MIMO-specific base URL resolution.
func (e *MIMOExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	e.applyMIMOBaseURL(auth)
	return e.OpenAICompatExecutor.Execute(ctx, auth, req, opts)
}

// ExecuteStream delegates to OpenAICompatExecutor with MIMO-specific base URL resolution.
func (e *MIMOExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	e.applyMIMOBaseURL(auth)
	return e.OpenAICompatExecutor.ExecuteStream(ctx, auth, req, opts)
}

// CountTokens delegates to OpenAICompatExecutor.
func (e *MIMOExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	e.applyMIMOBaseURL(auth)
	return e.OpenAICompatExecutor.CountTokens(ctx, auth, req, opts)
}

// Refresh is a no-op for API-key based MiMo provider.
func (e *MIMOExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if refreshed, handled, err := helps.RefreshAuthViaHome(ctx, e.cfg, auth); handled {
		return refreshed, err
	}
	return auth, nil
}

// applyMIMOBaseURL ensures auth has the MiMo base URL set.
func (e *MIMOExecutor) applyMIMOBaseURL(auth *cliproxyauth.Auth) {
	if auth == nil {
		return
	}
	baseURL, _ := mimoCreds(auth)
	if baseURL == "" {
		if auth.Attributes == nil {
			auth.Attributes = make(map[string]string)
		}
		auth.Attributes["base_url"] = "https://api.xiaomimimo.com"
	}
}

// mimoCreds extracts the base URL and API key from auth.
func mimoCreds(a *cliproxyauth.Auth) (baseURL, apiKey string) {
	if a == nil {
		return "", ""
	}
	if a.Attributes != nil {
		baseURL = a.Attributes["base_url"]
		apiKey = a.Attributes["api_key"]
	}
	if baseURL == "" && a.Metadata != nil {
		if v, ok := a.Metadata["base_url"].(string); ok {
			baseURL = v
		}
	}
	if apiKey == "" && a.Metadata != nil {
		if v, ok := a.Metadata["api_key"].(string); ok {
			apiKey = v
		}
	}
	return baseURL, apiKey
}

// mimoLockThinkingParams locks temperature and top_p when thinking is enabled.
// Per MiMo docs: "mimo-v2.5-pro, mimo-v2.5, mimo-v2-pro, mimo-v2-omni models
// do not support custom temperature and top_p parameters in deep thinking mode."
//
// This must run AFTER ApplyPayloadConfigWithRequest so that config-level
// overrides are themselves overwritten by the locked values.
func mimoLockThinkingParams(body []byte) []byte {
	thinkingType := gjson.GetBytes(body, "thinking.type")
	if thinkingType.Exists() && thinkingType.String() == "enabled" {
		body, _ = sjson.SetBytes(body, "temperature", mimoThinkingTemperature)
		body, _ = sjson.SetBytes(body, "top_p", mimoThinkingTopP)
	}
	return body
}
