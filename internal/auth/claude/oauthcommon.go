package claude

import (
	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/oauthcommon"
)

// OAuthServer is the shared OAuth callback server configured for Claude.
type OAuthServer = oauthcommon.Server

// OAuthResult contains the result of the OAuth callback.
type OAuthResult = oauthcommon.Result

// GeneratePKCECodes generates a PKCE code verifier and challenge pair via the shared implementation.
var GeneratePKCECodes = oauthcommon.GeneratePKCECodes

// NewOAuthServer creates a new OAuth callback server wired to Claude's callback route and branding.
func NewOAuthServer(port int) *OAuthServer {
	return oauthcommon.NewServer(port, oauthcommon.Options{
		CallbackPath:        "/callback",
		DefaultPlatformURL:  "https://console.anthropic.com/",
		SuccessTemplate:     LoginSuccessHtml,
		SetupNoticeTemplate: SetupNoticeHtml,
	})
}
