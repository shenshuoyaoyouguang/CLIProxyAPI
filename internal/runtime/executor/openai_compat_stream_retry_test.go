package executor

import (
	"errors"
	"io"
	"testing"

	"github.com/tidwall/gjson"
)

// TestIsRetryableStreamDisconnect verifies the retry eligibility logic for
// stream disconnect errors. Only an unexpected EOF before any SSE data is
// considered retryable.
func TestIsRetryableStreamDisconnect(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		gotSSEData bool
		want       bool
	}{
		{name: "nil error is not retryable", err: nil, gotSSEData: false, want: false},
		{name: "ErrUnexpectedEOF with no SSE data is retryable", err: io.ErrUnexpectedEOF, gotSSEData: false, want: true},
		{name: "ErrUnexpectedEOF after SSE data is not retryable", err: io.ErrUnexpectedEOF, gotSSEData: true, want: false},
		{name: "other error is not retryable", err: errors.New("other"), gotSSEData: false, want: false},
		{name: "io.EOF is not retryable (only ErrUnexpectedEOF)", err: io.EOF, gotSSEData: false, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isRetryableStreamDisconnect(tc.err, tc.gotSSEData)
			if got != tc.want {
				t.Fatalf("isRetryableStreamDisconnect(%v, %v) = %v, want %v", tc.err, tc.gotSSEData, got, tc.want)
			}
		})
	}
}

// TestDetectReasoningEffort verifies detection of reasoning effort fields in
// flat (reasoning_effort), nested (reasoning.effort), and Claude
// (output_config.effort) formats.
func TestDetectReasoningEffort(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantEffort string
		wantFormat string
	}{
		{
			name:       "flat reasoning_effort",
			body:       `{"reasoning_effort":"high"}`,
			wantEffort: "high",
			wantFormat: "flat",
		},
		{
			name:       "nested reasoning.effort",
			body:       `{"reasoning":{"effort":"high"}}`,
			wantEffort: "high",
			wantFormat: "nested",
		},
		{
			name:       "Claude output_config.effort",
			body:       `{"output_config":{"effort":"max"}}`,
			wantEffort: "max",
			wantFormat: "claude_effort",
		},
		{
			name:       "no effort field returns empty",
			body:       `{}`,
			wantEffort: "",
			wantFormat: "",
		},
		{
			name:       "flat takes priority over nested",
			body:       `{"reasoning_effort":"high","reasoning":{"effort":"low"}}`,
			wantEffort: "high",
			wantFormat: "flat",
		},
		{
			name:       "flat takes priority over Claude effort",
			body:       `{"reasoning_effort":"high","output_config":{"effort":"low"}}`,
			wantEffort: "high",
			wantFormat: "flat",
		},
		{
			name:       "nested takes priority over Claude effort",
			body:       `{"reasoning":{"effort":"medium"},"output_config":{"effort":"low"}}`,
			wantEffort: "medium",
			wantFormat: "nested",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotEffort, gotFormat := detectReasoningEffort([]byte(tc.body))
			if gotEffort != tc.wantEffort || gotFormat != tc.wantFormat {
				t.Fatalf("detectReasoningEffort(%s) = (%q, %q), want (%q, %q)",
					tc.body, gotEffort, gotFormat, tc.wantEffort, tc.wantFormat)
			}
		})
	}
}

// TestDegradeReasoningForRetry verifies that degradeReasoningForRetry lowers
// the reasoning effort by one notch in all supported formats, and removes the
// field entirely when degradation reaches the bottom of the chain.
func TestDegradeReasoningForRetry(t *testing.T) {
	t.Run("flat format max→high", func(t *testing.T) {
		body := []byte(`{"reasoning_effort":"max"}`)
		got := degradeReasoningForRetry(body)
		if v := gjson.GetBytes(got, "reasoning_effort"); !v.Exists() {
			t.Fatalf("reasoning_effort field missing after degradation: %s", string(got))
		} else if v.String() != "high" {
			t.Fatalf("reasoning_effort = %q, want %q", v.String(), "high")
		}
	})

	t.Run("nested format max→high", func(t *testing.T) {
		body := []byte(`{"reasoning":{"effort":"max"}}`)
		got := degradeReasoningForRetry(body)
		if v := gjson.GetBytes(got, "reasoning.effort"); !v.Exists() {
			t.Fatalf("reasoning.effort field missing after degradation: %s", string(got))
		} else if v.String() != "high" {
			t.Fatalf("reasoning.effort = %q, want %q", v.String(), "high")
		}
	})

	t.Run("Claude effort max→high", func(t *testing.T) {
		body := []byte(`{"output_config":{"effort":"max"}}`)
		got := degradeReasoningForRetry(body)
		if v := gjson.GetBytes(got, "output_config.effort"); !v.Exists() {
			t.Fatalf("output_config.effort field missing after degradation: %s", string(got))
		} else if v.String() != "high" {
			t.Fatalf("output_config.effort = %q, want %q", v.String(), "high")
		}
	})

	t.Run("minimal is removed (flat)", func(t *testing.T) {
		body := []byte(`{"reasoning_effort":"minimal"}`)
		got := degradeReasoningForRetry(body)
		if v := gjson.GetBytes(got, "reasoning_effort"); v.Exists() {
			t.Fatalf("reasoning_effort should be removed after degrading minimal, got %s; body=%s", v.Raw, string(got))
		}
	})

	t.Run("minimal is removed (nested)", func(t *testing.T) {
		body := []byte(`{"reasoning":{"effort":"minimal"}}`)
		got := degradeReasoningForRetry(body)
		if v := gjson.GetBytes(got, "reasoning.effort"); v.Exists() {
			t.Fatalf("reasoning.effort should be removed after degrading minimal, got %s; body=%s", v.Raw, string(got))
		}
	})

	t.Run("minimal is removed (Claude)", func(t *testing.T) {
		body := []byte(`{"output_config":{"effort":"minimal"}}`)
		got := degradeReasoningForRetry(body)
		if v := gjson.GetBytes(got, "output_config.effort"); v.Exists() {
			t.Fatalf("output_config.effort should be removed after degrading minimal, got %s; body=%s", v.Raw, string(got))
		}
		// Empty output_config should also be cleaned up.
		if v := gjson.GetBytes(got, "output_config"); v.Exists() {
			t.Fatalf("output_config should be removed when empty, got %s; body=%s", v.Raw, string(got))
		}
	})

	t.Run("no effort field returns body unchanged", func(t *testing.T) {
		body := []byte(`{"model":"gpt-4"}`)
		got := degradeReasoningForRetry(body)
		// Body must be unchanged (no effort field → nothing to degrade).
		if string(got) != string(body) {
			t.Fatalf("body should be unchanged when no effort field exists\n got: %s\nwant: %s", string(got), string(body))
		}
	})

	t.Run("high→medium (flat)", func(t *testing.T) {
		body := []byte(`{"reasoning_effort":"high"}`)
		got := degradeReasoningForRetry(body)
		if v := gjson.GetBytes(got, "reasoning_effort"); !v.Exists() {
			t.Fatalf("reasoning_effort field missing: %s", string(got))
		} else if v.String() != "medium" {
			t.Fatalf("reasoning_effort = %q, want %q", v.String(), "medium")
		}
	})

	t.Run("low→minimal (flat)", func(t *testing.T) {
		body := []byte(`{"reasoning_effort":"low"}`)
		got := degradeReasoningForRetry(body)
		if v := gjson.GetBytes(got, "reasoning_effort"); !v.Exists() {
			t.Fatalf("reasoning_effort field missing: %s", string(got))
		} else if v.String() != "minimal" {
			t.Fatalf("reasoning_effort = %q, want %q", v.String(), "minimal")
		}
	})

	t.Run("preserves other fields", func(t *testing.T) {
		body := []byte(`{"model":"gpt-4","reasoning_effort":"max","stream":true}`)
		got := degradeReasoningForRetry(body)
		// Other fields must be preserved.
		if v := gjson.GetBytes(got, "model"); v.String() != "gpt-4" {
			t.Fatalf("model field not preserved: %s", string(got))
		}
		if v := gjson.GetBytes(got, "stream"); !v.Exists() || !v.Bool() {
			t.Fatalf("stream field not preserved: %s", string(got))
		}
		if v := gjson.GetBytes(got, "reasoning_effort"); v.String() != "high" {
			t.Fatalf("reasoning_effort = %q, want %q", v.String(), "high")
		}
	})

	t.Run("Claude effort low degrades to removal (Claude lacks minimal)", func(t *testing.T) {
		body := []byte(`{"output_config":{"effort":"low"}}`)
		got := degradeReasoningForRetry(body)
		// Claude does not support "minimal" effort, so "low" degrades directly to
		// field removal (one step, not low→minimal→removed).
		if v := gjson.GetBytes(got, "output_config.effort"); v.Exists() {
			t.Fatalf("output_config.effort should be removed after degrading low, got %s; body=%s", v.Raw, string(got))
		}
		// Empty output_config should also be cleaned up.
		if v := gjson.GetBytes(got, "output_config"); v.Exists() {
			t.Fatalf("output_config should be removed when empty, got %s; body=%s", v.Raw, string(got))
		}
	})

	t.Run("Claude effort high→medium (maps normally)", func(t *testing.T) {
		body := []byte(`{"output_config":{"effort":"high"}}`)
		got := degradeReasoningForRetry(body)
		// "high" → "medium" is a normal degradation that Claude supports.
		if v := gjson.GetBytes(got, "output_config.effort"); !v.Exists() {
			t.Fatalf("output_config.effort field missing after degrading high: %s", string(got))
		} else if v.String() != "medium" {
			t.Fatalf("output_config.effort = %q, want %q; body=%s", v.String(), "medium", string(got))
		}
	})
}
