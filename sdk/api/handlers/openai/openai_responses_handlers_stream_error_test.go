package openai

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestForwardResponsesStreamTerminalErrorEmitsResponseFailed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, nil)
	h := NewOpenAIResponsesAPIHandler(base)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		t.Fatalf("expected gin writer to implement http.Flusher")
	}

	data := make(chan []byte)
	errs := make(chan *interfaces.ErrorMessage, 1)
	errs <- &interfaces.ErrorMessage{StatusCode: http.StatusInternalServerError, Error: errors.New("unexpected EOF")}
	close(errs)

	h.forwardResponsesStream(c, flusher, func(error) {}, data, errs, nil)
	body := recorder.Body.String()
	if !strings.Contains(body, "event: response.failed") {
		t.Fatalf("expected response.failed event, got: %q", body)
	}
	if !strings.Contains(body, `"type":"response.failed"`) {
		t.Fatalf("expected response.failed chunk type, got: %q", body)
	}
	if !strings.Contains(body, `"status":"failed"`) {
		t.Fatalf("expected failed response status, got: %q", body)
	}
	if !strings.Contains(body, `"message":"unexpected EOF"`) {
		t.Fatalf("expected structured error message, got: %q", body)
	}
	if strings.Contains(body, "event: error") {
		t.Fatalf("expected no generic error event, got: %q", body)
	}
}

func TestWriteResponsesTerminalErrorEmitsResponseFailed(t *testing.T) {
	framer := &responsesSSEFramer{}
	var buf bytes.Buffer
	writeResponsesTerminalError(&buf, framer, &interfaces.ErrorMessage{
		StatusCode: http.StatusInternalServerError,
		Error:      errors.New("unexpected EOF"),
	})

	body := buf.String()
	if !strings.Contains(body, "event: response.failed") {
		t.Fatalf("expected response.failed event, got: %q", body)
	}
	if !strings.Contains(body, `"type":"response.failed"`) {
		t.Fatalf("expected response.failed chunk type, got: %q", body)
	}
	if !strings.Contains(body, `"status":"failed"`) {
		t.Fatalf("expected failed response status, got: %q", body)
	}
	if !strings.Contains(body, `"message":"unexpected EOF"`) {
		t.Fatalf("expected structured error message, got: %q", body)
	}
	if strings.Contains(body, "event: error") {
		t.Fatalf("expected no generic error event, got: %q", body)
	}
}

func TestWriteResponsesTerminalErrorSynthesizesIdentityWithoutCreated(t *testing.T) {
	framer := &responsesSSEFramer{}
	var buf bytes.Buffer
	writeResponsesTerminalError(&buf, framer, &interfaces.ErrorMessage{
		StatusCode: http.StatusInternalServerError,
		Error:      errors.New("unexpected EOF"),
	})

	failedPayload := failedEventPayload(t, buf.String())
	var payload struct {
		Response struct {
			ID        string `json:"id"`
			CreatedAt int64  `json:"created_at"`
		} `json:"response"`
	}
	if err := json.Unmarshal(failedPayload, &payload); err != nil {
		t.Fatalf("unmarshal failed event: %v", err)
	}
	if !strings.HasPrefix(payload.Response.ID, "resp_") || strings.TrimPrefix(payload.Response.ID, "resp_") == "" {
		t.Fatalf("response.id = %q, want synthesized resp_ id", payload.Response.ID)
	}
	if payload.Response.CreatedAt != 0 {
		t.Fatalf("response.created_at = %d, want 0", payload.Response.CreatedAt)
	}
}

func TestWriteResponsesTerminalErrorSynthesizesEmptyIDFromCreated(t *testing.T) {
	framer := &responsesSSEFramer{}
	var buf bytes.Buffer
	framer.WriteChunk(&buf, []byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"\",\"created_at\":1234567890}}\n\n"))
	writeResponsesTerminalError(&buf, framer, &interfaces.ErrorMessage{
		StatusCode: http.StatusInternalServerError,
		Error:      errors.New("unexpected EOF"),
	})

	failedPayload := failedEventPayload(t, buf.String())
	var payload struct {
		Response struct {
			ID        string `json:"id"`
			CreatedAt int64  `json:"created_at"`
		} `json:"response"`
	}
	if err := json.Unmarshal(failedPayload, &payload); err != nil {
		t.Fatalf("unmarshal failed event: %v", err)
	}
	if !strings.HasPrefix(payload.Response.ID, "resp_") || strings.TrimPrefix(payload.Response.ID, "resp_") == "" {
		t.Fatalf("response.id = %q, want synthesized resp_ id", payload.Response.ID)
	}
	if payload.Response.CreatedAt != 1234567890 {
		t.Fatalf("response.created_at = %d, want %d", payload.Response.CreatedAt, int64(1234567890))
	}
}

func TestWriteResponsesTerminalErrorContinuesSequenceNumber(t *testing.T) {
	tests := []struct {
		name    string
		frames  []string
		wantSeq int64
	}{
		{name: "no sequence frames", wantSeq: 0},
		{name: "single frame", frames: []string{`{"type":"response.created","sequence_number":4}`}, wantSeq: 5},
		{name: "monotonic frames", frames: []string{
			`{"type":"response.created","sequence_number":1}`,
			`{"type":"response.output_item.done","sequence_number":3,"output_index":0,"item":{"type":"message","id":"msg-1"}}`,
			`{"type":"response.output_item.done","sequence_number":7,"output_index":1,"item":{"type":"function_call","id":"fc-1"}}`,
		}, wantSeq: 8},
		{name: "out of order frames", frames: []string{
			`{"type":"response.created","sequence_number":9}`,
			`{"type":"response.output_item.done","sequence_number":2,"output_index":0,"item":{"type":"message","id":"msg-1"}}`,
		}, wantSeq: 10},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			framer := &responsesSSEFramer{}
			var buf bytes.Buffer
			for _, frame := range test.frames {
				framer.WriteChunk(&buf, []byte("data: "+frame+"\n\n"))
			}
			writeResponsesTerminalError(&buf, framer, &interfaces.ErrorMessage{
				StatusCode: http.StatusInternalServerError,
				Error:      errors.New("unexpected EOF"),
			})

			failedPayload := failedEventPayload(t, buf.String())
			var payload struct {
				SequenceNumber int64 `json:"sequence_number"`
			}
			if err := json.Unmarshal(failedPayload, &payload); err != nil {
				t.Fatalf("unmarshal failed event: %v", err)
			}
			if payload.SequenceNumber != test.wantSeq {
				t.Fatalf("sequence_number = %d, want %d; payload=%s", payload.SequenceNumber, test.wantSeq, failedPayload)
			}
		})
	}
}

// failedEventPayload extracts the data payload of the event: response.failed
// frame from an SSE body.
func failedEventPayload(t *testing.T, body string) []byte {
	t.Helper()
	for _, frame := range strings.Split(body, "\n\n") {
		if strings.Contains(frame, "event: response.failed") {
			payload, _ := responsesSSEDataPayload([]byte(frame))
			return payload
		}
	}
	t.Fatalf("no response.failed frame in body: %q", body)
	return nil
}

func TestWriteResponsesTerminalErrorPassesResponseIdentityFromCreated(t *testing.T) {
	framer := &responsesSSEFramer{}
	var buf bytes.Buffer
	framer.WriteChunk(&buf, []byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_abc123\",\"created_at\":1234567890}}\n\n"))
	writeResponsesTerminalError(&buf, framer, &interfaces.ErrorMessage{
		StatusCode: http.StatusInternalServerError,
		Error:      errors.New("unexpected EOF"),
	})

	failedPayload := failedEventPayload(t, buf.String())
	var payload struct {
		Response struct {
			ID        string `json:"id"`
			CreatedAt int64  `json:"created_at"`
		} `json:"response"`
	}
	if err := json.Unmarshal(failedPayload, &payload); err != nil {
		t.Fatalf("unmarshal failed event: %v", err)
	}
	if payload.Response.ID != "resp_abc123" {
		t.Fatalf("response.id = %q, want %q", payload.Response.ID, "resp_abc123")
	}
	if payload.Response.CreatedAt != 1234567890 {
		t.Fatalf("response.created_at = %d, want %d", payload.Response.CreatedAt, int64(1234567890))
	}
}

func TestWriteResponsesTerminalErrorAfterTerminalEventKeepsErrorEvent(t *testing.T) {
	framer := &responsesSSEFramer{}
	var buf bytes.Buffer
	framer.WriteChunk(&buf, []byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\",\"output\":[]}}\n\n"))
	writeResponsesTerminalError(&buf, framer, &interfaces.ErrorMessage{
		StatusCode: http.StatusInternalServerError,
		Error:      errors.New("unexpected EOF"),
	})

	body := buf.String()
	if !strings.Contains(body, `"type":"response.completed"`) {
		t.Fatalf("expected completed event to be forwarded, got: %q", body)
	}
	if strings.Contains(body, "response.failed") {
		t.Fatalf("expected no second terminal event after response.completed, got: %q", body)
	}
	if !strings.Contains(body, "event: error") {
		t.Fatalf("expected non-terminal error event after terminal event, got: %q", body)
	}
}
