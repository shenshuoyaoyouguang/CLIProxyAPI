package tui

import (
	"strings"
	"testing"
)

func TestAuthTabRenderDetailIncludesModelStates(t *testing.T) {
	model := authTabModel{}

	out := model.renderDetail(map[string]any{
		"name": "compat-auth",
		"model_states": []any{
			map[string]any{
				"model":            "alias/upstream-a",
				"status":           "error",
				"status_message":   "rate limited",
				"unavailable":      true,
				"next_retry_after": "2026-07-14T10:30:00Z",
			},
		},
	})

	for _, want := range []string{"Model States", "alias/upstream-a", "rate limited", "cooldown"} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderDetail() missing %q in:\n%s", want, out)
		}
	}
}
