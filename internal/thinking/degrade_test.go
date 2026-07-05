package thinking

import (
	"testing"
)

func TestDegradeThinkingLevel(t *testing.T) {
	cases := []struct {
		name  string
		level ThinkingLevel
		want  ThinkingLevel
	}{
		{"max degrades to high", LevelMax, LevelHigh},
		{"xhigh degrades to high", LevelXHigh, LevelHigh},
		{"high degrades to medium", LevelHigh, LevelMedium},
		{"medium degrades to low", LevelMedium, LevelLow},
		{"low degrades to minimal", LevelLow, LevelMinimal},
		{"minimal degrades to empty", LevelMinimal, ""},
		{"none returns empty (no-op)", LevelNone, ""},
		{"auto returns empty (no-op)", LevelAuto, ""},
		{"unknown value returns empty", ThinkingLevel("bogus"), ""},
		{"empty returns empty", ThinkingLevel(""), ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DegradeThinkingLevel(tc.level)
			if got != tc.want {
				t.Fatalf("DegradeThinkingLevel(%q) = %q, want %q", tc.level, got, tc.want)
			}
		})
	}
}

func TestDegradeThinkingConfig_Level(t *testing.T) {
	cases := []struct {
		name string
		cfg  ThinkingConfig
		want ThinkingConfig
	}{
		{
			name: "xhigh level → high level",
			cfg:  ThinkingConfig{Mode: ModeLevel, Level: LevelXHigh},
			want: ThinkingConfig{Mode: ModeLevel, Level: LevelHigh},
		},
		{
			name: "high level → medium level",
			cfg:  ThinkingConfig{Mode: ModeLevel, Level: LevelHigh},
			want: ThinkingConfig{Mode: ModeLevel, Level: LevelMedium},
		},
		{
			name: "low level → minimal level",
			cfg:  ThinkingConfig{Mode: ModeLevel, Level: LevelLow},
			want: ThinkingConfig{Mode: ModeLevel, Level: LevelMinimal},
		},
		{
			name: "minimal level → ModeNone",
			cfg:  ThinkingConfig{Mode: ModeLevel, Level: LevelMinimal},
			want: ThinkingConfig{Mode: ModeNone, Budget: 0, Level: ""},
		},
		{
			name: "none level → ModeNone (no-op)",
			cfg:  ThinkingConfig{Mode: ModeLevel, Level: LevelNone},
			want: ThinkingConfig{Mode: ModeNone, Budget: 0, Level: ""},
		},
		{
			name: "auto level → ModeNone (no-op)",
			cfg:  ThinkingConfig{Mode: ModeLevel, Level: LevelAuto},
			want: ThinkingConfig{Mode: ModeNone, Budget: 0, Level: ""},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DegradeThinkingConfig(tc.cfg)
			if got.Mode != tc.want.Mode || got.Budget != tc.want.Budget || got.Level != tc.want.Level {
				t.Fatalf("DegradeThinkingConfig(%+v) = %+v, want %+v", tc.cfg, got, tc.want)
			}
		})
	}
}

func TestDegradeThinkingConfig_Budget(t *testing.T) {
	cases := []struct {
		name string
		cfg  ThinkingConfig
		want ThinkingConfig
	}{
		{
			name: "budget 32768 (xhigh) → 24576 (high)",
			cfg:  ThinkingConfig{Mode: ModeBudget, Budget: 32768},
			want: ThinkingConfig{Mode: ModeBudget, Budget: 24576},
		},
		{
			name: "budget 24576 (high) → 8192 (medium)",
			cfg:  ThinkingConfig{Mode: ModeBudget, Budget: 24576},
			want: ThinkingConfig{Mode: ModeBudget, Budget: 8192},
		},
		{
			name: "budget 8192 (medium) → 1024 (low)",
			cfg:  ThinkingConfig{Mode: ModeBudget, Budget: 8192},
			want: ThinkingConfig{Mode: ModeBudget, Budget: 1024},
		},
		{
			name: "budget 1024 (low) → 512 (minimal)",
			cfg:  ThinkingConfig{Mode: ModeBudget, Budget: 1024},
			want: ThinkingConfig{Mode: ModeBudget, Budget: 512},
		},
		{
			name: "budget 512 (minimal) → ModeNone",
			cfg:  ThinkingConfig{Mode: ModeBudget, Budget: 512},
			want: ThinkingConfig{Mode: ModeNone, Budget: 0, Level: ""},
		},
		{
			name: "budget 0 (none) → no-op",
			cfg:  ThinkingConfig{Mode: ModeNone, Budget: 0},
			want: ThinkingConfig{Mode: ModeNone, Budget: 0},
		},
		{
			name: "budget -1 (auto) → no-op",
			cfg:  ThinkingConfig{Mode: ModeAuto, Budget: -1},
			want: ThinkingConfig{Mode: ModeAuto, Budget: -1},
		},
		{
			name: "budget 200 (between 1-512, minimal) → ModeNone",
			cfg:  ThinkingConfig{Mode: ModeBudget, Budget: 200},
			want: ThinkingConfig{Mode: ModeNone, Budget: 0},
		},
		{
			name: "budget 15000 (between 8193-24576, high) → 8192 (medium)",
			cfg:  ThinkingConfig{Mode: ModeBudget, Budget: 15000},
			want: ThinkingConfig{Mode: ModeBudget, Budget: 8192},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DegradeThinkingConfig(tc.cfg)
			if got.Mode != tc.want.Mode || got.Budget != tc.want.Budget || got.Level != tc.want.Level {
				t.Fatalf("DegradeThinkingConfig(%+v) = %+v, want %+v", tc.cfg, got, tc.want)
			}
		})
	}
}
