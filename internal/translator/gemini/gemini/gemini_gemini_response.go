package gemini

import (
	"bytes"
	"context"

	translatorcommon "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/common"
)

// emptyGeminiResponseEnvelope is a valid empty Gemini API response envelope:
// no content, finishReason=STOP, zero usage. Used when the upstream returns an
// empty body so no empty bytes are passed through.
var emptyGeminiResponseEnvelope = []byte(`{"candidates":[{"finishReason":"STOP","index":0,"content":{"parts":[],"role":"model"}}],"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0,"totalTokenCount":0}}`)

// PassthroughGeminiResponseStream forwards Gemini responses unchanged.
func PassthroughGeminiResponseStream(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) [][]byte {
	if bytes.HasPrefix(rawJSON, []byte("data:")) {
		rawJSON = bytes.TrimSpace(rawJSON[5:])
	}

	if bytes.Equal(rawJSON, []byte("[DONE]")) {
		return [][]byte{}
	}

	// Do not pass through an empty body: downstream would parse an empty SSE frame.
	if len(bytes.TrimSpace(rawJSON)) == 0 {
		return [][]byte{}
	}

	return [][]byte{rawJSON}
}

// PassthroughGeminiResponseNonStream forwards Gemini responses unchanged.
func PassthroughGeminiResponseNonStream(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) []byte {
	// Normalize an empty/malformed body to a valid empty envelope instead of
	// passing through empty bytes.
	if len(bytes.TrimSpace(rawJSON)) == 0 {
		return emptyGeminiResponseEnvelope
	}
	return rawJSON
}

func GeminiTokenCount(ctx context.Context, count int64) []byte {
	return translatorcommon.GeminiTokenCountJSON(count)
}
