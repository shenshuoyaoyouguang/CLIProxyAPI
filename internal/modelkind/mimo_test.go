package modelkind

import "testing"

func TestIsMIMOModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"mimo-v2.5-pro", true},
		{"mimo-v2.5", true},
		{"mimo-v2-pro", true},
		{"mimo-v2-omni", true},
		{"mimo-v2-flash", true},
		{"MIMO-V2.5-PRO", true},
		{" mimo-v2.5-pro ", true},
		{"gpt-4", false},
		{"claude-opus-4-6", false},
		{"deepseek-r1", false},
		{"", false},
	}
	for _, tt := range tests {
		got := IsMIMOModel(tt.model)
		if got != tt.want {
			t.Errorf("IsMIMOModel(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}
