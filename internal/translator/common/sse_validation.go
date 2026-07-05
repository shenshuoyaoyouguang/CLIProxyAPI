package common

import (
	"bytes"
	"fmt"

	"github.com/tidwall/gjson"
)

// SSEValidationError describes a single validation problem.
type SSEValidationError struct {
	Event   string // event type (empty for frame-level issues)
	Field   string // field or aspect that failed
	Message string // human-readable description
}

func (e SSEValidationError) Error() string {
	if e.Event != "" {
		return fmt.Sprintf("SSE validation error [%s]: %s: %s", e.Event, e.Field, e.Message)
	}
	return fmt.Sprintf("SSE validation error: %s: %s", e.Field, e.Message)
}

// ValidateSSEEvent validates the structure of a single SSE event.
// For known protocol events, it checks required JSON fields.
// For unknown events, it only checks that the data is valid JSON.
func ValidateSSEEvent(eventType string, data []byte) []SSEValidationError {
	var errs []SSEValidationError
	if eventType == "" {
		errs = append(errs, SSEValidationError{Field: "event", Message: "event type is empty"})
		return errs
	}
	if len(data) == 0 {
		errs = append(errs, SSEValidationError{Event: eventType, Field: "data", Message: "data payload is empty"})
		return errs
	}
	// All known events should have valid JSON data.
	if !gjson.ValidBytes(data) {
		errs = append(errs, SSEValidationError{Event: eventType, Field: "data", Message: "data is not valid JSON"})
		return errs
	}
	// Protocol-specific field validation.
	switch eventType {
	case SSEEventMessageStart:
		if !gjson.GetBytes(data, "message.id").Exists() {
			errs = append(errs, SSEValidationError{Event: eventType, Field: "message.id", Message: "missing message.id"})
		}
	case SSEEventContentBlockStart:
		if !gjson.GetBytes(data, "index").Exists() {
			errs = append(errs, SSEValidationError{Event: eventType, Field: "index", Message: "missing index"})
		}
	case SSEEventContentBlockDelta:
		if !gjson.GetBytes(data, "index").Exists() {
			errs = append(errs, SSEValidationError{Event: eventType, Field: "index", Message: "missing index"})
		}
		if !gjson.GetBytes(data, "delta").Exists() {
			errs = append(errs, SSEValidationError{Event: eventType, Field: "delta", Message: "missing delta"})
		}
	case SSEEventContentBlockStop:
		if !gjson.GetBytes(data, "index").Exists() {
			errs = append(errs, SSEValidationError{Event: eventType, Field: "index", Message: "missing index"})
		}
	case SSEEventMessageDelta:
		if !gjson.GetBytes(data, "delta").Exists() {
			errs = append(errs, SSEValidationError{Event: eventType, Field: "delta", Message: "missing delta"})
		}
	case SSEEventError:
		if !gjson.GetBytes(data, "error").Exists() {
			errs = append(errs, SSEValidationError{Event: eventType, Field: "error", Message: "missing error object"})
		}
	}
	return errs
}

// ValidateSSEFrame validates the structure of a raw SSE frame (byte slice).
// It checks for required "event:" and "data:" lines and proper termination.
func ValidateSSEFrame(frame []byte) []SSEValidationError {
	var errs []SSEValidationError
	if len(frame) == 0 {
		errs = append(errs, SSEValidationError{Field: "frame", Message: "frame is empty"})
		return errs
	}
	if !bytes.HasPrefix(frame, []byte("event: ")) {
		errs = append(errs, SSEValidationError{Field: "frame", Message: "frame does not start with 'event: '"})
	}
	if !bytes.Contains(frame, []byte("\ndata: ")) && !bytes.Contains(frame, []byte("\ndata:")) {
		errs = append(errs, SSEValidationError{Field: "frame", Message: "frame missing 'data:' line"})
	}
	if !bytes.HasSuffix(frame, []byte("\n\n")) {
		errs = append(errs, SSEValidationError{Field: "frame", Message: "frame does not end with double newline"})
	}
	return errs
}
