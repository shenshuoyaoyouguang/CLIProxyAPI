// Package thinking_test provides black-box coverage for the canonical
// Level<->Budget conversion helpers. These conversions are the semantic core
// of cross-provider thinking translation, so their threshold boundaries must be
// pinned against regression.
package thinking_test

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
)

// TestConvertLevelToBudget verifies the canonical level->budget mapping,
// including case-insensitivity and rejection of unknown levels.
func TestConvertLevelToBudget(t *testing.T) {
	cases := []struct {
		level      string
		wantBudget int
		wantOK     bool
	}{
		{"none", 0, true},
		{"auto", -1, true},
		{"minimal", 512, true},
		{"low", 1024, true},
		{"medium", 8192, true},
		{"high", 24576, true},
		{"xhigh", 32768, true},
		{"max", 128000, true},
		{"HIGH", 24576, true},  // case-insensitive
		{"Medium", 8192, true}, // case-insensitive
		{"ultra", 0, false},    // unknown level
		{"", 0, false},         // empty
	}
	for _, tc := range cases {
		t.Run(tc.level, func(t *testing.T) {
			budget, ok := thinking.ConvertLevelToBudget(tc.level)
			if ok != tc.wantOK {
				t.Fatalf("ConvertLevelToBudget(%q) ok = %v, want %v", tc.level, ok, tc.wantOK)
			}
			if ok && budget != tc.wantBudget {
				t.Errorf("ConvertLevelToBudget(%q) = %d, want %d", tc.level, budget, tc.wantBudget)
			}
		})
	}
}

// TestConvertBudgetToLevel pins the threshold-based budget->level mapping.
// The exact boundary values (512/513, 1024/1025, 8192/8193, 24576/24577) are
// the highest-risk regression points: an off-by-one shift silently changes the
// reasoning effort delivered to level-only upstreams.
func TestConvertBudgetToLevel(t *testing.T) {
	cases := []struct {
		name      string
		budget    int
		wantLevel string
		wantOK    bool
	}{
		{"invalid_below_neg1", -2, "", false},
		{"auto", -1, "auto", true},
		{"none", 0, "none", true},
		{"minimal_low_edge", 1, "minimal", true},
		{"minimal_high_edge", 512, "minimal", true},
		{"low_low_edge", 513, "low", true},
		{"low_high_edge", 1024, "low", true},
		{"medium_low_edge", 1025, "medium", true},
		{"medium_high_edge", 8192, "medium", true},
		{"high_low_edge", 8193, "high", true},
		{"high_high_edge", 24576, "high", true},
		{"xhigh_low_edge", 24577, "xhigh", true},
		{"xhigh_large", 1_000_000, "xhigh", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			level, ok := thinking.ConvertBudgetToLevel(tc.budget)
			if ok != tc.wantOK {
				t.Fatalf("ConvertBudgetToLevel(%d) ok = %v, want %v", tc.budget, ok, tc.wantOK)
			}
			if ok && level != tc.wantLevel {
				t.Errorf("ConvertBudgetToLevel(%d) = %q, want %q", tc.budget, level, tc.wantLevel)
			}
		})
	}
}

// TestConvertRoundTripStability ensures Level->Budget->Level is stable for the
// canonical levels (a level maps to a budget that maps back to the same level).
// This guards against the two mapping tables drifting out of sync.
func TestConvertRoundTripStability(t *testing.T) {
	for _, level := range []string{"minimal", "low", "medium", "high", "xhigh"} {
		budget, ok := thinking.ConvertLevelToBudget(level)
		if !ok {
			t.Fatalf("ConvertLevelToBudget(%q) unexpectedly failed", level)
		}
		back, ok := thinking.ConvertBudgetToLevel(budget)
		if !ok {
			t.Fatalf("ConvertBudgetToLevel(%d) unexpectedly failed", budget)
		}
		if back != level {
			t.Errorf("round-trip %q -> %d -> %q not stable", level, budget, back)
		}
	}
}
