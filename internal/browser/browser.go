// Package browser provides cross-platform functionality for opening URLs in the default web browser.
// It abstracts the underlying operating system commands and provides a simple interface.
package browser

import (
	"fmt"
	"os/exec"
	"runtime"

	log "github.com/sirupsen/logrus"
	"github.com/skratchdot/open-golang/open"
)

// linuxBrowsers lists the browser-opening commands tried in order of
// preference when open-golang fails on Linux.
var linuxBrowsers = []string{"xdg-open", "x-www-browser", "www-browser", "firefox", "chromium", "google-chrome"}

// OpenURL opens the specified URL in the default web browser.
// It first attempts to use a platform-agnostic library and falls back to
// platform-specific commands if that fails.
//
// Parameters:
//   - url: The URL to open.
//
// Returns:
//   - An error if the URL cannot be opened, otherwise nil.
func OpenURL(url string) error {
	fmt.Printf("Attempting to open URL in browser: %s\n", url)

	err := open.Run(url)
	if err == nil {
		log.Debug("Successfully opened URL using open-golang library")
		return nil
	}

	log.Debugf("open-golang failed: %v, trying platform-specific commands", err)

	return openURLPlatformSpecific(url)
}

// openURLPlatformSpecific opens a URL using OS-specific commands, serving as a
// fallback for OpenURL.
func openURLPlatformSpecific(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "linux":
		for _, browser := range linuxBrowsers {
			if _, err := exec.LookPath(browser); err == nil {
				cmd = exec.Command(browser, url)
				break
			}
		}
		if cmd == nil {
			return fmt.Errorf("no suitable browser found on Linux system")
		}
	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	log.Debugf("Running command: %s %v", cmd.Path, cmd.Args[1:])
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start browser command: %w", err)
	}

	log.Debug("Successfully opened URL using platform-specific command")
	return nil
}

// IsAvailable checks if the system has a command available to open a web browser.
//
// Returns:
//   - true if a browser can be opened, false otherwise.
func IsAvailable() bool {
	if open.Run("about:blank") == nil {
		return true
	}

	switch runtime.GOOS {
	case "darwin":
		_, err := exec.LookPath("open")
		return err == nil
	case "windows":
		_, err := exec.LookPath("rundll32")
		return err == nil
	case "linux":
		for _, browser := range linuxBrowsers {
			if _, err := exec.LookPath(browser); err == nil {
				return true
			}
		}
		return false
	default:
		return false
	}
}
