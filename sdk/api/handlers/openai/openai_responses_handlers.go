// Package openai provides HTTP handlers for OpenAIResponses API endpoints.
// This package implements the OpenAIResponses-compatible API interface, including model listing
// and chat completion functionality. It supports both streaming and non-streaming responses,
// and manages a pool of clients to interact with backend services.
// The handlers translate OpenAIResponses API requests to the appropriate backend format and
// convert responses back to OpenAIResponses-compatible format.
package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	multiagentv2 "github.com/router-for-me/CLIProxyAPI/v7/internal/client/codex/optimize-multi-agent-v2"
	. "github.com/router-for-me/CLIProxyAPI/v7/internal/constant"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// OpenAIResponsesAPIHandler contains the handlers for OpenAIResponses API endpoints.
// It holds a pool of clients to interact with the backend service.
type OpenAIResponsesAPIHandler struct {
	*handlers.BaseAPIHandler
}

// NewOpenAIResponsesAPIHandler creates a new OpenAIResponses API handlers instance.
// It takes an BaseAPIHandler instance as input and returns an OpenAIResponsesAPIHandler.
//
// Parameters:
//   - apiHandlers: The base API handlers instance
//
// Returns:
//   - *OpenAIResponsesAPIHandler: A new OpenAIResponses API handlers instance
func NewOpenAIResponsesAPIHandler(apiHandlers *handlers.BaseAPIHandler) *OpenAIResponsesAPIHandler {
	return &OpenAIResponsesAPIHandler{
		BaseAPIHandler: apiHandlers,
	}
}

// HandlerType returns the identifier for this handler implementation.
func (h *OpenAIResponsesAPIHandler) HandlerType() string {
	return OpenaiResponse
}

// Models returns the OpenAIResponses-compatible model metadata supported by this handler.
func (h *OpenAIResponsesAPIHandler) Models() []map[string]any {
	// Get dynamic models from the global registry
	modelRegistry := registry.GetGlobalRegistry()
	return modelRegistry.GetAvailableModels("openai")
}

// OpenAIResponsesModels handles the /v1/models endpoint.
// It returns a list of available AI models with their capabilities
// and specifications in OpenAIResponses-compatible format.
func (h *OpenAIResponsesAPIHandler) OpenAIResponsesModels(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   h.Models(),
	})
}

func (h *OpenAIResponsesAPIHandler) prepareCodexMultiAgentV2Tools(c *gin.Context, payload []byte) []byte {
	if h == nil || h.Cfg == nil {
		return payload
	}

	requestCtx := context.Background()
	if c != nil && c.Request != nil {
		requestCtx = c.Request.Context()
	}
	requestCtx = context.WithValue(requestCtx, "gin", c)

	var requestHeaders http.Header
	if c != nil && c.Request != nil {
		requestHeaders = c.Request.Header
	}
	homeEnabled := h.AuthManager != nil && h.AuthManager.HomeEnabled()
	updated, prepared := multiagentv2.PrepareCodexMultiAgentV2Tools(
		requestCtx,
		requestHeaders,
		payload,
		h.Cfg.CodexOptimizeMultiAgentV2,
		homeEnabled,
	)
	if prepared && c != nil {
		c.Set(multiagentv2.CodexMultiAgentV2ToolsPreparedContextKey, true)
	}
	return updated
}

// Responses handles the /v1/responses endpoint.
// It determines whether the request is for a streaming or non-streaming response
// and calls the appropriate handler based on the model provider.
//
// Parameters:
//   - c: The Gin context containing the HTTP request and response
func (h *OpenAIResponsesAPIHandler) Responses(c *gin.Context) {
	rawJSON, err := handlers.ReadRequestBody(c)
	// If data retrieval fails, return a 400 Bad Request error.
	if err != nil {
		c.JSON(http.StatusBadRequest, handlers.ErrorResponse{
			Error: handlers.ErrorDetail{
				Message: fmt.Sprintf("Invalid request: %v", err),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	rawJSON = h.prepareCodexMultiAgentV2Tools(c, rawJSON)

	// Check if the client requested a streaming response.
	streamResult := gjson.GetBytes(rawJSON, "stream")
	if streamResult.Type == gjson.True {
		h.handleStreamingResponse(c, rawJSON)
	} else {
		h.handleNonStreamingResponse(c, rawJSON)
	}

}

func (h *OpenAIResponsesAPIHandler) Compact(c *gin.Context) {
	rawJSON, err := handlers.ReadRequestBody(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, handlers.ErrorResponse{
			Error: handlers.ErrorDetail{
				Message: fmt.Sprintf("Invalid request: %v", err),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	streamResult := gjson.GetBytes(rawJSON, "stream")
	if streamResult.Type == gjson.True {
		c.JSON(http.StatusBadRequest, handlers.ErrorResponse{
			Error: handlers.ErrorDetail{
				Message: "Streaming not supported for compact responses",
				Type:    "invalid_request_error",
			},
		})
		return
	}
	if streamResult.Exists() {
		if updated, err := sjson.DeleteBytes(rawJSON, "stream"); err == nil {
			rawJSON = updated
		}
	}

	c.Header("Content-Type", "application/json")
	modelName := gjson.GetBytes(rawJSON, "model").String()
	cliCtx, cliCancel := h.GetContextWithCancel(h, c, context.Background())
	stopKeepAlive := h.StartNonStreamingKeepAlive(c, cliCtx)
	resp, upstreamHeaders, errMsg := h.ExecuteWithAuthManager(cliCtx, h.HandlerType(), modelName, rawJSON, "responses/compact")
	stopKeepAlive()
	if errMsg != nil {
		h.WriteErrorResponse(c, errMsg)
		cliCancel(errMsg.Error)
		return
	}
	handlers.WriteUpstreamHeaders(c.Writer.Header(), upstreamHeaders)
	_, _ = c.Writer.Write(resp)
	cliCancel()
}

// handleNonStreamingResponse handles non-streaming chat completion responses
// for Gemini models. It selects a client from the pool, sends the request, and
// aggregates the response before sending it back to the client in OpenAIResponses format.
//
// Parameters:
//   - c: The Gin context containing the HTTP request and response
//   - rawJSON: The raw JSON bytes of the OpenAIResponses-compatible request
func (h *OpenAIResponsesAPIHandler) handleNonStreamingResponse(c *gin.Context, rawJSON []byte) {
	c.Header("Content-Type", "application/json")

	modelName := gjson.GetBytes(rawJSON, "model").String()
	cliCtx, cliCancel := h.GetContextWithCancel(h, c, context.Background())
	stopKeepAlive := h.StartNonStreamingKeepAlive(c, cliCtx)

	resp, upstreamHeaders, errMsg := h.ExecuteWithAuthManager(cliCtx, h.HandlerType(), modelName, rawJSON, "")
	stopKeepAlive()
	if errMsg != nil {
		h.WriteErrorResponse(c, errMsg)
		cliCancel(errMsg.Error)
		return
	}
	handlers.WriteUpstreamHeaders(c.Writer.Header(), upstreamHeaders)
	_, _ = c.Writer.Write(resp)
	cliCancel()
}

// handleStreamingResponse handles streaming responses for Gemini models.
// It establishes a streaming connection with the backend service and forwards
// the response chunks to the client in real-time using Server-Sent Events.
//
// Parameters:
//   - c: The Gin context containing the HTTP request and response
//   - rawJSON: The raw JSON bytes of the OpenAIResponses-compatible request
func (h *OpenAIResponsesAPIHandler) handleStreamingResponse(c *gin.Context, rawJSON []byte) {
	// Get the http.Flusher interface to manually flush the response.
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, handlers.ErrorResponse{
			Error: handlers.ErrorDetail{
				Message: "Streaming not supported",
				Type:    "server_error",
			},
		})
		return
	}

	// New core execution path
	modelName := gjson.GetBytes(rawJSON, "model").String()
	cliCtx, cliCancel := h.GetContextWithCancel(h, c, context.Background())
	dataChan, errChan, bootstrapCommitter := h.ExecuteStreamWithAuthManagerBootstrapCommit(cliCtx, h.HandlerType(), modelName, rawJSON, "")

	framer := &responsesSSEFramer{}
	h.forwardInitialResponsesStream(
		c,
		flusher,
		func(err error) { cliCancel(err) },
		dataChan,
		errChan,
		func() http.Header { return bootstrapCommitter.CommitContext(c.Request.Context()) },
		bootstrapCommitter.Headers,
		framer,
		handlers.StreamingKeepAliveInterval(h.Cfg),
	)
}

func (h *OpenAIResponsesAPIHandler) forwardInitialResponsesStream(
	c *gin.Context,
	flusher http.Flusher,
	cancel func(error),
	dataChan <-chan []byte,
	errChan <-chan *interfaces.ErrorMessage,
	commitHeaders func() http.Header,
	selectedHeaders func() http.Header,
	framer *responsesSSEFramer,
	keepAliveInterval time.Duration,
) {
	setSSEHeaders := func() {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("Access-Control-Allow-Origin", "*")
	}
	var keepAlive *time.Ticker
	var keepAliveC <-chan time.Time
	if keepAliveInterval > 0 {
		keepAlive = time.NewTicker(keepAliveInterval)
		keepAliveC = keepAlive.C
	}
	stopKeepAlive := func() {
		if keepAlive != nil {
			keepAlive.Stop()
			keepAlive = nil
			keepAliveC = nil
		}
	}
	defer stopKeepAlive()

	for {
		select {
		case <-c.Request.Context().Done():
			cancel(c.Request.Context().Err())
			return
		case errMsg, ok := <-errChan:
			if !ok {
				// Err channel closed cleanly; wait for data channel.
				errChan = nil
				continue
			}
			// Upstream failed immediately. Return proper error status and JSON.
			if selectedHeaders != nil {
				handlers.WriteUpstreamHeaders(c.Writer.Header(), selectedHeaders())
			}
			h.WriteErrorResponse(c, errMsg)
			if errMsg != nil {
				cancel(errMsg.Error)
			} else {
				cancel(nil)
			}
			return
		case chunk, ok := <-dataChan:
			var upstreamHeaders http.Header
			if selectedHeaders != nil {
				upstreamHeaders = selectedHeaders()
			}
			if !ok {
				select {
				case errMsg, okErr := <-errChan:
					if okErr && errMsg != nil {
						handlers.WriteUpstreamHeaders(c.Writer.Header(), upstreamHeaders)
						h.WriteErrorResponse(c, errMsg)
						cancel(errMsg.Error)
						return
					}
				default:
				}
				// Stream closed without data? Send headers and done.
				setSSEHeaders()
				handlers.WriteUpstreamHeaders(c.Writer.Header(), upstreamHeaders)
				_, _ = c.Writer.Write([]byte("\n"))
				flusher.Flush()
				cancel(nil)
				return
			}

			// Success! Set headers.
			setSSEHeaders()
			handlers.WriteUpstreamHeaders(c.Writer.Header(), upstreamHeaders)

			// Write first chunk logic (matching forwardResponsesStream)
			framer.WriteChunk(c.Writer, chunk)
			flusher.Flush()

			// Continue
			stopKeepAlive()
			h.forwardResponsesStream(c, flusher, cancel, dataChan, errChan, framer)
			return
		case <-keepAliveC:
			// Prefer a bootstrap failure that is already pending over committing
			// a heartbeat: before any byte is written the error can still be
			// returned as a proper JSON status instead of a committed SSE frame.
			select {
			case errMsg, ok := <-errChan:
				if ok && errMsg != nil {
					if selectedHeaders != nil {
						handlers.WriteUpstreamHeaders(c.Writer.Header(), selectedHeaders())
					}
					h.WriteErrorResponse(c, errMsg)
					cancel(errMsg.Error)
					return
				}
			default:
			}
			var upstreamHeaders http.Header
			if commitHeaders != nil {
				upstreamHeaders = commitHeaders()
			}
			if err := c.Request.Context().Err(); err != nil {
				cancel(err)
				return
			}
			setSSEHeaders()
			handlers.WriteUpstreamHeaders(c.Writer.Header(), upstreamHeaders)
			_, _ = c.Writer.Write([]byte(": keep-alive\n\n"))
			flusher.Flush()
			stopKeepAlive()
			h.forwardResponsesStream(c, flusher, cancel, dataChan, errChan, framer)
			return
		}
	}
}

// writeResponsesTerminalError writes the terminal SSE frame for a forwarding
// error: a response.failed lifecycle event when the stream has not yet ended
// with a terminal event, otherwise a non-terminal event: error frame.
func writeResponsesTerminalError(w io.Writer, framer *responsesSSEFramer, errMsg *interfaces.ErrorMessage) {
	if framer != nil {
		framer.Flush(w)
	}
	if errMsg == nil {
		return
	}
	status := http.StatusInternalServerError
	if errMsg.StatusCode > 0 {
		status = errMsg.StatusCode
	}
	errText := http.StatusText(status)
	if errMsg.Error != nil && errMsg.Error.Error() != "" {
		errText = errMsg.Error.Error()
	}
	if framer != nil && framer.terminalSeen {
		// A terminal lifecycle event already ended the stream; a trailing
		// response.failed would violate the single-terminal-event contract, so
		// fall back to the non-terminal error event for diagnostics.
		chunk := handlers.BuildOpenAIResponsesStreamErrorChunk(status, errText, 0)
		_, _ = fmt.Fprintf(w, "\nevent: error\ndata: %s\n\n", string(chunk))
		return
	}
	var responseID string
	var createdAt int64
	sequenceNumber := 0
	if framer != nil {
		responseID = framer.responseID
		createdAt = framer.responseCreatedAt
		if framer.hasLastSequenceNumber {
			sequenceNumber = framer.lastSequenceNumber + 1
		}
	}
	chunk := handlers.BuildOpenAIResponsesStreamFailedChunk(status, errText, sequenceNumber, responseID, createdAt)
	_, _ = fmt.Fprintf(w, "\nevent: response.failed\ndata: %s\n\n", string(chunk))
}

func (h *OpenAIResponsesAPIHandler) forwardResponsesStream(c *gin.Context, flusher http.Flusher, cancel func(error), data <-chan []byte, errs <-chan *interfaces.ErrorMessage, framer *responsesSSEFramer) {
	if framer == nil {
		framer = &responsesSSEFramer{}
	}
	h.ForwardStream(c, flusher, cancel, data, errs, handlers.StreamForwardOptions{
		WriteChunk: func(chunk []byte) {
			framer.WriteChunk(c.Writer, chunk)
		},
		WriteTerminalError: func(errMsg *interfaces.ErrorMessage) {
			writeResponsesTerminalError(c.Writer, framer, errMsg)
		},
		WriteDone: func() {
			framer.Flush(c.Writer)
			_, _ = c.Writer.Write([]byte("\n"))
		},
	})
}
