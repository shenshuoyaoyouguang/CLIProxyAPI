package common

// SSEEventTypeCategory classifies SSE event types into categories.
type SSEEventTypeCategory int

const (
	// CategoryProtocol indicates a protocol-mandated event (e.g. message_start, message_stop).
	CategoryProtocol SSEEventTypeCategory = iota
	// CategoryControl indicates a control event (e.g. ping, error).
	CategoryControl
	// CategoryExtension indicates an extension event (e.g. heartbeat, data_update).
	CategoryExtension
	// CategoryUnknown indicates an unrecognized event type.
	CategoryUnknown
)

// Claude protocol event types.
const (
	SSEEventMessageStart      = "message_start"
	SSEEventContentBlockStart = "content_block_start"
	SSEEventContentBlockDelta = "content_block_delta"
	SSEEventContentBlockStop  = "content_block_stop"
	SSEEventMessageDelta      = "message_delta"
	SSEEventMessageStop       = "message_stop"
)

// Control event types.
const (
	SSEEventPing  = "ping"
	SSEEventError = "error"
)

// Extension event types for observability and control.
const (
	SSEEventHeartbeat         = "heartbeat"
	SSEEventDataUpdate        = "data_update"
	SSEEventErrorNotification = "error_notification"
)

// SSEStandardTrailingNewlines is the standard number of trailing newlines
// for an SSE event frame per the W3C specification.
const SSEStandardTrailingNewlines = 2

// ClassifySSEEventType returns the category of an SSE event type.
func ClassifySSEEventType(eventType string) SSEEventTypeCategory {
	switch eventType {
	case SSEEventMessageStart, SSEEventContentBlockStart,
		SSEEventContentBlockDelta, SSEEventContentBlockStop,
		SSEEventMessageDelta, SSEEventMessageStop:
		return CategoryProtocol
	case SSEEventPing, SSEEventError:
		return CategoryControl
	case SSEEventHeartbeat, SSEEventDataUpdate, SSEEventErrorNotification:
		return CategoryExtension
	default:
		return CategoryUnknown
	}
}

// IsKnownSSEEventType reports whether the event type is in the registry.
func IsKnownSSEEventType(eventType string) bool {
	return ClassifySSEEventType(eventType) != CategoryUnknown
}

// IsContentBlockEvent reports whether the event type is a content_block_* event.
func IsContentBlockEvent(eventType string) bool {
	return eventType == SSEEventContentBlockStart ||
		eventType == SSEEventContentBlockDelta ||
		eventType == SSEEventContentBlockStop
}

// ClaudeProtocolOrder returns the expected protocol order of Claude SSE events.
func ClaudeProtocolOrder() []string {
	return []string{
		SSEEventMessageStart,
		SSEEventContentBlockStart,
		SSEEventContentBlockDelta,
		SSEEventContentBlockStop,
		SSEEventMessageDelta,
		SSEEventMessageStop,
	}
}
