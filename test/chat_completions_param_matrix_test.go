package test

import (
	"testing"

	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

// TestChatCompletionsParamMatrix documents and guards provider asymmetry for
// OpenAI Chat Completions sampling / generation fields when translated toward
// each backend format. This is the P1 matrix recommended by the
// /v1/chat/completions protocol analysis report.
func TestChatCompletionsParamMatrix(t *testing.T) {
	const baseModel = "matrix-model"
	const baseMessages = `{"role":"user","content":"hi"}`

	type expect struct {
		path    string
		want    string // empty means "field must exist" when mode=exist; exact string match otherwise
		mode    string // "eq", "absent", "exist", "eq_float", "eq_int"
		float   float64
		integer int64
	}

	type row struct {
		name   string
		to     sdktranslator.Format
		body   string
		checks []expect
	}

	// Full sampling payload used by most rows; individual rows may override body.
	fullBody := `{
		"model":"matrix-model",
		"messages":[` + baseMessages + `],
		"temperature":0.4,
		"top_p":0.85,
		"n":3,
		"max_tokens":100,
		"max_completion_tokens":200,
		"presence_penalty":0.5,
		"frequency_penalty":0.25,
		"response_format":{"type":"json_object"},
		"seed":42,
		"user":"matrix-user"
	}`

	rows := []row{
		// --- Claude ---
		{
			name: "claude drops temperature",
			to:   sdktranslator.FormatClaude,
			body: fullBody,
			checks: []expect{
				{path: "temperature", mode: "absent"},
				{path: "top_p", mode: "eq_float", float: 0.85},
				// max_tokens preferred; Claude does not read max_completion_tokens.
				{path: "max_tokens", mode: "eq_int", integer: 100},
			},
		},
		{
			name: "claude ignores max_completion_tokens only",
			to:   sdktranslator.FormatClaude,
			body: `{
				"model":"matrix-model",
				"messages":[{"role":"user","content":"hi"}],
				"max_completion_tokens":77
			}`,
			checks: []expect{
				// Default template max_tokens remains when only max_completion_tokens is set.
				{path: "max_tokens", mode: "eq_int", integer: 32000},
			},
		},
		{
			name: "claude has no multi-candidate n",
			to:   sdktranslator.FormatClaude,
			body: fullBody,
			checks: []expect{
				{path: "n", mode: "absent"},
				{path: "candidate_count", mode: "absent"},
				{path: "presence_penalty", mode: "absent"},
				{path: "frequency_penalty", mode: "absent"},
			},
		},

		// --- Gemini ---
		{
			name: "gemini maps temperature top_p and max_tokens preference",
			to:   sdktranslator.FormatGemini,
			body: fullBody,
			checks: []expect{
				{path: "generationConfig.temperature", mode: "eq_float", float: 0.4},
				{path: "generationConfig.topP", mode: "eq_float", float: 0.85},
				// max_tokens wins over max_completion_tokens.
				{path: "generationConfig.maxOutputTokens", mode: "eq_int", integer: 100},
				{path: "generationConfig.candidateCount", mode: "eq_int", integer: 3},
			},
		},
		{
			name: "gemini max_completion_tokens fallback",
			to:   sdktranslator.FormatGemini,
			body: `{
				"model":"matrix-model",
				"messages":[{"role":"user","content":"hi"}],
				"max_completion_tokens":55
			}`,
			checks: []expect{
				{path: "generationConfig.maxOutputTokens", mode: "eq_int", integer: 55},
			},
		},
		{
			name: "gemini n equals 1 does not set candidateCount",
			to:   sdktranslator.FormatGemini,
			body: `{
				"model":"matrix-model",
				"messages":[{"role":"user","content":"hi"}],
				"n":1
			}`,
			checks: []expect{
				{path: "generationConfig.candidateCount", mode: "absent"},
			},
		},

		// --- Codex (Chat Completions → Responses) ---
		{
			name: "codex drops sampling params and hardcodes parallel_tool_calls",
			to:   sdktranslator.FormatCodex,
			body: fullBody,
			checks: []expect{
				{path: "temperature", mode: "absent"},
				{path: "top_p", mode: "absent"},
				{path: "max_output_tokens", mode: "absent"},
				{path: "parallel_tool_calls", mode: "eq", want: "true"},
			},
		},
		{
			name: "codex maps response_format json_object to text.format",
			to:   sdktranslator.FormatCodex,
			body: `{
				"model":"matrix-model",
				"messages":[{"role":"user","content":"hi"}],
				"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":{"type":"object"}}}
			}`,
			checks: []expect{
				{path: "text.format.type", mode: "eq", want: "json_schema"},
				{path: "text.format.name", mode: "eq", want: "answer"},
			},
		},

		// --- OpenAI-compat passthrough ---
		{
			name: "openai-compat keeps sampling fields",
			to:   sdktranslator.FormatOpenAI,
			body: fullBody,
			checks: []expect{
				{path: "temperature", mode: "eq_float", float: 0.4},
				{path: "top_p", mode: "eq_float", float: 0.85},
				{path: "n", mode: "eq_int", integer: 3},
				{path: "max_tokens", mode: "eq_int", integer: 100},
				{path: "max_completion_tokens", mode: "eq_int", integer: 200},
				{path: "presence_penalty", mode: "eq_float", float: 0.5},
				{path: "frequency_penalty", mode: "eq_float", float: 0.25},
				{path: "model", mode: "eq", want: baseModel},
			},
		},

		// --- Interactions ---
		{
			name: "interactions maps generation config including penalties and n",
			to:   sdktranslator.FormatInteractions,
			body: fullBody,
			checks: []expect{
				{path: "generation_config.temperature", mode: "eq_float", float: 0.4},
				{path: "generation_config.top_p", mode: "eq_float", float: 0.85},
				// max_completion_tokens preferred over max_tokens for interactions.
				{path: "generation_config.max_output_tokens", mode: "eq_int", integer: 200},
				{path: "generation_config.presence_penalty", mode: "eq_float", float: 0.5},
				{path: "generation_config.frequency_penalty", mode: "eq_float", float: 0.25},
				{path: "generation_config.candidate_count", mode: "eq_int", integer: 3},
				{path: "response_format.type", mode: "eq", want: "json_object"},
			},
		},
		{
			name: "interactions max_tokens when no max_completion_tokens",
			to:   sdktranslator.FormatInteractions,
			body: `{
				"model":"matrix-model",
				"messages":[{"role":"user","content":"hi"}],
				"max_tokens":88
			}`,
			checks: []expect{
				{path: "generation_config.max_output_tokens", mode: "eq_int", integer: 88},
			},
		},
	}

	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			out := sdktranslator.TranslateRequest(
				sdktranslator.FormatOpenAI,
				r.to,
				baseModel,
				[]byte(r.body),
				false,
			)
			if len(out) == 0 {
				t.Fatalf("empty translation output for %s → %s", sdktranslator.FormatOpenAI, r.to)
			}
			root := gjson.ParseBytes(out)
			for _, c := range r.checks {
				val := root.Get(c.path)
				switch c.mode {
				case "absent":
					if val.Exists() {
						t.Fatalf("%s: path %q should be absent, got %s\nfull=%s", r.name, c.path, val.Raw, out)
					}
				case "exist":
					if !val.Exists() {
						t.Fatalf("%s: path %q should exist\nfull=%s", r.name, c.path, out)
					}
				case "eq":
					if !val.Exists() {
						t.Fatalf("%s: path %q missing (want %q)\nfull=%s", r.name, c.path, c.want, out)
					}
					// Bool true serializes as raw true; String() for bool is "true".
					got := val.String()
					if val.Type == gjson.True {
						got = "true"
					} else if val.Type == gjson.False {
						got = "false"
					}
					if got != c.want {
						t.Fatalf("%s: %s = %q, want %q\nfull=%s", r.name, c.path, got, c.want, out)
					}
				case "eq_float":
					if !val.Exists() {
						t.Fatalf("%s: path %q missing (want %v)\nfull=%s", r.name, c.path, c.float, out)
					}
					if val.Float() != c.float {
						t.Fatalf("%s: %s = %v, want %v\nfull=%s", r.name, c.path, val.Float(), c.float, out)
					}
				case "eq_int":
					if !val.Exists() {
						t.Fatalf("%s: path %q missing (want %d)\nfull=%s", r.name, c.path, c.integer, out)
					}
					if val.Int() != c.integer {
						t.Fatalf("%s: %s = %d, want %d\nfull=%s", r.name, c.path, val.Int(), c.integer, out)
					}
				default:
					t.Fatalf("unknown check mode %q", c.mode)
				}
			}
		})
	}
}
