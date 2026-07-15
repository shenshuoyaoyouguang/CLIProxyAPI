package cliproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	internalregistry "github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestRegisterModelsForAuth_OpenAICompatibilityDiscoveryDisabledDoesNotFetch(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	service := &Service{
		cfg: &config.Config{
			OpenAICompatibility: []config.OpenAICompatibility{{
				Name:    "compat",
				BaseURL: server.URL + "/v1",
				Models: []config.OpenAICompatibilityModel{{
					Name:  "configured-upstream",
					Alias: "configured-alias",
				}},
			}},
		},
	}
	auth := &coreauth.Auth{
		ID:       "auth-openai-compat-discovery-disabled",
		Provider: "openai-compatibility",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"auth_kind":    "api_key",
			"api_key":      "sk-test",
			"base_url":     server.URL + "/v1",
			"compat_name":  "compat",
			"provider_key": "compat",
		},
	}

	registry := internalregistry.GetGlobalRegistry()
	registry.UnregisterClient(auth.ID)
	t.Cleanup(func() {
		registry.UnregisterClient(auth.ID)
	})

	service.registerModelsForAuth(context.Background(), auth)

	if got := requests.Load(); got != 0 {
		t.Fatalf("discovery requests = %d, want 0", got)
	}
	if model := findRegisteredModel(registry.GetModelsForClient(auth.ID), "configured-alias"); model == nil {
		t.Fatal("expected configured model to remain registered")
	}
}

func TestRegisterModelsForAuth_OpenAICompatibilityDiscoveryAppendsOnlyUnknownModels(t *testing.T) {
	var sawAuth bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q, want /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		if got := r.Header.Get("X-Compat-Test"); got != "yes" {
			t.Fatalf("X-Compat-Test = %q, want yes", got)
		}
		sawAuth = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [
				{"id": "configured-upstream"},
				{"id": "configured-alias"},
				{"id": "remote-only"}
			]
		}`))
	}))
	defer server.Close()

	service := &Service{
		cfg: &config.Config{
			OpenAICompatibility: []config.OpenAICompatibility{{
				Name:           "compat",
				BaseURL:        server.URL + "/v1",
				DiscoverModels: true,
				Headers: map[string]string{
					"X-Compat-Test": "yes",
				},
				Models: []config.OpenAICompatibilityModel{{
					Name:  "configured-upstream",
					Alias: "configured-alias",
					Tools: config.Bool(true),
				}},
			}},
		},
	}
	auth := &coreauth.Auth{
		ID:       "auth-openai-compat-discovery-enabled",
		Provider: "openai-compatibility",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"auth_kind":    "api_key",
			"api_key":      "sk-test",
			"base_url":     server.URL + "/v1",
			"compat_name":  "compat",
			"provider_key": "compat",
		},
	}

	registry := internalregistry.GetGlobalRegistry()
	registry.UnregisterClient(auth.ID)
	t.Cleanup(func() {
		registry.UnregisterClient(auth.ID)
	})

	service.registerModelsForAuth(context.Background(), auth)

	if !sawAuth {
		t.Fatal("expected discovery request to include auth and custom headers")
	}
	models := registry.GetModelsForClient(auth.ID)
	configured := findRegisteredModel(models, "configured-alias")
	if configured == nil {
		t.Fatal("expected configured model to remain registered")
	}
	if !configured.SupportsTools {
		t.Fatal("configured capability profile was not preserved")
	}
	if duplicate := findRegisteredModel(models, "configured-upstream"); duplicate != nil {
		t.Fatalf("discovered model duplicated configured upstream name: %#v", duplicate)
	}
	discovered := findRegisteredModel(models, "remote-only")
	if discovered == nil {
		t.Fatal("expected remote-only model to be appended")
	}
	if discovered.SupportsTools || discovered.Thinking != nil || len(discovered.SupportedInputModalities) != 0 {
		t.Fatalf("discovered model should use conservative empty capabilities: %#v", discovered)
	}
}

func TestRegisterModelsForAuth_OpenAICompatibilityDiscoveryIgnoresBadResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "non-2xx", status: http.StatusBadGateway, body: `{"error":"bad gateway"}`},
		{name: "bad-json", status: http.StatusOK, body: `{bad json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			service := &Service{
				cfg: &config.Config{
					OpenAICompatibility: []config.OpenAICompatibility{{
						Name:           "compat",
						BaseURL:        server.URL + "/v1",
						DiscoverModels: true,
						Models: []config.OpenAICompatibilityModel{{
							Name:  "configured-upstream",
							Alias: "configured-alias",
						}},
					}},
				},
			}
			auth := &coreauth.Auth{
				ID:       "auth-openai-compat-discovery-" + strings.ReplaceAll(tc.name, "-", "_"),
				Provider: "openai-compatibility",
				Status:   coreauth.StatusActive,
				Attributes: map[string]string{
					"auth_kind":    "api_key",
					"api_key":      "sk-test",
					"base_url":     server.URL + "/v1",
					"compat_name":  "compat",
					"provider_key": "compat",
				},
			}

			registry := internalregistry.GetGlobalRegistry()
			registry.UnregisterClient(auth.ID)
			t.Cleanup(func() {
				registry.UnregisterClient(auth.ID)
			})

			service.registerModelsForAuth(context.Background(), auth)

			models := registry.GetModelsForClient(auth.ID)
			if model := findRegisteredModel(models, "configured-alias"); model == nil {
				t.Fatal("expected configured model to remain registered")
			}
			if len(models) != 1 {
				t.Fatalf("registered model count = %d, want 1", len(models))
			}
		})
	}
}

func TestRegisterModelsForAuth_OpenAICompatibilityDiscoveryTimesOut(t *testing.T) {
	originalTimeout := openAICompatibilityModelDiscoveryTimeout
	openAICompatibilityModelDiscoveryTimeout = 50 * time.Millisecond
	t.Cleanup(func() {
		openAICompatibilityModelDiscoveryTimeout = originalTimeout
	})

	var requests atomic.Int64
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = w
		requests.Add(1)
		<-release
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	service := &Service{
		cfg: &config.Config{
			OpenAICompatibility: []config.OpenAICompatibility{{
				Name:           "compat",
				BaseURL:        server.URL + "/v1",
				DiscoverModels: true,
				Models: []config.OpenAICompatibilityModel{{
					Name:  "configured-upstream",
					Alias: "configured-alias",
				}},
			}},
		},
	}
	auth := &coreauth.Auth{
		ID:       "auth-openai-compat-discovery-timeout",
		Provider: "openai-compatibility",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"auth_kind":    "api_key",
			"api_key":      "sk-test",
			"base_url":     server.URL + "/v1",
			"compat_name":  "compat",
			"provider_key": "compat",
		},
	}

	registry := internalregistry.GetGlobalRegistry()
	registry.UnregisterClient(auth.ID)
	t.Cleanup(func() {
		registry.UnregisterClient(auth.ID)
	})

	start := time.Now()
	service.registerModelsForAuth(context.Background(), auth)
	elapsed := time.Since(start)

	if got := requests.Load(); got == 0 {
		t.Fatal("expected discovery request to be attempted")
	}
	if elapsed > time.Second {
		t.Fatalf("discovery registration took %s, want bounded by timeout", elapsed)
	}
	if model := findRegisteredModel(registry.GetModelsForClient(auth.ID), "configured-alias"); model == nil {
		t.Fatal("expected configured model to remain registered after discovery timeout")
	}
}

func findRegisteredModel(models []*internalregistry.ModelInfo, id string) *internalregistry.ModelInfo {
	for _, model := range models {
		if model != nil && strings.EqualFold(strings.TrimSpace(model.ID), id) {
			return model
		}
	}
	return nil
}
