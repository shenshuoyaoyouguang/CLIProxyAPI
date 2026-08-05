package executor

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestClassifyBareJSONErrorStatus verifies that a bare-JSON upstream error
// chunk (emitted without an SSE data: prefix) is mapped to an accurate HTTP
// status code based on the OpenAI error.type convention. Previously the
// streaming path hardcoded 502 Bad Gateway for all bare-JSON errors, which
// caused conductor cooldown to misclassify rate-limit and auth errors as
// transient upstream failures (P1-5).
func TestClassifyBareJSONErrorStatus(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{
			name: "rate_limit_error maps to 429",
			body: `{"error":{"message":"slow down","type":"rate_limit_error","code":"rate_limit_exceeded"}}`,
			want: http.StatusTooManyRequests,
		},
		{
			name: "authentication_error maps to 401",
			body: `{"error":{"message":"bad key","type":"authentication_error","code":"invalid_api_key"}}`,
			want: http.StatusUnauthorized,
		},
		{
			name: "permission_error maps to 403",
			body: `{"error":{"message":"no quota","type":"permission_error","code":"insufficient_quota"}}`,
			want: http.StatusForbidden,
		},
		{
			name: "invalid_request_error maps to 400",
			body: `{"error":{"message":"bad request","type":"invalid_request_error","code":"context_length_exceeded"}}`,
			want: http.StatusBadRequest,
		},
		{
			name: "unrecognized type falls back to 502",
			body: `{"error":{"message":"weird","type":"some_new_type"}}`,
			want: http.StatusBadGateway,
		},
		{
			name: "missing type field falls back to 502",
			body: `{"error":{"message":"no type info"}}`,
			want: http.StatusBadGateway,
		},
		{
			name: "nested error object without type falls back to 502",
			body: `{"errors":[{"detail":"something"}]}`,
			want: http.StatusBadGateway,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyBareJSONErrorStatus([]byte(tc.body))
			assert.Equal(t, tc.want, got)
		})
	}
}
