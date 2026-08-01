package gemini

import (
	"bytes"
	"context"

	translatorcommon "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/common"
)

// emptyGeminiResponseEnvelope 是 Gemini API 的有效空响应 envelope：
// 无内容、finishReason=STOP、零 usage。用于上游返回空 body 时避免透传空字节。
var emptyGeminiResponseEnvelope = []byte(`{"candidates":[{"finishReason":"STOP","index":0,"content":{"parts":[],"role":"model"}}],"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0,"totalTokenCount":0}}`)

// PassthroughGeminiResponseStream forwards Gemini responses unchanged.
func PassthroughGeminiResponseStream(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) [][]byte {
	if bytes.HasPrefix(rawJSON, []byte("data:")) {
		rawJSON = bytes.TrimSpace(rawJSON[5:])
	}

	if bytes.Equal(rawJSON, []byte("[DONE]")) {
		return [][]byte{}
	}

	// 空 body 不透传，避免下游解析到空 SSE 帧。
	if len(bytes.TrimSpace(rawJSON)) == 0 {
		return [][]byte{}
	}

	return [][]byte{rawJSON}
}

// PassthroughGeminiResponseNonStream forwards Gemini responses unchanged.
func PassthroughGeminiResponseNonStream(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) []byte {
	// 空/malformed body 归一化为有效空 envelope，而非透传空字节。
	if len(bytes.TrimSpace(rawJSON)) == 0 {
		return emptyGeminiResponseEnvelope
	}
	return rawJSON
}

func GeminiTokenCount(ctx context.Context, count int64) []byte {
	return translatorcommon.GeminiTokenCountJSON(count)
}
