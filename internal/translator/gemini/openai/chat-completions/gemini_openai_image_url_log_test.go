package chat_completions

import (
	"bytes"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// captureTranslatorLogs redirects logrus output to a buffer at Debug level and
// returns the captured text. Used to assert log levels emitted by translator
// code paths that have no other observable side effect.
func captureTranslatorLogs(fn func()) string {
	var buf bytes.Buffer
	orig := log.StandardLogger().Out
	log.SetOutput(&buf)
	defer log.SetOutput(orig)
	origLevel := log.GetLevel()
	log.SetLevel(log.DebugLevel)
	defer log.SetLevel(origLevel)
	fn()
	return buf.String()
}

// TestConvertOpenAIRequestToGeminiNonDataImageURLLogsError verifies that a
// non-data (https://) image_url is skipped AND the skip is logged at error
// level so operators can monitor silent image-drop frequency (P0-2).
// Previously this was log.Warn, which was easy to miss in production logs.
func TestConvertOpenAIRequestToGeminiNonDataImageURLLogsError(t *testing.T) {
	inputJSON := `{
		"model": "gemini-3-flash",
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "image_url", "image_url": {"url": "https://example.com/cat.png"}},
					{"type": "text", "text": "describe this"}
				]
			}
		]
	}`

	logs := captureTranslatorLogs(func() {
		result := ConvertOpenAIRequestToGemini("gemini-3-flash", []byte(inputJSON), false)
		resultJSON := gjson.ParseBytes(result)
		parts := resultJSON.Get("contents.0.parts").Array()
		for _, p := range parts {
			if p.Get("inlineData").Exists() {
				t.Fatalf("https:// image_url must be skipped, found inlineData: %s", p.Raw)
			}
		}
	})

	if !strings.Contains(logs, "skipping non-data image_url") {
		t.Fatalf("expected skip log, got: %s", logs)
	}
	if !strings.Contains(logs, "level=error") {
		t.Fatalf("expected error-level log for skipped image_url, got: %s", logs)
	}
}

// TestConvertOpenAIRequestToGeminiNonDataVideoURLLogsError verifies the same
// escalation for video_url (same silent-drop class of bug).
func TestConvertOpenAIRequestToGeminiNonDataVideoURLLogsError(t *testing.T) {
	inputJSON := `{
		"model": "gemini-3-flash",
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "video_url", "video_url": {"url": "https://example.com/clip.mp4"}},
					{"type": "text", "text": "describe"}
				]
			}
		]
	}`

	logs := captureTranslatorLogs(func() {
		_ = ConvertOpenAIRequestToGemini("gemini-3-flash", []byte(inputJSON), false)
	})

	if !strings.Contains(logs, "skipping non-data video_url") {
		t.Fatalf("expected skip log, got: %s", logs)
	}
	if !strings.Contains(logs, "level=error") {
		t.Fatalf("expected error-level log for skipped video_url, got: %s", logs)
	}
}
