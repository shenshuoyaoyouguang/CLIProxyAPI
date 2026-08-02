// Package thinking benchmarks quantify the proxy-side overhead of the thinking
// pipeline (ApplyThinking) across request body sizes and provider targets.
//
// Dimension E of the reasoning test plan: proxy overhead. These benchmarks
// measure ns/op, B/op and allocs/op so that any future optimization decision is
// data-driven rather than speculative. Run with:
//
//	go test -run=^$ -bench=. -benchmem ./internal/thinking/
package thinking_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/claude"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/gemini"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/openai"
	"github.com/tidwall/gjson"
)

// benchBody builds a Claude-style request body padded to roughly targetKB
// kilobytes by filling a user message with filler text. This models the real
// cost driver: gjson/sjson scanning over large request payloads.
func benchBody(targetKB int, thinkingField string) []byte {
	filler := strings.Repeat("lorem ipsum dolor sit amet ", targetKB*40) // ~1KB per 40 reps
	return []byte(fmt.Sprintf(
		`{"model":"claude-sonnet-4-5","max_tokens":32000,%s"messages":[{"role":"user","content":%q}]}`,
		thinkingField, filler,
	))
}

func benchRegister(b *testing.B, provider string, models []*registry.ModelInfo) {
	b.Helper()
	reg := registry.GetGlobalRegistry()
	clientID := "bench-" + provider
	reg.RegisterClient(clientID, provider, models)
	b.Cleanup(func() { reg.UnregisterClient(clientID) })
}

// BenchmarkApplyThinking_BodySize measures how ApplyThinking overhead scales
// with request body size on the Claude budget path (same-family, suffix config).
func BenchmarkApplyThinking_BodySize(b *testing.B) {
	benchRegister(b, "claude", registry.GetClaudeModels())
	for _, kb := range []int{1, 50, 500} {
		body := benchBody(kb, "")
		b.Run(fmt.Sprintf("claude_suffix_%dkb", kb), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for i := 0; i < b.N; i++ {
				if _, err := thinking.ApplyThinking(body, "claude-sonnet-4-5(16384)", "claude", "claude", "claude"); err != nil {
					b.Fatalf("ApplyThinking: %v", err)
				}
			}
		})
	}
}

// BenchmarkApplyThinking_Passthrough measures the cheapest path: no thinking
// config present, so the pipeline should exit early.
func BenchmarkApplyThinking_Passthrough(b *testing.B) {
	benchRegister(b, "claude", registry.GetClaudeModels())
	body := benchBody(50, "")
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	for i := 0; i < b.N; i++ {
		if _, err := thinking.ApplyThinking(body, "claude-sonnet-4-5", "claude", "claude", "claude"); err != nil {
			b.Fatalf("ApplyThinking: %v", err)
		}
	}
}

// BenchmarkApplyThinking_CrossFamily measures the cost of the clamp path where
// an OpenAI-style level config is translated toward a Gemini budget model.
func BenchmarkApplyThinking_CrossFamily(b *testing.B) {
	benchRegister(b, "gemini", registry.GetGeminiModels())
	body := benchBody(50, `"reasoning_effort":"high",`)
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	for i := 0; i < b.N; i++ {
		if _, err := thinking.ApplyThinking(body, "gemini-2.5-pro", "openai", "gemini", "gemini"); err != nil {
			b.Fatalf("ApplyThinking: %v", err)
		}
	}
}

// BenchmarkApplyThinking_BodyConfig measures the production-common path where
// thinking config comes from the request BODY (not a model-name suffix). Unlike
// the suffix path, this exercises the full extract → validate → apply chain,
// including the initial gjson.ValidBytes over the whole body. This is the path
// most real clients hit, so it is the right target for any hot-path optimization.
func BenchmarkApplyThinking_BodyConfig(b *testing.B) {
	benchRegister(b, "claude", registry.GetClaudeModels())
	for _, kb := range []int{1, 50, 500} {
		body := benchBody(kb, `"thinking":{"type":"enabled","budget_tokens":16384},`)
		b.Run(fmt.Sprintf("claude_body_%dkb", kb), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for i := 0; i < b.N; i++ {
				if _, err := thinking.ApplyThinking(body, "claude-sonnet-4-5", "claude", "claude", "claude"); err != nil {
					b.Fatalf("ApplyThinking: %v", err)
				}
			}
		})
	}
}

// BenchmarkApplyThinking_ValidBytesOnly isolates the cost of the single
// gjson.ValidBytes(body) guard that extractThinkingConfig runs before any field
// lookup. Comparing this against BenchmarkApplyThinking_BodyConfig reveals how
// much of the body-path cost is the full-body JSON validation scan versus the
// targeted field gets and the sjson rewrite.
func BenchmarkApplyThinking_ValidBytesOnly(b *testing.B) {
	for _, kb := range []int{1, 50, 500} {
		body := benchBody(kb, `"thinking":{"type":"enabled","budget_tokens":16384},`)
		b.Run(fmt.Sprintf("validbytes_%dkb", kb), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for i := 0; i < b.N; i++ {
				if !gjson.ValidBytes(body) {
					b.Fatal("invalid body")
				}
			}
		})
	}
}
