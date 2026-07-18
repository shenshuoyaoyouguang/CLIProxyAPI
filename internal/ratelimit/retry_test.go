package ratelimit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDoWithRetryAcceptsAny2xx(t *testing.T) {
	t.Parallel()
	attempts := 0
	resp, err := DoWithRetry(context.Background(), RetryConfig{MaxAttempts: 1}, func(context.Context) (*http.Response, error) {
		attempts++
		return &http.Response{
			StatusCode: http.StatusCreated,
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	_ = resp.Body.Close()
}

func TestDoWithRetryReturnsNonRetryable4xxWithoutError(t *testing.T) {
	t.Parallel()
	resp, err := DoWithRetry(context.Background(), RetryConfig{MaxAttempts: 3}, func(context.Context) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(strings.NewReader(`{"error":"bad"}`)),
		}, nil
	})
	if err != nil {
		t.Fatalf("expected nil err for 4xx handoff, got %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 response, got %+v", resp)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != `{"error":"bad"}` {
		t.Fatalf("body = %q", body)
	}
}

func TestDoWithRetryExhausted429ReturnsStatusCode(t *testing.T) {
	t.Parallel()
	cfg := RetryConfig{
		MaxAttempts:       2,
		BaseDelay:         time.Millisecond,
		MaxDelay:          time.Millisecond,
		RespectRetryAfter: false,
		JitterFactor:      0,
	}
	attempts := 0
	resp, err := DoWithRetry(context.Background(), cfg, func(context.Context) (*http.Response, error) {
		attempts++
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("rate")),
		}, nil
	})
	if resp != nil {
		t.Fatalf("expected nil resp on exhausted 429, got %+v", resp)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	var re *RetryableError
	if !errors.As(err, &re) {
		t.Fatalf("err type = %T (%v), want RetryableError", err, err)
	}
	if re.StatusCode() != http.StatusTooManyRequests {
		t.Fatalf("StatusCode() = %d, want 429", re.StatusCode())
	}
}

func TestDoWithRetryMaxAttemptsIsTotalAttempts(t *testing.T) {
	t.Parallel()
	cfg := RetryConfig{
		MaxAttempts:  3,
		BaseDelay:    time.Millisecond,
		MaxDelay:     time.Millisecond,
		JitterFactor: 0,
	}
	attempts := 0
	_, err := DoWithRetry(context.Background(), cfg, func(context.Context) (*http.Response, error) {
		attempts++
		return nil, errors.New("network down")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3 (total including first)", attempts)
	}
}

func TestDoWithRetryRespectsRetryAfterWithoutDoubleBackoff(t *testing.T) {
	t.Parallel()
	cfg := RetryConfig{
		MaxAttempts:       2,
		BaseDelay:         50 * time.Millisecond,
		MaxDelay:          time.Second,
		RespectRetryAfter: true,
		JitterFactor:      0,
	}
	attempts := 0
	start := time.Now()
	_, err := DoWithRetry(context.Background(), cfg, func(context.Context) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			h := make(http.Header)
			h.Set("Retry-After", "0")
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     h,
				Body:       io.NopCloser(strings.NewReader("rate")),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	// With Retry-After:0 and skipBackoff, should not pay BaseDelay on second attempt.
	if elapsed >= 40*time.Millisecond {
		t.Fatalf("elapsed %v suggests exponential backoff ran after Retry-After", elapsed)
	}
}

func TestStatusErrorStatusCode(t *testing.T) {
	t.Parallel()
	err := &StatusError{Code: 403, Body: "forbidden"}
	if err.StatusCode() != 403 {
		t.Fatalf("StatusCode() = %d", err.StatusCode())
	}
}
