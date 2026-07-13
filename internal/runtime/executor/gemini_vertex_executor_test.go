package executor

import (
	"encoding/json"
	"testing"

	"github.com/tidwall/gjson"
)

func TestIsImagenModel(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{name: "imagen lowercase", model: "imagen-3.0", want: true},
		{name: "imagen mixed case", model: "Imagen-2", want: true},
		{name: "gemini model", model: "gemini-2.0-flash", want: false},
		{name: "empty string", model: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isImagenModel(tt.model); got != tt.want {
				t.Errorf("isImagenModel(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestGetVertexAction(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		isStream bool
		want     string
	}{
		{name: "imagen no stream", model: "imagen-3.0", isStream: false, want: "predict"},
		{name: "imagen with stream", model: "imagen-3.0", isStream: true, want: "predict"},
		{name: "gemini stream", model: "gemini-2.0-flash", isStream: true, want: "streamGenerateContent"},
		{name: "gemini no stream", model: "gemini-2.0-flash", isStream: false, want: "generateContent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getVertexAction(tt.model, tt.isStream); got != tt.want {
				t.Errorf("getVertexAction(%q, %v) = %q, want %q", tt.model, tt.isStream, got, tt.want)
			}
		})
	}
}

func TestConvertImagenToGeminiResponse(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		model   string
		checkFn func(t *testing.T, result []byte)
	}{
		{
			name:  "valid predictions with bytesBase64Encoded",
			model: "imagen-3.0",
			data:  []byte(`{"predictions":[{"bytesBase64Encoded":"abc123","mimeType":"image/jpeg"}]}`),
			checkFn: func(t *testing.T, result []byte) {
				candidates := gjson.GetBytes(result, "candidates")
				if !candidates.Exists() {
					t.Fatal("expected candidates field")
				}
				inlineData := gjson.GetBytes(result, "candidates.0.content.parts.0.inlineData")
				if !inlineData.Exists() {
					t.Fatal("expected inlineData in parts")
				}
				if got := gjson.GetBytes(result, "candidates.0.content.parts.0.inlineData.data").String(); got != "abc123" {
					t.Errorf("data = %q, want %q", got, "abc123")
				}
				if got := gjson.GetBytes(result, "candidates.0.content.parts.0.inlineData.mimeType").String(); got != "image/jpeg" {
					t.Errorf("mimeType = %q, want %q", got, "image/jpeg")
				}
			},
		},
		{
			name:  "no predictions field returns original",
			model: "imagen-3.0",
			data:  []byte(`{"something":"else"}`),
			checkFn: func(t *testing.T, result []byte) {
				if string(result) != `{"something":"else"}` {
					t.Errorf("expected original data returned, got %s", result)
				}
			},
		},
		{
			name:  "empty predictions array",
			model: "imagen-3.0",
			data:  []byte(`{"predictions":[]}`),
			checkFn: func(t *testing.T, result []byte) {
				parts := gjson.GetBytes(result, "candidates.0.content.parts")
				if !parts.Exists() {
					t.Fatal("expected parts field")
				}
				if len(parts.Array()) != 0 {
					t.Errorf("expected empty parts, got %d", len(parts.Array()))
				}
			},
		},
		{
			name:  "prediction with mimeType specified",
			model: "imagen-3.0",
			data:  []byte(`{"predictions":[{"bytesBase64Encoded":"data1","mimeType":"image/webp"}]}`),
			checkFn: func(t *testing.T, result []byte) {
				mime := gjson.GetBytes(result, "candidates.0.content.parts.0.inlineData.mimeType").String()
				if mime != "image/webp" {
					t.Errorf("mimeType = %q, want %q", mime, "image/webp")
				}
			},
		},
		{
			name:  "prediction without mimeType defaults to image/png",
			model: "imagen-3.0",
			data:  []byte(`{"predictions":[{"bytesBase64Encoded":"data2"}]}`),
			checkFn: func(t *testing.T, result []byte) {
				mime := gjson.GetBytes(result, "candidates.0.content.parts.0.inlineData.mimeType").String()
				if mime != "image/png" {
					t.Errorf("mimeType = %q, want %q", mime, "image/png")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertImagenToGeminiResponse(tt.data, tt.model)
			tt.checkFn(t, result)
		})
	}
}

func TestConvertToImagenRequest(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		checkFn func(t *testing.T, result []byte)
		wantErr bool
	}{
		{
			name:    "gemini-style contents extracts prompt",
			payload: []byte(`{"contents":[{"parts":[{"text":"draw a cat"}]}]}`),
			checkFn: func(t *testing.T, result []byte) {
				prompt := gjson.GetBytes(result, "instances.0.prompt").String()
				if prompt != "draw a cat" {
					t.Errorf("prompt = %q, want %q", prompt, "draw a cat")
				}
			},
		},
		{
			name:    "with aspectRatio parameter",
			payload: []byte(`{"contents":[{"parts":[{"text":"a dog"}]}],"aspectRatio":"16:9"}`),
			checkFn: func(t *testing.T, result []byte) {
				ar := gjson.GetBytes(result, "parameters.aspectRatio").String()
				if ar != "16:9" {
					t.Errorf("aspectRatio = %q, want %q", ar, "16:9")
				}
			},
		},
		{
			name:    "with negativePrompt",
			payload: []byte(`{"contents":[{"parts":[{"text":"sunset"}]}],"negativePrompt":"blurry"}`),
			checkFn: func(t *testing.T, result []byte) {
				np := gjson.GetBytes(result, "instances.0.negativePrompt").String()
				if np != "blurry" {
					t.Errorf("negativePrompt = %q, want %q", np, "blurry")
				}
			},
		},
		{
			name:    "no prompt returns error",
			payload: []byte(`{"contents":[]}`),
			wantErr: true,
		},
		{
			name:    "with sampleCount",
			payload: []byte(`{"contents":[{"parts":[{"text":"tree"}]}],"sampleCount":4}`),
			checkFn: func(t *testing.T, result []byte) {
				sc := gjson.GetBytes(result, "parameters.sampleCount").Int()
				if sc != 4 {
					t.Errorf("sampleCount = %d, want %d", sc, 4)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertToImagenRequest(tt.payload)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Verify result is valid JSON
			if !json.Valid(result) {
				t.Fatalf("result is not valid JSON: %s", result)
			}
			tt.checkFn(t, result)
		})
	}
}
