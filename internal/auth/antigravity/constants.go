// Package antigravity provides OAuth2 authentication functionality for the Antigravity provider.
package antigravity

import (
	"os"
	"strings"
)

// OAuth client credentials and configuration.
//
// These values originate from the official Antigravity CLI client and are
// embedded in the released binary. The client ID is public by OAuth design.
// The client secret (GOCSPX-...) is a Google client secret; while installed-
// client secrets are not strongly confidential, this value must still be
// rotated if compromised. The secret can be overridden at runtime via the
// ANTIGRAVITY_OAUTH_CLIENT_SECRET environment variable so it can be rotated
// without rebuilding the binary.
const (
	// ClientID is Google's public OAuth client ID for the Antigravity CLI.
	ClientID = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"

	// defaultClientSecret is the Antigravity CLI's OAuth client secret shipped
	// with the binary; overridable via ANTIGRAVITY_OAUTH_CLIENT_SECRET.
	defaultClientSecret = "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"

	CallbackPort = 51121
)

// ClientSecret is the Antigravity OAuth client secret used to refresh tokens.
// It prefers the ANTIGRAVITY_OAUTH_CLIENT_SECRET environment variable over the
// compiled-in default so the secret can be rotated without a rebuild. It is
// resolved once at package initialization.
var ClientSecret = antigravityClientSecret()

func antigravityClientSecret() string {
	if v := strings.TrimSpace(os.Getenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET")); v != "" {
		return v
	}
	return defaultClientSecret
}

// Scopes defines the OAuth scopes required for Antigravity authentication
var Scopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
	"https://www.googleapis.com/auth/cclog",
	"https://www.googleapis.com/auth/experimentsandconfigs",
}

// OAuth2 endpoints for Google authentication
const (
	TokenEndpoint    = "https://oauth2.googleapis.com/token"
	AuthEndpoint     = "https://accounts.google.com/o/oauth2/v2/auth"
	UserInfoEndpoint = "https://www.googleapis.com/oauth2/v2/userinfo?alt=json"
)

// Antigravity API configuration
const (
	APIEndpoint      = "https://cloudcode-pa.googleapis.com"
	DailyAPIEndpoint = "https://daily-cloudcode-pa.googleapis.com"
	APIVersion       = "v1internal"
)
