package responses

import (
	"context"

	translatorcommon "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/common"
	"github.com/tidwall/gjson"
)

// ConvertCodexResponseToOpenAIResponses converts OpenAI Chat Completions streaming chunks
// to OpenAI Responses SSE events (response.*).

func ConvertCodexResponseToOpenAIResponses(_ context.Context, _ string, _, _, rawJSON []byte, _ *any) [][]byte {
	if payload, ok := translatorcommon.SSEDataPayload(rawJSON); ok {
		out := make([]byte, 0, len(payload)+len("data: "))
		out = append(out, []byte("data: ")...)
		out = append(out, payload...)
		return [][]byte{out}
	}
	return [][]byte{rawJSON}
}

// ConvertCodexResponseToOpenAIResponsesNonStream builds a single Responses JSON
// from a non-streaming OpenAI Chat Completions response.
func ConvertCodexResponseToOpenAIResponsesNonStream(_ context.Context, _ string, _, _, rawJSON []byte, _ *any) []byte {
	rootResult := gjson.ParseBytes(rawJSON)
	// Verify this is a terminal response event.
	responseType := rootResult.Get("type").String()
	if responseType != "response.completed" && responseType != "response.incomplete" {
		return []byte{}
	}
	responseResult := rootResult.Get("response")
	return []byte(responseResult.Raw)
}
