package helps

import (
	"io"
	"net/http"
	"strings"
	"time"
)

// RetryDecision represents whether a stream error should be retried.
type RetryDecision int

const (
	// RetryNone indicates the error should not be retried.
	RetryNone RetryDecision = iota
	// RetryWithBackoff indicates the request should be retried after a backoff delay.
	RetryWithBackoff
	// RetryImmediately indicates the request should be retried immediately (e.g. 429 with Retry-After=0).
	RetryImmediately
)

// statusErrChecker is an interface matching the statusErr type used in executor packages.
// RetryAfter returns the delay parsed from the Retry-After header; nil when unset or zero.
type statusErrChecker interface {
	StatusCode() int
	Error() string
	RetryAfter() *time.Duration
}

// statusCodeOnlyChecker is a fallback interface for error types that only
// expose StatusCode() without Retry-After information (e.g. legacy executors
// or non-HTTP status errors). Those are still classified but with retryAfter=nil.
type statusCodeOnlyChecker interface {
	StatusCode() int
	Error() string
}

// ClassifyStreamError determines the retry decision for a stream reading error.
// gotSSEData indicates whether any SSE data was received before the error occurred.
func ClassifyStreamError(err error, gotSSEData bool) RetryDecision {
	if err == nil {
		return RetryNone
	}
	// Once SSE data has been received, retrying is unsafe (could produce duplicates).
	if gotSSEData {
		return RetryNone
	}
	// Check for statusErr (HTTP errors from upstream) carrying Retry-After.
	if se, ok := err.(statusErrChecker); ok {
		return classifyHTTPError(se.StatusCode(), se.Error(), se.RetryAfter())
	}
	// Fallback for error types that only implement StatusCode() (no Retry-After).
	if se, ok := err.(statusCodeOnlyChecker); ok {
		return classifyHTTPError(se.StatusCode(), se.Error(), nil)
	}
	// Network-level errors.
	if err == io.ErrUnexpectedEOF {
		return RetryWithBackoff
	}
	// Connection errors are generally retryable.
	if isConnectionError(err) {
		return RetryWithBackoff
	}
	// Default: don't retry unknown errors.
	return RetryNone
}

// classifyHTTPError classifies an HTTP error for retry decisions.
// retryAfter is the delay parsed from the Retry-After header; nil when unset.
func classifyHTTPError(statusCode int, body string, retryAfter *time.Duration) RetryDecision {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		// Auth failures are never retryable with the same credential.
		return RetryNone
	case http.StatusBadRequest:
		// Some 400 errors are permanent (invalid params, context too long).
		if isPermanentBadRequest(body) {
			return RetryNone
		}
		// Other 400s might be transient (e.g. provider-specific flakes).
		return RetryWithBackoff
	case http.StatusUnprocessableEntity:
		return RetryNone
	case http.StatusTooManyRequests:
		// When upstream specifies Retry-After > 0, honour it via backoff
		// to avoid amplifying rate limiting with immediate retries.
		if retryAfter != nil && *retryAfter > 0 {
			return RetryWithBackoff
		}
		return RetryImmediately
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return RetryWithBackoff
	case http.StatusInternalServerError:
		// Server errors are generally retryable.
		return RetryWithBackoff
	default:
		return RetryNone
	}
}

// isPermanentBadRequest checks if a 400 response contains a permanent error.
func isPermanentBadRequest(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "invalid_request_error") ||
		strings.Contains(lower, "invalid_params") ||
		strings.Contains(lower, "invalid_argument") ||
		strings.Contains(lower, "context_length_exceeded") ||
		strings.Contains(lower, "max_context_window")
}

// isConnectionError checks if an error is a network connection error.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "TLS handshake") ||
		strings.Contains(msg, "i/o timeout")
}
