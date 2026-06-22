package executor

import (
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

// TestKimiEnsureAttributesNilSafe verifies that setting base_url on an Auth
// with nil Attributes does not panic.
// Bug: kimi_executor.go writes auth.Attributes["base_url"] without checking
// that Attributes is non-nil, causing a runtime panic.
func TestKimiEnsureAttributesNilSafe(t *testing.T) {
	auth := &cliproxyauth.Auth{
		ID:       "test-kimi",
		Provider: "kimi",
		// Attributes is nil — the default for a new Auth struct
	}

	// This should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ensureAuthAttributes panicked: %v", r)
		}
	}()

	ensureAuthAttributes(auth)

	if auth.Attributes == nil {
		t.Fatal("Attributes should be initialized after ensureAuthAttributes")
	}

	// Should be safe to write now
	auth.Attributes["base_url"] = "https://example.com"
	if auth.Attributes["base_url"] != "https://example.com" {
		t.Error("failed to set base_url after ensureAuthAttributes")
	}
}

// TestKimiEnsureAttributesPreservesExisting verifies that ensureAuthAttributes
// does not overwrite existing Attributes.
func TestKimiEnsureAttributesPreservesExisting(t *testing.T) {
	auth := &cliproxyauth.Auth{
		ID:       "test-kimi",
		Provider: "kimi",
		Attributes: map[string]string{
			"api_key": "existing-key",
		},
	}

	ensureAuthAttributes(auth)

	if auth.Attributes["api_key"] != "existing-key" {
		t.Error("ensureAuthAttributes overwrote existing Attributes")
	}
}
