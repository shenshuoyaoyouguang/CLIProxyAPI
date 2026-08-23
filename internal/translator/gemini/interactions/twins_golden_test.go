package interactions

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// TestGoldenGeminiTwins* pins the exact wire output of the gemini-side twins
// slated to move to the shared interactionscommon package (batch B-B).

func TestGoldenGeminiInlineDataPartJSON(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"camel", `{"mimeType":"image/png","data":"QUJD"}`, `{"inlineData":{"mimeType":"image/png","data":"QUJD"}}`},
		{"snake", `{"mime_type":"image/png","data":"QUJD"}`, `{"inlineData":{"mimeType":"image/png","data":"QUJD"}}`},
		{"missing mime", `{"data":"QUJD"}`, ``},
	}
	for _, tc := range cases {
		if got := string(geminiInlineDataPartJSON(gjson.Parse(tc.input))); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestGoldenGeminiFileDataPartJSON(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"camel", `{"mimeType":"application/pdf","fileUri":"gs://b/f.pdf"}`, `{"fileData":{"mimeType":"application/pdf","fileUri":"gs://b/f.pdf"}}`},
		{"snake", `{"mime_type":"application/pdf","file_uri":"gs://b/f.pdf"}`, `{"fileData":{"mimeType":"application/pdf","fileUri":"gs://b/f.pdf"}}`},
	}
	for _, tc := range cases {
		if got := string(geminiFileDataPartJSON(gjson.Parse(tc.input))); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestGoldenGeminiInlineDataPartFromDataURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"valid", "data:image/png;base64,QUJD", `{"inlineData":{"mimeType":"image/png","data":"QUJD"}}`},
		{"no prefix", "image/png;base64,QUJD", ""},
		{"not base64", "data:text/plain,hello", ""},
	}
	for _, tc := range cases {
		if got := string(geminiInlineDataPartFromDataURL(tc.in)); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestGoldenInteractionsNativeGeminiPart(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"text passthrough", `{"text":"hi"}`, `{"text":"hi"}`},
		{"functionCall passthrough", `{"functionCall":{"name":"f"}}`, `{"functionCall":{"name":"f"}}`},
		{"camel inline", `{"inlineData":{"mimeType":"image/png","data":"QUJD"}}`, `{"inlineData":{"mimeType":"image/png","data":"QUJD"}}`},
		{"snake file", `{"file_data":{"mime_type":"application/pdf","file_uri":"gs://b/f.pdf"}}`, `{"fileData":{"mimeType":"application/pdf","fileUri":"gs://b/f.pdf"}}`},
		{"unknown", `{"other":1}`, ``},
	}
	for _, tc := range cases {
		if got := string(interactionsNativeGeminiPart(gjson.Parse(tc.input))); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestGoldenInteractionsGeminiContentRole(t *testing.T) {
	cases := []struct{ role, def, want string }{
		{"Model", "", "model"},
		{"assistant ", "", "model"},
		{"USER", "", "user"},
		{"other", "model", "model"},
		{"other", "", "user"},
	}
	for _, tc := range cases {
		if got := interactionsGeminiContentRole(tc.role, tc.def); got != tc.want {
			t.Errorf("role=%q def=%q: got %q want %q", tc.role, tc.def, got, tc.want)
		}
	}
}

func TestGoldenInteractionsInputAudioMimeType(t *testing.T) {
	cases := []struct{ in, want string }{
		{"wav", "audio/wav"}, {"MP3", "audio/mpeg"}, {"flac", "audio/flac"},
		{"opus", "audio/opus"}, {"pcm16", "audio/pcm"}, {"unknown", "audio/mpeg"},
	}
	for _, tc := range cases {
		if got := interactionsInputAudioMimeType(tc.in); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestGoldenInteractionsThinkingSummariesIncludeThoughts(t *testing.T) {
	cases := []struct {
		in            string
		wantV, wantOK bool
	}{
		{`"auto"`, true, true},
		{`"none"`, false, true},
		{`"other"`, false, false},
		{"123", false, false},
	}
	for _, tc := range cases {
		v, ok := interactionsThinkingSummariesIncludeThoughts(gjson.Parse(tc.in))
		if v != tc.wantV || ok != tc.wantOK {
			t.Errorf("%s: got (%v,%v) want (%v,%v)", tc.in, v, ok, tc.wantV, tc.wantOK)
		}
	}
}

func TestGoldenInteractionsFunctionPartIDAndThoughtSignature(t *testing.T) {
	if got := interactionsFunctionPartID(gjson.Parse(`{"call_id":"c"}`)); got != "c" {
		t.Errorf("call_id: got %q", got)
	}
	if got := interactionsFunctionPartID(gjson.Parse(`{}`)); got != "" {
		t.Errorf("empty: got %q", got)
	}
	sigCases := []struct{ in, want string }{
		{`{"thoughtSignature":" sig "}`, "sig"},
		{`{"thought_signature":"s2"}`, "s2"},
		{`{"extra_content":{"google":{"thought_signature":"s3"}}}`, "s3"},
		{`{}`, ""},
	}
	for _, tc := range sigCases {
		if got := interactionsThoughtSignature(gjson.Parse(tc.in)); got != tc.want {
			t.Errorf("sig %s: got %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestGoldenJoinAndCaseHelpers(t *testing.T) {
	if got := joinJSONPath("a.b", "c"); got != "a.b.c" {
		t.Errorf("join: got %q", got)
	}
	if got := convertSnakeCaseKeysToCamelCase([]byte(`{"prompt_token_count":1,"nested_key":{"inner_value":2}}`)); string(got) != `{"promptTokenCount":1,"nestedKey":{"innerValue":2}}` {
		t.Errorf("walker: got %s", got)
	}
	if got := convertCamelCaseKeysToSnakeCase([]byte(`{"promptTokenCount":1,"nestedKey":{"innerValue":2}}`)); string(got) != `{"prompt_token_count":1,"nested_key":{"inner_value":2}}` {
		t.Errorf("inverse walker: got %s", got)
	}
	if toSnakeCase("promptTokenCount") != "prompt_token_count" {
		t.Errorf("toSnakeCase: got %q", toSnakeCase("promptTokenCount"))
	}
}

func TestGoldenStreamAppends(t *testing.T) {
	st := &StreamState{ID: "i-1"}
	out := appendInteractionsCreated(nil, st, "m1")
	if len(out) != 1 || !strings.Contains(string(out[0]), `"id":"i-1"`) ||
		!strings.Contains(string(out[0]), `"model":"m1"`) ||
		!strings.Contains(string(out[0]), "event: interaction.created") {
		t.Fatalf("created: unexpected %s", out[0])
	}
	out = appendInteractionsStatusUpdate(out, st)
	if !strings.Contains(string(out[1]), `"interaction_id":"i-1"`) ||
		!strings.Contains(string(out[1]), "event: interaction.status_update") {
		t.Fatalf("status: unexpected %s", out[1])
	}
	root := gjson.Parse(`{"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":4,"totalTokenCount":7}}`)
	out = appendInteractionsCompleted(out, st, "m1", root)
	if !st.Completed || !strings.Contains(string(out[2]), `"total_input_tokens":3`) ||
		!strings.Contains(string(out[2]), `"total_output_tokens":4`) ||
		!strings.Contains(string(out[2]), "event: interaction.completed") {
		t.Fatalf("completed: unexpected %s st=%v", out[2], st)
	}
	n := len(out)
	out = appendInteractionsDone(out, st)
	if len(out) != n+1 || !strings.Contains(string(out[n]), "[DONE]") {
		t.Fatalf("done: unexpected %v", out)
	}
	if out2 := appendInteractionsDone(out, st); len(out2) != len(out) {
		t.Fatal("done not idempotent")
	}
}
