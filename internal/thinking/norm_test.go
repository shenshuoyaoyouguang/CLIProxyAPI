package thinking

import "testing"

func TestMapXHighToMax(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "xhigh_maps_to_max", input: "xhigh", expected: "max"},
		{name: "high_passes_through", input: "high", expected: "high"},
		{name: "max_passes_through", input: "max", expected: "max"},
		{name: "medium_passes_through", input: "medium", expected: "medium"},
		{name: "low_passes_through", input: "low", expected: "low"},
		{name: "minimal_passes_through", input: "minimal", expected: "minimal"},
		{name: "auto_passes_through", input: "auto", expected: "auto"},
		{name: "none_passes_through", input: "none", expected: "none"},
		{name: "empty_passes_through", input: "", expected: ""},
		{name: "unknown_passes_through", input: "unknown", expected: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapXHighToMax(tt.input)
			if got != tt.expected {
				t.Fatalf("MapXHighToMax(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestMapXHighToMax_CaseSensitive(t *testing.T) {
	// MapXHighToMax is case-sensitive, only lowercase "xhigh" is mapped
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "XHIGH_uppercase_not_mapped", input: "XHIGH", expected: "XHIGH"},
		{name: "XHigh_mixed_case_not_mapped", input: "XHigh", expected: "XHigh"},
		{name: "xhigh_lowercase_mapped", input: "xhigh", expected: "max"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapXHighToMax(tt.input)
			if got != tt.expected {
				t.Fatalf("MapXHighToMax(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
