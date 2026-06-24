package executor

import (
	"testing"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestMimoLockThinkingParams_LocksWhenEnabled(t *testing.T) {
	body := []byte(`{
		"model": "mimo-v2.5-pro",
		"thinking": {"type": "enabled"},
		"messages": [{"role": "user", "content": "hello"}],
		"temperature": 0.1,
		"top_p": 0.5
	}`)

	out := mimoLockThinkingParams(body)

	if got := gjson.GetBytes(out, "temperature").Float(); got != 1.0 {
		t.Fatalf("temperature = %v, want 1.0", got)
	}
	if got := gjson.GetBytes(out, "top_p").Float(); got != 0.95 {
		t.Fatalf("top_p = %v, want 0.95", got)
	}
}

func TestMimoLockThinkingParams_NoLockWhenDisabled(t *testing.T) {
	body := []byte(`{
		"model": "mimo-v2.5-pro",
		"thinking": {"type": "disabled"},
		"messages": [{"role": "user", "content": "hello"}],
		"temperature": 0.1,
		"top_p": 0.5
	}`)

	out := mimoLockThinkingParams(body)

	if got := gjson.GetBytes(out, "temperature").Float(); got != 0.1 {
		t.Fatalf("temperature = %v, want 0.1 (unchanged)", got)
	}
	if got := gjson.GetBytes(out, "top_p").Float(); got != 0.5 {
		t.Fatalf("top_p = %v, want 0.5 (unchanged)", got)
	}
}

func TestMimoLockThinkingParams_NoLockWhenAbsent(t *testing.T) {
	body := []byte(`{
		"model": "mimo-v2.5-pro",
		"messages": [{"role": "user", "content": "hello"}],
		"temperature": 0.7,
		"top_p": 0.8
	}`)

	out := mimoLockThinkingParams(body)

	if got := gjson.GetBytes(out, "temperature").Float(); got != 0.7 {
		t.Fatalf("temperature = %v, want 0.7 (unchanged)", got)
	}
	if got := gjson.GetBytes(out, "top_p").Float(); got != 0.8 {
		t.Fatalf("top_p = %v, want 0.8 (unchanged)", got)
	}
}

func TestMimoLockThinkingParams_OverwritesExistingValues(t *testing.T) {
	body := []byte(`{
		"model": "mimo-v2.5-pro",
		"thinking": {"type": "enabled"},
		"messages": [{"role": "user", "content": "hello"}],
		"temperature": 0.0,
		"top_p": 0.1
	}`)

	out := mimoLockThinkingParams(body)

	if got := gjson.GetBytes(out, "temperature").Float(); got != 1.0 {
		t.Fatalf("temperature = %v, want 1.0 (overwritten)", got)
	}
	if got := gjson.GetBytes(out, "top_p").Float(); got != 0.95 {
		t.Fatalf("top_p = %v, want 0.95 (overwritten)", got)
	}
}

func TestMimoLockThinkingParams_AddsMissingParams(t *testing.T) {
	body := []byte(`{
		"model": "mimo-v2.5-pro",
		"thinking": {"type": "enabled"},
		"messages": [{"role": "user", "content": "hello"}]
	}`)

	out := mimoLockThinkingParams(body)

	if !gjson.GetBytes(out, "temperature").Exists() {
		t.Fatalf("temperature should be added when thinking is enabled")
	}
	if got := gjson.GetBytes(out, "temperature").Float(); got != 1.0 {
		t.Fatalf("temperature = %v, want 1.0", got)
	}
	if !gjson.GetBytes(out, "top_p").Exists() {
		t.Fatalf("top_p should be added when thinking is enabled")
	}
	if got := gjson.GetBytes(out, "top_p").Float(); got != 0.95 {
		t.Fatalf("top_p = %v, want 0.95", got)
	}
}

func TestMimoLockThinkingParams_PreservesOtherFields(t *testing.T) {
	body := []byte(`{
		"model": "mimo-v2.5-pro",
		"thinking": {"type": "enabled"},
		"messages": [{"role": "user", "content": "hello"}],
		"max_completion_tokens": 1024,
		"frequency_penalty": 0.5,
		"presence_penalty": 0.3
	}`)

	out := mimoLockThinkingParams(body)

	if got := gjson.GetBytes(out, "max_completion_tokens").Int(); got != 1024 {
		t.Fatalf("max_completion_tokens = %v, want 1024 (preserved)", got)
	}
	if got := gjson.GetBytes(out, "frequency_penalty").Float(); got != 0.5 {
		t.Fatalf("frequency_penalty = %v, want 0.5 (preserved)", got)
	}
	if got := gjson.GetBytes(out, "presence_penalty").Float(); got != 0.3 {
		t.Fatalf("presence_penalty = %v, want 0.3 (preserved)", got)
	}
}

// TestMimoLockThinkingParams_SimulatedConfigOverride simulates the full flow:
// ApplyPayloadConfigWithRequest sets custom temperature/top_p, then
// mimoLockThinkingParams overwrites them. This verifies the fix for the
// ordering issue where the lock must run AFTER config application.
func TestMimoLockThinkingParams_SimulatedConfigOverride(t *testing.T) {
	body := []byte(`{
		"model": "mimo-v2.5-pro",
		"thinking": {"type": "enabled"},
		"messages": [{"role": "user", "content": "hello"}]
	}`)

	// Step 1: simulate ApplyPayloadConfigWithRequest setting custom values
	body, _ = sjson.SetBytes(body, "temperature", 0.3)
	body, _ = sjson.SetBytes(body, "top_p", 0.7)

	if got := gjson.GetBytes(body, "temperature").Float(); got != 0.3 {
		t.Fatalf("pre-lock temperature = %v, want 0.3", got)
	}

	// Step 2: lock params (runs after ApplyPayloadConfigWithRequest)
	body = mimoLockThinkingParams(body)

	if got := gjson.GetBytes(body, "temperature").Float(); got != 1.0 {
		t.Fatalf("post-lock temperature = %v, want 1.0 (config overridden by lock)", got)
	}
	if got := gjson.GetBytes(body, "top_p").Float(); got != 0.95 {
		t.Fatalf("post-lock top_p = %v, want 0.95 (config overridden by lock)", got)
	}
}
