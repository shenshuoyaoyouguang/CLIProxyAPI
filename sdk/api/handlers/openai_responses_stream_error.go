package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type openAIResponsesStreamErrorChunk struct {
	Type           string `json:"type"`
	Code           string `json:"code"`
	Message        string `json:"message"`
	SequenceNumber int    `json:"sequence_number"`
}

type openAIResponsesStreamFailedChunk struct {
	Type           string                              `json:"type"`
	SequenceNumber int                                 `json:"sequence_number"`
	Response       openAIResponsesStreamFailedResponse `json:"response"`
}

type openAIResponsesStreamFailedResponse struct {
	ID        string                           `json:"id"`
	Object    string                           `json:"object"`
	CreatedAt int64                            `json:"created_at"`
	Status    string                           `json:"status"`
	Error     openAIResponsesStreamErrorDetail `json:"error"`
	Output    []any                            `json:"output"`
	Usage     any                              `json:"usage"`
}

type openAIResponsesStreamErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Param   any    `json:"param"`
	Type    string `json:"type"`
}

func openAIResponsesStreamErrorCode(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "invalid_api_key"
	case http.StatusForbidden:
		return "insufficient_quota"
	case http.StatusTooManyRequests:
		return "rate_limit_exceeded"
	case http.StatusNotFound:
		return "model_not_found"
	case http.StatusRequestTimeout:
		return "request_timeout"
	default:
		if status >= http.StatusInternalServerError {
			return "internal_server_error"
		}
		if status >= http.StatusBadRequest {
			return "invalid_request_error"
		}
		return "unknown_error"
	}
}

// openAIResponsesStreamErrorType maps an HTTP status to the error type carried
// in the response.failed event's response.error object.
func openAIResponsesStreamErrorType(status int) string {
	if status <= 0 {
		status = http.StatusInternalServerError
	}
	switch status {
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	default:
		if status >= http.StatusInternalServerError {
			return "server_error"
		}
		return "invalid_request_error"
	}
}

// openAIResponsesStreamErrorDetails extracts the code, message and sequence
// number for a Responses stream error from an HTTP status and optional error
// text, honoring upstream JSON error payloads.
func openAIResponsesStreamErrorDetails(status int, errText string, sequenceNumber int) (code, message string, seq int) {
	if status <= 0 {
		status = http.StatusInternalServerError
	}
	if sequenceNumber < 0 {
		sequenceNumber = 0
	}
	seq = sequenceNumber

	message = strings.TrimSpace(errText)
	if message == "" {
		message = http.StatusText(status)
	}

	code = openAIResponsesStreamErrorCode(status)

	trimmed := strings.TrimSpace(errText)
	if trimmed != "" && json.Valid([]byte(trimmed)) {
		var payload map[string]any
		if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
			if t, ok := payload["type"].(string); ok && strings.TrimSpace(t) == "error" {
				if m, ok := payload["message"].(string); ok && strings.TrimSpace(m) != "" {
					message = strings.TrimSpace(m)
				}
				if v, ok := payload["code"]; ok && v != nil {
					if c, ok := v.(string); ok && strings.TrimSpace(c) != "" {
						code = strings.TrimSpace(c)
					} else {
						code = strings.TrimSpace(fmt.Sprint(v))
					}
				}
				if v, ok := payload["sequence_number"].(float64); ok && seq == 0 {
					seq = int(v)
				}
			}
			if e, ok := payload["error"].(map[string]any); ok {
				if m, ok := e["message"].(string); ok && strings.TrimSpace(m) != "" {
					message = strings.TrimSpace(m)
				}
				if v, ok := e["code"]; ok && v != nil {
					if c, ok := v.(string); ok && strings.TrimSpace(c) != "" {
						code = strings.TrimSpace(c)
					} else {
						code = strings.TrimSpace(fmt.Sprint(v))
					}
				}
			}
		}
	}

	if strings.TrimSpace(code) == "" {
		code = "unknown_error"
	}

	return code, message, seq
}

// BuildOpenAIResponsesStreamErrorChunk builds an OpenAI Responses streaming error chunk.
//
// Important: OpenAI's HTTP error bodies are shaped like {"error":{...}}; those are valid for
// non-streaming responses, but streaming clients validate SSE `data:` payloads against a union
// of chunks that requires a top-level `type` field.
func BuildOpenAIResponsesStreamErrorChunk(status int, errText string, sequenceNumber int) []byte {
	code, message, seq := openAIResponsesStreamErrorDetails(status, errText, sequenceNumber)

	data, err := json.Marshal(openAIResponsesStreamErrorChunk{
		Type:           "error",
		Code:           code,
		Message:        message,
		SequenceNumber: seq,
	})
	if err == nil {
		return data
	}

	// json.Marshal cannot fail on this plain struct; this is a defensive
	// fallback only.
	return []byte(`{"type":"error","code":"internal_server_error","message":"internal error","sequence_number":0}`)
}

// openAIResponsesFailedErrorCode maps an extracted error code to the
// response.error code vocabulary documented for the Responses API (e.g.
// server_error, rate_limit_exceeded, invalid_prompt). Codes already in that
// vocabulary pass through; everything else is derived from the HTTP status.
func openAIResponsesFailedErrorCode(status int, code string) string {
	switch strings.TrimSpace(code) {
	case "server_error", "rate_limit_exceeded", "invalid_prompt", "data_residency_mismatch", "bio_policy", "vector_store_timeout",
		"invalid_image", "invalid_image_format", "invalid_base64_image", "invalid_image_url", "image_too_large", "image_too_small",
		"image_parse_error", "image_content_policy_violation", "invalid_image_mode", "image_file_too_large",
		"unsupported_image_media_type", "empty_image_file", "failed_to_download_image", "image_file_not_found":
		return strings.TrimSpace(code)
	}
	switch status {
	case http.StatusTooManyRequests:
		return "rate_limit_exceeded"
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestTimeout:
		return "server_error"
	}
	if status >= http.StatusBadRequest && status < http.StatusInternalServerError {
		return "invalid_prompt"
	}
	return "server_error"
}

// BuildOpenAIResponsesStreamFailedChunk builds the data payload of a
// response.failed event, the terminal failure event of the Responses API
// lifecycle (OpenAI docs: emitted when a response fails). Unlike the generic
// `error` event, it carries the failed response object, so downstream
// consumers that validate terminal events see a complete stream instead of a
// 200 stream that ends without response.completed or response.failed.
// responseID and createdAt are the identity of the response as streamed in
// response.created; empty values keep the payload minimal when the stream
// failed before any response identity was seen.
func BuildOpenAIResponsesStreamFailedChunk(status int, errText string, sequenceNumber int, responseID string, createdAt int64) []byte {
	code, message, seq := openAIResponsesStreamErrorDetails(status, errText, sequenceNumber)
	code = openAIResponsesFailedErrorCode(status, code)
	if strings.TrimSpace(responseID) == "" {
		responseID = "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	}

	data, err := json.Marshal(openAIResponsesStreamFailedChunk{
		Type:           "response.failed",
		SequenceNumber: seq,
		Response: openAIResponsesStreamFailedResponse{
			ID:        responseID,
			Object:    "response",
			CreatedAt: createdAt,
			Status:    "failed",
			Error: openAIResponsesStreamErrorDetail{
				Code:    code,
				Message: message,
				Param:   nil,
				Type:    openAIResponsesStreamErrorType(status),
			},
			Output: []any{},
			Usage:  nil,
		},
	})
	if err == nil {
		return data
	}

	// json.Marshal cannot fail on this plain struct; this is a defensive
	// fallback only.
	return []byte(`{"type":"response.failed","sequence_number":0,"response":{"id":"","object":"response","created_at":0,"status":"failed","error":{"code":"internal_server_error","message":"internal error","param":null,"type":"server_error"},"output":[],"usage":null}}`)
}
