package common

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestSSETransformEngine_EventTypeMapping(t *testing.T) {
	engine := NewSSETransformEngine(
		NewEventTypeMappingRule("message_start", "msg_start"),
	)
	chunk := []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n")
	out := engine.Transform(chunk)
	if len(out) != 1 {
		t.Fatalf("expected 1 output frame, got %d", len(out))
	}
	if !contains(out[0], []byte("event: msg_start")) {
		t.Fatalf("expected event type msg_start, got %q", out[0])
	}
}

func TestSSETransformEngine_FieldRewrite(t *testing.T) {
	engine := NewSSETransformEngine(
		NewFieldRewriteRule(SSEEventContentBlockDelta, "delta.text", func(v gjson.Result) (interface{}, error) {
			return "rewritten", nil
		}),
	)
	chunk := []byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n")
	out := engine.Transform(chunk)
	if len(out) != 1 {
		t.Fatalf("expected 1 output frame, got %d", len(out))
	}
	if !contains(out[0], []byte(`"rewritten"`)) {
		t.Fatalf("expected rewritten text, got %q", out[0])
	}
}

func TestSSETransformEngine_Condition(t *testing.T) {
	engine := NewSSETransformEngine(SSETransformRule{
		SourceEvent: SSEEventContentBlockDelta,
		TargetEvent: "filtered_delta",
		Condition: func(eventType string, data []byte) bool {
			return gjson.GetBytes(data, "index").Int() == 1
		},
	})
	chunk := []byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"a\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"b\"}}\n\n")
	out := engine.Transform(chunk)
	if len(out) != 2 {
		t.Fatalf("expected 2 output frames, got %d", len(out))
	}
	// First event (index=0) should keep original name
	if !contains(out[0], []byte("event: content_block_delta")) {
		t.Fatalf("first event should keep original name, got %q", out[0])
	}
	// Second event (index=1) should be renamed
	if !contains(out[1], []byte("event: filtered_delta")) {
		t.Fatalf("second event should be renamed, got %q", out[1])
	}
}

func TestSSEIntegrityChecker_NoViolations(t *testing.T) {
	checker := NewSSEIntegrityChecker()
	input := []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\"}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	checker.RecordInput(input)
	checker.RecordOutput(input) // same as input
	violations := checker.Verify()
	if len(violations) != 0 {
		t.Fatalf("expected no violations, got %d: %v", len(violations), violations)
	}
}

func TestSSEIntegrityChecker_EventCountMismatch(t *testing.T) {
	checker := NewSSEIntegrityChecker()
	input := []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\"}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	output := []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n") // missing content_block and message_delta
	checker.RecordInput(input)
	checker.RecordOutput(output)
	violations := checker.Verify()
	if len(violations) == 0 {
		t.Fatal("expected violations for missing events")
	}
}

func contains(data, sub []byte) bool {
	return len(data) >= len(sub) && hasSubstr(data, sub)
}

func hasSubstr(data, sub []byte) bool {
	for i := 0; i <= len(data)-len(sub); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if data[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
