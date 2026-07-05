package thinking

import (
	"testing"
)

// TestExtractMIMOConfig verifies extraction of MiMo thinking configuration from
// request bodies. Covers both the primary thinking.type field and the
// reasoning_effort fallback, including priority behavior when both are present.
func TestExtractMIMOConfig(t *testing.T) {
	cases := []struct {
		name string
		body string
		want ThinkingConfig
	}{
		{
			name: "thinking.type=enabled maps to ModeLevel/LevelHigh",
			body: `{"thinking":{"type":"enabled"}}`,
			want: ThinkingConfig{Mode: ModeLevel, Level: LevelHigh},
		},
		{
			name: "thinking.type=disabled maps to ModeNone",
			body: `{"thinking":{"type":"disabled"}}`,
			want: ThinkingConfig{Mode: ModeNone, Budget: 0},
		},
		{
			name: "thinking.type=unknown maps to empty config",
			body: `{"thinking":{"type":"unknown"}}`,
			want: ThinkingConfig{},
		},
		{
			name: "no thinking.type + reasoning_effort=none → ModeNone",
			body: `{"reasoning_effort":"none"}`,
			want: ThinkingConfig{Mode: ModeNone, Budget: 0},
		},
		{
			name: "no thinking.type + reasoning_effort=low → ModeBudget/8192",
			body: `{"reasoning_effort":"low"}`,
			want: ThinkingConfig{Mode: ModeBudget, Budget: 8192},
		},
		{
			name: "no thinking.type + reasoning_effort=medium → ModeBudget/24576",
			body: `{"reasoning_effort":"medium"}`,
			want: ThinkingConfig{Mode: ModeBudget, Budget: 24576},
		},
		{
			name: "no thinking.type + reasoning_effort=high → ModeBudget/64512",
			body: `{"reasoning_effort":"high"}`,
			want: ThinkingConfig{Mode: ModeBudget, Budget: 64512},
		},
		{
			name: "no thinking.type + reasoning_effort=max → ModeBudget/64512",
			body: `{"reasoning_effort":"max"}`,
			want: ThinkingConfig{Mode: ModeBudget, Budget: 64512},
		},
		{
			name: "no thinking.type + reasoning_effort=auto → ModeAuto/-1",
			body: `{"reasoning_effort":"auto"}`,
			want: ThinkingConfig{Mode: ModeAuto, Budget: -1},
		},
		{
			name: "no thinking.type + no reasoning_effort → empty config",
			body: `{"model":"mimo-v2"}`,
			want: ThinkingConfig{},
		},
		{
			name: "thinking.type takes priority over reasoning_effort",
			body: `{"thinking":{"type":"enabled"},"reasoning_effort":"low"}`,
			want: ThinkingConfig{Mode: ModeLevel, Level: LevelHigh},
		},
		{
			name: "empty body → empty config",
			body: `{}`,
			want: ThinkingConfig{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractMIMOConfig([]byte(tc.body))
			if got != tc.want {
				t.Fatalf("extractMIMOConfig(%s) = %+v, want %+v", tc.body, got, tc.want)
			}
		})
	}
}

// TestExtractMIMOConfig_InvalidJSON verifies that invalid JSON returns an empty
// ThinkingConfig (gjson.GetBytes returns non-existent results for invalid JSON).
func TestExtractMIMOConfig_InvalidJSON(t *testing.T) {
	got := extractMIMOConfig([]byte(`not-json`))
	want := ThinkingConfig{}
	if got != want {
		t.Fatalf("extractMIMOConfig(invalid) = %+v, want %+v", got, want)
	}
}

// TestExtractMIMOConfig_EmptyBody verifies that an empty byte slice returns an
// empty ThinkingConfig without panicking.
func TestExtractMIMOConfig_EmptyBody(t *testing.T) {
	got := extractMIMOConfig(nil)
	want := ThinkingConfig{}
	if got != want {
		t.Fatalf("extractMIMOConfig(nil) = %+v, want %+v", got, want)
	}
}
