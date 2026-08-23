package codex

import (
	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/oauthcommon"
)

// Aliases to the shared OAuth implementation so existing importers of this
// package keep working unchanged.

type (
	// OAuthServer is the shared OAuth callback server configured for Codex.
	OAuthServer = oauthcommon.Server
	// OAuthResult contains the result of the OAuth callback.
	OAuthResult = oauthcommon.Result
	// OAuthError represents an OAuth-specific error.
	OAuthError = oauthcommon.OAuthError
	// AuthenticationError represents authentication-related errors.
	AuthenticationError = oauthcommon.AuthenticationError
)

var (
	// GeneratePKCECodes generates a PKCE code verifier and challenge pair via the shared implementation.
	GeneratePKCECodes = oauthcommon.GeneratePKCECodes
	// NewOAuthError creates a new OAuth error with the specified code, description, and status code.
	NewOAuthError = oauthcommon.NewOAuthError
	// NewAuthenticationError creates a new authentication error with a cause based on a base error.
	NewAuthenticationError = oauthcommon.NewAuthenticationError
	// IsAuthenticationError checks if an error is an authentication error.
	IsAuthenticationError = oauthcommon.IsAuthenticationError
	// IsOAuthError checks if an error is an OAuth error.
	IsOAuthError = oauthcommon.IsOAuthError
	// GetUserFriendlyMessage returns a user-friendly error message based on the error type.
	GetUserFriendlyMessage = oauthcommon.GetUserFriendlyMessage

	ErrInvalidState       = oauthcommon.ErrInvalidState
	ErrCodeExchangeFailed = oauthcommon.ErrCodeExchangeFailed
	ErrServerStartFailed  = oauthcommon.ErrServerStartFailed
	ErrPortInUse          = oauthcommon.ErrPortInUse
	ErrCallbackTimeout    = oauthcommon.ErrCallbackTimeout
	// ErrBrowserOpenFailed represents an error when opening the browser for authentication fails.
	ErrBrowserOpenFailed = oauthcommon.ErrBrowserOpenFailed
)

// NewOAuthServer creates a new OAuth callback server wired to Codex's callback route and branding.
func NewOAuthServer(port int) *OAuthServer {
	return oauthcommon.NewServer(port, oauthcommon.CodexServerOptions())
}
