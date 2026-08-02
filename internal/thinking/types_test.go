// Package thinking types unit tests.
package thinking

import "testing"

func TestThinkingConfigConstructors(t *testing.T) {
	cases := []struct {
		name string
		got  ThinkingConfig
		want ThinkingConfig
	}{
		{
			name: "budget",
			got:  NewBudgetConfig(8192),
			want: ThinkingConfig{Mode: ModeBudget, Budget: 8192, Level: ""},
		},
		{
			name: "level",
			got:  NewLevelConfig(LevelHigh),
			want: ThinkingConfig{Mode: ModeLevel, Level: LevelHigh, Budget: 0},
		},
		{
			name: "none",
			got:  NewModeNoneConfig(),
			want: ThinkingConfig{Mode: ModeNone, Budget: 0, Level: ""},
		},
		{
			name: "auto",
			got:  NewModeAutoConfig(),
			want: ThinkingConfig{Mode: ModeAuto, Budget: -1, Level: ""},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("got %+v, want %+v", tc.got, tc.want)
			}
		})
	}
}

// TestNewModeNoneConfig_Exact verifies NewModeNoneConfig is exactly the disabled zero-value combo.
func TestNewModeNoneConfig_Exact(t *testing.T) {
	got := NewModeNoneConfig()
	if got.Mode != ModeNone || got.Budget != 0 || got.Level != "" {
		t.Fatalf("NewModeNoneConfig() = %+v, want {ModeNone,0,\"\"}", got)
	}
}
