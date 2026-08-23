package cmd

import (
	"context"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
)

// doProviderLogin runs the shared login flow for a provider: builds login
// options, calls the auth manager, and prints the saved path (and optional
// record label) on success. Error handling is delegated to errHandler so each
// provider keeps its own error semantics.
func doProviderLogin(
	cfg *config.Config,
	options *LoginOptions,
	provider, label string,
	useDefaultPrompt, printLabel bool,
	metadata map[string]string,
	errHandler func(error),
) {
	if options == nil {
		options = &LoginOptions{}
	}

	promptFn := options.Prompt
	if useDefaultPrompt && promptFn == nil {
		promptFn = defaultProjectPrompt()
	}

	manager := newAuthManager()
	authOpts := &sdkAuth.LoginOptions{
		NoBrowser:    options.NoBrowser,
		CallbackPort: options.CallbackPort,
		Metadata:     metadata,
		Prompt:       promptFn,
	}

	record, savedPath, err := manager.Login(context.Background(), provider, cfg, authOpts)
	if err != nil {
		errHandler(err)
		return
	}

	if savedPath != "" {
		fmt.Printf("Authentication saved to %s\n", savedPath)
	}
	if printLabel && record != nil && record.Label != "" {
		fmt.Printf("Authenticated as %s\n", record.Label)
	}
	fmt.Printf("%s authentication successful!\n", label)
}
