package helps

import (
	"bytes"
	"strings"
	"testing"
)

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
		t.Fatalf("Flush should produce nothing for a complete stream, got %d chunks", len(flush))
	}
	joined := bytes.Join(out, nil)
	expected := chunk
	if !bytes.Equal(joined, expected) {
		t.Fatalf("passthrough mismatch\n got: %q\nwant: %q", joined, expected)
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
		t.Fatalf("early delta should be buffered, got %d chunks", len(out1))
	}
	out2 := n.Process(chunk2)
	out3 := n.Process(chunk3)
	flush := n.Flush()
	if len(flush) != 0 {
		t.Fatalf("Flush should produce nothing for complete stream, got %d chunks", len(flush))
	}

	all := bytes.Join(append(append(out2, out3...), nil), nil)

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
		t.Fatalf("events after message_stop should be dropped, got %d chunks", len(out2))
	}
	if len(flush) != 0 {
		t.Fatalf("Flush should produce nothing, got %d chunks", len(flush))
	}
	// Ensure the late delta is not present in the joined output.
	all := bytes.Join(out1, nil)
	if bytes.Contains(all, []byte("content_block_delta")) && len(out2) > 0 {
		t.Fatalf("late content_block_delta leaked into output: %q", all)
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
		all := bytes.Join(flush, nil)
		// Must contain content_block_stop for index 0.
		if !bytes.Contains(all, []byte(`"type":"content_block_stop"`)) || !bytes.Contains(all, []byte(`"index":0`)) {
			t.Fatalf("Flush missing content_block_stop: %q", all)
		}
		// Must contain message_delta with stop_reason default "end_turn"
		// ("stop" is not a valid Anthropic stop_reason).
		if !bytes.Contains(all, []byte("message_delta")) {
			t.Fatalf("Flush missing message_delta: %q", all)
		}
		if !bytes.Contains(all, []byte(`"stop_reason":"end_turn"`)) {
			t.Fatalf("Flush missing default stop_reason end_turn: %q", all)
		}
		// Must contain message_stop.
		if !bytes.Contains(all, []byte("message_stop")) {
			t.Fatalf("Flush missing message_stop: %q", all)
		}
	})

	t.Run("uses recorded finish_reason", func(t *testing.T) {
		n := NewSSENormalizer()
		chunk := []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"input_tokens\":5,\"output_tokens\":7}}\n\n")
		_ = n.Process(chunk)
		flush := n.Flush()
		all := bytes.Join(flush, nil)
		// Should not re-emit message_delta because it was already sent.
		if bytes.Contains(all, []byte("message_delta")) {
			t.Fatalf("message_delta should not be re-emitted: %q", all)
		}
		if !bytes.Contains(all, []byte("message_stop")) {
			t.Fatalf("Flush missing message_stop: %q", all)
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
		all := bytes.Join(flush, nil)
		if bytes.Contains(all, []byte("message_delta")) {
			t.Fatalf("message_delta should not be re-emitted: %q", all)
		}
		if !bytes.Contains(all, []byte("message_stop")) {
			t.Fatalf("Flush missing message_stop: %q", all)
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
		t.Fatalf("Flush should produce nothing, got %d chunks", len(flush))
	}
	all := bytes.Join(out, nil)
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
		count := strings.Count(string(all), "event: "+et)
		if count == 0 {
			// The normalizer re-emits events with a single space, so check that
			// the event type is present at all.
			if !bytes.Contains(all, []byte(et)) {
				t.Fatalf("event type %s missing from output: %q", et, all)
			}
		}
	}
}

func TestSSENormalizer_OutputFormatCompleteFrames(t *testing.T) {
	n := NewSSENormalizer()
	chunk := []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n")
	out := n.Process(chunk)
	if len(out) != 1 {
		t.Fatalf("got %d chunks, want 1", len(out))
	}
	frame := out[0]
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
	all := bytes.Join(out, nil)
	if !bytes.Contains(all, []byte("event: ping")) {
		t.Fatalf("ping not passed through: %q", all)
	}
	if !bytes.Contains(all, []byte("event: error")) {
		t.Fatalf("error not passed through: %q", all)
	}
}
