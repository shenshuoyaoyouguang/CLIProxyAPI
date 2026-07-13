package tui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGetString(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		key  string
		want string
	}{
		{"key exists with string value", map[string]any{"foo": "bar"}, "foo", "bar"},
		{"key exists with int value", map[string]any{"foo": 42}, "foo", ""},
		{"key exists with nil value", map[string]any{"foo": nil}, "foo", ""},
		{"key does not exist", map[string]any{"other": "val"}, "foo", ""},
		{"empty map", map[string]any{}, "foo", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getString(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("getString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetFloat(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		key  string
		want float64
	}{
		{"key with float64", map[string]any{"n": 3.14}, "n", 3.14},
		{"key with json.Number", map[string]any{"n": json.Number("2.5")}, "n", 2.5},
		{"key with json.Number integer", map[string]any{"n": json.Number("10")}, "n", 10},
		{"key missing", map[string]any{}, "n", 0},
		{"key with wrong type string", map[string]any{"n": "hello"}, "n", 0},
		{"key with wrong type bool", map[string]any{"n": true}, "n", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getFloat(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("getFloat() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetBool(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		key  string
		want bool
	}{
		{"key with true", map[string]any{"b": true}, "b", true},
		{"key with false", map[string]any{"b": false}, "b", false},
		{"key missing", map[string]any{}, "b", false},
		{"key with non-bool string", map[string]any{"b": "true"}, "b", false},
		{"key with non-bool int", map[string]any{"b": 1}, "b", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getBool(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("getBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBoolEmoji(t *testing.T) {
	SetLocale("en")
	defer func() { currentLocale = "en" }()

	tests := []struct {
		name  string
		input bool
		want  string
	}{
		{"true returns Yes", true, "Yes ✓"},
		{"false returns No", false, "No"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := boolEmoji(tt.input)
			if got != tt.want {
				t.Errorf("boolEmoji(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatLargeNumber(t *testing.T) {
	tests := []struct {
		name  string
		input int64
		want  string
	}{
		{"zero", 0, "0"},
		{"below 1K", 999, "999"},
		{"exactly 1K", 1000, "1.0K"},
		{"1.5K", 1500, "1.5K"},
		{"just below 1M", 999999, "1000.0K"},
		{"exactly 1M", 1000000, "1.0M"},
		{"1.5M", 1500000, "1.5M"},
		{"large number", 10000000, "10.0M"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatLargeNumber(tt.input)
			if got != tt.want {
				t.Errorf("formatLargeNumber(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{"short string unchanged", "hi", 10, "hi"},
		{"exact length unchanged", "hello", 5, "hello"},
		{"long string truncated", "hello world", 8, "hello..."},
		{"truncate to minimum", "abcdef", 4, "a..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.s, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestMinInt(t *testing.T) {
	tests := []struct {
		name string
		a, b int
		want int
	}{
		{"a less than b", 3, 7, 3},
		{"a greater than b", 9, 2, 2},
		{"a equals b", 5, 5, 5},
		{"negative values", -3, -7, -7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := minInt(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("minInt(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestFormatKV(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantKey string
		wantVal string
	}{
		{"basic key value", "Host", "localhost", "Host:", "localhost"},
		{"empty value", "Port", "", "Port:", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatKV(tt.key, tt.value)
			if !strings.Contains(got, tt.wantKey) {
				t.Errorf("formatKV() output %q does not contain key %q", got, tt.wantKey)
			}
			if !strings.Contains(got, tt.wantVal) {
				t.Errorf("formatKV() output %q does not contain value %q", got, tt.wantVal)
			}
			if !strings.HasPrefix(got, "  ") {
				t.Errorf("formatKV() output %q should start with two spaces", got)
			}
			if !strings.HasSuffix(got, "\n") {
				t.Errorf("formatKV() output %q should end with newline", got)
			}
		})
	}
}
