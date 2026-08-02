// Package thinking provides unified thinking configuration processing logic.
package thinking

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

// statusCoder mirrors the duck-typed interface used by the HTTP/auth layers
// (see sdk/cliproxy/auth/conductor.go:statusCodeFromError and the codex alpha
// search handler in internal/api/server.go) to map an error to an HTTP status
// code. It is duplicated here so the thinking package tests do not need to
// import the sdk (which would create an import cycle through provider
// appliers). The behaviour under test is identical: an error implementing
// `StatusCode() int` must surface that code via errors.As.
type statusCoder interface {
	StatusCode() int
}

// TestThinkingError_StatusCodeReturnsBadRequest pins the contract that every
// ThinkingError maps to HTTP 400 Bad Request. Downstream HTTP handlers and
// the auth conductor rely on this mapping to convert ThinkingError values
// into the correct 4xx response instead of falling back to 5xx.
func TestThinkingError_StatusCodeReturnsBadRequest(t *testing.T) {
	cases := []struct {
		name string
		err  *ThinkingError
	}{
		{name: "invalid suffix", err: NewThinkingError(ErrInvalidSuffix, "suffix is malformed")},
		{name: "unknown level", err: NewThinkingError(ErrUnknownLevel, "level not recognised")},
		{name: "thinking not supported", err: NewThinkingErrorWithModel(ErrThinkingNotSupported, "thinking unavailable", "claude-haiku-4-5")},
		{name: "level not supported", err: NewThinkingErrorWithModel(ErrLevelNotSupported, "level mode unavailable", "custom-model")},
		{name: "budget out of range", err: NewThinkingErrorWithModel(ErrBudgetOutOfRange, "budget exceeds maximum", "claude-sonnet-4-5")},
		{name: "provider mismatch", err: NewThinkingError(ErrProviderMismatch, "provider does not match model")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.StatusCode(); got != http.StatusBadRequest {
				t.Fatalf("StatusCode() = %d, want %d", got, http.StatusBadRequest)
			}
		})
	}
}

// TestThinkingError_SatisfiesStatusCoderInterface verifies that *ThinkingError
// satisfies the duck-typed `StatusCode() int` interface used by the HTTP and
// auth layers. If this test fails, ThinkingError values will be reported to
// clients as 5xx instead of 4xx.
func TestThinkingError_SatisfiesStatusCoderInterface(t *testing.T) {
	var s statusCoder = (*ThinkingError)(nil)
	if s == nil {
		t.Fatalf("(*ThinkingError)(nil) must satisfy statusCoder interface")
	}

	err := NewThinkingError(ErrBudgetOutOfRange, "budget 64000 exceeds max 20000")
	sc, ok := interface{}(err).(statusCoder)
	if !ok {
		t.Fatalf("ThinkingError does not satisfy statusCoder interface")
	}
	if got := sc.StatusCode(); got != http.StatusBadRequest {
		t.Fatalf("StatusCode() = %d, want %d", got, http.StatusBadRequest)
	}
}

// TestThinkingError_ErrorsAsExtractsStatusCode mirrors the exact pattern used
// in sdk/cliproxy/auth/conductor.go:statusCodeFromError. When a ThinkingError
// is wrapped (e.g., via fmt.Errorf("...: %w", err)) the conductor must still
// be able to extract the original 400 status code via errors.As.
func TestThinkingError_ErrorsAsExtractsStatusCode(t *testing.T) {
	base := NewThinkingErrorWithModel(ErrBudgetOutOfRange, "budget 64000 exceeds max 20000", "claude-sonnet-4-5")

	t.Run("errors.Join wrapped", func(t *testing.T) {
		wrapped := errors.Join(errors.New("apply pipeline failed"), base)
		var sc statusCoder
		if !errors.As(wrapped, &sc) {
			t.Fatalf("errors.As failed to find statusCoder on wrapped ThinkingError")
		}
		if sc == nil {
			t.Fatalf("statusCoder extracted from wrapped error is nil")
		}
		if got := sc.StatusCode(); got != http.StatusBadRequest {
			t.Fatalf("extracted StatusCode() = %d, want %d", got, http.StatusBadRequest)
		}
	})

	t.Run("fmt.Errorf %w wrapped", func(t *testing.T) {
		wrapped := fmt.Errorf("apply pipeline: %w", base)
		var sc statusCoder
		if !errors.As(wrapped, &sc) {
			t.Fatalf("errors.As failed to find statusCoder on fmt.Errorf-wrapped ThinkingError")
		}
		if got := sc.StatusCode(); got != http.StatusBadRequest {
			t.Fatalf("extracted StatusCode() from fmt-wrapped error = %d, want %d", got, http.StatusBadRequest)
		}
	})
}

// TestThinkingError_ErrorMethodReturnsMessage pins the contract that
// Error() returns the human-readable message verbatim, without any code
// prefix. HTTP handlers join this message into JSON error bodies, so the
// format must remain stable.
func TestThinkingError_ErrorMethodReturnsMessage(t *testing.T) {
	msg := "budget 64000 exceeds maximum 20000 for claude-sonnet-4-5"
	err := NewThinkingErrorWithModel(ErrBudgetOutOfRange, msg, "claude-sonnet-4-5")
	if got := err.Error(); got != msg {
		t.Fatalf("Error() = %q, want %q", got, msg)
	}
}
