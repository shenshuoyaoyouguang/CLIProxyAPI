package qwen

import (
	"strings"
	"testing"
	"time"
)

func TestFormatQwenOAuthErrorFromJSON(t *testing.T) {
	body := []byte(`{"error":"invalid_grant","error_description":"refresh token expired"}`)
	got := formatQwenOAuthError(body)
	if got != "invalid_grant - refresh token expired" {
		t.Fatalf("unexpected formatted error: %s", got)
	}
}

func TestFormatQwenOAuthErrorTruncatesRawBody(t *testing.T) {
	body := []byte(strings.Repeat("x", qwenErrorBodyPreviewMax+64))
	got := formatQwenOAuthError(body)
	if !strings.HasSuffix(got, "...(truncated)") {
		t.Fatalf("expected truncated suffix, got: %s", got)
	}
}

func TestTrimQwenErrorPreviewEmpty(t *testing.T) {
	got := trimQwenErrorPreview([]byte("   "))
	if got != "empty response body" {
		t.Fatalf("unexpected empty body message: %s", got)
	}
}

func TestQwenRetryDelayCapped(t *testing.T) {
	if got := qwenRetryDelay(0); got != 0 {
		t.Fatalf("expected 0 delay for attempt 0, got %s", got)
	}
	if got := qwenRetryDelay(2); got != 2*time.Second {
		t.Fatalf("expected 2s delay, got %s", got)
	}
	if got := qwenRetryDelay(60); got != qwenMaxRetryBackoff {
		t.Fatalf("expected capped delay %s, got %s", qwenMaxRetryBackoff, got)
	}
}

