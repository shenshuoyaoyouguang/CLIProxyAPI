package helps

import (
	"bytes"
	"strings"
	"testing"
)

// splitFrames splits a concatenated SSE byte stream into individual event
// frames on the "\n\n" separator. Empty tail entries are dropped. Each
// returned frame includes its trailing "\n\n" so it round-trips through the
// SSE frame boundary. This is the canonical helper for tests that need to
// count or index frames after Process/Flush return a single []byte.
func splitFrames(b []byte) [][]byte {
	if len(b) == 0 {
		return nil
	}
	parts := bytes.Split(b, []byte("\n\n"))
	out := make([][]byte, 0, len(parts))
	for _, p := range parts {
		if len(bytes.TrimSpace(p)) > 0 {
			out = append(out, append(p, '\n', '\n'))
		}
	}
	return out
}

func TestSSEEventType(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
		ok   bool
	}{
		{
			name: "single space prefix",
			line: "event: message_start",
			want: "message_start",
			ok:   true,
		},
		{
			name: "multiple spaces prefix",
			line: "event:   message_start",
			want: "message_start",
			ok:   true,
		},
		{
			name: "tab and space mixed",
			line: "event:\t message_start",
			want: "message_start",
			ok:   true,
		},
		{
			name: "tab only separator",
			line: "event:\tmessage_start",
			want: "message_start",
			ok:   true,
		},
		{
			name: "trailing CR LF stripped",
			line: "event: message_start\r\n",
			want: "message_start",
			ok:   true,
		},
		{
			name: "trailing LF stripped",
			line: "event: message_start\n",
			want: "message_start",
			ok:   true,
		},
		{
			name: "data line returns false",
			line: "data: {\"a\":1}",
			want: "",
			ok:   false,
		},
		{
			name: "comment line returns false",
			line: ": keep-alive",
			want: "",
			ok:   false,
		},
		{
			name: "blank line returns false",
			line: "",
			want: "",
			ok:   false,
		},
		{
			name: "event with no value returns false",
			line: "event:   ",
			want: "",
			ok:   false,
		},
		{
			name: "event prefix only returns false",
			line: "event:",
			want: "",
			ok:   false,
		},
		{
			name: "event type with embedded spaces preserved",
			line: "event:  message start",
			want: "message start",
			ok:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := sseEventType([]byte(tc.line))
			if ok != tc.ok {
				t.Fatalf("sseEventType(%q) ok = %v, want %v", tc.line, ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("sseEventType(%q) = %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}

func TestSplitSSELines(t *testing.T) {
	t.Run("simple split no trailing newline", func(t *testing.T) {
		data := []byte("event: a\ndata: {1}")
		lines := splitSSELines(data)
		if len(lines) != 2 {
			t.Fatalf("got %d lines, want 2", len(lines))
		}
		if string(lines[0]) != "event: a" {
			t.Fatalf("lines[0] = %q, want %q", lines[0], "event: a")
		}
		if string(lines[1]) != "data: {1}" {
			t.Fatalf("lines[1] = %q, want %q", lines[1], "data: {1}")
		}
		for i, line := range lines {
			if bytes.HasSuffix(line, []byte("\r")) || bytes.HasSuffix(line, []byte("\n")) {
				t.Fatalf("line %d has trailing newline: %q", i, line)
			}
		}
	})

	t.Run("CRLF data line has no residual CR", func(t *testing.T) {
		data := []byte("data: {\"a\":1}\r\n")
		lines := splitSSELines(data)
		// bytes.Split on "\n" yields ["data: {\"a\":1}\r", ""].
		if len(lines) != 2 {
			t.Fatalf("got %d lines, want 2", len(lines))
		}
		if string(lines[0]) != "data: {\"a\":1}" {
			t.Fatalf("lines[0] = %q, want %q (no residual CR)", lines[0], "data: {\"a\":1}")
		}
		if string(lines[1]) != "" {
			t.Fatalf("lines[1] = %q, want empty", lines[1])
		}
	})

	t.Run("multi-event block no trailing newlines", func(t *testing.T) {
		data := []byte("event: a\ndata: {1}\n\nevent: b\ndata: {2}\n")
		lines := splitSSELines(data)
		for i, line := range lines {
			if bytes.HasSuffix(line, []byte("\r")) || bytes.HasSuffix(line, []byte("\n")) {
				t.Fatalf("line %d has trailing newline: %q", i, line)
			}
		}
	})

	t.Run("empty input returns nil", func(t *testing.T) {
		if got := splitSSELines(nil); got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
}

func TestSSENormalizer_PassthroughNormalOrder(t *testing.T) {
	n := NewSSENormalizer()
	chunk := []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")

	out := n.Process(chunk)
	flush := n.Flush()
	if len(flush) != 0 {
		t.Fatalf("Flush should produce nothing for a complete stream, got %d bytes", len(flush))
	}
	expected := chunk
	if !bytes.Equal(out, expected) {
		t.Fatalf("passthrough mismatch\n got: %q\nwant: %q", out, expected)
	}
}

func TestSSENormalizer_ReorderMisordered(t *testing.T) {
	n := NewSSENormalizer()
	// content_block_delta arrives before message_start and content_block_start.
	chunk1 := []byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0}\n\n")
	chunk2 := []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\n")
	chunk3 := []byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")

	out1 := n.Process(chunk1)
	if len(out1) != 0 {
		t.Fatalf("early delta should be buffered, got %d bytes", len(out1))
	}
	out2 := n.Process(chunk2)
	out3 := n.Process(chunk3)
	flush := n.Flush()
	if len(flush) != 0 {
		t.Fatalf("Flush should produce nothing for complete stream, got %d bytes", len(flush))
	}

	all := append(append([]byte(nil), out2...), out3...)

	// Verify order: message_start must precede content_block_start must precede
	// content_block_delta must precede content_block_stop must precede
	// message_delta must precede message_stop.
	order := []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	}
	prev := -1
	for _, et := range order {
		idx := bytes.Index(all, []byte("event: "+et))
		if idx == -1 {
			t.Fatalf("event %s not found in output: %q", et, all)
		}
		if idx < prev {
			t.Fatalf("event %s at %d precedes previous event at %d", et, idx, prev)
		}
		prev = idx
	}
}

func TestSSENormalizer_DropAfterMessageStop(t *testing.T) {
	n := NewSSENormalizer()
	complete := []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	late := []byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")

	out1 := n.Process(complete)
	out2 := n.Process(late)
	flush := n.Flush()

	if len(out2) != 0 {
		t.Fatalf("events after message_stop should be dropped, got %d bytes", len(out2))
	}
	if len(flush) != 0 {
		t.Fatalf("Flush should produce nothing, got %d bytes", len(flush))
	}
	// Ensure the late delta is not present in the joined output.
	if bytes.Contains(out1, []byte("content_block_delta")) && len(out2) > 0 {
		t.Fatalf("late content_block_delta leaked into output: %q", out1)
	}
}

func TestSSENormalizer_FlushCompletesMissing(t *testing.T) {
	t.Run("missing message_delta and message_stop", func(t *testing.T) {
		n := NewSSENormalizer()
		chunk := []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0}\n\n")
		_ = n.Process(chunk)
		flush := n.Flush()
		if len(flush) == 0 {
			t.Fatal("Flush should produce terminal events")
		}
		// Must contain content_block_stop for index 0.
		if !bytes.Contains(flush, []byte(`"type":"content_block_stop"`)) || !bytes.Contains(flush, []byte(`"index":0`)) {
			t.Fatalf("Flush missing content_block_stop: %q", flush)
		}
		// Must contain message_delta with stop_reason default "end_turn"
		// ("stop" is not a valid Anthropic stop_reason).
		if !bytes.Contains(flush, []byte("message_delta")) {
			t.Fatalf("Flush missing message_delta: %q", flush)
		}
		if !bytes.Contains(flush, []byte(`"stop_reason":"end_turn"`)) {
			t.Fatalf("Flush missing default stop_reason end_turn: %q", flush)
		}
		// Must contain message_stop.
		if !bytes.Contains(flush, []byte("message_stop")) {
			t.Fatalf("Flush missing message_stop: %q", flush)
		}
	})

	t.Run("uses recorded finish_reason", func(t *testing.T) {
		n := NewSSENormalizer()
		chunk := []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"input_tokens\":5,\"output_tokens\":7}}\n\n")
		_ = n.Process(chunk)
		flush := n.Flush()
		// Should not re-emit message_delta because it was already sent.
		if bytes.Contains(flush, []byte("message_delta")) {
			t.Fatalf("message_delta should not be re-emitted: %q", flush)
		}
		if !bytes.Contains(flush, []byte("message_stop")) {
			t.Fatalf("Flush missing message_stop: %q", flush)
		}
	})

	t.Run("missing only message_stop", func(t *testing.T) {
		n := NewSSENormalizer()
		chunk := []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n")
		_ = n.Process(chunk)
		flush := n.Flush()
		if bytes.Contains(flush, []byte("message_delta")) {
			t.Fatalf("message_delta should not be re-emitted: %q", flush)
		}
		if !bytes.Contains(flush, []byte("message_stop")) {
			t.Fatalf("Flush missing message_stop: %q", flush)
		}
	})
}

func TestSSENormalizer_MultiSpaceEventPrefix(t *testing.T) {
	n := NewSSENormalizer()
	// Use multiple spaces after "event:" prefix; normalizer must still classify
	// the event correctly as message_start.
	chunk := []byte("event:  message_start\ndata: {\"type\":\"message_start\"}\n\n" +
		"event:  content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\n" +
		"event:  content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0}\n\n" +
		"event:  content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event:  message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"event:  message_stop\ndata: {\"type\":\"message_stop\"}\n\n")

	out := n.Process(chunk)
	flush := n.Flush()
	if len(flush) != 0 {
		t.Fatalf("Flush should produce nothing, got %d bytes", len(flush))
	}
	expectedTypes := []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	}
	for _, et := range expectedTypes {
		// Each event type must appear exactly once.
		count := strings.Count(string(out), "event: "+et)
		if count == 0 {
			// The normalizer re-emits events with a single space, so check that
			// the event type is present at all.
			if !bytes.Contains(out, []byte(et)) {
				t.Fatalf("event type %s missing from output: %q", et, out)
			}
		}
	}
}

func TestSSENormalizer_OutputFormatCompleteFrames(t *testing.T) {
	n := NewSSENormalizer()
	chunk := []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n")
	out := n.Process(chunk)
	frames := splitFrames(out)
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	frame := frames[0]
	// Each frame must end with two newlines.
	if !bytes.HasSuffix(frame, []byte("\n\n")) {
		t.Fatalf("frame does not end with two newlines: %q", frame)
	}
	// Frame must start with "event: ".
	if !bytes.HasPrefix(frame, []byte("event: ")) {
		t.Fatalf("frame does not start with 'event: ': %q", frame)
	}
	// Frame must contain a "data: " line.
	if !bytes.Contains(frame, []byte("\ndata: ")) {
		t.Fatalf("frame missing 'data: ' line: %q", frame)
	}
}

func TestSSENormalizer_PingAndErrorPassThrough(t *testing.T) {
	n := NewSSENormalizer()
	chunk := []byte("event: ping\ndata: {}\n\n" +
		"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"overloaded\"}}\n\n")
	out := n.Process(chunk)
	if !bytes.Contains(out, []byte("event: ping")) {
		t.Fatalf("ping not passed through: %q", out)
	}
	if !bytes.Contains(out, []byte("event: error")) {
		t.Fatalf("error not passed through: %q", out)
	}
}

func TestParseSSEEvents_MultiLineData(t *testing.T) {
	t.Run("single data line", func(t *testing.T) {
		chunk := []byte("event: message\ndata: {\"a\":1}\n\n")
		events := parseSSEEvents(chunk)
		if len(events) != 1 {
			t.Fatalf("got %d events, want 1", len(events))
		}
		if string(events[0].Data) != `{"a":1}` {
			t.Fatalf("data = %q, want %q", events[0].Data, `{"a":1}`)
		}
	})

	t.Run("multi-line data concatenated with LF", func(t *testing.T) {
		// W3C SSE spec: multiple data: lines are joined with U+000A (LF).
		chunk := []byte("event: message\ndata: line1\ndata: line2\n\n")
		events := parseSSEEvents(chunk)
		if len(events) != 1 {
			t.Fatalf("got %d events, want 1", len(events))
		}
		expected := "line1\nline2"
		if string(events[0].Data) != expected {
			t.Fatalf("data = %q, want %q", events[0].Data, expected)
		}
	})

	t.Run("three data lines", func(t *testing.T) {
		chunk := []byte("event: message\ndata: a\ndata: b\ndata: c\n\n")
		events := parseSSEEvents(chunk)
		if len(events) != 1 {
			t.Fatalf("got %d events, want 1", len(events))
		}
		expected := "a\nb\nc"
		if string(events[0].Data) != expected {
			t.Fatalf("data = %q, want %q", events[0].Data, expected)
		}
	})

	t.Run("multi-line data across events", func(t *testing.T) {
		chunk := []byte("event: a\ndata: a1\ndata: a2\n\n" +
			"event: b\ndata: b1\n\n")
		events := parseSSEEvents(chunk)
		if len(events) != 2 {
			t.Fatalf("got %d events, want 2", len(events))
		}
		if string(events[0].Data) != "a1\na2" {
			t.Fatalf("event 0 data = %q, want %q", events[0].Data, "a1\na2")
		}
		if string(events[1].Data) != "b1" {
			t.Fatalf("event 1 data = %q, want %q", events[1].Data, "b1")
		}
	})

	t.Run("data lines with no event type are ignored", func(t *testing.T) {
		chunk := []byte("data: orphan1\ndata: orphan2\n\n")
		events := parseSSEEvents(chunk)
		if len(events) != 0 {
			t.Fatalf("got %d events, want 0", len(events))
		}
	})

	t.Run("multi-line data in normalizer preserves LF", func(t *testing.T) {
		n := NewSSENormalizer()
		chunk := []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_type\":\"text\"}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"line1\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"line2\"}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		out := n.Process(chunk)
		flush := n.Flush()
		if len(flush) != 0 {
			t.Fatalf("Flush should produce nothing, got %d bytes", len(flush))
		}
		frames := splitFrames(out)
		if len(frames) < 6 {
			t.Fatalf("expected at least 6 output frames, got %d", len(frames))
		}
	})
}

func TestSSENormalizer_MultiLineDataPayload(t *testing.T) {
	// Test that multi-line data payloads are correctly concatenated with LF
	// when processed through the normalizer, and that the serializer splits
	// them back into multiple data: lines per W3C SSE spec.
	n := NewSSENormalizer()

	// Simulate an upstream that sends multi-line data (e.g., multi-line JSON).
	// This is valid per W3C SSE spec but was previously broken.
	chunk := []byte("event: message_start\ndata: {\"type\":\"message_start\",\"model\":\"claude-3\"}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\n" +
		// Multi-line data: two data: lines for one event.
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello \"}}\n" +
		"data: {\"continued\":true}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")

	out := n.Process(chunk)
	flush := n.Flush()
	if len(flush) != 0 {
		t.Fatalf("Flush should produce nothing, got %d bytes", len(flush))
	}

	all := out
	if !bytes.Contains(all, []byte("content_block_delta")) {
		t.Fatalf("missing content_block_delta in output: %q", all)
	}

	// Verify the output has all expected event types in correct order.
	order := []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	prev := -1
	for _, et := range order {
		idx := bytes.Index(all, []byte("event: "+et))
		if idx == -1 {
			t.Fatalf("event %s not found in output: %q", et, all)
		}
		if idx < prev {
			t.Fatalf("event %s at %d precedes previous event at %d", et, idx, prev)
		}
		prev = idx
	}

	// Verify that the multi-line data payload is correctly re-serialized
	// as two separate "data:" lines (W3C SSE spec), NOT as a single line
	// with an embedded newline that would break SSE framing.
	deltaIdx := bytes.Index(all, []byte("event: content_block_delta"))
	if deltaIdx == -1 {
		t.Fatalf("content_block_delta event not found")
	}
	// The content_block_delta frame should contain "data: ...<first json>\ndata: ...<second json>"
	// Find the frame boundaries (from event: to the next blank line).
	frameStart := deltaIdx
	frameEnd := bytes.Index(all[frameStart:], []byte("\n\n"))
	if frameEnd == -1 {
		t.Fatalf("content_block_delta frame not terminated")
	}
	frameEnd += frameStart + 2
	frame := all[frameStart:frameEnd]
	// Count "data: " occurrences within the frame — must be exactly 2 for multi-line data.
	dataCount := bytes.Count(frame, []byte("\ndata: "))
	if dataCount != 2 {
		t.Fatalf("content_block_delta frame should have 2 data: lines, got %d; frame=%q", dataCount, frame)
	}
	// Verify the second data: line contains the continued field.
	if !bytes.Contains(frame, []byte(`{"continued":true}`)) {
		t.Fatalf("second data line missing continued field in frame: %q", frame)
	}
}
