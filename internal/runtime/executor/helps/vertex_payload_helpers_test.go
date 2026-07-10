package helps

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestStripVertexOpenAIResponsesToolCallIDs(t *testing.T) {
	tests := []struct {
		name         string
		payload      string
		sourceFormat string
		checkStrip   bool // true = verify IDs removed; false = verify unchanged
	}{
		{
			name:         "openai-chat returns unchanged",
			payload:      `{"contents":[{"parts":[{"functionCall":{"id":"call_1","name":"fn"}}]}]}`,
			sourceFormat: "openai-chat",
			checkStrip:   false,
		},
		{
			name:         "empty sourceFormat returns unchanged",
			payload:      `{"contents":[{"parts":[{"functionCall":{"id":"call_1","name":"fn"}}]}]}`,
			sourceFormat: "",
			checkStrip:   false,
		},
		{
			name:         "openai-response with no contents returns unchanged",
			payload:      `{"model":"gemini-pro"}`,
			sourceFormat: "openai-response",
			checkStrip:   false,
		},
		{
			name:         "openai-response strips functionCall.id",
			payload:      `{"contents":[{"parts":[{"functionCall":{"id":"call_123","name":"get_weather","args":{"city":"NYC"}}}]}]}`,
			sourceFormat: "openai-response",
			checkStrip:   true,
		},
		{
			name:         "openai-response strips functionResponse.id",
			payload:      `{"contents":[{"parts":[{"functionResponse":{"id":"resp_456","name":"get_weather","response":{"result":"sunny"}}}]}]}`,
			sourceFormat: "openai-response",
			checkStrip:   true,
		},
		{
			name:         "case insensitive OpenAI-Response",
			payload:      `{"contents":[{"parts":[{"functionCall":{"id":"call_x","name":"fn"}}]}]}`,
			sourceFormat: "OpenAI-Response",
			checkStrip:   true,
		},
		{
			name:         "trimmed sourceFormat with spaces",
			payload:      `{"contents":[{"parts":[{"functionCall":{"id":"call_y","name":"fn"}}]}]}`,
			sourceFormat: " openai-response ",
			checkStrip:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripVertexOpenAIResponsesToolCallIDs([]byte(tt.payload), tt.sourceFormat)

			if !tt.checkStrip {
				if string(result) != tt.payload {
					t.Fatalf("expected payload unchanged, got %s", string(result))
				}
				return
			}

			// Verify IDs are removed
			parsed := gjson.ParseBytes(result)
			contents := parsed.Get("contents")
			if !contents.IsArray() {
				t.Fatal("expected contents array in result")
			}
			for ci, content := range contents.Array() {
				parts := content.Get("parts")
				if !parts.IsArray() {
					continue
				}
				for pi, part := range parts.Array() {
					if part.Get("functionCall.id").Exists() {
						t.Fatalf("contents[%d].parts[%d].functionCall.id should be removed", ci, pi)
					}
					if part.Get("functionResponse.id").Exists() {
						t.Fatalf("contents[%d].parts[%d].functionResponse.id should be removed", ci, pi)
					}
				}
			}
		})
	}
}

func TestStripVertexOpenAIResponsesToolCallIDs_MultipleContents(t *testing.T) {
	payload := `{"contents":[{"parts":[{"functionCall":{"id":"c1","name":"fn1","args":{"a":"1"}}},{"functionResponse":{"id":"r1","name":"fn1","response":{"ok":true}}}]},{"parts":[{"functionCall":{"id":"c2","name":"fn2","args":{"b":"2"}}}]}]}`

	result := StripVertexOpenAIResponsesToolCallIDs([]byte(payload), "openai-response")
	parsed := gjson.ParseBytes(result)

	contents := parsed.Get("contents")
	if !contents.IsArray() {
		t.Fatal("expected contents array")
	}

	for ci, content := range contents.Array() {
		parts := content.Get("parts")
		if !parts.IsArray() {
			continue
		}
		for pi, part := range parts.Array() {
			if part.Get("functionCall.id").Exists() {
				t.Fatalf("contents[%d].parts[%d].functionCall.id should be removed", ci, pi)
			}
			if part.Get("functionResponse.id").Exists() {
				t.Fatalf("contents[%d].parts[%d].functionResponse.id should be removed", ci, pi)
			}
			// Verify other fields preserved
			if part.Get("functionCall.name").Exists() {
				if !part.Get("functionCall.args").Exists() {
					t.Fatalf("contents[%d].parts[%d].functionCall.args should be preserved", ci, pi)
				}
			}
			if part.Get("functionResponse.name").Exists() {
				if !part.Get("functionResponse.response").Exists() {
					t.Fatalf("contents[%d].parts[%d].functionResponse.response should be preserved", ci, pi)
				}
			}
		}
	}

	// Verify structure: 2 contents, first has 2 parts, second has 1 part
	arr := contents.Array()
	if len(arr) != 2 {
		t.Fatalf("expected 2 contents, got %d", len(arr))
	}
	if len(arr[0].Get("parts").Array()) != 2 {
		t.Fatalf("expected 2 parts in contents[0], got %d", len(arr[0].Get("parts").Array()))
	}
	if len(arr[1].Get("parts").Array()) != 1 {
		t.Fatalf("expected 1 part in contents[1], got %d", len(arr[1].Get("parts").Array()))
	}
}
