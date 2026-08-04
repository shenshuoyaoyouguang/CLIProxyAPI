package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestBuildOpenAIResponsesStreamErrorChunk(t *testing.T) {
	chunk := BuildOpenAIResponsesStreamErrorChunk(http.StatusInternalServerError, "unexpected EOF", 0)
	var payload map[string]any
	if err := json.Unmarshal(chunk, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["type"] != "error" {
		t.Fatalf("type = %v, want %q", payload["type"], "error")
	}
	if payload["code"] != "internal_server_error" {
		t.Fatalf("code = %v, want %q", payload["code"], "internal_server_error")
	}
	if payload["message"] != "unexpected EOF" {
		t.Fatalf("message = %v, want %q", payload["message"], "unexpected EOF")
	}
	if payload["sequence_number"] != float64(0) {
		t.Fatalf("sequence_number = %v, want %v", payload["sequence_number"], 0)
	}
}

func TestBuildOpenAIResponsesStreamErrorChunkExtractsHTTPErrorBody(t *testing.T) {
	chunk := BuildOpenAIResponsesStreamErrorChunk(
		http.StatusInternalServerError,
		`{"error":{"message":"oops","type":"server_error","code":"internal_server_error"}}`,
		0,
	)
	var payload map[string]any
	if err := json.Unmarshal(chunk, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["type"] != "error" {
		t.Fatalf("type = %v, want %q", payload["type"], "error")
	}
	if payload["code"] != "internal_server_error" {
		t.Fatalf("code = %v, want %q", payload["code"], "internal_server_error")
	}
	if payload["message"] != "oops" {
		t.Fatalf("message = %v, want %q", payload["message"], "oops")
	}
}

func TestBuildOpenAIResponsesStreamFailedChunk(t *testing.T) {
	chunk := BuildOpenAIResponsesStreamFailedChunk(http.StatusInternalServerError, "unexpected EOF", 0, "", 0)
	var payload map[string]any
	if err := json.Unmarshal(chunk, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["type"] != "response.failed" {
		t.Fatalf("type = %v, want %q", payload["type"], "response.failed")
	}
	if payload["sequence_number"] != float64(0) {
		t.Fatalf("sequence_number = %v, want %v", payload["sequence_number"], 0)
	}
	response, ok := payload["response"].(map[string]any)
	if !ok {
		t.Fatalf("response = %v, want object", payload["response"])
	}
	if response["status"] != "failed" {
		t.Fatalf("response.status = %v, want %q", response["status"], "failed")
	}
	if response["object"] != "response" {
		t.Fatalf("response.object = %v, want %q", response["object"], "response")
	}
	if response["output"] == nil {
		t.Fatalf("response.output = nil, want []")
	}
	if _, ok := response["output"].([]any); !ok {
		t.Fatalf("response.output = %T, want []any", response["output"])
	}
	errorDetail, ok := response["error"].(map[string]any)
	if !ok {
		t.Fatalf("response.error = %v, want object", response["error"])
	}
	if errorDetail["code"] != "server_error" {
		t.Fatalf("error.code = %v, want %q", errorDetail["code"], "server_error")
	}
	if errorDetail["message"] != "unexpected EOF" {
		t.Fatalf("error.message = %v, want %q", errorDetail["message"], "unexpected EOF")
	}
	if errorDetail["type"] != "server_error" {
		t.Fatalf("error.type = %v, want %q", errorDetail["type"], "server_error")
	}
	if errorDetail["param"] != nil {
		t.Fatalf("error.param = %v, want nil", errorDetail["param"])
	}
}

func TestOpenAIResponsesFailedErrorCode(t *testing.T) {
	tests := []struct {
		name   string
		status int
		code   string
		want   string
	}{
		{name: "transport failure", status: http.StatusInternalServerError, code: "internal_server_error", want: "server_error"},
		{name: "transport timeout", status: http.StatusRequestTimeout, code: "request_timeout", want: "server_error"},
		{name: "authentication failure", status: http.StatusUnauthorized, code: "invalid_api_key", want: "server_error"},
		{name: "permission failure", status: http.StatusForbidden, code: "permission_denied", want: "server_error"},
		{name: "model not found", status: http.StatusNotFound, code: "model_not_found", want: "invalid_prompt"},
		{name: "overloaded", status: http.StatusServiceUnavailable, code: "server_overloaded", want: "server_error"},
		{name: "rate limit", status: http.StatusTooManyRequests, code: "rate_limit_exceeded", want: "rate_limit_exceeded"},
		{name: "bad request", status: http.StatusBadRequest, code: "invalid_request_error", want: "invalid_prompt"},
		{name: "image code", status: http.StatusBadRequest, code: "invalid_image", want: "invalid_image"},
		{name: "known code preserved", status: http.StatusInternalServerError, code: "server_error", want: "server_error"},
		{name: "data residency preserved", status: http.StatusBadRequest, code: "data_residency_mismatch", want: "data_residency_mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := openAIResponsesFailedErrorCode(test.status, test.code); got != test.want {
				t.Fatalf("code = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBuildOpenAIResponsesStreamFailedChunkSynthesizesID(t *testing.T) {
	tests := []struct {
		name       string
		responseID string
		wantPrefix string
		wantExact  string
	}{
		{name: "empty id synthesized", responseID: "", wantPrefix: "resp_"},
		{name: "known id preserved", responseID: "resp_known", wantExact: "resp_known"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			chunk := BuildOpenAIResponsesStreamFailedChunk(http.StatusInternalServerError, "unexpected EOF", 0, test.responseID, 0)
			var payload map[string]any
			if err := json.Unmarshal(chunk, &payload); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			response, ok := payload["response"].(map[string]any)
			if !ok {
				t.Fatalf("response = %v, want object", payload["response"])
			}
			id, _ := response["id"].(string)
			if test.wantExact != "" {
				if id != test.wantExact {
					t.Fatalf("response.id = %q, want %q", id, test.wantExact)
				}
				return
			}
			if !strings.HasPrefix(id, test.wantPrefix) || strings.TrimPrefix(id, test.wantPrefix) == "" {
				t.Fatalf("response.id = %q, want %q prefix with non-empty suffix", id, test.wantPrefix)
			}
		})
	}
}

func TestBuildOpenAIResponsesStreamFailedChunkCarriesIdentity(t *testing.T) {
	chunk := BuildOpenAIResponsesStreamFailedChunk(http.StatusInternalServerError, "unexpected EOF", 0, "resp_xyz", 1700000000)
	var payload map[string]any
	if err := json.Unmarshal(chunk, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	response, ok := payload["response"].(map[string]any)
	if !ok {
		t.Fatalf("response = %v, want object", payload["response"])
	}
	if response["id"] != "resp_xyz" {
		t.Fatalf("response.id = %v, want %q", response["id"], "resp_xyz")
	}
	if response["created_at"] != float64(1700000000) {
		t.Fatalf("response.created_at = %v, want %v", response["created_at"], 1700000000)
	}
}

func TestBuildOpenAIResponsesStreamFailedChunkExtractsHTTPErrorBody(t *testing.T) {
	chunk := BuildOpenAIResponsesStreamFailedChunk(
		http.StatusInternalServerError,
		`{"error":{"message":"oops","type":"server_error","code":"internal_server_error"}}`,
		0,
		"",
		0,
	)
	var payload map[string]any
	if err := json.Unmarshal(chunk, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	response, ok := payload["response"].(map[string]any)
	if !ok {
		t.Fatalf("response = %v, want object", payload["response"])
	}
	errorDetail, ok := response["error"].(map[string]any)
	if !ok {
		t.Fatalf("response.error = %v, want object", response["error"])
	}
	if errorDetail["code"] != "server_error" {
		t.Fatalf("error.code = %v, want %q", errorDetail["code"], "server_error")
	}
	if errorDetail["message"] != "oops" {
		t.Fatalf("error.message = %v, want %q", errorDetail["message"], "oops")
	}
}
