package thinking

import (
	"testing"

	"github.com/tidwall/gjson"
)

// TestObjectSubtreeRaw covers the shared object-subtree reader used by the
// claude/kimi/gemini appliers: object returns raw JSON, array is non-writable,
// and absent/null/scalar promote to an empty writeable object.
func TestObjectSubtreeRaw(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		path     string
		wantRaw  string
		writable bool
	}{
		{name: "object", body: `{"thinking":{"type":"enabled"}}`, path: "thinking", wantRaw: `{"type":"enabled"}`, writable: true},
		{name: "object nested", body: `{"generationConfig":{"thinkingConfig":{"thinkingBudget":1}}}`, path: "generationConfig.thinkingConfig", wantRaw: `{"thinkingBudget":1}`, writable: true},
		{name: "absent", body: `{}`, path: "thinking", wantRaw: `{}`, writable: true},
		{name: "null", body: `{"thinking":null}`, path: "thinking", wantRaw: `{}`, writable: true},
		{name: "scalar", body: `{"thinking":"value"}`, path: "thinking", wantRaw: `{}`, writable: true},
		{name: "array", body: `{"thinking":[]}`, path: "thinking", wantRaw: "", writable: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, writable := ObjectSubtreeRaw([]byte(tt.body), tt.path)
			if writable != tt.writable {
				t.Fatalf("writable = %v, want %v", writable, tt.writable)
			}
			if string(raw) != tt.wantRaw {
				t.Fatalf("raw = %q, want %q", raw, tt.wantRaw)
			}
		})
	}
}

// TestSetObjectSubtreeRaw verifies the shared writer creates and replaces the
// subtree in a single full-body pass.
func TestSetObjectSubtreeRaw(t *testing.T) {
	updated := SetObjectSubtreeRaw([]byte(`{"thinking":{"type":"enabled"}}`), "thinking", []byte(`{"type":"disabled"}`))
	if got := gjson.GetBytes(updated, "thinking.type").String(); got != "disabled" {
		t.Fatalf("thinking.type = %q, want disabled", got)
	}

	created := SetObjectSubtreeRaw([]byte(`{}`), "generationConfig.thinkingConfig", []byte(`{"thinkingBudget":8192}`))
	if got := gjson.GetBytes(created, "generationConfig.thinkingConfig.thinkingBudget").Int(); got != 8192 {
		t.Fatalf("thinkingBudget = %d, want 8192", got)
	}
}
