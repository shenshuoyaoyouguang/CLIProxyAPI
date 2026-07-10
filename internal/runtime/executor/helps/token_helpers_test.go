package helps

import (
	"encoding/json"
	"testing"

	"github.com/tiktoken-go/tokenizer"
)

func TestTokenizerForModel(t *testing.T) {
	tests := []struct {
		name  string
		model string
	}{
		{"gpt-4o-mini", "gpt-4o-mini"},
		{"GPT-4O case insensitive", "GPT-4O"},
		{"gpt-4 with spaces", " gpt-4 "},
		{"gpt-5-turbo", "gpt-5-turbo"},
		{"o3-mini", "o3-mini"},
		{"o4-mini", "o4-mini"},
		{"claude-3 default", "claude-3"},
		{"empty string", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec, err := TokenizerForModel(tt.model)
			if err != nil {
				t.Fatalf("TokenizerForModel(%q) returned error: %v", tt.model, err)
			}
			if codec == nil {
				t.Fatalf("TokenizerForModel(%q) returned nil codec", tt.model)
			}
		})
	}
}

func TestCountOpenAIChatTokens_NilCodec(t *testing.T) {
	_, err := CountOpenAIChatTokens(nil, []byte(`{"messages":[]}`))
	if err == nil {
		t.Fatal("CountOpenAIChatTokens(nil, ...) error = nil, want error")
	}
	if err.Error() != "encoder is nil" {
		t.Fatalf("CountOpenAIChatTokens(nil, ...) error = %q, want %q", err.Error(), "encoder is nil")
	}
}

func TestCountOpenAIChatTokens_EmptyPayload(t *testing.T) {
	codec, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		t.Fatalf("failed to get tokenizer: %v", err)
	}
	count, err := CountOpenAIChatTokens(codec, []byte{})
	if err != nil {
		t.Fatalf("CountOpenAIChatTokens(codec, empty) error = %v", err)
	}
	if count != 0 {
		t.Fatalf("CountOpenAIChatTokens(codec, empty) = %d, want 0", count)
	}
}

func TestCountOpenAIChatTokens_Messages(t *testing.T) {
	codec, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		t.Fatalf("failed to get tokenizer: %v", err)
	}
	payload := []byte(`{"messages":[{"role":"user","content":"hello world"}]}`)
	count, err := CountOpenAIChatTokens(codec, payload)
	if err != nil {
		t.Fatalf("CountOpenAIChatTokens error = %v", err)
	}
	if count <= 0 {
		t.Fatalf("CountOpenAIChatTokens = %d, want positive count", count)
	}
}

func TestCountOpenAIChatTokens_MessagesWithTools(t *testing.T) {
	codec, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		t.Fatalf("failed to get tokenizer: %v", err)
	}

	messagesOnly := []byte(`{"messages":[{"role":"user","content":"test"}]}`)
	countMessages, err := CountOpenAIChatTokens(codec, messagesOnly)
	if err != nil {
		t.Fatalf("CountOpenAIChatTokens(messages) error = %v", err)
	}

	withTools := []byte(`{"messages":[{"role":"user","content":"test"}],"tools":[{"type":"function","function":{"name":"get_weather","description":"Get weather"}}]}`)
	countWithTools, err := CountOpenAIChatTokens(codec, withTools)
	if err != nil {
		t.Fatalf("CountOpenAIChatTokens(messages+tools) error = %v", err)
	}

	if countWithTools <= countMessages {
		t.Fatalf("CountOpenAIChatTokens with tools (%d) should be greater than messages only (%d)", countWithTools, countMessages)
	}
}

func TestCountOpenAIChatTokens_Input(t *testing.T) {
	codec, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		t.Fatalf("failed to get tokenizer: %v", err)
	}
	payload := []byte(`{"input":"test input"}`)
	count, err := CountOpenAIChatTokens(codec, payload)
	if err != nil {
		t.Fatalf("CountOpenAIChatTokens error = %v", err)
	}
	if count <= 0 {
		t.Fatalf("CountOpenAIChatTokens = %d, want positive count", count)
	}
}

func TestCountOpenAIChatTokens_Prompt(t *testing.T) {
	codec, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		t.Fatalf("failed to get tokenizer: %v", err)
	}
	payload := []byte(`{"prompt":"test prompt"}`)
	count, err := CountOpenAIChatTokens(codec, payload)
	if err != nil {
		t.Fatalf("CountOpenAIChatTokens error = %v", err)
	}
	if count <= 0 {
		t.Fatalf("CountOpenAIChatTokens = %d, want positive count", count)
	}
}

func TestBuildOpenAIUsageJSON(t *testing.T) {
	tests := []struct {
		name  string
		count int64
		want  string
	}{
		{"zero", 0, `{"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`},
		{"42", 42, `{"usage":{"prompt_tokens":42,"completion_tokens":0,"total_tokens":42}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(BuildOpenAIUsageJSON(tt.count))
			// Validate JSON is parseable
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(got), &parsed); err != nil {
				t.Fatalf("BuildOpenAIUsageJSON(%d) produced invalid JSON: %v", tt.count, err)
			}
			if got != tt.want {
				t.Fatalf("BuildOpenAIUsageJSON(%d) = %s, want %s", tt.count, got, tt.want)
			}
		})
	}
}
