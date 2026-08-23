package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/claude"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	log "github.com/sirupsen/logrus"
)

// DoClaudeLogin triggers the Claude OAuth flow through the shared authentication manager.
// It initiates the OAuth authentication process for Anthropic Claude services and saves
// the authentication tokens to the configured auth directory.
//
// Parameters:
//   - cfg: The application configuration
//   - options: Login options including browser behavior and prompts
func DoClaudeLogin(cfg *config.Config, options *LoginOptions) {
	doProviderLogin(cfg, options, "claude", "Claude", true, false, map[string]string{}, func(err error) {
		if authErr, ok := errors.AsType[*claude.AuthenticationError](err); ok {
			log.Error(claude.GetUserFriendlyMessage(authErr))
			if authErr.Type == claude.ErrPortInUse.Type {
				os.Exit(claude.ErrPortInUse.Code)
			}
			return
		}
		fmt.Printf("Claude authentication failed: %v\n", err)
	})
}
