package claude

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// updateGolden regenerates the golden wire fixtures from the current translator
// output. Run `go test -run TestOpenAIClaudeStreamingDiff -update` against the
// PRE-refactor code to capture the baseline, then refactor and re-run without
// the flag to prove byte-identity.
var updateGolden = flag.Bool("update", false, "update golden files")

// diffFixture describes one upstream OpenAI streaming scenario. chunks are the
// raw JSON payloads (without the leading "data: "), fed in order; a final
// "[DONE]" marker is appended automatically by runDiffFixture.
type diffFixture struct {
	name        string
	originalReq string
	chunks      []string
}

// diffFixtures is the mandatory coverage set:
//   - plain_text: text-only stream
//   - thinking_text: reasoning_content thinking block followed by text
//   - single_tool: one tool call with streamed arguments
//   - parallel_tools: two tool calls opened in the same chunk (proves the
//     index-on-open invariant: block 0 and block 1 both open before either stops)
//   - usage_finish: usage-bearing finish carrying cache_read_input_tokens
var diffFixtures = []diffFixture{
	{
		name:        "plain_text",
		originalReq: `{"stream":true}`,
		chunks: []string{
			`{"id":"c1","model":"m","created":1,"choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"}}]}`,
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":" world"}}]}`,
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		},
	},
	{
		name:        "thinking_text",
		originalReq: `{"stream":true}`,
		chunks: []string{
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"Let me think"}}]}`,
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"reasoning_content":" more"}}]}`,
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":"Answer"}}]}`,
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		},
	},
	{
		name:        "single_tool",
		originalReq: `{"stream":true}`,
		chunks: []string{
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","function":{"name":"do_it","arguments":""}}]}}]}`,
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"x\":1}"}}]}}]}`,
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		},
	},
	{
		name:        "parallel_tools",
		originalReq: `{"stream":true}`,
		chunks: []string{
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","function":{"name":"tool_a","arguments":""}},{"index":1,"id":"call_b","function":{"name":"tool_b","arguments":""}}]}}]}`,
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"p\":1}"}},{"index":1,"function":{"arguments":"{\"q\":2}"}}]}}]}`,
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		},
	},
	{
		name:        "usage_finish",
		originalReq: `{"stream":true}`,
		chunks: []string{
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"}}]}`,
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":3}}}`,
		},
	},
}

var (
	reTooluID = regexp.MustCompile(`toolu_\d+_\d+`)
	reMsgID   = regexp.MustCompile(`msg_\d+`)
)

// maskNonDeterministic replaces the only non-deterministic value shapes that the
// translator can emit: synthesized tool ids (toolu_<nanos>_<n>) and synthesized
// message ids (msg_<nanos>). Deterministic upstream-provided ids (e.g. call_a)
// are left untouched so a scheme regression cannot hide behind the mask.
func maskNonDeterministic(b []byte) []byte {
	b = reTooluID.ReplaceAll(b, []byte("toolu_MASKED"))
	b = reMsgID.ReplaceAll(b, []byte("msg_MASKED"))
	return b
}

// runDiffFixture feeds every chunk (plus a trailing [DONE]) through the
// streaming translator and returns the concatenated wire bytes.
func runDiffFixture(f diffFixture) []byte {
	var paramAny any
	var buf bytes.Buffer
	emit := func(chunk string) {
		outs := ConvertOpenAIResponseToClaude(
			context.Background(),
			"",
			[]byte(f.originalReq),
			nil,
			[]byte("data: "+chunk),
			&paramAny,
		)
		for _, o := range outs {
			buf.Write(o)
		}
	}
	for _, c := range f.chunks {
		emit(c)
	}
	emit("[DONE]")
	return buf.Bytes()
}

// TestOpenAIClaudeStreamingDiff is the byte-identity safety net. Golden files are
// captured from the pre-refactor translator; after migrating onto ClaudeSSEBuilder
// the masked wire output must match byte-for-byte.
func TestOpenAIClaudeStreamingDiff(t *testing.T) {
	for _, f := range diffFixtures {
		f := f
		t.Run(f.name, func(t *testing.T) {
			got := maskNonDeterministic(runDiffFixture(f))
			golden := filepath.Join("testdata", f.name+".golden")
			if *updateGolden {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatalf("mkdir testdata: %v", err)
				}
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden %s: %v (regenerate with -update)", golden, err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("byte mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", f.name, got, want)
			}
		})
	}
}

// TestOpenAIClaudeStreamingToolIDFormat asserts the tool-id scheme is preserved
// on the UNMASKED output. Upstream-provided ids must survive verbatim through
// SanitizeClaudeToolID so masking cannot conceal a scheme change.
func TestOpenAIClaudeStreamingToolIDFormat(t *testing.T) {
	byName := make(map[string]diffFixture, len(diffFixtures))
	for _, f := range diffFixtures {
		byName[f.name] = f
	}

	cases := []struct {
		fixture string
		wantIDs []string
	}{
		{fixture: "single_tool", wantIDs: []string{`"id":"call_a"`}},
		{fixture: "parallel_tools", wantIDs: []string{`"id":"call_a"`, `"id":"call_b"`}},
	}
	for _, c := range cases {
		f, ok := byName[c.fixture]
		if !ok {
			t.Fatalf("unknown fixture %s", c.fixture)
		}
		out := runDiffFixture(f)
		for _, want := range c.wantIDs {
			if !bytes.Contains(out, []byte(want)) {
				t.Fatalf("fixture %s: expected tool id %s in output, got:\n%s", c.fixture, want, out)
			}
		}
	}
}
