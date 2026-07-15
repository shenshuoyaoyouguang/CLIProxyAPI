package cliproxy

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

func TestBuildOpenAICompatibilityConfigModels_InputModalities(t *testing.T) {
	compat := &config.OpenAICompatibility{
		Name: "mimo",
		Models: []config.OpenAICompatibilityModel{
			{
				Name:            "upstream-vision",
				Alias:           "mimo-v2.5-pro",
				DisplayName:     "Mimo Vision",
				InputModalities: []string{"TEXT", "image", "image"},
			},
			{
				Name:  "upstream-image",
				Alias: "compat-image",
				Image: true,
			},
		},
	}

	models := buildOpenAICompatibilityConfigModels(compat)
	if len(models) != 2 {
		t.Fatalf("model count = %d, want 2", len(models))
	}

	var vision *ModelInfo
	var imageModel *ModelInfo
	for _, model := range models {
		if model == nil {
			continue
		}
		switch model.ID {
		case "mimo-v2.5-pro":
			vision = model
		case "compat-image":
			imageModel = model
		}
	}
	if vision == nil {
		t.Fatal("expected vision model")
	}
	if vision.DisplayName != "Mimo Vision" {
		t.Fatalf("DisplayName = %q, want Mimo Vision", vision.DisplayName)
	}
	if got := joinModalities(vision.SupportedInputModalities); got != "text,image" {
		t.Fatalf("SupportedInputModalities = %q, want text,image", got)
	}
	if imageModel == nil {
		t.Fatal("expected image model")
	}
	if imageModel.DisplayName != "compat-image" {
		t.Fatalf("image DisplayName = %q, want compat-image", imageModel.DisplayName)
	}
	if imageModel.Type != registry.OpenAIImageModelType {
		t.Fatalf("image model type = %q, want %q", imageModel.Type, registry.OpenAIImageModelType)
	}
	if len(imageModel.SupportedInputModalities) != 0 {
		t.Fatalf("image model input modalities = %+v, want none", imageModel.SupportedInputModalities)
	}
}

func TestBuildOpenAICompatibilityConfigModels_CapabilityProfile(t *testing.T) {
	compat := &config.OpenAICompatibility{
		Name: "strict",
		Models: []config.OpenAICompatibilityModel{
			{
				Name:                  "upstream-capable",
				Alias:                 "capable-alias",
				Tools:                 config.Bool(true),
				ParallelToolCalls:     config.Bool(true),
				JSONSchema:            config.Bool(true),
				Streaming:             config.Bool(true),
				ResponsesAPI:          config.Bool(true),
				ReasoningTypes:        []string{"LEVEL", "budget", "budget"},
				ContextLength:         128000,
				MaxOutput:             16384,
				UnsupportedParameters: []string{"temperature", "top_p", "temperature"},
				LockedParameters: map[string]any{
					"temperature": 1.0,
					"top_p":       0.95,
				},
			},
		},
	}

	models := buildOpenAICompatibilityConfigModels(compat)
	if len(models) != 1 {
		t.Fatalf("model count = %d, want 1", len(models))
	}
	model := models[0]
	if !model.SupportsTools {
		t.Fatal("SupportsTools = false, want true")
	}
	if !model.SupportsParallelToolCalls {
		t.Fatal("SupportsParallelToolCalls = false, want true")
	}
	if !model.SupportsJSONSchema {
		t.Fatal("SupportsJSONSchema = false, want true")
	}
	if !model.SupportsStreaming {
		t.Fatal("SupportsStreaming = false, want true")
	}
	if !model.SupportsResponsesAPI {
		t.Fatal("SupportsResponsesAPI = false, want true")
	}
	if got := joinModalities(model.ReasoningTypes); got != "level,budget" {
		t.Fatalf("ReasoningTypes = %q, want level,budget", got)
	}
	if model.ContextLength != 128000 {
		t.Fatalf("ContextLength = %d, want 128000", model.ContextLength)
	}
	if model.MaxCompletionTokens != 16384 || model.OutputTokenLimit != 16384 {
		t.Fatalf("max output = (%d,%d), want 16384 for both", model.MaxCompletionTokens, model.OutputTokenLimit)
	}
	if got := joinModalities(model.UnsupportedParameters); got != "temperature,top_p" {
		t.Fatalf("UnsupportedParameters = %q, want temperature,top_p", got)
	}
	if model.LockedParameters["temperature"] != 1.0 || model.LockedParameters["top_p"] != 0.95 {
		t.Fatalf("LockedParameters = %#v, want temperature/top_p locks", model.LockedParameters)
	}
}

func joinModalities(modalities []string) string {
	if len(modalities) == 0 {
		return ""
	}
	out := modalities[0]
	for i := 1; i < len(modalities); i++ {
		out += "," + modalities[i]
	}
	return out
}
