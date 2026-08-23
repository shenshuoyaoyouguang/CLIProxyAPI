package cmd

import (
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	log "github.com/sirupsen/logrus"
)

// DoKimiLogin triggers the OAuth device flow for Kimi (Moonshot AI) and saves tokens.
// It initiates the device flow authentication, displays the verification URL for the user,
// and waits for authorization before saving the tokens.
//
// Parameters:
//   - cfg: The application configuration containing proxy and auth directory settings
//   - options: Login options including browser behavior settings
func DoKimiLogin(cfg *config.Config, options *LoginOptions) {
	doProviderLogin(cfg, options, "kimi", "Kimi", false, true, map[string]string{}, func(err error) {
		log.Errorf("Kimi authentication failed: %v", err)
	})
}
