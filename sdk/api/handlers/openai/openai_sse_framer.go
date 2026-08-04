package openai

import (
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// writeResponsesSSEChunk writes an SSE frame to w, appending a blank-line
// delimiter when the frame does not already end with one.
func writeResponsesSSEChunk(w io.Writer, chunk []byte) {
	if w == nil || len(chunk) == 0 {
		return
	}
	if _, err := w.Write(chunk); err != nil {
		return
	}
	if bytes.HasSuffix(chunk, []byte("\n\n")) || bytes.HasSuffix(chunk, []byte("\r\n\r\n")) {
		return
	}
	suffix := []byte("\n\n")
	if bytes.HasSuffix(chunk, []byte("\r\n")) {
		suffix = []byte("\r\n")
	} else if bytes.HasSuffix(chunk, []byte("\n")) {
		suffix = []byte("\n")
	}
	if _, err := w.Write(suffix); err != nil {
		return
	}
}

// responsesSSEFramer is the single shared SSE frame reassembler for the openai
// handlers. It owns frame-boundary detection, emit semantics, and the responses
// output_item.done -> response.completed repair, and is used by both the
// responses streaming path and the images stream accumulator.
type responsesSSEFramer struct {
	pending              []byte
	pendingStart         int
	outputItems          map[int][]byte
	outputOrder          []int
	unindexedOutputItems [][]byte
	// terminalSeen records whether a terminal Responses lifecycle event
	// (response.completed/failed/incomplete) has been emitted, so callers can
	// avoid appending a second terminal event after the stream already ended.
	terminalSeen bool
	// responseID and responseCreatedAt capture the response identity from the
	// first response.created frame, so a synthesized response.failed terminal
	// event can carry the same id/created_at the consumer already saw.
	responseID        string
	responseCreatedAt int64
	// lastSequenceNumber tracks the largest sequence_number seen so far, so a
	// synthesized response.failed event can continue the stream's numbering.
	lastSequenceNumber    int
	hasLastSequenceNumber bool
}

// responsesSSECompactThreshold is the number of already-emitted bytes that may
// accumulate in front of the pending buffer before it is compacted back to the
// start. Delaying compaction keeps frame extraction O(1) per frame instead of
// shifting the whole buffer on every emitted frame.
const responsesSSECompactThreshold = 4096

// WriteChunk reassembles chunk and writes every complete frame to w.
func (f *responsesSSEFramer) WriteChunk(w io.Writer, chunk []byte) {
	f.emitChunk(chunk, func(frame []byte) {
		writeResponsesSSEChunk(w, frame)
	})
}

// Flush writes any remaining recoverable frames to w, dropping a trailing
// partial frame that cannot be emitted without a delimiter.
func (f *responsesSSEFramer) Flush(w io.Writer) {
	f.emitFlush(func(frame []byte) {
		writeResponsesSSEChunk(w, frame)
	})
}

// emitChunk appends chunk and calls emit for every complete frame plus any
// trailing frame that can be emitted without an explicit blank-line delimiter.
// Frames are views into the pending buffer and stay valid until the next
// append or compaction, so callers must consume each frame before the next
// WriteChunk/AddChunk.
func (f *responsesSSEFramer) emitChunk(chunk []byte, emit func(frame []byte)) {
	if len(chunk) == 0 {
		return
	}
	f.compactPendingIfLarge()
	if responsesSSENeedsLineBreak(f.pendingSlice(), chunk) {
		f.pending = append(f.pending, '\n')
	}
	f.pending = append(f.pending, chunk...)
	f.emitCompleteFrames(emit)
	if len(bytes.TrimSpace(f.pendingSlice())) == 0 {
		f.resetPending()
		return
	}
	if len(f.pendingSlice()) == 0 || !responsesSSECanEmitWithoutDelimiter(f.pendingSlice()) {
		return
	}
	emit(f.repairFrame(f.pendingSlice()))
	f.resetPending()
}

// emitFlush calls emit for every recoverable remaining frame, dropping a
// trailing partial frame that cannot be emitted without a delimiter.
func (f *responsesSSEFramer) emitFlush(emit func(frame []byte)) {
	if len(f.pendingSlice()) == 0 {
		return
	}
	f.emitCompleteFrames(emit)
	if len(bytes.TrimSpace(f.pendingSlice())) == 0 {
		f.resetPending()
		return
	}
	if responsesSSECanEmitWithoutDelimiter(f.pendingSlice()) {
		emit(f.repairFrame(f.pendingSlice()))
	}
	f.resetPending()
}

// pendingSlice returns the portion of the pending buffer that has not yet been
// emitted as frames.
func (f *responsesSSEFramer) pendingSlice() []byte {
	return f.pending[f.pendingStart:]
}

// compactPendingIfLarge drops already-emitted bytes from the front of the
// pending buffer once they exceed responsesSSECompactThreshold, so appended
// chunks do not accumulate behind consumed frames.
func (f *responsesSSEFramer) compactPendingIfLarge() {
	if f.pendingStart == 0 || f.pendingStart <= responsesSSECompactThreshold {
		return
	}
	n := copy(f.pending, f.pending[f.pendingStart:])
	f.pending = f.pending[:n]
	f.pendingStart = 0
}

// resetPending drops the entire pending buffer.
func (f *responsesSSEFramer) resetPending() {
	f.pending = f.pending[:0]
	f.pendingStart = 0
}

// emitCompleteFrames drains every complete frame from pending, calling emit
// (with the responses repair applied) for each. Frames are tracked by a
// growing start offset instead of shifting the buffer, so each frame costs
// O(1) regardless of how many frames a chunk holds.
func (f *responsesSSEFramer) emitCompleteFrames(emit func(frame []byte)) {
	for {
		pending := f.pending[f.pendingStart:]
		frameLen := responsesSSEFrameLen(pending)
		if frameLen == 0 {
			break
		}
		emit(f.repairFrame(pending[:frameLen]))
		f.pendingStart += frameLen
	}
}

func (f *responsesSSEFramer) repairFrame(frame []byte) []byte {
	payload, ok := responsesSSEDataPayload(frame)
	if !ok || len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) || !json.Valid(payload) {
		return frame
	}

	eventType := gjson.GetBytes(payload, "type").String()
	f.recordSequenceNumber(payload)

	switch eventType {
	case "response.output_item.done":
		f.recordOutputItem(payload)
	case "response.created":
		f.recordResponseIdentity(payload)
	case "response.completed":
		f.terminalSeen = true
		repaired := f.repairCompletedPayload(payload)
		if !bytes.Equal(repaired, payload) {
			return responsesSSEFrameWithData(frame, repaired)
		}
	case "response.failed", "response.incomplete":
		f.terminalSeen = true
	}
	return frame
}

func responsesSSEDataPayload(frame []byte) ([]byte, bool) {
	var payload []byte
	found := false
	for _, line := range bytes.Split(frame, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		trimmed := bytes.TrimSpace(line)
		if !bytes.HasPrefix(trimmed, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(trimmed[len("data:"):])
		if found {
			payload = append(payload, '\n')
		}
		payload = append(payload, data...)
		found = true
	}
	return payload, found
}

func responsesSSEFrameWithData(frame, payload []byte) []byte {
	var out bytes.Buffer
	for _, line := range bytes.Split(frame, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || bytes.HasPrefix(trimmed, []byte("data:")) {
			continue
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	for _, line := range bytes.Split(payload, []byte("\n")) {
		out.WriteString("data: ")
		out.Write(line)
		out.WriteByte('\n')
	}
	out.WriteByte('\n')
	return out.Bytes()
}

// recordSequenceNumber keeps the largest sequence_number seen in the stream.
func (f *responsesSSEFramer) recordSequenceNumber(payload []byte) {
	sequenceNumber := gjson.GetBytes(payload, "sequence_number")
	if !sequenceNumber.Exists() {
		return
	}
	value := int(sequenceNumber.Int())
	if !f.hasLastSequenceNumber || value > f.lastSequenceNumber {
		f.lastSequenceNumber = value
		f.hasLastSequenceNumber = true
	}
}

// recordResponseIdentity captures the response id and created_at from a
// response.created frame, keeping the first non-empty values seen.
func (f *responsesSSEFramer) recordResponseIdentity(payload []byte) {
	if id := strings.TrimSpace(gjson.GetBytes(payload, "response.id").String()); id != "" && f.responseID == "" {
		f.responseID = id
	}
	if createdAt := gjson.GetBytes(payload, "response.created_at").Int(); createdAt > 0 && f.responseCreatedAt == 0 {
		f.responseCreatedAt = createdAt
	}
}

func (f *responsesSSEFramer) recordOutputItem(payload []byte) {
	item := gjson.GetBytes(payload, "item")
	if !item.Exists() || !item.IsObject() || item.Get("type").String() == "" {
		return
	}

	if outputIndex := gjson.GetBytes(payload, "output_index"); outputIndex.Exists() {
		index := int(outputIndex.Int())
		if f.outputItems == nil {
			f.outputItems = make(map[int][]byte)
		}
		if _, exists := f.outputItems[index]; !exists {
			f.outputOrder = append(f.outputOrder, index)
		}
		f.outputItems[index] = append([]byte(nil), item.Raw...)
		return
	}

	f.unindexedOutputItems = append(f.unindexedOutputItems, append([]byte(nil), item.Raw...))
}

func (f *responsesSSEFramer) repairCompletedPayload(payload []byte) []byte {
	if len(f.outputOrder) == 0 && len(f.unindexedOutputItems) == 0 {
		return payload
	}
	output := gjson.GetBytes(payload, "response.output")
	if output.Exists() && (!output.IsArray() || len(output.Array()) > 0) {
		return payload
	}

	var outputJSON bytes.Buffer
	outputJSON.WriteByte('[')
	indexes := append([]int(nil), f.outputOrder...)
	sort.Ints(indexes)
	written := 0
	for _, index := range indexes {
		item, ok := f.outputItems[index]
		if !ok {
			continue
		}
		if written > 0 {
			outputJSON.WriteByte(',')
		}
		outputJSON.Write(item)
		written++
	}
	for _, item := range f.unindexedOutputItems {
		if written > 0 {
			outputJSON.WriteByte(',')
		}
		outputJSON.Write(item)
		written++
	}
	outputJSON.WriteByte(']')

	repaired, err := sjson.SetRawBytes(payload, "response.output", outputJSON.Bytes())
	if err != nil {
		return payload
	}
	return repaired
}

func responsesSSEFrameLen(chunk []byte) int {
	if len(chunk) == 0 {
		return 0
	}
	lf := bytes.Index(chunk, []byte("\n\n"))
	crlf := bytes.Index(chunk, []byte("\r\n\r\n"))
	switch {
	case lf < 0:
		if crlf < 0 {
			return 0
		}
		return crlf + 4
	case crlf < 0:
		return lf + 2
	case lf < crlf:
		return lf + 2
	default:
		return crlf + 4
	}
}

func responsesSSENeedsMoreData(chunk []byte) bool {
	trimmed := bytes.TrimSpace(chunk)
	if len(trimmed) == 0 {
		return false
	}
	return responsesSSEHasField(trimmed, []byte("event:")) && !responsesSSEHasField(trimmed, []byte("data:"))
}

func responsesSSEHasField(chunk []byte, prefix []byte) bool {
	s := chunk
	for len(s) > 0 {
		line := s
		if i := bytes.IndexByte(s, '\n'); i >= 0 {
			line = s[:i]
			s = s[i+1:]
		} else {
			s = nil
		}
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func responsesSSECanEmitWithoutDelimiter(chunk []byte) bool {
	trimmed := bytes.TrimSpace(chunk)
	if len(trimmed) == 0 || responsesSSENeedsMoreData(trimmed) || !responsesSSEHasField(trimmed, []byte("data:")) {
		return false
	}
	return responsesSSEDataLinesValid(trimmed)
}

func responsesSSEDataLinesValid(chunk []byte) bool {
	s := chunk
	for len(s) > 0 {
		line := s
		if i := bytes.IndexByte(s, '\n'); i >= 0 {
			line = s[:i]
			s = s[i+1:]
		} else {
			s = nil
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 || !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(line[len("data:"):])
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		if !json.Valid(data) {
			return false
		}
	}
	return true
}

func responsesSSENeedsLineBreak(pending, chunk []byte) bool {
	if len(pending) == 0 || len(chunk) == 0 {
		return false
	}
	if bytes.HasSuffix(pending, []byte("\n")) || bytes.HasSuffix(pending, []byte("\r")) {
		return false
	}
	if chunk[0] == '\n' || chunk[0] == '\r' {
		return false
	}
	trimmed := bytes.TrimLeft(chunk, " \t")
	if len(trimmed) == 0 {
		return false
	}
	for _, prefix := range [][]byte{[]byte("data:"), []byte("event:"), []byte("id:"), []byte("retry:"), []byte(":")} {
		if bytes.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}
