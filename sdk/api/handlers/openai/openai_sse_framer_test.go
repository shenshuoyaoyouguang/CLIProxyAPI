package openai

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestResponsesSSEWriteChunk covers writeResponsesSSEChunk, verifying the
// blank-line delimiter is appended only when the chunk does not already end
// with one.
func TestResponsesSSEWriteChunk(t *testing.T) {
	t.Run("nil writer no panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			writeResponsesSSEChunk(nil, []byte("data: x\n\n"))
		})
	})

	t.Run("empty chunk no write", func(t *testing.T) {
		var buf bytes.Buffer
		writeResponsesSSEChunk(&buf, []byte{})
		assert.Empty(t, buf.Bytes())
	})

	t.Run("nil chunk no write", func(t *testing.T) {
		var buf bytes.Buffer
		writeResponsesSSEChunk(&buf, nil)
		assert.Empty(t, buf.Bytes())
	})

	t.Run("already LF LF suffix no append", func(t *testing.T) {
		var buf bytes.Buffer
		writeResponsesSSEChunk(&buf, []byte("data: x\n\n"))
		assert.Equal(t, []byte("data: x\n\n"), buf.Bytes())
	})

	t.Run("already CRLF CRLF suffix no append", func(t *testing.T) {
		var buf bytes.Buffer
		writeResponsesSSEChunk(&buf, []byte("data: x\r\n\r\n"))
		assert.Equal(t, []byte("data: x\r\n\r\n"), buf.Bytes())
	})

	t.Run("only LF suffix append LF", func(t *testing.T) {
		var buf bytes.Buffer
		writeResponsesSSEChunk(&buf, []byte("data: x\n"))
		assert.Equal(t, []byte("data: x\n\n"), buf.Bytes())
	})

	t.Run("only CRLF suffix append CRLF", func(t *testing.T) {
		var buf bytes.Buffer
		writeResponsesSSEChunk(&buf, []byte("data: x\r\n"))
		assert.Equal(t, []byte("data: x\r\n\r\n"), buf.Bytes())
	})

	t.Run("no newline suffix append LF LF", func(t *testing.T) {
		var buf bytes.Buffer
		writeResponsesSSEChunk(&buf, []byte("data: x"))
		assert.Equal(t, []byte("data: x\n\n"), buf.Bytes())
	})
}

// TestResponsesSSEFramerWriteChunk covers responsesSSEFramer.WriteChunk frame
// reassembly semantics.
func TestResponsesSSEFramerWriteChunk(t *testing.T) {
	t.Run("single complete frame emitted directly", func(t *testing.T) {
		var buf bytes.Buffer
		f := &responsesSSEFramer{}
		f.WriteChunk(&buf, []byte("data: {\"x\":1}\n\n"))
		assert.Equal(t, []byte("data: {\"x\":1}\n\n"), buf.Bytes())
	})

	t.Run("frame split across chunks reassembled", func(t *testing.T) {
		var buf bytes.Buffer
		f := &responsesSSEFramer{}
		// First chunk has incomplete JSON, cannot be emitted on its own.
		f.WriteChunk(&buf, []byte("data: {\"type\":\"fo"))
		assert.Empty(t, buf.Bytes())
		// Second chunk completes the JSON and adds the blank-line delimiter.
		f.WriteChunk(&buf, []byte("o\"}\n\n"))
		assert.Equal(t, []byte("data: {\"type\":\"foo\"}\n\n"), buf.Bytes())
	})

	t.Run("multiple frames in one chunk all emitted", func(t *testing.T) {
		var buf bytes.Buffer
		f := &responsesSSEFramer{}
		f.WriteChunk(&buf, []byte("data: {\"a\":1}\n\ndata: {\"b\":2}\n\n"))
		assert.Equal(t, []byte("data: {\"a\":1}\n\ndata: {\"b\":2}\n\n"), buf.Bytes())
	})

	t.Run("whitespace only pending reset", func(t *testing.T) {
		var buf bytes.Buffer
		f := &responsesSSEFramer{}
		f.WriteChunk(&buf, []byte("   \t  \n  "))
		assert.Empty(t, buf.Bytes())
		assert.Equal(t, 0, f.pendingStart)
		assert.Empty(t, f.pending)
	})
}

// TestResponsesSSEFramerFlush covers responsesSSEFramer.Flush trailing-frame
// recovery and discard semantics.
func TestResponsesSSEFramerFlush(t *testing.T) {
	t.Run("recoverable frame emitted", func(t *testing.T) {
		var buf bytes.Buffer
		f := &responsesSSEFramer{}
		f.pending = []byte("data: {\"x\":1}")
		f.Flush(&buf)
		assert.Equal(t, []byte("data: {\"x\":1}\n\n"), buf.Bytes())
	})

	t.Run("unrecoverable partial frame discarded", func(t *testing.T) {
		var buf bytes.Buffer
		f := &responsesSSEFramer{}
		// event: without data: cannot be emitted without a delimiter.
		f.pending = []byte("event: foo")
		f.Flush(&buf)
		assert.Empty(t, buf.Bytes())
		assert.Equal(t, 0, f.pendingStart)
		assert.Empty(t, f.pending)
	})

	t.Run("empty pending no output", func(t *testing.T) {
		var buf bytes.Buffer
		f := &responsesSSEFramer{}
		f.Flush(&buf)
		assert.Empty(t, buf.Bytes())
	})
}

// TestResponsesSSEFramerRepairFrame covers repairFrame dispatch and the
// response.output_item.done -> response.completed repair pipeline.
func TestResponsesSSEFramerRepairFrame(t *testing.T) {
	t.Run("non JSON payload returned as is", func(t *testing.T) {
		f := &responsesSSEFramer{}
		frame := []byte("data: hello world\n\n")
		result := f.repairFrame(frame)
		assert.Equal(t, frame, result)
	})

	t.Run("DONE payload returned as is", func(t *testing.T) {
		f := &responsesSSEFramer{}
		frame := []byte("data: [DONE]\n\n")
		result := f.repairFrame(frame)
		assert.Equal(t, frame, result)
	})

	t.Run("output_item.done records item", func(t *testing.T) {
		f := &responsesSSEFramer{}
		payload := `{"type":"response.output_item.done","output_index":0,"item":{"type":"message","content":"hello"}}`
		frame := []byte("data: " + payload + "\n\n")
		result := f.repairFrame(frame)
		// Frame is returned unchanged; the side effect is the recorded item.
		assert.Equal(t, frame, result)
		assert.Contains(t, f.outputItems, 0)
		assert.Equal(t, []byte(`{"type":"message","content":"hello"}`), f.outputItems[0])
		assert.Equal(t, []int{0}, f.outputOrder)
	})

	t.Run("completed with empty output repaired", func(t *testing.T) {
		f := &responsesSSEFramer{}
		// Seed the framer with a recorded output item.
		donePayload := `{"type":"response.output_item.done","output_index":0,"item":{"type":"message","content":"hello"}}`
		_ = f.repairFrame([]byte("data: " + donePayload + "\n\n"))

		completedPayload := `{"type":"response.completed","response":{"output":[]}}`
		frame := []byte("data: " + completedPayload + "\n\n")
		result := f.repairFrame(frame)
		// The repaired frame must carry the recorded item inside response.output.
		resultStr := string(result)
		assert.NotEqual(t, string(frame), resultStr)
		assert.Contains(t, resultStr, `"type":"message"`)
		assert.Contains(t, resultStr, `"content":"hello"`)
		assert.Contains(t, resultStr, `"output":[`)
	})

	t.Run("completed with non empty output not repaired", func(t *testing.T) {
		f := &responsesSSEFramer{}
		// Seed the framer with a recorded output item.
		donePayload := `{"type":"response.output_item.done","output_index":0,"item":{"type":"message","content":"hello"}}`
		_ = f.repairFrame([]byte("data: " + donePayload + "\n\n"))

		completedPayload := `{"type":"response.completed","response":{"output":[{"type":"existing"}]}}`
		frame := []byte("data: " + completedPayload + "\n\n")
		result := f.repairFrame(frame)
		// Non-empty output short-circuits the repair; frame is returned as-is.
		assert.Equal(t, frame, result)
	})
}

// TestResponsesSSEFrameLen covers responsesSSEFrameLen delimiter detection.
func TestResponsesSSEFrameLen(t *testing.T) {
	t.Run("LF delimiter", func(t *testing.T) {
		// "data: x" = 7 bytes, \n\n at index 7, returns 7+2 = 9.
		assert.Equal(t, 9, responsesSSEFrameLen([]byte("data: x\n\nfoo")))
	})

	t.Run("CRLF delimiter", func(t *testing.T) {
		// "data: x" = 7 bytes, \r\n\r\n at index 7, returns 7+4 = 11.
		assert.Equal(t, 11, responsesSSEFrameLen([]byte("data: x\r\n\r\nfoo")))
	})

	t.Run("mixed LF before CRLF", func(t *testing.T) {
		// LF at index 7, CRLF at index 16; LF wins.
		assert.Equal(t, 9, responsesSSEFrameLen([]byte("data: x\n\ndata: y\r\n\r\n")))
	})

	t.Run("mixed CRLF before LF", func(t *testing.T) {
		// CRLF at index 7, LF at index 18; CRLF wins.
		assert.Equal(t, 11, responsesSSEFrameLen([]byte("data: x\r\n\r\ndata: y\n\n")))
	})

	t.Run("no delimiter returns 0", func(t *testing.T) {
		assert.Equal(t, 0, responsesSSEFrameLen([]byte("data: x")))
	})

	t.Run("empty input returns 0", func(t *testing.T) {
		assert.Equal(t, 0, responsesSSEFrameLen([]byte{}))
	})
}

// TestResponsesSSECanEmitWithoutDelimiter covers the emit-without-delimiter
// gate: a pending frame can be emitted only when it carries at least one valid
// JSON data line and is not waiting on an event: field.
func TestResponsesSSECanEmitWithoutDelimiter(t *testing.T) {
	t.Run("data line with valid JSON true", func(t *testing.T) {
		assert.True(t, responsesSSECanEmitWithoutDelimiter([]byte("data: {\"x\":1}")))
	})

	t.Run("data line with invalid JSON false", func(t *testing.T) {
		assert.False(t, responsesSSECanEmitWithoutDelimiter([]byte("data: {invalid}")))
	})

	t.Run("event only no data false", func(t *testing.T) {
		assert.False(t, responsesSSECanEmitWithoutDelimiter([]byte("event: foo")))
	})

	t.Run("whitespace only false", func(t *testing.T) {
		assert.False(t, responsesSSECanEmitWithoutDelimiter([]byte("   \t  ")))
	})

	t.Run("empty false", func(t *testing.T) {
		assert.False(t, responsesSSECanEmitWithoutDelimiter([]byte{}))
	})
}

// TestResponsesSSECompactPendingIfLarge covers the pending-buffer compaction
// threshold.
func TestResponsesSSECompactPendingIfLarge(t *testing.T) {
	t.Run("pendingStart exceeds threshold compacted", func(t *testing.T) {
		content := []byte("hello world")
		threshold := responsesSSECompactThreshold // 4096
		f := &responsesSSEFramer{
			pending:      make([]byte, threshold+100+len(content)),
			pendingStart: threshold + 100,
		}
		copy(f.pending[f.pendingStart:], content)
		f.compactPendingIfLarge()
		assert.Equal(t, 0, f.pendingStart)
		assert.Equal(t, content, f.pending)
	})

	t.Run("pendingStart below threshold not compacted", func(t *testing.T) {
		content := []byte("hello")
		f := &responsesSSEFramer{
			pending:      make([]byte, 100+len(content)),
			pendingStart: 100,
		}
		copy(f.pending[100:], content)
		f.compactPendingIfLarge()
		assert.Equal(t, 100, f.pendingStart)
		assert.Equal(t, content, f.pending[100:])
	})

	t.Run("pendingStart zero not compacted", func(t *testing.T) {
		content := []byte("hello")
		f := &responsesSSEFramer{
			pending:      content,
			pendingStart: 0,
		}
		f.compactPendingIfLarge()
		assert.Equal(t, 0, f.pendingStart)
		assert.Equal(t, content, f.pending)
	})
}

// TestResponsesSSENeedsLineBreak covers the inter-chunk line-break insertion
// rule: a break is needed when the previous pending does not end with a newline
// and the incoming chunk begins a new SSE field.
func TestResponsesSSENeedsLineBreak(t *testing.T) {
	t.Run("pending ends with LF false", func(t *testing.T) {
		assert.False(t, responsesSSENeedsLineBreak([]byte("abc\n"), []byte("data: x")))
	})

	t.Run("pending ends with CR false", func(t *testing.T) {
		assert.False(t, responsesSSENeedsLineBreak([]byte("abc\r"), []byte("data: x")))
	})

	t.Run("chunk starts with LF false", func(t *testing.T) {
		assert.False(t, responsesSSENeedsLineBreak([]byte("abc"), []byte("\ndata: x")))
	})

	t.Run("chunk starts with CR false", func(t *testing.T) {
		assert.False(t, responsesSSENeedsLineBreak([]byte("abc"), []byte("\rdata: x")))
	})

	t.Run("chunk starts with data true", func(t *testing.T) {
		assert.True(t, responsesSSENeedsLineBreak([]byte("abc"), []byte("data: x")))
	})

	t.Run("chunk starts with event true", func(t *testing.T) {
		assert.True(t, responsesSSENeedsLineBreak([]byte("abc"), []byte("event: x")))
	})

	t.Run("chunk starts with id true", func(t *testing.T) {
		assert.True(t, responsesSSENeedsLineBreak([]byte("abc"), []byte("id: x")))
	})

	t.Run("chunk starts with retry true", func(t *testing.T) {
		assert.True(t, responsesSSENeedsLineBreak([]byte("abc"), []byte("retry: x")))
	})

	t.Run("chunk starts with comment true", func(t *testing.T) {
		assert.True(t, responsesSSENeedsLineBreak([]byte("abc"), []byte(": comment")))
	})

	t.Run("pending empty false", func(t *testing.T) {
		assert.False(t, responsesSSENeedsLineBreak([]byte{}, []byte("data: x")))
	})

	t.Run("chunk empty false", func(t *testing.T) {
		assert.False(t, responsesSSENeedsLineBreak([]byte("abc"), []byte{}))
	})

	t.Run("chunk only whitespace false", func(t *testing.T) {
		assert.False(t, responsesSSENeedsLineBreak([]byte("abc"), []byte("   \t")))
	})

	t.Run("chunk non SSE prefix false", func(t *testing.T) {
		assert.False(t, responsesSSENeedsLineBreak([]byte("abc"), []byte("foobar")))
	})

	t.Run("chunk with leading whitespace then data true", func(t *testing.T) {
		assert.True(t, responsesSSENeedsLineBreak([]byte("abc"), []byte("  data: x")))
	})
}
