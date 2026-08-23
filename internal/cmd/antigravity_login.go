package cmd

import (
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	log "github.com/sirupsen/logrus"
)

// DoAntigravityLogin triggers the OAuth flow for the antigravity provider and saves tokens.
func DoAntigravityLogin(cfg *config.Config, options *LoginOptions) {
	doProviderLogin(cfg, options, "antigravity", "Antigravity", true, true, map[string]string{}, func(err error) {
		log.Errorf("Antigravity authentication failed: %v", err)
	})
}
