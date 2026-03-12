package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/interfaces"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func TestForwardStreamEmitsKeepAliveBeforeFirstChunk(t *testing.T) {
	gin.SetMode(gin.TestMode)

	base := NewBaseAPIHandlers(&sdkconfig.SDKConfig{
		Streaming: sdkconfig.StreamingConfig{
			KeepAliveSeconds: 1,
		},
	}, nil)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		t.Fatalf("expected gin writer to implement http.Flusher")
	}

	data := make(chan []byte)
	errs := make(chan *interfaces.ErrorMessage)
	interval := 10 * time.Millisecond

	done := make(chan struct{})
	go func() {
		base.ForwardStream(c, flusher, func(error) {}, data, errs, StreamForwardOptions{
			KeepAliveInterval: &interval,
			WriteChunk: func(chunk []byte) {
				_, _ = c.Writer.Write(chunk)
			},
		})
		close(done)
	}()

	time.Sleep(25 * time.Millisecond)
	data <- []byte("payload")
	close(data)
	close(errs)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ForwardStream to finish")
	}

	body := recorder.Body.String()
	keepAliveIndex := strings.Index(body, ": keep-alive\n\n")
	if keepAliveIndex < 0 {
		t.Fatalf("expected keep-alive before first chunk, got %q", body)
	}
	payloadIndex := strings.Index(body, "payload")
	if payloadIndex < 0 {
		t.Fatalf("expected payload in body, got %q", body)
	}
	if keepAliveIndex > payloadIndex {
		t.Fatalf("expected keep-alive before payload, got %q", body)
	}
}

func TestForwardStreamWritesTerminalErrorBeforeFirstChunk(t *testing.T) {
	gin.SetMode(gin.TestMode)

	base := NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, nil)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		t.Fatalf("expected gin writer to implement http.Flusher")
	}

	data := make(chan []byte)
	errs := make(chan *interfaces.ErrorMessage, 1)
	wantErr := errors.New("boom")
	errs <- &interfaces.ErrorMessage{StatusCode: http.StatusBadGateway, Error: wantErr}
	close(errs)

	var canceled error
	base.ForwardStream(c, flusher, func(err error) {
		canceled = err
	}, data, errs, StreamForwardOptions{
		WriteChunk: func(chunk []byte) {
			_, _ = c.Writer.Write(chunk)
		},
		WriteTerminalError: func(errMsg *interfaces.ErrorMessage) {
			_, _ = c.Writer.Write([]byte("terminal-error"))
		},
		WriteDone: func() {
			_, _ = c.Writer.Write([]byte("done"))
		},
	})

	if body := recorder.Body.String(); body != "terminal-error" {
		t.Fatalf("expected terminal error body, got %q", body)
	}
	if !errors.Is(canceled, wantErr) {
		t.Fatalf("expected cancel to receive %v, got %v", wantErr, canceled)
	}
}
