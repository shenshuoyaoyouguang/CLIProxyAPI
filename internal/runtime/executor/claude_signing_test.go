package executor

import (
	"encoding/json"
	"testing"

	xxHash64 "github.com/pierrec/xxHash/xxHash64"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
)

// ---------------------------------------------------------------------------
// signAnthropicMessagesBody
// ---------------------------------------------------------------------------

func TestSignAnthropicMessagesBody(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantSame  bool // true if output should equal input
		wantCCH   string
	}{
		{
			name:     "no system field returns unchanged",
			body:     `{"messages":[{"role":"user","content":"hi"}]}`,
			wantSame: true,
		},
		{
			name:     "system text not billing header returns unchanged",
			body:     `{"system":[{"type":"text","text":"You are a helpful assistant."}],"messages":[]}`,
			wantSame: true,
		},
		{
			name:     "billing header without cch pattern returns unchanged",
			body:     `{"system":[{"type":"text","text":"x-anthropic-billing-header:org=abc;ver=1"}],"messages":[]}`,
			wantSame: true,
		},
		{
			name: "valid billing header computes cch",
			body: `{"system":[{"type":"text","text":"x-anthropic-billing-header:org=abc;cch=00000;ver=1"}],"messages":[]}`,
		},
		{
			name: "already signed body is idempotent",
			// Will be filled dynamically below
		},
	}

	// Pre-compute the expected cch for the "valid billing header" case.
	validBody := []byte(`{"system":[{"type":"text","text":"x-anthropic-billing-header:org=abc;cch=00000;ver=1"}],"messages":[]}`)
	expectedCCH := computeExpectedCCH(t, validBody)

	tests[3].wantCCH = expectedCCH

	// For the idempotency test, sign once and use the result as input.
	firstSigned := signAnthropicMessagesBody(validBody)
	tests[4].body = string(firstSigned)
	tests[4].wantCCH = expectedCCH

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte(tc.body)
			got := signAnthropicMessagesBody(input)

			if tc.wantSame {
				if string(got) != string(input) {
					t.Fatalf("expected body unchanged\n  got:  %s\n  want: %s", got, input)
				}
				return
			}

			// Verify it is valid JSON.
			if !json.Valid(got) {
				t.Fatalf("output is not valid JSON: %s", got)
			}

			// Extract cch from output.
			systemText := gjson.GetBytes(got, "system.0.text").String()
			matches := claudeBillingHeaderCCHPattern.FindStringSubmatch(systemText)
			if len(matches) < 2 {
				t.Fatalf("no cch found in signed output: %s", systemText)
			}
			gotCCH := matches[1]
			if gotCCH != tc.wantCCH {
				t.Errorf("cch mismatch\n  got:  %s\n  want: %s", gotCCH, tc.wantCCH)
			}
		})
	}
}

// computeExpectedCCH reproduces the signing logic to derive the expected hash.
func computeExpectedCCH(t *testing.T, body []byte) string {
	t.Helper()
	// The unsigned body has cch=00000 — since input already has 00000, it is the unsigned form.
	hash := xxHash64.Checksum(body, claudeCCHSeed) & 0xFFFFF
	cch := make([]byte, 5)
	for i := 4; i >= 0; i-- {
		cch[i] = "0123456789abcdef"[hash&0xF]
		hash >>= 4
	}
	return string(cch)
}

// ---------------------------------------------------------------------------
// resolveClaudeKeyConfig
// ---------------------------------------------------------------------------

func TestResolveClaudeKeyConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		auth    *cliproxyauth.Auth
		wantNil bool
		wantKey string // expected APIKey of matched entry
	}{
		{
			name:    "nil config returns nil",
			cfg:     nil,
			auth:    &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "sk-123"}},
			wantNil: true,
		},
		{
			name:    "nil auth returns nil",
			cfg:     &config.Config{ClaudeKey: []config.ClaudeKey{{APIKey: "sk-123"}}},
			auth:    nil,
			wantNil: true,
		},
		{
			name:    "empty api_key returns nil",
			cfg:     &config.Config{ClaudeKey: []config.ClaudeKey{{APIKey: "sk-123"}}},
			auth:    &cliproxyauth.Auth{Attributes: map[string]string{"api_key": ""}},
			wantNil: true,
		},
		{
			name: "match by api_key case insensitive",
			cfg:  &config.Config{ClaudeKey: []config.ClaudeKey{{APIKey: "SK-ABC"}}},
			auth: &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "sk-abc"}},
			wantKey: "SK-ABC",
		},
		{
			name:    "no match returns nil",
			cfg:     &config.Config{ClaudeKey: []config.ClaudeKey{{APIKey: "sk-xyz"}}},
			auth:    &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "sk-other"}},
			wantNil: true,
		},
		{
			name: "match with baseURL constraint",
			cfg: &config.Config{ClaudeKey: []config.ClaudeKey{
				{APIKey: "sk-abc", BaseURL: "https://api.example.com"},
				{APIKey: "sk-abc", BaseURL: "https://api.other.com"},
			}},
			auth:    &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "sk-abc", "base_url": "https://api.other.com"}},
			wantKey: "sk-abc",
		},
		{
			name: "baseURL mismatch skips entry",
			cfg: &config.Config{ClaudeKey: []config.ClaudeKey{
				{APIKey: "sk-abc", BaseURL: "https://api.example.com"},
			}},
			auth:    &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "sk-abc", "base_url": "https://api.other.com"}},
			wantNil: true,
		},
		{
			name: "fallback to metadata access_token",
			cfg:  &config.Config{ClaudeKey: []config.ClaudeKey{{APIKey: "token-from-meta"}}},
			auth: &cliproxyauth.Auth{
				Attributes: map[string]string{},
				Metadata:   map[string]any{"access_token": "token-from-meta"},
			},
			wantKey: "token-from-meta",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveClaudeKeyConfig(tc.cfg, tc.auth)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil result")
			}
			if got.APIKey != tc.wantKey {
				t.Errorf("APIKey = %q, want %q", got.APIKey, tc.wantKey)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// resolveClaudeKeyCloakConfig
// ---------------------------------------------------------------------------

func TestResolveClaudeKeyCloakConfig(t *testing.T) {
	cloak := &config.CloakConfig{Mode: "always"}
	cfg := &config.Config{ClaudeKey: []config.ClaudeKey{
		{APIKey: "sk-abc", Cloak: cloak},
		{APIKey: "sk-nocloak"},
	}}

	t.Run("returns cloak when entry matches", func(t *testing.T) {
		auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "sk-abc"}}
		got := resolveClaudeKeyCloakConfig(cfg, auth)
		if got != cloak {
			t.Fatalf("expected cloak config, got %v", got)
		}
	})

	t.Run("returns nil when entry has no cloak", func(t *testing.T) {
		auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "sk-nocloak"}}
		got := resolveClaudeKeyCloakConfig(cfg, auth)
		if got != nil {
			t.Fatalf("expected nil, got %+v", got)
		}
	})

	t.Run("returns nil when no match", func(t *testing.T) {
		auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "sk-unknown"}}
		got := resolveClaudeKeyCloakConfig(cfg, auth)
		if got != nil {
			t.Fatalf("expected nil, got %+v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// experimentalCCHSigningEnabled
// ---------------------------------------------------------------------------

func TestExperimentalCCHSigningEnabled(t *testing.T) {
	cfg := &config.Config{ClaudeKey: []config.ClaudeKey{
		{APIKey: "sk-enabled", ExperimentalCCHSigning: true},
		{APIKey: "sk-disabled", ExperimentalCCHSigning: false},
	}}

	tests := []struct {
		name string
		auth *cliproxyauth.Auth
		want bool
	}{
		{
			name: "enabled entry returns true",
			auth: &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "sk-enabled"}},
			want: true,
		},
		{
			name: "disabled entry returns false",
			auth: &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "sk-disabled"}},
			want: false,
		},
		{
			name: "no match returns false",
			auth: &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "sk-unknown"}},
			want: false,
		},
		{
			name: "nil auth returns false",
			auth: nil,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := experimentalCCHSigningEnabled(cfg, tc.auth)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// rebuildMidSystemMessageEnabled
// ---------------------------------------------------------------------------

func TestRebuildMidSystemMessageEnabled(t *testing.T) {
	cfg := &config.Config{ClaudeKey: []config.ClaudeKey{
		{APIKey: "sk-rebuild", RebuildMidSystemMessage: true},
		{APIKey: "sk-norebuild", RebuildMidSystemMessage: false},
	}}

	tests := []struct {
		name string
		cfg  *config.Config
		auth *cliproxyauth.Auth
		want bool
	}{
		{
			name: "config entry has RebuildMidSystemMessage",
			cfg:  cfg,
			auth: &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "sk-rebuild"}},
			want: true,
		},
		{
			name: "config entry disabled",
			cfg:  cfg,
			auth: &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "sk-norebuild"}},
			want: false,
		},
		{
			name: "auth attribute true overrides config",
			cfg:  cfg,
			auth: &cliproxyauth.Auth{Attributes: map[string]string{
				"api_key":                    "sk-norebuild",
				"rebuild_mid_system_message": "true",
			}},
			want: true,
		},
		{
			name: "auth attribute TRUE case insensitive",
			cfg:  cfg,
			auth: &cliproxyauth.Auth{Attributes: map[string]string{
				"api_key":                    "sk-norebuild",
				"rebuild_mid_system_message": " TRUE ",
			}},
			want: true,
		},
		{
			name: "auth attribute false does not override",
			cfg:  cfg,
			auth: &cliproxyauth.Auth{Attributes: map[string]string{
				"api_key":                    "sk-norebuild",
				"rebuild_mid_system_message": "false",
			}},
			want: false,
		},
		{
			name: "nil auth returns false",
			cfg:  cfg,
			auth: nil,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rebuildMidSystemMessageEnabled(tc.cfg, tc.auth)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
