package helps

import (
	"context"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// EmitWebSocketResponseEvent delivers an upstream WebSocket response frame to any configured observer.
func EmitWebSocketResponseEvent(ctx context.Context, opts cliproxyexecutor.Options, auth *cliproxyauth.Auth, provider, model string, payload []byte) {
	if opts.WebSocketResponseObserver == nil || len(payload) == 0 {
		return
	}
	var authID, authLabel, authType string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, _ = auth.AccountInfo()
	}
	eventType := gjson.GetBytes(payload, "type").String()
	var requestID string
	var traceID string
	if opts.Metadata != nil {
		if reqID, ok := opts.Metadata["request_id"].(string); ok {
			requestID = reqID
		}
		if trID, ok := opts.Metadata["trace_id"].(string); ok {
			traceID = trID
		}
	}
	requestedModel := PayloadRequestedModel(opts, model)
	// Contain a panicking observer: when a caller bypasses the plugin host (which
	// already fuses panicking plugins) and injects its own observer, a panic here
	// would otherwise unwind straight into the executor's websocket read loop.
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Errorf("helps: websocket response observer panicked for provider %s: %v", provider, recovered)
		}
	}()
	opts.WebSocketResponseObserver(ctx, cliproxyexecutor.WebSocketResponseEvent{
		RequestID:      requestID,
		TraceID:        traceID,
		SourceFormat:   opts.SourceFormat.String(),
		Model:          model,
		RequestedModel: requestedModel,
		Provider:       provider,
		AuthID:         authID,
		AuthLabel:      authLabel,
		AuthType:       authType,
		EventType:      eventType,
		Payload:        payload,
		Metadata:       opts.Metadata,
	})
}
