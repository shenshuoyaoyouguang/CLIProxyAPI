package cmd

import (
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	log "github.com/sirupsen/logrus"
)

// DoXAILogin triggers the OAuth device-code flow for the xAI provider and saves tokens.
func DoXAILogin(cfg *config.Config, options *LoginOptions) {
	doProviderLogin(cfg, options, "xai", "xAI", true, true, map[string]string{}, func(err error) {
		log.Errorf("xAI authentication failed: %v", err)
	})
}
