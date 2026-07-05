package helps

import (
	"io"
	"testing"
)

// testStatusErr implements statusErrChecker for testing.
type testStatusErr struct {
	code int
	msg  string
}

func (e testStatusErr) StatusCode() int { return e.code }
func (e testStatusErr) Error() string   { return e.msg }

func TestClassifyStreamError_NilError(t *testing.T) {
	if decision := ClassifyStreamError(nil, false); decision != RetryNone {
		t.Fatalf("nil error should be RetryNone, got %d", decision)
	}
}

func TestClassifyStreamError_GotSSEData(t *testing.T) {
	err := io.ErrUnexpectedEOF
	if decision := ClassifyStreamError(err, true); decision != RetryNone {
		t.Fatalf("error with SSE data should be RetryNone, got %d", decision)
	}
}

func TestClassifyStreamError_UnexpectedEOF(t *testing.T) {
	if decision := ClassifyStreamError(io.ErrUnexpectedEOF, false); decision != RetryWithBackoff {
		t.Fatalf("UnexpectedEOF should be RetryWithBackoff, got %d", decision)
	}
}

func TestClassifyStreamError_HTTPAuthFailures(t *testing.T) {
	tests := []struct {
		code int
		msg  string
	}{
		{401, "authentication_error"},
		{403, "access denied"},
	}
	for _, tc := range tests {
		err := testStatusErr{code: tc.code, msg: tc.msg}
		if decision := ClassifyStreamError(err, false); decision != RetryNone {
			t.Fatalf("HTTP %d should be RetryNone, got %d", tc.code, decision)
		}
	}
}

func TestClassifyStreamError_BadRequest(t *testing.T) {
	tests := []struct {
		code int
		msg  string
		want RetryDecision
	}{
		{400, "invalid_request_error: bad input", RetryNone},
		{400, "invalid_params: missing field", RetryNone},
		{400, "INVALID_ARGUMENT", RetryNone},
		{400, "context_length_exceeded", RetryNone},
		{400, "max_context_window exceeded", RetryNone},
		{400, "some transient error", RetryWithBackoff},
	}
	for _, tc := range tests {
		err := testStatusErr{code: tc.code, msg: tc.msg}
		if decision := ClassifyStreamError(err, false); decision != tc.want {
			t.Fatalf("HTTP %d msg=%q: got %d, want %d", tc.code, tc.msg, decision, tc.want)
		}
	}
}

func TestClassifyStreamError_HTTP422(t *testing.T) {
	err := testStatusErr{code: 422, msg: "unprocessable"}
	if decision := ClassifyStreamError(err, false); decision != RetryNone {
		t.Fatalf("422 should be RetryNone, got %d", decision)
	}
}

func TestClassifyStreamError_HTTP429(t *testing.T) {
	err := testStatusErr{code: 429, msg: "rate limited"}
	if decision := ClassifyStreamError(err, false); decision != RetryImmediately {
		t.Fatalf("429 should be RetryImmediately, got %d", decision)
	}
}

func TestClassifyStreamError_ServerErrors(t *testing.T) {
	tests := []struct {
		code int
	}{
		{500}, {502}, {503}, {504},
	}
	for _, tc := range tests {
		err := testStatusErr{code: tc.code, msg: "server error"}
		if decision := ClassifyStreamError(err, false); decision != RetryWithBackoff {
			t.Fatalf("HTTP %d should be RetryWithBackoff, got %d", tc.code, decision)
		}
	}
}
