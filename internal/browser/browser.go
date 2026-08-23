// Package browser provides cross-platform functionality for opening URLs in the default web browser.
package browser

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	"github.com/skratchdot/open-golang/open"
)

// OpenURL opens the specified URL in the default web browser.
//
// Parameters:
//   - url: The URL to open.
//
// Returns:
//   - An error if the URL cannot be opened, otherwise nil.
func OpenURL(url string) error {
	fmt.Printf("Attempting to open URL in browser: %s\n", url)

	err := open.Run(url)
	if err != nil {
		log.Debugf("open-golang failed: %v", err)
		return err
	}
	log.Debug("Successfully opened URL")
	return nil
}

// IsAvailable checks whether the platform can open a web browser.
//
// Returns:
//   - true if a browser can be opened, false otherwise.
func IsAvailable() bool {
	return open.Run("about:blank") == nil
}
