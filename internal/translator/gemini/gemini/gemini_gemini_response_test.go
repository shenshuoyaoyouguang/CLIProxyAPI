package gemini

import (
	"context"
	"testing"

	"github.com/tidwall/gjson"
)

// TestPassthroughGeminiResponseNonStream_NormalizesEmptyBody 验证上游返回空 body 时，
// 非流式 passthrough 归一化为有效空 envelope，而非透传空字节（issue #31）。
func TestPassthroughGeminiResponseNonStream_NormalizesEmptyBody(t *testing.T) {
	cases := [][]byte{nil, []byte(""), []byte("   "), []byte("\n\t  \n")}
	for i, raw := range cases {
		out := PassthroughGeminiResponseNonStream(context.Background(), "gemini-3-flash", nil, nil, raw, nil)
		if len(out) == 0 {
			t.Fatalf("case %d: expected normalized empty envelope, got empty bytes", i)
		}
		if !gjson.ValidBytes(out) {
			t.Fatalf("case %d: normalized envelope must be valid JSON, got %s", i, string(out))
		}
		if got := gjson.GetBytes(out, "candidates.0.finishReason").String(); got != "STOP" {
			t.Fatalf("case %d: finishReason = %q, want STOP. envelope=%s", i, got, string(out))
		}
		if got := gjson.GetBytes(out, "usageMetadata.totalTokenCount").Int(); got != 0 {
			t.Fatalf("case %d: totalTokenCount = %d, want 0", i, got)
		}
	}
}

// TestPassthroughGeminiResponseNonStream_PreservesNonEmptyBody 验证非空 body 仍透传。
func TestPassthroughGeminiResponseNonStream_PreservesNonEmptyBody(t *testing.T) {
	raw := []byte(`{"candidates":[{"content":{"parts":[{"text":"hi"}],"role":"model"}}]}`)
	out := PassthroughGeminiResponseNonStream(context.Background(), "gemini-3-flash", nil, nil, raw, nil)
	if string(out) != string(raw) {
		t.Fatalf("non-empty body should pass through unchanged; got %s", string(out))
	}
}

// TestPassthroughGeminiResponseStream_EmptyBodyReturnsNoFrames 验证流式空 body 不产生帧。
func TestPassthroughGeminiResponseStream_EmptyBodyReturnsNoFrames(t *testing.T) {
	cases := [][]byte{nil, []byte(""), []byte("   "), []byte("\n\t  \n")}
	for i, raw := range cases {
		out := PassthroughGeminiResponseStream(context.Background(), "gemini-3-flash", nil, nil, raw, nil)
		if len(out) != 0 {
			t.Fatalf("case %d: expected no frames for empty body, got %d frames", i, len(out))
		}
	}
}
