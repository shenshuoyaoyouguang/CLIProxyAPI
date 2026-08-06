package helps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
)

func TestRecordAPIRequestClonesDeferredBodyWhenRequestLogDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ctx := context.WithValue(context.Background(), "gin", ginCtx)
	body := []byte(`{"model":"original"}`)

	RecordAPIRequest(ctx, &config.Config{}, UpstreamRequestLog{
		URL:    "https://api.example.com/v1/responses",
		Method: http.MethodPost,
		Body:   body,
	})
	body[10] = 'X'

	value, exists := ginCtx.Get(logging.DeferredAPIRequestContextKey)
	if !exists {
		t.Fatal("deferred API request was not captured")
	}
	requests, ok := value.([]logging.DeferredAPIRequest)
	if !ok || len(requests) != 1 {
		t.Fatalf("deferred API requests = %#v, want one request", value)
	}
	captured := string(requests[0]())
	if !strings.Contains(captured, `{"model":"original"}`) {
		t.Fatalf("captured API request = %q, want original body", captured)
	}
}

func TestRecordAPIResponseMetadataStoresHeadersWhenRequestLogDisabled(t *testing.T) {
	ctx := logging.WithResponseHeadersHolder(context.Background())
	headers := http.Header{}
	headers.Add("X-Upstream-Request-Id", "upstream-req-1")

	RecordAPIResponseMetadata(ctx, &config.Config{}, http.StatusOK, headers)
	headers.Set("X-Upstream-Request-Id", "mutated")

	got := logging.GetResponseHeaders(ctx)
	if got.Get("X-Upstream-Request-Id") != "upstream-req-1" {
		t.Fatalf("response header = %q, want %q", got.Get("X-Upstream-Request-Id"), "upstream-req-1")
	}
}

func TestSummarizeErrorBodyTruncatesOversizedBodies(t *testing.T) {
	long := strings.Repeat("x", maxErrorBodySummaryLen*2)
	got := SummarizeErrorBody("text/plain", []byte(long))
	want := strings.Repeat("x", maxErrorBodySummaryLen) + "...(truncated)"
	if got != want {
		t.Fatalf("summary = %q, want exact truncation (%d runes + marker)", got, maxErrorBodySummaryLen)
	}
}

func TestSummarizeErrorBodyStripsControlCharacters(t *testing.T) {
	// C0 (NUL..US), CR, ESC, DEL (0x7F) and a C1 control (0x85 NEL).
	input := "line1\x00\x01\x02\r\nline2\x1b\x7f\x85"
	got := SummarizeErrorBody("text/plain", []byte(input))
	// No control runes other than the permitted newline and tab.
	for _, r := range got {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			t.Fatalf("summary contains control rune %q: %q", r, got)
		}
	}
	if !strings.Contains(got, "\n") {
		t.Fatalf("summary should keep newlines for readability: %q", got)
	}
	if !strings.Contains(got, "line1") || !strings.Contains(got, "line2") {
		t.Fatalf("summary lost printable content: %q", got)
	}
}

func TestSummarizeErrorBodyTruncatesToExactRuneCount(t *testing.T) {
	long := strings.Repeat("x", maxErrorBodySummaryLen+50)
	got := SummarizeErrorBody("text/plain", []byte(long))
	want := strings.Repeat("x", maxErrorBodySummaryLen) + "...(truncated)"
	if got != want {
		t.Fatalf("summary = %d runes, want exactly %d (%d + marker)", len([]rune(got)), len([]rune(want)), maxErrorBodySummaryLen)
	}
}

func TestSummarizeErrorBodyRepairsInvalidUTF8(t *testing.T) {
	got := SummarizeErrorBody("text/plain", []byte{0xff, 0xfe, 'a', 'b', 'c'})
	if !strings.Contains(got, "abc") {
		t.Fatalf("summary lost valid runes after invalid prefix: %q", got)
	}
	if strings.ToValidUTF8(got, "") != got {
		t.Fatalf("summary is not valid UTF-8: %q", got)
	}
}

func TestSummarizeErrorBodyJSONMessageAlsoCapped(t *testing.T) {
	longMsg := strings.Repeat("m", maxErrorBodySummaryLen+100)
	body := []byte(`{"error":{"message":"` + longMsg + `"}}`)
	got := SummarizeErrorBody("application/json", body)
	want := strings.Repeat("m", maxErrorBodySummaryLen) + "...(truncated)"
	if got != want {
		t.Fatalf("JSON summary = %q, want exact truncation (%d runes + marker)", got, maxErrorBodySummaryLen)
	}
}

func TestSummarizeErrorBodyKeepsShortBodiesVerbatim(t *testing.T) {
	body := "upstream refused connection"
	if got := SummarizeErrorBody("text/plain", []byte(body)); got != body {
		t.Fatalf("short body summary = %q, want %q", got, body)
	}
}
