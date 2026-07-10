package helps

import (
	"bytes"
	"testing"

	"github.com/tidwall/gjson"
)

func TestNormalizeDeepSeekOpenAIUsageJSONAddsPromptCachedTokens(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":64,"completion_tokens":8,"total_tokens":72,"prompt_cache_hit_tokens":32,"prompt_cache_miss_tokens":32}}`)

	out := NormalizeDeepSeekOpenAIUsage(body)

	if got := gjson.GetBytes(out, "usage.prompt_tokens_details.cached_tokens").Int(); got != 32 {
		t.Fatalf("usage.prompt_tokens_details.cached_tokens = %d, want 32", got)
	}
	if got := gjson.GetBytes(out, "usage.prompt_cache_hit_tokens").Int(); got != 32 {
		t.Fatalf("usage.prompt_cache_hit_tokens = %d, want 32", got)
	}
}

func TestNormalizeDeepSeekOpenAIUsageSSEAddsPromptCachedTokens(t *testing.T) {
	line := []byte(`data: {"usage":{"prompt_tokens":64,"completion_tokens":8,"total_tokens":72,"prompt_cache_hit_tokens":32}}`)

	out := NormalizeDeepSeekOpenAIUsage(line)

	if !bytes.HasPrefix(out, []byte("data: ")) {
		t.Fatalf("normalized line must keep SSE data prefix, got %q", string(out))
	}
	if got := gjson.GetBytes(JSONPayload(out), "usage.prompt_tokens_details.cached_tokens").Int(); got != 32 {
		t.Fatalf("usage.prompt_tokens_details.cached_tokens = %d, want 32", got)
	}
}

func TestNormalizeDeepSeekOpenAIUsageKeepsExistingStandardField(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":64,"completion_tokens":8,"total_tokens":72,"prompt_cache_hit_tokens":32,"prompt_tokens_details":{"cached_tokens":7}}}`)

	out := NormalizeDeepSeekOpenAIUsage(body)

	if !bytes.Equal(out, body) {
		t.Fatalf("body should remain unchanged when standard cached_tokens already exists, got %s", string(out))
	}
}

func TestParseOpenAIUsageDeepSeekPromptCacheHitTokens(t *testing.T) {
	data := []byte(`{"usage":{"prompt_tokens":64,"completion_tokens":8,"total_tokens":72,"prompt_cache_hit_tokens":32,"prompt_cache_miss_tokens":32}}`)

	detail := ParseOpenAIUsage(data)

	if detail.InputTokens != 64 {
		t.Fatalf("input tokens = %d, want 64", detail.InputTokens)
	}
	if detail.OutputTokens != 8 {
		t.Fatalf("output tokens = %d, want 8", detail.OutputTokens)
	}
	if detail.TotalTokens != 72 {
		t.Fatalf("total tokens = %d, want 72", detail.TotalTokens)
	}
	if detail.CachedTokens != 32 {
		t.Fatalf("cached tokens = %d, want 32", detail.CachedTokens)
	}
}

func TestParseOpenAIUsagePrefersStandardCachedTokensOverDeepSeekFallback(t *testing.T) {
	data := []byte(`{"usage":{"prompt_tokens":64,"completion_tokens":8,"total_tokens":72,"prompt_cache_hit_tokens":32,"prompt_tokens_details":{"cached_tokens":7}}}`)

	detail := ParseOpenAIUsage(data)

	if detail.CachedTokens != 7 {
		t.Fatalf("cached tokens = %d, want 7", detail.CachedTokens)
	}
}
