package common

import (
	"bytes"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// PayloadTransform transforms an SSE event's type and data payload.
// It returns the new event type, new data, and an optional error.
type PayloadTransform func(eventType string, data []byte) (newEventType string, newData []byte, err error)

// TransformCondition determines whether a transform rule should be applied.
type TransformCondition func(eventType string, data []byte) bool

// SSETransformRule describes a single SSE event transformation rule.
type SSETransformRule struct {
	// SourceEvent is the event type this rule matches. Empty matches all events.
	SourceEvent string
	// TargetEvent is the new event type. Empty means keep the original.
	TargetEvent string
	// PayloadTransform is an optional function to transform the data payload.
	PayloadTransform PayloadTransform
	// Condition is an optional predicate; the rule is only applied when it returns true.
	Condition TransformCondition
}

// SSETransformEngine applies a set of SSETransformRules to SSE chunks.
type SSETransformEngine struct {
	wildcardRules []SSETransformRule
	rulesByEvent  map[string][]SSETransformRule
}

// NewSSETransformEngine creates a new transform engine with the given rules.
func NewSSETransformEngine(rules ...SSETransformRule) *SSETransformEngine {
	e := &SSETransformEngine{
		rulesByEvent: make(map[string][]SSETransformRule),
	}
	for _, r := range rules {
		e.AddRule(r)
	}
	return e
}

// AddRule adds a transform rule to the engine.
func (e *SSETransformEngine) AddRule(rule SSETransformRule) {
	if rule.SourceEvent == "" {
		e.wildcardRules = append(e.wildcardRules, rule)
	} else {
		e.rulesByEvent[rule.SourceEvent] = append(e.rulesByEvent[rule.SourceEvent], rule)
	}
}

// RemoveRule removes all rules matching a source event type.
func (e *SSETransformEngine) RemoveRule(sourceEvent string) {
	if sourceEvent == "" {
		e.wildcardRules = nil
	} else {
		delete(e.rulesByEvent, sourceEvent)
	}
}

// Transform applies matching rules to an SSE chunk and returns the transformed frames.
func (e *SSETransformEngine) Transform(chunk []byte) [][]byte {
	events := parseSSEEvents(chunk)
	var out [][]byte
	for _, ev := range events {
		eventType := ev.Type
		data := ev.Data

		// Apply event-specific rules, then wildcard rules.
		rules := e.rulesByEvent[eventType]
		rules = append(rules, e.wildcardRules...)

		for _, rule := range rules {
			if rule.Condition != nil && !rule.Condition(eventType, data) {
				continue
			}
			if rule.TargetEvent != "" {
				eventType = rule.TargetEvent
			}
			if rule.PayloadTransform != nil {
				newType, newData, err := rule.PayloadTransform(eventType, data)
				if err != nil {
					continue // skip this rule on error
				}
				eventType = newType
				data = newData
			}
		}

		out = append(out, AppendSSEEventBytes(nil, eventType, data, SSEStandardTrailingNewlines))
	}
	return out
}

// parseSSEEvents is a minimal SSE parser for the transform engine.
// It splits on blank lines and extracts event:/data: pairs.
func parseSSEEvents(chunk []byte) []struct {
	Type string
	Data []byte
} {
	type sseEvent struct {
		Type string
		Data []byte
	}
	var events []sseEvent
	var currentType string
	var currentData []byte
	hasData := false

	flush := func() {
		if currentType == "" {
			currentType = ""
			currentData = nil
			hasData = false
			return
		}
		data := currentData
		if !hasData {
			data = nil
		}
		dataCopy := make([]byte, len(data))
		copy(dataCopy, data)
		events = append(events, sseEvent{Type: currentType, Data: dataCopy})
		currentType = ""
		currentData = nil
		hasData = false
	}

	lines := splitSSELines(chunk)
	for _, line := range lines {
		trimmed := bytes.Trim(line, " \t")
		if len(trimmed) == 0 {
			flush()
			continue
		}
		if et, ok := sseEventType(line); ok {
			if currentType != "" {
				flush()
			}
			currentType = et
			continue
		}
		if data, ok := extractDataLine(line); ok {
			if currentType == "" {
				continue
			}
			if hasData {
				currentData = append(currentData, '\n')
			}
			currentData = append(currentData, data...)
			hasData = true
			continue
		}
	}
	if currentType != "" {
		flush()
	}

	// Convert to anonymous struct
	result := make([]struct {
		Type string
		Data []byte
	}, len(events))
	for i, ev := range events {
		result[i].Type = ev.Type
		result[i].Data = ev.Data
	}
	return result
}

// --- Pre-built rule factories ---

// NewEventTypeMappingRule creates a rule that renames an event type.
func NewEventTypeMappingRule(from, to string) SSETransformRule {
	return SSETransformRule{
		SourceEvent: from,
		TargetEvent: to,
	}
}

// NewFieldRewriteRule creates a rule that rewrites a specific JSON field.
func NewFieldRewriteRule(eventType, jsonPath string, rewriteFunc func(gjson.Result) (interface{}, error)) SSETransformRule {
	return SSETransformRule{
		SourceEvent: eventType,
		PayloadTransform: func(et string, data []byte) (string, []byte, error) {
			val := gjson.GetBytes(data, jsonPath)
			if !val.Exists() {
				return et, data, nil
			}
			newVal, err := rewriteFunc(val)
			if err != nil {
				return et, data, err
			}
			newJSON, err := sjson.SetBytes(data, jsonPath, newVal)
			if err != nil {
				return et, data, err
			}
			return et, newJSON, nil
		},
	}
}

// NewPayloadFilterRule creates a rule that keeps only specified fields in the payload.
func NewPayloadFilterRule(eventType string, keepFields []string) SSETransformRule {
	return SSETransformRule{
		SourceEvent: eventType,
		PayloadTransform: func(et string, data []byte) (string, []byte, error) {
			result := gjson.ParseBytes(data)
			if !result.IsObject() {
				return et, data, nil
			}
			filtered := make(map[string]interface{})
			for _, field := range keepFields {
				v := result.Get(field)
				if v.Exists() {
					filtered[field] = v.Value()
				}
			}
			newJSON, err := sjson.SetBytes(nil, "", filtered)
			if err != nil {
				return et, data, err
			}
			return et, newJSON, nil
		},
	}
}

// --- Internal SSE parsing helpers ---

// splitSSELines splits SSE bytes by '\n' and trims trailing '\r' from each line.
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

// sseEventType extracts the event type from an SSE "event:" line.
func sseEventType(line []byte) (eventType string, ok bool) {
	rest, found := bytes.CutPrefix(line, []byte("event:"))
	if !found {
		return "", false
	}
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t') {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return "", false
	}
	return string(rest), true
}

// extractDataLine extracts the data payload from an SSE "data:" line.
func extractDataLine(line []byte) ([]byte, bool) {
	rest, found := bytes.CutPrefix(line, []byte("data:"))
	if !found {
		return nil, false
	}
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t') {
		rest = rest[1:]
	}
	return rest, true
}
