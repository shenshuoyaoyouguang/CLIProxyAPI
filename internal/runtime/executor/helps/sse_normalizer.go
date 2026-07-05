package helps

import (
	"bytes"
	"strconv"

	translatorcommon "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/common"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// sseEventType extracts the event type from an SSE "event:" line.
// It tolerates any number of spaces and tabs after the "event:" prefix
// (e.g. "event: message_start", "event:  message_start", "event:\t message_start").
// Trailing carriage returns and newlines are trimmed first. For non-"event:"
// lines (e.g. "data:", ":" comment, blank line) it returns ("", false).
func sseEventType(line []byte) (eventType string, ok bool) {
	// Trim trailing CR/LF first so CRLF line endings don't pollute the value.
	line = bytes.TrimRight(line, "\r\n")
	rest, found := bytes.CutPrefix(line, []byte("event:"))
	if !found {
		return "", false
	}
	// Skip any number of spaces and tabs after "event:".
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t') {
		rest = rest[1:]
	}
	// If nothing remains after stripping whitespace, treat it as a malformed
	// event line rather than an empty event type.
	if len(rest) == 0 {
		return "", false
	}
	return string(rest), true
}

// splitSSELines splits SSE bytes by '\n' and trims trailing '\r' and '\n'
// from each line. This is the canonical helper for fixing the "duplicate
// newline injection" problem where bytes.Split(..., "\n") leaves a trailing
// '\r' on CRLF-terminated lines.
func splitSSELines(data []byte) [][]byte {
	if len(data) == 0 {
		return nil
	}
	lines := bytes.Split(data, []byte("\n"))
	out := make([][]byte, len(lines))
	for i, line := range lines {
		out[i] = bytes.TrimRight(line, "\r\n")
	}
	return out
}

// sseEvent represents a single parsed SSE event with its type and data payload.
type sseEvent struct {
	Type string
	Data []byte
}

// parseSSEEvents parses a chunk of SSE bytes into a list of events. Each event
// is composed of an "event:" line followed by a "data:" line. Blank lines
// separate events. Lines that are not part of a recognised event/data pair
// (e.g. ":" comments or stray data lines without an event) are skipped.
// The returned Data slices reference copies of the input to avoid aliasing
// surprises when callers buffer events across chunks.
func parseSSEEvents(chunk []byte) []sseEvent {
	lines := splitSSELines(chunk)
	events := make([]sseEvent, 0, len(lines)/2+1)
	currentType := ""
	currentData := []byte(nil)
	hasData := false
	flush := func() {
		if currentType == "" {
			// Reset without emitting an event when there is no event type.
			currentType = ""
			currentData = nil
			hasData = false
			return
		}
		data := currentData
		if !hasData {
			data = nil
		}
		// Copy to avoid aliasing input buffers held by callers.
		dataCopy := make([]byte, len(data))
		copy(dataCopy, data)
		events = append(events, sseEvent{Type: currentType, Data: dataCopy})
		currentType = ""
		currentData = nil
		hasData = false
	}
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			// Blank line terminates the current event.
			flush()
			continue
		}
		if et, ok := sseEventType(line); ok {
			// If a previous event was accumulating without a blank separator,
			// flush it first to preserve event boundaries.
			if currentType != "" {
				flush()
			}
			if !translatorcommon.IsKnownSSEEventType(et) {
				log.Debugf("SSENormalizer: unknown event type in parseSSEEvents: %s", et)
			}
			currentType = et
			continue
		}
		if data, ok := extractDataLine(line); ok {
			if currentType == "" {
				// data line without a preceding event line; ignore.
				continue
			}
			// Append data lines (W3C SSE spec: multiple data: lines are
			// concatenated with U+000A LINE FEED between them).
			//
			// NOTE: When multiple data: lines produce a payload containing
			// embedded '\n', downstream gjson.GetBytes calls (e.g. blockIndex,
			// handleMessageDelta) only parse the first JSON object. Fields
			// present only in subsequent objects are invisible to the normalizer's
			// internal logic. This is acceptable because (a) the Anthropic SSE
			// protocol never uses multi-line data, and (b) the full concatenated
			// payload is preserved and correctly re-serialized by
			// WriteSSEEventBytes which splits on '\n' into multiple data: lines.
			if hasData {
				currentData = append(currentData, '\n')
			}
			currentData = append(currentData, data...)
			hasData = true
			continue
		}
		// Comment (": keep-alive") or any other line: ignore.
	}
	// Flush a trailing event if the chunk did not end with a blank line.
	if currentType != "" {
		flush()
	}
	return events
}

// extractDataLine extracts the JSON payload from an SSE "data:" line. It
// accepts both "data: " (single space) and "data:" (no space) prefixes and
// tolerates extra leading spaces/tabs after the colon.
func extractDataLine(line []byte) ([]byte, bool) {
	rest, found := bytes.CutPrefix(line, []byte("data:"))
	if !found {
		return nil, false
	}
	// Skip a single optional space first, then any further spaces/tabs so we
	// mirror the tolerance of sseEventType.
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t') {
		rest = rest[1:]
	}
	return rest, true
}

// SSENormalizer is a stateful streaming SSE event-order normalizer. It ensures
// output conforms to the Anthropic SSE protocol order:
//
//	message_start -> content_block_start -> content_block_delta ->
//	content_block_stop -> message_delta -> message_stop
//
// Events that arrive early are buffered until the required predecessor has
// been emitted. Events that arrive after message_stop are dropped. Missing
// terminal events are emitted by Flush.
type SSENormalizer struct {
	messageStartSent  bool
	activeBlocks      map[int64]bool
	messageDeltaSent  bool
	messageStopSent   bool
	finishReason      string
	usageInputTokens  int64
	usageOutputTokens int64
	pending           []sseEvent
}

// NewSSENormalizer creates a new normalizer with empty state.
func NewSSENormalizer() *SSENormalizer {
	return &SSENormalizer{
		activeBlocks: make(map[int64]bool),
	}
}

// Process processes a single SSE chunk (which may contain multiple events)
// and returns a single concatenated frame ready to be forwarded to the client.
// The returned slice is caller-owned; each embedded SSE event frame ends with
// two newlines. Returns nil when the chunk is empty or when nothing is
// eligible to be emitted yet.
func (n *SSENormalizer) Process(chunk []byte) []byte {
	if len(chunk) == 0 {
		return nil
	}
	var buf bytes.Buffer
	buf.Grow(len(chunk) + 128)
	events := parseSSEEvents(chunk)
	for _, ev := range events {
		n.processEvent(ev, &buf)
	}
	// After processing a new chunk, try to release any previously buffered
	// events that are now ready.
	n.releaseReady(&buf)
	if buf.Len() == 0 {
		return nil
	}
	return buf.Bytes()
}

// processEvent handles a single event according to its type and the current
// normalizer state. It mutates state and appends emitted frames into buf.
func (n *SSENormalizer) processEvent(ev sseEvent, buf *bytes.Buffer) {
	switch ev.Type {
	case translatorcommon.SSEEventMessageStart:
		n.handleMessageStart(ev, buf)
	case translatorcommon.SSEEventContentBlockStart:
		n.handleContentBlockStart(ev, buf)
	case translatorcommon.SSEEventContentBlockDelta:
		n.handleContentBlockDelta(ev, buf)
	case translatorcommon.SSEEventContentBlockStop:
		n.handleContentBlockStop(ev, buf)
	case translatorcommon.SSEEventMessageDelta:
		n.handleMessageDelta(ev, buf)
	case translatorcommon.SSEEventMessageStop:
		n.handleMessageStop(ev, buf)
	case translatorcommon.SSEEventPing, translatorcommon.SSEEventError:
		// Pass through control events untouched.
		translatorcommon.WriteSSEEventBytes(buf, ev.Type, ev.Data, 2)
	case translatorcommon.SSEEventHeartbeat, translatorcommon.SSEEventDataUpdate:
		// Extension events pass through untouched.
		translatorcommon.WriteSSEEventBytes(buf, ev.Type, ev.Data, 2)
	case translatorcommon.SSEEventErrorNotification:
		// Error notification events pass through but are logged.
		log.Debugf("SSENormalizer: error notification event: %s", ev.Data)
		translatorcommon.WriteSSEEventBytes(buf, ev.Type, ev.Data, 2)
	default:
		// Unknown event types are forwarded as-is to avoid dropping data
		// from upstream providers that emit non-Anthropic events.
		translatorcommon.WriteSSEEventBytes(buf, ev.Type, ev.Data, 2)
	}
}

func (n *SSENormalizer) handleMessageStart(ev sseEvent, buf *bytes.Buffer) {
	if n.messageStartSent {
		// Duplicate message_start; drop to keep output idempotent.
		return
	}
	n.messageStartSent = true
	translatorcommon.WriteSSEEventBytes(buf, ev.Type, ev.Data, 2)
}

func (n *SSENormalizer) handleContentBlockStart(ev sseEvent, buf *bytes.Buffer) {
	if n.messageStopSent {
		// Late arrival after message_stop: drop.
		return
	}
	if !n.messageStartSent {
		// Buffer until message_start has been sent.
		n.pending = append(n.pending, ev)
		return
	}
	idx := blockIndex(ev.Data)
	n.activeBlocks[idx] = true
	translatorcommon.WriteSSEEventBytes(buf, ev.Type, ev.Data, 2)
}

func (n *SSENormalizer) handleContentBlockDelta(ev sseEvent, buf *bytes.Buffer) {
	if n.messageStopSent {
		return
	}
	idx := blockIndex(ev.Data)
	if !n.activeBlocks[idx] {
		// Corresponding block has not started yet; buffer.
		n.pending = append(n.pending, ev)
		return
	}
	translatorcommon.WriteSSEEventBytes(buf, ev.Type, ev.Data, 2)
}

func (n *SSENormalizer) handleContentBlockStop(ev sseEvent, buf *bytes.Buffer) {
	if n.messageStopSent {
		return
	}
	idx := blockIndex(ev.Data)
	if !n.activeBlocks[idx] {
		// Block never started; drop the stop to avoid emitting an orphan.
		return
	}
	delete(n.activeBlocks, idx)
	translatorcommon.WriteSSEEventBytes(buf, ev.Type, ev.Data, 2)
}

func (n *SSENormalizer) handleMessageDelta(ev sseEvent, buf *bytes.Buffer) {
	if n.messageStopSent {
		return
	}
	// Always update finish_reason and usage from the delta payload so the
	// final synthesized message_delta (if any) carries the latest values.
	if reason := gjson.GetBytes(ev.Data, "delta.stop_reason"); reason.Exists() {
		n.finishReason = reason.String()
	}
	if usage := gjson.GetBytes(ev.Data, "usage"); usage.Exists() {
		if v := usage.Get("input_tokens"); v.Exists() {
			n.usageInputTokens = v.Int()
		}
		if v := usage.Get("output_tokens"); v.Exists() {
			n.usageOutputTokens = v.Int()
		}
	}
	if n.messageDeltaSent {
		// A message_delta has already been emitted; suppress duplicates so
		// the client observes exactly one message_delta per stream. Internal
		// state (finishReason/usage) is still updated above.
		return
	}
	// Buffer the message_delta until every active content_block has been
	// stopped. Anthropic protocol requires message_delta to appear after
	// all content_block_stop events.
	if len(n.activeBlocks) > 0 {
		n.pending = append(n.pending, ev)
		return
	}
	n.messageDeltaSent = true
	translatorcommon.WriteSSEEventBytes(buf, ev.Type, ev.Data, 2)
}

func (n *SSENormalizer) handleMessageStop(ev sseEvent, buf *bytes.Buffer) {
	if n.messageStopSent {
		return
	}
	// If message_delta hasn't been emitted yet (e.g. it's buffered in
	// pending because activeBlocks was non-empty), buffer message_stop too
	// so releaseReady can enforce protocol order: message_delta before
	// message_stop.
	if !n.messageDeltaSent {
		n.pending = append(n.pending, ev)
		return
	}
	n.messageStopSent = true
	translatorcommon.WriteSSEEventBytes(buf, ev.Type, ev.Data, 2)
}

// releaseReady emits any buffered events that are now eligible to be sent
// into buf. It iterates until no further progress is made.
func (n *SSENormalizer) releaseReady(buf *bytes.Buffer) {
	for {
		progress := false
		kept := n.pending[:0]
		for _, ev := range n.pending {
			if n.messageStopSent {
				// Stream is terminating; drop all remaining content_block_*.
				if isContentBlockEvent(ev.Type) {
					continue
				}
				if ev.Type == translatorcommon.SSEEventMessageDelta {
					n.messageDeltaSent = true
				}
				translatorcommon.WriteSSEEventBytes(buf, ev.Type, ev.Data, 2)
				continue
			}
			if !n.messageStartSent {
				// Still waiting for message_start; keep buffering.
				kept = append(kept, ev)
				continue
			}
			switch ev.Type {
			case translatorcommon.SSEEventContentBlockStart:
				idx := blockIndex(ev.Data)
				n.activeBlocks[idx] = true
				translatorcommon.WriteSSEEventBytes(buf, ev.Type, ev.Data, 2)
				progress = true
			case translatorcommon.SSEEventContentBlockDelta:
				idx := blockIndex(ev.Data)
				if n.activeBlocks[idx] {
					translatorcommon.WriteSSEEventBytes(buf, ev.Type, ev.Data, 2)
					progress = true
				} else {
					kept = append(kept, ev)
				}
			case translatorcommon.SSEEventContentBlockStop:
				idx := blockIndex(ev.Data)
				if n.activeBlocks[idx] {
					delete(n.activeBlocks, idx)
					translatorcommon.WriteSSEEventBytes(buf, ev.Type, ev.Data, 2)
					progress = true
				} else {
					kept = append(kept, ev)
				}
			case translatorcommon.SSEEventMessageDelta:
				// Only release message_delta after every active content_block
				// has been stopped (protocol ordering requirement).
				if len(n.activeBlocks) == 0 {
					n.messageDeltaSent = true
					translatorcommon.WriteSSEEventBytes(buf, ev.Type, ev.Data, 2)
					progress = true
				} else {
					kept = append(kept, ev)
				}
			case translatorcommon.SSEEventMessageStop:
				// Only release message_stop after message_delta has been
				// emitted (protocol ordering requirement).
				if n.messageDeltaSent {
					n.messageStopSent = true
					translatorcommon.WriteSSEEventBytes(buf, ev.Type, ev.Data, 2)
					progress = true
				} else {
					kept = append(kept, ev)
				}
			default:
				translatorcommon.WriteSSEEventBytes(buf, ev.Type, ev.Data, 2)
				progress = true
			}
		}
		n.pending = kept
		if !progress {
			break
		}
	}
}

// Flush is called when the stream ends. It emits any missing terminal events:
//   - content_block_stop for every still-active block
//   - message_delta if not already sent (using finishReason or "stop")
//   - message_stop if not already sent
//
// The returned slice is a single concatenated frame that is caller-owned.
// Returns nil when there is nothing to emit.
func (n *SSENormalizer) Flush() []byte {
	var buf bytes.Buffer
	buf.Grow(256)

	// First, try to release any buffered content events that became eligible.
	n.releaseReady(&buf)

	// Emit content_block_stop for every active block (sorted by index for
	// deterministic output).
	for _, idx := range sortedActiveBlockIndexes(n.activeBlocks) {
		payload := []byte(`{"type":"content_block_stop","index":` + strconv.FormatInt(idx, 10) + `}`)
		translatorcommon.WriteSSEEventBytes(&buf, translatorcommon.SSEEventContentBlockStop, payload, 2)
	}
	n.activeBlocks = map[int64]bool{}

	if !n.messageDeltaSent {
		reason := n.finishReason
		if reason == "" {
			// "stop" is not a valid Anthropic stop_reason; use "end_turn" as
			// the canonical default when the upstream never supplied one.
			reason = "end_turn"
		}
		payload := buildMessageDeltaPayload(reason, n.usageInputTokens, n.usageOutputTokens)
		translatorcommon.WriteSSEEventBytes(&buf, translatorcommon.SSEEventMessageDelta, payload, 2)
		n.messageDeltaSent = true
	}

	if !n.messageStopSent {
		translatorcommon.WriteSSEEventBytes(&buf, translatorcommon.SSEEventMessageStop, []byte(`{"type":"message_stop"}`), 2)
		n.messageStopSent = true
	}

	if buf.Len() == 0 {
		return nil
	}
	return buf.Bytes()
}

// blockIndex extracts the "index" field from a content_block_* event payload.
// Returns 0 when the field is absent (default block index).
func blockIndex(data []byte) int64 {
	v := gjson.GetBytes(data, "index")
	if !v.Exists() {
		return 0
	}
	return v.Int()
}

// isContentBlockEvent reports whether the event type is one of the
// content_block_* family.
func isContentBlockEvent(t string) bool {
	switch t {
	case translatorcommon.SSEEventContentBlockStart, translatorcommon.SSEEventContentBlockDelta, translatorcommon.SSEEventContentBlockStop:
		return true
	}
	return false
}

// sortedActiveBlockIndexes returns the keys of the active blocks map sorted in
// ascending order so Flush output is deterministic.
func sortedActiveBlockIndexes(m map[int64]bool) []int64 {
	indexes := make([]int64, 0, len(m))
	for idx := range m {
		indexes = append(indexes, idx)
	}
	// Simple insertion sort: the number of active blocks is small.
	for i := 1; i < len(indexes); i++ {
		for j := i; j > 0 && indexes[j-1] > indexes[j]; j-- {
			indexes[j-1], indexes[j] = indexes[j], indexes[j-1]
		}
	}
	return indexes
}

// buildMessageDeltaPayload constructs the JSON payload for a synthesized
// message_delta event.
func buildMessageDeltaPayload(reason string, inputTokens, outputTokens int64) []byte {
	out := make([]byte, 0, 96)
	out = append(out, `{"type":"message_delta","delta":{"stop_reason":"`...)
	out = append(out, reason...)
	out = append(out, `","stop_sequence":null},"usage":{"input_tokens":`...)
	out = strconv.AppendInt(out, inputTokens, 10)
	out = append(out, `,"output_tokens":`...)
	out = strconv.AppendInt(out, outputTokens, 10)
	out = append(out, `}}`...)
	if log.IsLevelEnabled(log.DebugLevel) {
		log.Debugf("SSENormalizer: synthesizing message_delta reason=%s input=%d output=%d", reason, inputTokens, outputTokens)
	}
	return out
}
