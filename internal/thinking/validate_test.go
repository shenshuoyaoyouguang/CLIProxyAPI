package thinking

import "testing"

// TestIsLevelSupported_TrimsBothSides verifies that isLevelSupported trims
// whitespace from both the level parameter and the supported list entries.
// Bug: the original code only trims supported entries, not the level input.
func TestIsLevelSupported_TrimsBothSides(t *testing.T) {
	tests := []struct {
		name      string
		level     string
		supported []string
		want      bool
	}{
		{
			name:      "exact match",
			level:     "high",
			supported: []string{"low", "medium", "high"},
			want:      true,
		},
		{
			name:      "case insensitive",
			level:     "HIGH",
			supported: []string{"low", "medium", "high"},
			want:      true,
		},
		{
			name:      "whitespace in supported entry",
			level:     "high",
			supported: []string{"low", " high "},
			want:      true,
		},
		{
			name:      "whitespace in level input",
			level:     " high ",
			supported: []string{"low", "medium", "high"},
			want:      true,
		},
		{
			name:      "whitespace in both",
			level:     " Medium ",
			supported: []string{" low ", " medium ", " high "},
			want:      true,
		},
		{
			name:      "no match",
			level:     "ultra",
			supported: []string{"low", "medium", "high"},
			want:      false,
		},
		{
			name:      "empty supported list",
			level:     "high",
			supported: []string{},
			want:      false,
		},
		{
			name:      "empty level",
			level:     "",
			supported: []string{"low", "medium", "high"},
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLevelSupported(tt.level, tt.supported)
			if got != tt.want {
				t.Errorf("isLevelSupported(%q, %v) = %v, want %v", tt.level, tt.supported, got, tt.want)
			}
		})
	}
}
