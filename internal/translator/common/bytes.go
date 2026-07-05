package common

import (
	"bytes"
	"strconv"
	"strings"
)

func GeminiTokenCountJSON(count int64) []byte {
	out := make([]byte, 0, 96)
	out = append(out, `{"totalTokens":`...)
	out = strconv.AppendInt(out, count, 10)
	out = append(out, `,"promptTokensDetails":[{"modality":"TEXT","tokenCount":`...)
	out = strconv.AppendInt(out, count, 10)
	out = append(out, `}]}`...)
	return out
}

func ClaudeInputTokensJSON(count int64) []byte {
	out := make([]byte, 0, 32)
	out = append(out, `{"input_tokens":`...)
	out = strconv.AppendInt(out, count, 10)
	out = append(out, '}')
	return out
}

func SSEEventData(event string, payload []byte) []byte {
	out := make([]byte, 0, len(event)+len(payload)+14)
	out = append(out, "event: "...)
	out = append(out, event...)
	out = append(out, '\n')
	for _, line := range bytes.Split(payload, []byte("\n")) {
		out = append(out, "data: "...)
		out = append(out, line...)
		out = append(out, '\n')
	}
	return out
}

// AppendSSEEventString appends an SSE event frame to out.
// The payload is split on '\n' so each line becomes its own "data: " line
// per the W3C SSE spec. trailingNewlines controls how many blank lines
// terminate the frame after the last "data:" line; typical usage is 2 for
// a standard frame end. Values <= 0 produce no extra blank line beyond
// the one already following the last data line (i.e. no frame termination).
func AppendSSEEventString(out []byte, event, payload string, trailingNewlines int) []byte {
	out = append(out, "event: "...)
	out = append(out, event...)
	out = append(out, '\n')
	for _, line := range strings.Split(payload, "\n") {
		out = append(out, "data: "...)
		out = append(out, line...)
		out = append(out, '\n')
	}
	for i := 1; i < trailingNewlines; i++ {
		out = append(out, '\n')
	}
	return out
}

// AppendSSEEventBytes is the []byte variant of AppendSSEEventString.
func AppendSSEEventBytes(out []byte, event string, payload []byte, trailingNewlines int) []byte {
	out = append(out, "event: "...)
	out = append(out, event...)
	out = append(out, '\n')
	for _, line := range bytes.Split(payload, []byte("\n")) {
		out = append(out, "data: "...)
		out = append(out, line...)
		out = append(out, '\n')
	}
	for i := 1; i < trailingNewlines; i++ {
		out = append(out, '\n')
	}
	return out
}
