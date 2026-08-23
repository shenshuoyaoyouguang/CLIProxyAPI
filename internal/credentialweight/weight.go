// Package credentialweight defines shared credential weight validation and parsing.
package credentialweight

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	// Default is used when a credential does not define a weight.
	Default int64 = 1
	// Max bounds scheduler arithmetic while allowing practical proportional routing.
	Max int64 = 1_000_000
)

// Normalize validates and normalizes an explicit weight. Non-positive values are
// valid and normalize to zero, which excludes the credential from weighted routing.
func Normalize(weight int64) (int64, error) {
	if weight <= 0 {
		return 0, nil
	}
	if weight > Max {
		return 0, fmt.Errorf("weight must not exceed %d", Max)
	}
	return weight, nil
}

// ParseString parses a scheduler attribute. An empty value uses the default weight.
func ParseString(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Default, nil
	}
	weight, errParse := strconv.ParseInt(raw, 10, 64)
	if errParse != nil {
		return 0, fmt.Errorf("weight must be an integer: %w", errParse)
	}
	return Normalize(weight)
}

// ParseValue parses a JSON-compatible auth-file metadata value.
func ParseValue(value any) (int64, error) {
	switch typed := value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		// All numeric kinds route through one float64 normalizer. This is exact
		// for weights because Max is far below 2^53, so the narrower integer and
		// float32 kinds (not produced by JSON/YAML decoding) yield the same weight
		// as their int/float64 counterpart.
		f, _ := toFloat64(typed)
		return normalizeWeight(f)
	case json.Number:
		weight, errParse := typed.Int64()
		if errParse != nil {
			return 0, fmt.Errorf("weight must be an integer: %w", errParse)
		}
		return Normalize(weight)
	case string:
		return ParseString(typed)
	default:
		return 0, fmt.Errorf("weight must be an integer")
	}
}

// toFloat64 converts the numeric kinds ParseValue routes through normalizeWeight.
func toFloat64(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	default:
		return 0, false
	}
}

// normalizeWeight validates a numeric weight and enforces the scheduler bounds.
func normalizeWeight(value float64) (int64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value {
		return 0, fmt.Errorf("weight must be an integer")
	}
	if value <= 0 {
		return 0, nil
	}
	if value > float64(Max) {
		return 0, fmt.Errorf("weight must not exceed %d", Max)
	}
	return int64(value), nil
}
