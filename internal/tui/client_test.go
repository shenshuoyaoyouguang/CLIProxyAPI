package tui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func setupTestServer(handler http.Handler) (*httptest.Server, *Client) {
	srv := httptest.NewServer(handler)
	c := NewClient(0, "test-secret")
	c.baseURL = srv.URL
	return srv, c
}

func TestGetConfig(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"port":  8080,
			"debug": true,
		})
	})

	srv, c := setupTestServer(mux)
	defer srv.Close()

	result, err := c.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() error = %v", err)
	}
	if result["debug"] != true {
		t.Errorf("expected debug=true, got %v", result["debug"])
	}
	if result["port"] != float64(8080) {
		t.Errorf("expected port=8080, got %v", result["port"])
	}
}

func TestGetConfigYAML(t *testing.T) {
	yamlContent := "port: 8080\ndebug: true\n"
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/config.yaml", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Write([]byte(yamlContent))
	})

	srv, c := setupTestServer(mux)
	defer srv.Close()

	result, err := c.GetConfigYAML()
	if err != nil {
		t.Fatalf("GetConfigYAML() error = %v", err)
	}
	if result != yamlContent {
		t.Errorf("expected %q, got %q", yamlContent, result)
	}
}

func TestPutConfigYAML(t *testing.T) {
	var receivedBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/config.yaml", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data, _ := io.ReadAll(r.Body)
		receivedBody = string(data)
		w.WriteHeader(http.StatusOK)
	})

	srv, c := setupTestServer(mux)
	defer srv.Close()

	yamlContent := "port: 9090\n"
	err := c.PutConfigYAML(yamlContent)
	if err != nil {
		t.Fatalf("PutConfigYAML() error = %v", err)
	}
	if receivedBody != yamlContent {
		t.Errorf("expected body %q, got %q", yamlContent, receivedBody)
	}
}

func TestGetAuthFiles(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/auth-files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{
				{"name": "key1.json", "disabled": false},
				{"name": "key2.json", "disabled": true},
			},
		})
	})

	srv, c := setupTestServer(mux)
	defer srv.Close()

	files, err := c.GetAuthFiles()
	if err != nil {
		t.Fatalf("GetAuthFiles() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0]["name"] != "key1.json" {
		t.Errorf("expected first file name=key1.json, got %v", files[0]["name"])
	}
}

func TestDeleteAuthFile(t *testing.T) {
	var receivedName string
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/auth-files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		receivedName = r.URL.Query().Get("name")
		w.WriteHeader(http.StatusOK)
	})

	srv, c := setupTestServer(mux)
	defer srv.Close()

	err := c.DeleteAuthFile("my-auth.json")
	if err != nil {
		t.Fatalf("DeleteAuthFile() error = %v", err)
	}
	if receivedName != "my-auth.json" {
		t.Errorf("expected name=my-auth.json, got %q", receivedName)
	}
}

func TestToggleAuthFile(t *testing.T) {
	var receivedBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/auth-files/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &receivedBody)
		w.WriteHeader(http.StatusOK)
	})

	srv, c := setupTestServer(mux)
	defer srv.Close()

	err := c.ToggleAuthFile("auth.json", true)
	if err != nil {
		t.Fatalf("ToggleAuthFile() error = %v", err)
	}
	if receivedBody["name"] != "auth.json" {
		t.Errorf("expected name=auth.json, got %v", receivedBody["name"])
	}
	if receivedBody["disabled"] != true {
		t.Errorf("expected disabled=true, got %v", receivedBody["disabled"])
	}
}

func TestGetAPIKeys(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/api-keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"api-keys": []string{"sk-key1", "sk-key2"},
		})
	})

	srv, c := setupTestServer(mux)
	defer srv.Close()

	keys, err := c.GetAPIKeys()
	if err != nil {
		t.Fatalf("GetAPIKeys() error = %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	if keys[0] != "sk-key1" {
		t.Errorf("expected first key=sk-key1, got %q", keys[0])
	}
}

func TestAddAPIKey(t *testing.T) {
	var receivedBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/api-keys", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &receivedBody)
		w.WriteHeader(http.StatusOK)
	})

	srv, c := setupTestServer(mux)
	defer srv.Close()

	err := c.AddAPIKey("sk-new-key")
	if err != nil {
		t.Fatalf("AddAPIKey() error = %v", err)
	}
	if receivedBody["new"] != "sk-new-key" {
		t.Errorf("expected new=sk-new-key, got %v", receivedBody["new"])
	}
	if receivedBody["old"] != nil {
		t.Errorf("expected old=nil, got %v", receivedBody["old"])
	}
}

func TestDeleteAPIKey(t *testing.T) {
	var receivedIndex string
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/api-keys", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		receivedIndex = r.URL.Query().Get("index")
		w.WriteHeader(http.StatusOK)
	})

	srv, c := setupTestServer(mux)
	defer srv.Close()

	err := c.DeleteAPIKey(3)
	if err != nil {
		t.Fatalf("DeleteAPIKey() error = %v", err)
	}
	if receivedIndex != "3" {
		t.Errorf("expected index=3, got %q", receivedIndex)
	}
}

func TestGetLogs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/logs", func(w http.ResponseWriter, r *http.Request) {
		after := r.URL.Query().Get("after")
		limit := r.URL.Query().Get("limit")
		if after != "100" || limit != "50" {
			t.Errorf("expected after=100, limit=50, got after=%q, limit=%q", after, limit)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"lines":            []string{"log line 1", "log line 2"},
			"latest-timestamp": float64(200),
		})
	})

	srv, c := setupTestServer(mux)
	defer srv.Close()

	lines, latest, err := c.GetLogs(100, 50)
	if err != nil {
		t.Fatalf("GetLogs() error = %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0] != "log line 1" {
		t.Errorf("expected first line='log line 1', got %q", lines[0])
	}
	if latest != 200 {
		t.Errorf("expected latest=200, got %d", latest)
	}
}

func TestGetLogsTimestampFloat64(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/logs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Simulate JSON number that unmarshals as float64
		w.Write([]byte(`{"lines":[],"latest-timestamp":1720000000}`))
	})

	srv, c := setupTestServer(mux)
	defer srv.Close()

	_, latest, err := c.GetLogs(0, 0)
	if err != nil {
		t.Fatalf("GetLogs() error = %v", err)
	}
	if latest != 1720000000 {
		t.Errorf("expected latest=1720000000, got %d", latest)
	}
}

func TestGetDebug(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/debug", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"debug": true})
	})

	srv, c := setupTestServer(mux)
	defer srv.Close()

	debug, err := c.GetDebug()
	if err != nil {
		t.Fatalf("GetDebug() error = %v", err)
	}
	if !debug {
		t.Errorf("expected debug=true, got false")
	}
}

func TestPutBoolField(t *testing.T) {
	var receivedBody map[string]any
	var receivedPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		receivedPath = r.URL.Path
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &receivedBody)
		w.WriteHeader(http.StatusOK)
	})

	srv, c := setupTestServer(mux)
	defer srv.Close()

	err := c.PutBoolField("debug", true)
	if err != nil {
		t.Fatalf("PutBoolField() error = %v", err)
	}
	if receivedPath != "/v0/management/debug" {
		t.Errorf("expected path=/v0/management/debug, got %q", receivedPath)
	}
	if receivedBody["value"] != true {
		t.Errorf("expected value=true, got %v", receivedBody["value"])
	}
}

func TestSetSecretKey(t *testing.T) {
	c := NewClient(8080, "initial-key")
	c.SetSecretKey("  new-key  ")
	if c.secretKey != "new-key" {
		t.Errorf("expected secretKey='new-key', got %q", c.secretKey)
	}
}

func TestAuthorizationHeader(t *testing.T) {
	var receivedAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/config", func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	})

	srv, c := setupTestServer(mux)
	defer srv.Close()

	c.GetConfig()
	if receivedAuth != "Bearer test-secret" {
		t.Errorf("expected Authorization='Bearer test-secret', got %q", receivedAuth)
	}
}

func TestAuthorizationHeaderEmpty(t *testing.T) {
	var receivedAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/config", func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient(0, "")
	c.baseURL = srv.URL

	c.GetConfig()
	if receivedAuth != "" {
		t.Errorf("expected no Authorization header, got %q", receivedAuth)
	}
}

func TestHTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/config", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})

	srv, c := setupTestServer(mux)
	defer srv.Close()

	_, err := c.GetConfig()
	if err == nil {
		t.Fatal("expected error for 403 response, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected error to contain '403', got %q", err.Error())
	}
}

func TestConnectionError(t *testing.T) {
	c := NewClient(0, "test-secret")
	c.baseURL = "http://127.0.0.1:1" // port 1 should be unreachable

	_, err := c.GetConfig()
	if err == nil {
		t.Fatal("expected connection error, got nil")
	}
}

func TestDeleteAuthFileHTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/auth-files", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	srv, c := setupTestServer(mux)
	defer srv.Close()

	err := c.DeleteAuthFile("nonexistent.json")
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected error to contain '404', got %q", err.Error())
	}
}

func TestDeleteAPIKeyHTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/api-keys", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	})

	srv, c := setupTestServer(mux)
	defer srv.Close()

	err := c.DeleteAPIKey(99)
	if err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected error to contain '400', got %q", err.Error())
	}
}

func TestGetLogsPreservesAfterOnError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/management/logs", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})

	srv, c := setupTestServer(mux)
	defer srv.Close()

	_, latest, err := c.GetLogs(500, 10)
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if latest != 500 {
		t.Errorf("expected latest to preserve after=500 on error, got %d", latest)
	}
}
