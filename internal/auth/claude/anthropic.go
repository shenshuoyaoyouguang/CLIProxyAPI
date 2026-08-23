package claude

import (
	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/oauthcommon"
)

// PKCECodes holds PKCE verification codes for OAuth2 PKCE flow
type PKCECodes = oauthcommon.PKCECodes

// ClaudeTokenData holds OAuth token information from Anthropic
type ClaudeTokenData struct {
	// AccessToken is the OAuth2 access token for API access.
	AccessToken string `json:"access_token"`
	// RefreshToken is used to obtain new access tokens.
	RefreshToken string `json:"refresh_token"`
	// Email is the Anthropic account email.
	Email string `json:"email"`
	// AccountUUID identifies the Anthropic account returned by OAuth.
	AccountUUID string `json:"account_uuid"`
	// OrganizationUUID identifies the Anthropic organization returned by OAuth.
	OrganizationUUID string `json:"organization_uuid"`
	// OrganizationName is the display name returned by OAuth.
	OrganizationName string `json:"organization_name"`
	// Expire is the timestamp of the token expiry.
	Expire string `json:"expired"`
}

// ClaudeAuthBundle aggregates authentication data after OAuth flow completion
type ClaudeAuthBundle struct {
	// APIKey is the Anthropic API key obtained from token exchange.
	APIKey string `json:"api_key"`
	// TokenData contains the OAuth tokens from the authentication flow.
	TokenData ClaudeTokenData `json:"token_data"`
	// DeviceIDs contains the single device identity persisted with this credential.
	DeviceIDs []string `json:"claude_device_ids"`
	// LastRefresh is the timestamp of the last token refresh.
	LastRefresh string `json:"last_refresh"`
}
