// Package thinking_test contains tests for the data-driven provider disabled
// capability used by validate.isDisabledCapableProvider. These live in an external
// test package so they can blank-import provider packages, whose init() calls
// RegisterProvider, without creating an import cycle with internal/thinking.
package thinking_test

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"

	// Blank-import all provider packages so their init() registers appliers with
	// the thinking registry. Without these, GetProviderApplier returns nil.
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/antigravity"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/claude"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/codex"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/deepseek"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/gemini"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/interactions"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/kimi"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/openai"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/xai"
)

// TestProviderDisabledCapability verifies the data-driven source of truth for
// native disabled thinking markers. validate.isDisabledCapableProvider consults
// each provider's applier via SupportsNativeDisabled, so this pins the contract
// without relying on a hardcoded provider-name list. Adding a new disabled-capable
// provider only requires implementing the marker in its applier.
func TestProviderDisabledCapability(t *testing.T) {
	cases := []struct {
		provider string
		want     bool
	}{
		{"claude", true},
		{"deepseek", true},
		{"kimi", true},
		{"openai", false},
		{"codex", false},
		{"gemini", false},
		{"antigravity", false},
		{"interactions", false},
		{"xai", false},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			applier := thinking.GetProviderApplier(tc.provider)
			if tc.want {
				// A disabled-capable provider MUST be reachable and report true.
				// This is what validate.isDisabledCapableProvider relies on.
				if applier == nil {
					t.Fatalf("provider %q is expected disabled-capable but has no registered applier", tc.provider)
				}
				if got := applier.SupportsNativeDisabled(); got != tc.want {
					t.Errorf("SupportsNativeDisabled(%q) = %v, want %v", tc.provider, got, tc.want)
				}
				return
			}
			// Not disabled-capable: either unreachable (nil -> treated as false by
			// validate.isDisabledCapableProvider) or reachable and reporting false.
			// The test must never see a false-expected provider report true.
			if applier != nil && applier.SupportsNativeDisabled() {
				t.Errorf("SupportsNativeDisabled(%q) = true, want not disabled-capable", tc.provider)
			}
		})
	}
}

// TestUnknownProviderNotDisabledCapable guards the fallback: an unregistered
// provider has no applier, so it must be treated as not disabled-capable (false)
// rather than defaulting to enabled thinking.
func TestUnknownProviderNotDisabledCapable(t *testing.T) {
	if applier := thinking.GetProviderApplier("does-not-exist"); applier != nil {
		t.Fatalf("expected nil applier for unknown provider, got %T", applier)
	}
}
