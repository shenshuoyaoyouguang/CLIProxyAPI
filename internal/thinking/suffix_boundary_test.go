// Package thinking_test provides black-box coverage for model-name suffix
// parsing. The suffix (e.g. "model(high)") overrides body thinking config, so
// malformed and extreme inputs must be handled without panicking or silently
// misinterpreting user intent.
package thinking_test

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
)

// TestParseSuffix_Extraction verifies suffix extraction across well-formed,
// malformed, and nested-parenthesis inputs.
func TestParseSuffix_Extraction(t *testing.T) {
	cases := []struct {
		name          string
		model         string
		wantModelName string
		wantHasSuffix bool
		wantRaw       string
	}{
		{"no_suffix", "gemini-2.5-pro", "gemini-2.5-pro", false, ""},
		{"numeric", "claude-sonnet-4-5(16384)", "claude-sonnet-4-5", true, "16384"},
		{"level", "gpt-5.2(high)", "gpt-5.2", true, "high"},
		{"missing_close", "model(abc", "model(abc", false, ""},
		{"empty_parens", "model()", "model", true, ""},
		{"nested_parens", "model(a(b))", "model(a", true, "b)"}, // LastIndex of "(" wins
		{"trailing_space_not_close", "model(high) ", "model(high) ", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := thinking.ParseSuffix(tc.model)
			if got.HasSuffix != tc.wantHasSuffix {
				t.Fatalf("HasSuffix = %v, want %v", got.HasSuffix, tc.wantHasSuffix)
			}
			if got.ModelName != tc.wantModelName {
				t.Errorf("ModelName = %q, want %q", got.ModelName, tc.wantModelName)
			}
			if got.HasSuffix && got.RawSuffix != tc.wantRaw {
				t.Errorf("RawSuffix = %q, want %q", got.RawSuffix, tc.wantRaw)
			}
		})
	}
}

// TestParseNumericSuffix_Boundaries covers the integer parsing edge cases that
// matter for budget suffixes: leading zeros, negatives, non-numerics, and
// overflow beyond the platform int range.
func TestParseNumericSuffix_Boundaries(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantBudget int
		wantOK     bool
	}{
		{"zero", "0", 0, true},
		{"positive", "8192", 8192, true},
		{"leading_zeros", "08192", 8192, true},
		{"negative_one", "-1", 0, false}, // handled by ParseSpecialSuffix instead
		{"negative_other", "-5", 0, false},
		{"not_a_number", "high", 0, false},
		{"empty", "", 0, false},
		{"overflow_64bit", "9223372036854775808", 0, false}, // math.MaxInt64 + 1
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			budget, ok := thinking.ParseNumericSuffix(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("ParseNumericSuffix(%q) ok = %v, want %v", tc.raw, ok, tc.wantOK)
			}
			if ok && budget != tc.wantBudget {
				t.Errorf("ParseNumericSuffix(%q) = %d, want %d", tc.raw, budget, tc.wantBudget)
			}
		})
	}
}

// TestParseSpecialSuffix covers the special mode tokens (none/auto/-1) with
// case-insensitivity.
func TestParseSpecialSuffix(t *testing.T) {
	cases := []struct {
		raw      string
		wantMode thinking.ThinkingMode
		wantOK   bool
	}{
		{"none", thinking.ModeNone, true},
		{"NONE", thinking.ModeNone, true},
		{"auto", thinking.ModeAuto, true},
		{"AUTO", thinking.ModeAuto, true},
		{"-1", thinking.ModeAuto, true},
		{"high", thinking.ModeBudget, false},
		{"", thinking.ModeBudget, false},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			mode, ok := thinking.ParseSpecialSuffix(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("ParseSpecialSuffix(%q) ok = %v, want %v", tc.raw, ok, tc.wantOK)
			}
			if ok && mode != tc.wantMode {
				t.Errorf("ParseSpecialSuffix(%q) mode = %v, want %v", tc.raw, mode, tc.wantMode)
			}
		})
	}
}

// TestParseLevelSuffix covers discrete level parsing and rejection of
// special/numeric values that belong to other parsers.
func TestParseLevelSuffix(t *testing.T) {
	cases := []struct {
		raw       string
		wantLevel thinking.ThinkingLevel
		wantOK    bool
	}{
		{"minimal", thinking.LevelMinimal, true},
		{"low", thinking.LevelLow, true},
		{"medium", thinking.LevelMedium, true},
		{"high", thinking.LevelHigh, true},
		{"HIGH", thinking.LevelHigh, true},
		{"xhigh", thinking.LevelXHigh, true},
		{"max", thinking.LevelMax, true},
		{"none", "", false}, // special value, not a level
		{"auto", "", false}, // special value, not a level
		{"8192", "", false}, // numeric, not a level
		{"ultra", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			level, ok := thinking.ParseLevelSuffix(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("ParseLevelSuffix(%q) ok = %v, want %v", tc.raw, ok, tc.wantOK)
			}
			if ok && level != tc.wantLevel {
				t.Errorf("ParseLevelSuffix(%q) = %q, want %q", tc.raw, level, tc.wantLevel)
			}
		})
	}
}
