package modelkind

import "testing"

func TestIsDeepSeekModel(t *testing.T) {
	cases := []struct {
		name  string
		model string
		want  bool
	}{
		{name: "standard deepseek-chat", model: "deepseek-chat", want: true},
		{name: "case insensitive DeepSeek-Chat", model: "DeepSeek-Chat", want: true},
		{name: "deepseek-r1", model: "deepseek-r1", want: true},
		{name: "with leading/trailing spaces deepseek-v3", model: "  deepseek-v3  ", want: true},
		{name: "mimo-v2 is not deepseek", model: "mimo-v2", want: false},
		{name: "gpt-4 is not deepseek", model: "gpt-4", want: false},
		{name: "empty string is not deepseek", model: "", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsDeepSeekModel(tc.model)
			if got != tc.want {
				t.Fatalf("IsDeepSeekModel(%q) = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}

func TestIsMIMOModel(t *testing.T) {
	cases := []struct {
		name  string
		model string
		want  bool
	}{
		{name: "standard mimo-v2", model: "mimo-v2", want: true},
		{name: "case insensitive MIMO-V2.5", model: "MIMO-V2.5", want: true},
		{name: "with leading/trailing spaces mimo-flash", model: "  mimo-flash  ", want: true},
		{name: "deepseek-chat is not mimo", model: "deepseek-chat", want: false},
		{name: "gpt-4 is not mimo", model: "gpt-4", want: false},
		{name: "empty string is not mimo", model: "", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsMIMOModel(tc.model)
			if got != tc.want {
				t.Fatalf("IsMIMOModel(%q) = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}
