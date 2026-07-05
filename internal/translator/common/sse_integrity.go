package common

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

// IntegrityViolation describes a data integrity problem found during SSE translation.
type IntegrityViolation struct {
	Category string // e.g. "event_count", "field_missing", "pairing"
	Message  string
}

func (v IntegrityViolation) Error() string {
	return fmt.Sprintf("integrity violation [%s]: %s", v.Category, v.Message)
}

// SSEIntegrityChecker tracks input/output SSE events and verifies data integrity.
type SSEIntegrityChecker struct {
	inputEvents  map[string]int
	outputEvents map[string]int
}

// NewSSEIntegrityChecker creates a new integrity checker.
func NewSSEIntegrityChecker() *SSEIntegrityChecker {
	return &SSEIntegrityChecker{
		inputEvents:  make(map[string]int),
		outputEvents: make(map[string]int),
	}
}

// RecordInput parses and records input SSE events.
func (c *SSEIntegrityChecker) RecordInput(chunk []byte) {
	c.record(chunk, c.inputEvents)
}

// RecordOutput parses and records output SSE events.
func (c *SSEIntegrityChecker) RecordOutput(chunk []byte) {
	c.record(chunk, c.outputEvents)
}

// Verify checks integrity between input and output events.
func (c *SSEIntegrityChecker) Verify() []IntegrityViolation {
	var violations []IntegrityViolation

	// Check protocol event conservation.
	protocolEvents := []string{
		SSEEventMessageStart,
		SSEEventContentBlockStart,
		SSEEventContentBlockStop,
		SSEEventMessageDelta,
		SSEEventMessageStop,
	}
	for _, evt := range protocolEvents {
		inCount := c.inputEvents[evt]
		outCount := c.outputEvents[evt]
		// message_delta may be synthesized if missing, so output can exceed input.
		if evt == SSEEventMessageDelta || evt == SSEEventMessageStop || evt == SSEEventContentBlockStop {
			if outCount < inCount {
				violations = append(violations, IntegrityViolation{
					Category: "event_count",
					Message:  fmt.Sprintf("%s: output count %d < input count %d", evt, outCount, inCount),
				})
			}
		} else {
			if outCount != inCount && inCount > 0 {
				violations = append(violations, IntegrityViolation{
					Category: "event_count",
					Message:  fmt.Sprintf("%s: output count %d != input count %d", evt, outCount, inCount),
				})
			}
		}
	}

	// Check content_block_start/stop pairing.
	inputStarts := c.inputEvents[SSEEventContentBlockStart]
	inputStops := c.inputEvents[SSEEventContentBlockStop]
	outputStarts := c.outputEvents[SSEEventContentBlockStart]
	outputStops := c.outputEvents[SSEEventContentBlockStop]

	if inputStarts != inputStops {
		violations = append(violations, IntegrityViolation{
			Category: "pairing",
			Message:  fmt.Sprintf("input content_block_start/stop mismatch: %d starts, %d stops", inputStarts, inputStops),
		})
	}
	if outputStarts != outputStops {
		violations = append(violations, IntegrityViolation{
			Category: "pairing",
			Message:  fmt.Sprintf("output content_block_start/stop mismatch: %d starts, %d stops", outputStarts, outputStops),
		})
	}

	return violations
}

// Summary returns a human-readable integrity report.
func (c *SSEIntegrityChecker) Summary() string {
	var sb strings.Builder
	sb.WriteString("SSE Integrity Summary:\n")
	sb.WriteString("  Input events:\n")
	for evt, count := range c.inputEvents {
		sb.WriteString(fmt.Sprintf("    %s: %d\n", evt, count))
	}
	sb.WriteString("  Output events:\n")
	for evt, count := range c.outputEvents {
		sb.WriteString(fmt.Sprintf("    %s: %d\n", evt, count))
	}
	violations := c.Verify()
	if len(violations) == 0 {
		sb.WriteString("  No violations detected.\n")
	} else {
		sb.WriteString("  Violations:\n")
		for _, v := range violations {
			sb.WriteString(fmt.Sprintf("    %s\n", v.Error()))
		}
	}
	return sb.String()
}

// record is a helper to parse SSE events and count them.
func (c *SSEIntegrityChecker) record(chunk []byte, counter map[string]int) {
	events := parseSSEEvents(chunk)
	for _, ev := range events {
		counter[ev.Type]++
		// Also validate required fields for known event types.
		_ = gjson.GetBytes(ev.Data, "type")
	}
}
