package browser

import (
	"slices"
	"testing"
)

func TestLinuxBrowserFallbackChain(t *testing.T) {
	want := []string{"xdg-open", "x-www-browser", "www-browser", "firefox", "chromium", "google-chrome"}
	if !slices.Equal(linuxBrowsers, want) {
		t.Fatalf("linuxBrowsers = %v, want %v", linuxBrowsers, want)
	}
}
