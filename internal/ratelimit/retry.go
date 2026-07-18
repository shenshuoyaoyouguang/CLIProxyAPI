package ratelimit

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// RetryConfig configures retry behavior for upstream HTTP calls.
type RetryConfig struct {
	// MaxAttempts is the total number of attempts including the first try.
	MaxAttempts       int
	BaseDelay         time.Duration
	MaxDelay          time.Duration
	RespectRetryAfter bool
	JitterFactor      float64 // 0-1
}

// DefaultRetryConfig is the default retry configuration.
var DefaultRetryConfig = RetryConfig{
	MaxAttempts:       3,
	BaseDelay:         500 * time.Millisecond,
	MaxDelay:          30 * time.Second,
	RespectRetryAfter: true,
	JitterFactor:      0.25,
}

// RetryableError marks a retryable upstream failure.
type RetryableError struct {
	Err        error
	HTTPStatus int
	RetryAfter time.Duration
}

func (e *RetryableError) Error() string {
	if e == nil {
		return "retryable error"
	}
	if e.RetryAfter > 0 && e.Err != nil {
		return e.Err.Error() + " (retry after " + e.RetryAfter.String() + ")"
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "retryable error"
}

func (e *RetryableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// StatusCode implements the status-code interface used by handlers and the auth conductor.
func (e *RetryableError) StatusCode() int {
	if e == nil {
		return 0
	}
	return e.HTTPStatus
}

// IsRetryable reports whether err is a RetryableError.
func IsRetryable(err error) bool {
	var re *RetryableError
	return errors.As(err, &re)
}

// StatusError is a non-retryable HTTP status error.
type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string {
	if e == nil {
		return "status error"
	}
	return "status " + strconv.Itoa(e.Code) + ": " + e.Body
}

// StatusCode implements the status-code interface used by handlers and the auth conductor.
func (e *StatusError) StatusCode() int {
	if e == nil {
		return 0
	}
	return e.Code
}

// RetryExhaustedError reports that all attempts failed without a final HTTP status.
type RetryExhaustedError struct {
	Attempts int
}

func (e *RetryExhaustedError) Error() string {
	if e == nil {
		return "retry exhausted"
	}
	return "retry exhausted after " + strconv.Itoa(e.Attempts) + " attempts"
}

// DoWithRetry executes fn with retries for network errors, 429, and 5xx.
//
// Contract:
//   - Any final 2xx response is returned as (resp, nil).
//   - Non-retryable 4xx (except 429) is returned as (resp, nil) so callers can
//     read status/body exactly like a single Do path.
//   - Exhausted network / 429 / 5xx failures return (nil, err) with the body
//     already closed; err implements StatusCode() when an HTTP status is known.
func DoWithRetry(ctx context.Context, config RetryConfig, fn func(context.Context) (*http.Response, error)) (*http.Response, error) {
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = DefaultRetryConfig.MaxAttempts
	}
	if config.BaseDelay <= 0 {
		config.BaseDelay = DefaultRetryConfig.BaseDelay
	}
	if config.MaxDelay <= 0 {
		config.MaxDelay = DefaultRetryConfig.MaxDelay
	}

	var lastErr error
	skipBackoff := false

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		if attempt > 1 && !skipBackoff {
			delay := config.BaseDelay * time.Duration(math.Pow(2, float64(attempt-2)))
			if delay > config.MaxDelay {
				delay = config.MaxDelay
			}
			if config.JitterFactor > 0 {
				jitter := time.Duration(float64(delay) * config.JitterFactor * (2*rand.Float64() - 1))
				delay += jitter
				if delay < 0 {
					delay = 0
				}
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		skipBackoff = false

		resp, err := fn(ctx)
		if err != nil {
			lastErr = err
			if attempt == config.MaxAttempts {
				return nil, lastErr
			}
			continue
		}
		if resp == nil {
			lastErr = errors.New("nil response")
			if attempt == config.MaxAttempts {
				return nil, lastErr
			}
			continue
		}

		// Any 2xx is success (matches executor legacy acceptance).
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}

		// 429 - retryable
		if resp.StatusCode == http.StatusTooManyRequests {
			hasRetryGuidance := resp.Header.Get("Retry-After") != "" || resp.Header.Get("X-RateLimit-Reset") != ""
			retryAfter := parseRetryAfter(resp)
			_ = resp.Body.Close()
			lastErr = &RetryableError{
				Err:        &StatusError{Code: resp.StatusCode, Body: "rate limited"},
				HTTPStatus: resp.StatusCode,
				RetryAfter: retryAfter,
			}
			if attempt == config.MaxAttempts {
				return nil, lastErr
			}
			if config.RespectRetryAfter && hasRetryGuidance {
				if retryAfter > config.MaxDelay {
					retryAfter = config.MaxDelay
				}
				if retryAfter > 0 {
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(retryAfter):
					}
				}
				// Retry-After (or zero = retry immediately) already applied;
				// do not also pay exponential backoff on the next attempt.
				skipBackoff = true
			}
			continue
		}

		// 5xx - retryable
		if resp.StatusCode >= 500 && resp.StatusCode < 600 {
			_ = resp.Body.Close()
			lastErr = &RetryableError{
				Err:        &StatusError{Code: resp.StatusCode, Body: "server error"},
				HTTPStatus: resp.StatusCode,
			}
			if attempt == config.MaxAttempts {
				return nil, lastErr
			}
			continue
		}

		// Non-retryable HTTP response: hand body/status to caller unchanged.
		return resp, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, &RetryExhaustedError{Attempts: config.MaxAttempts}
}

func parseRetryAfter(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
		if t, err := http.ParseTime(v); err == nil {
			d := time.Until(t)
			if d < 0 {
				return 0
			}
			return d
		}
	}
	if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
		if unix, err := strconv.ParseInt(v, 10, 64); err == nil {
			d := time.Until(time.Unix(unix, 0))
			if d < 0 {
				return 0
			}
			return d
		}
	}
	return 0
}
