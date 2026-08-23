// Package interactionscommon holds helper logic shared verbatim by the
// antigravity and gemini interactions translators. Both providers translate
// between the interactions wire format and Gemini-style payloads, so their
// part builders, key-case converters, stream-event appends, and small
// accessors are byte-identical modulo names. This package is that single
// implementation.
//
// Provider-specific behavior stays out on purpose: usage extraction differs
// between the two providers (antigravity also reads cpaUsageMetadata and uses
// a different reader-helper style), so the completed-event append takes the
// provider's usage setter as an explicit hook instead of embedding either
// variant.
package interactionscommon

import (
	"fmt"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	translatorcommon "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/common"
)

// StreamState tracks where an interactions response stream is. Both provider
// translators use exactly these fields for the shared event appends below;
// antigravity embeds it and adds its own tool-name map.
type StreamState struct {
	Started         bool
	Finished        bool
	Completed       bool
	Done            bool
	ActiveStepOpen  bool
	ID              string
	StepID          string
	ActiveStepType  string
	ActiveStepIndex int
	StepIndex       int
}

// TextPartJSON builds a text content part, optionally flagged as thought.
func TextPartJSON(text string, thought bool) []byte {
	partJSON := []byte(`{"text":""}`)
	partJSON, _ = sjson.SetBytes(partJSON, "text", text)
	if thought {
		partJSON, _ = sjson.SetBytes(partJSON, "thought", true)
	}
	return partJSON
}

// InlineDataPartJSON builds an inline-data part, accepting both camelCase and
// snake_case input keys. Returns nil when mime type or data is missing.
func InlineDataPartJSON(inline gjson.Result) []byte {
	mimeType := inline.Get("mimeType").String()
	if mimeType == "" {
		mimeType = inline.Get("mime_type").String()
	}
	data := inline.Get("data").String()
	if mimeType == "" || data == "" {
		return nil
	}
	partJSON := []byte(`{"inlineData":{"mimeType":"","data":""}}`)
	partJSON, _ = sjson.SetBytes(partJSON, "inlineData.mimeType", mimeType)
	partJSON, _ = sjson.SetBytes(partJSON, "inlineData.data", data)
	return partJSON
}

// FileDataPartJSON builds a file-data part, accepting both camelCase and
// snake_case input keys. Returns nil when mime type or URI is missing.
func FileDataPartJSON(fileData gjson.Result) []byte {
	mimeType := fileData.Get("mimeType").String()
	if mimeType == "" {
		mimeType = fileData.Get("mime_type").String()
	}
	fileURI := fileData.Get("fileUri").String()
	if fileURI == "" {
		fileURI = fileData.Get("file_uri").String()
	}
	if mimeType == "" || fileURI == "" {
		return nil
	}
	partJSON := []byte(`{"fileData":{"mimeType":"","fileUri":""}}`)
	partJSON, _ = sjson.SetBytes(partJSON, "fileData.mimeType", mimeType)
	partJSON, _ = sjson.SetBytes(partJSON, "fileData.fileUri", fileURI)
	return partJSON
}

// NativePart converts a native (Gemini-shaped) content part into its
// interactions JSON form. Text and function parts pass through verbatim.
func NativePart(part gjson.Result) []byte {
	switch {
	case part.Get("text").Exists(), part.Get("functionCall").Exists(), part.Get("functionResponse").Exists():
		return []byte(part.Raw)
	case part.Get("inlineData").Exists():
		return InlineDataPartJSON(part.Get("inlineData"))
	case part.Get("fileData").Exists():
		return FileDataPartJSON(part.Get("fileData"))
	case part.Get("inline_data").Exists():
		return InlineDataPartJSON(part.Get("inline_data"))
	case part.Get("file_data").Exists():
		return FileDataPartJSON(part.Get("file_data"))
	}
	return nil
}

// Content wraps parts into a role-tagged content object.
func Content(role string, parts [][]byte) []byte {
	content := []byte(`{"role":"","parts":[]}`)
	content, _ = sjson.SetBytes(content, "role", role)
	content, _ = sjson.SetRawBytes(content, "parts", translatorcommon.JoinRawArray(parts))
	return content
}

// ContentRole normalizes an interactions role, falling back to defaultRole
// unless that resolves to "model".
func ContentRole(role, defaultRole string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "model", "assistant":
		return "model"
	case "user":
		return "user"
	}
	if defaultRole == "model" {
		return "model"
	}
	return "user"
}

// InputAudioMimeType maps an interactions audio format to its MIME type.
func InputAudioMimeType(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "wav":
		return "audio/wav"
	case "mp3":
		return "audio/mpeg"
	case "flac":
		return "audio/flac"
	case "opus":
		return "audio/opus"
	case "pcm16":
		return "audio/pcm"
	default:
		return "audio/mpeg"
	}
}

// ThinkingSummariesIncludeThoughts interprets an interactions thinking
// summaries preference. The second result reports whether the value was
// recognized at all.
func ThinkingSummariesIncludeThoughts(summary gjson.Result) (bool, bool) {
	if summary.Type != gjson.String {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(summary.String())) {
	case "auto":
		return true, true
	case "none":
		return false, true
	default:
		return false, false
	}
}

// FunctionPartID returns a function call/result part's identifier.
func FunctionPartID(part gjson.Result) string {
	if id := part.Get("id"); id.Exists() {
		return id.String()
	}
	if callID := part.Get("call_id"); callID.Exists() {
		return callID.String()
	}
	return ""
}

// ThoughtSignature returns a part's thought signature, probing the known
// locations in precedence order.
func ThoughtSignature(part gjson.Result) string {
	for _, path := range []string{"thoughtSignature", "thought_signature", "extra_content.google.thought_signature"} {
		if signature := strings.TrimSpace(part.Get(path).String()); signature != "" {
			return signature
		}
	}
	return ""
}

// InlineDataPartFromDataURL parses a base64 data URL into an inline-data
// part. Returns nil for non-data URLs or non-base64 payloads.
func InlineDataPartFromDataURL(dataURL string) []byte {
	mimeType, data, ok := translatorcommon.SplitBase64DataURL(dataURL)
	if !ok {
		return nil
	}
	return InlineDataPartJSON(gjson.Parse(fmt.Sprintf(`{"mime_type":%q,"data":%q}`, mimeType, data)))
}

// JoinJSONPath appends a key to a dot-separated sjson path.
func JoinJSONPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// ToCamelCase converts snake_case text to camelCase.
func ToCamelCase(s string) string {
	parts := strings.Split(s, "_")
	if len(parts) == 0 {
		return s
	}
	out := parts[0]
	for _, part := range parts[1:] {
		if part == "" {
			continue
		}
		out += strings.ToUpper(part[:1]) + part[1:]
	}
	return out
}

// ToSnakeCase converts camelCase text to snake_case.
func ToSnakeCase(s string) string {
	var out strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			out.WriteByte('_')
		}
		out.WriteRune(r)
	}
	return strings.ToLower(out.String())
}

// ConvertSnakeCaseKeysToCamelCase rewrites every snake_case object key in the
// document to camelCase. Returns the input unchanged when it is not valid
// JSON.
func ConvertSnakeCaseKeysToCamelCase(raw []byte) []byte {
	root := gjson.ParseBytes(raw)
	if !root.Exists() {
		return raw
	}
	out := []byte(`{}`)
	out = CopySnakeCaseValueToCamelCase(out, "", root)
	return out
}

// CopySnakeCaseValueToCamelCase is the recursive walker behind
// ConvertSnakeCaseKeysToCamelCase.
func CopySnakeCaseValueToCamelCase(out []byte, path string, node gjson.Result) []byte {
	if node.IsObject() {
		node.ForEach(func(key, value gjson.Result) bool {
			childPath := JoinJSONPath(path, ToCamelCase(key.String()))
			out = CopySnakeCaseValueToCamelCase(out, childPath, value)
			return true
		})
		return out
	}
	if node.IsArray() {
		node.ForEach(func(_, value gjson.Result) bool {
			childPath := path + ".-1"
			out = CopySnakeCaseValueToCamelCase(out, childPath, value)
			return true
		})
		return out
	}
	out, _ = sjson.SetRawBytes(out, path, []byte(node.Raw))
	return out
}

// AppendCreated adds the interaction.created stream event.
func AppendCreated(out [][]byte, st *StreamState, modelName string) [][]byte {
	created := []byte(`{"interaction":{"id":"","status":"in_progress","object":"interaction","model":""},"event_type":"interaction.created"}`)
	created, _ = sjson.SetBytes(created, "interaction.id", st.ID)
	created, _ = sjson.SetBytes(created, "interaction.model", modelName)
	return append(out, translatorcommon.SSEEventData("interaction.created", created))
}

// AppendStatusUpdate adds the interaction.status_update stream event.
func AppendStatusUpdate(out [][]byte, st *StreamState) [][]byte {
	statusUpdate := []byte(`{"interaction_id":"","status":"in_progress","event_type":"interaction.status_update"}`)
	statusUpdate, _ = sjson.SetBytes(statusUpdate, "interaction_id", st.ID)
	return append(out, translatorcommon.SSEEventData("interaction.status_update", statusUpdate))
}

// AppendCompleted adds the interaction.completed stream event. setUsage is
// the provider's usage setter (the one piece that differs between providers).
func AppendCompleted(out [][]byte, st *StreamState, modelName string, root gjson.Result, setUsage func(out []byte, path string, root gjson.Result) []byte) [][]byte {
	now := time.Now().UTC().Format(time.RFC3339)
	completed := []byte(`{"interaction":{"id":"","status":"completed","usage":{},"created":"","updated":"","service_tier":"standard","object":"interaction","model":""},"event_type":"interaction.completed"}`)
	completed, _ = sjson.SetBytes(completed, "interaction.id", st.ID)
	completed, _ = sjson.SetBytes(completed, "interaction.created", now)
	completed, _ = sjson.SetBytes(completed, "interaction.updated", now)
	completed, _ = sjson.SetBytes(completed, "interaction.model", modelName)
	if root.Exists() {
		completed = setUsage(completed, "interaction.usage", root)
	}
	out = append(out, translatorcommon.SSEEventData("interaction.completed", completed))
	st.Completed = true
	return out
}

// AppendDone adds the terminal done event once per stream.
func AppendDone(out [][]byte, st *StreamState) [][]byte {
	if st.Done {
		return out
	}
	out = append(out, translatorcommon.SSEEventData("done", []byte("[DONE]")))
	st.Done = true
	return out
}

// TextPartJSON builds a Gemini-style text part for interactions content,
// optionally flagged as thought. (Placed here with the other part builders.)
