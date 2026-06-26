package executor

import (
	"testing"

	"github.com/tidwall/gjson"
)

// degradeReasoningForRetry lowers the reasoning_effort by one notch to reduce
// thinking token consumption on retry. Returns the degraded payload.
// If no reasoning_effort is present, returns payload unchanged.
//
// Degradation chain: max/xhigh → high → medium → low → minimal → (remove field)
func TestDegradeReasoningForRetry_MaxToHigh(t *testing.T) {
	body := []byte(`{"model":"test","messages":[],"reasoning_effort":"max"}`)
	got := degradeReasoningForRetry(body)
	if v := gjson.GetBytes(got, "reasoning_effort").String(); v != "high" {
		t.Fatalf("reasoning_effort = %q, want %q", v, "high")
	}
}

func TestDegradeReasoningForRetry_HighToMedium(t *testing.T) {
	body := []byte(`{"model":"test","messages":[],"reasoning_effort":"high"}`)
	got := degradeReasoningForRetry(body)
	if v := gjson.GetBytes(got, "reasoning_effort").String(); v != "medium" {
		t.Fatalf("reasoning_effort = %q, want %q", v, "medium")
	}
}

func TestDegradeReasoningForRetry_MediumToLow(t *testing.T) {
	body := []byte(`{"model":"test","messages":[],"reasoning_effort":"medium"}`)
	got := degradeReasoningForRetry(body)
	if v := gjson.GetBytes(got, "reasoning_effort").String(); v != "low" {
		t.Fatalf("reasoning_effort = %q, want %q", v, "low")
	}
}

func TestDegradeReasoningForRetry_LowToMinimal(t *testing.T) {
	body := []byte(`{"model":"test","messages":[],"reasoning_effort":"low"}`)
	got := degradeReasoningForRetry(body)
	if v := gjson.GetBytes(got, "reasoning_effort").String(); v != "minimal" {
		t.Fatalf("reasoning_effort = %q, want %q", v, "minimal")
	}
}

func TestDegradeReasoningForRetry_MinimalRemovesField(t *testing.T) {
	body := []byte(`{"model":"test","messages":[],"reasoning_effort":"minimal"}`)
	got := degradeReasoningForRetry(body)
	if gjson.GetBytes(got, "reasoning_effort").Exists() {
		t.Fatalf("reasoning_effort should be removed for minimal, got %s", string(got))
	}
}

func TestDegradeReasoningForRetry_XhighToHigh(t *testing.T) {
	body := []byte(`{"model":"test","messages":[],"reasoning_effort":"xhigh"}`)
	got := degradeReasoningForRetry(body)
	if v := gjson.GetBytes(got, "reasoning_effort").String(); v != "high" {
		t.Fatalf("reasoning_effort = %q, want %q", v, "high")
	}
}

func TestDegradeReasoningForRetry_NoField(t *testing.T) {
	body := []byte(`{"model":"test","messages":[]}`)
	got := degradeReasoningForRetry(body)
	if gjson.GetBytes(got, "reasoning_effort").Exists() {
		t.Fatalf("reasoning_effort should not be added, got %s", string(got))
	}
}

// Also test reasoning.effort (Codex/Responses format)
func TestDegradeReasoningForRetry_CodexFormat(t *testing.T) {
	body := []byte(`{"model":"test","reasoning":{"effort":"high"}}`)
	got := degradeReasoningForRetry(body)
	if v := gjson.GetBytes(got, "reasoning.effort").String(); v != "medium" {
		t.Fatalf("reasoning.effort = %q, want %q", v, "medium")
	}
}
