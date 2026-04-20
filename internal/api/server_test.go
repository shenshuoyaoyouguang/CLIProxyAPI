package api

import (
	"encoding/json"
<<<<<<< HEAD
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
=======
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
>>>>>>> 27c1428b (feat: add core proxy server implementation)
	"strings"
	"testing"
	"time"

	gin "github.com/gin-gonic/gin"
<<<<<<< HEAD
=======
	managementHandlers "github.com/router-for-me/CLIProxyAPI/v6/internal/api/handlers/management"
>>>>>>> 27c1428b (feat: add core proxy server implementation)
	proxyconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	internallogging "github.com/router-for-me/CLIProxyAPI/v6/internal/logging"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v6/sdk/access"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
<<<<<<< HEAD
=======
	return newTestServerWithOptions(t)
}

func newTestServerWithOptions(t *testing.T, opts ...ServerOption) *Server {
	t.Helper()
>>>>>>> 27c1428b (feat: add core proxy server implementation)

	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}

	cfg := &proxyconfig.Config{
		SDKConfig: sdkconfig.SDKConfig{
			APIKeys: []string{"test-key"},
		},
		Port:                   0,
		AuthDir:                authDir,
		Debug:                  true,
		LoggingToFile:          false,
		UsageStatisticsEnabled: false,
	}

	authManager := auth.NewManager(nil, nil, nil)
	accessManager := sdkaccess.NewManager()

	configPath := filepath.Join(tmpDir, "config.yaml")
<<<<<<< HEAD
	return NewServer(cfg, authManager, accessManager, configPath)
=======
	return NewServer(cfg, authManager, accessManager, configPath, opts...)
>>>>>>> 27c1428b (feat: add core proxy server implementation)
}

func TestHealthz(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v; body=%s", err, rr.Body.String())
	}
	if resp.Status != "ok" {
		t.Fatalf("unexpected response status: got %q want %q", resp.Status, "ok")
	}
}

<<<<<<< HEAD
=======
func TestOAuthBrowserCallback_ReturnsConflictWhenSessionIsNotPending(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/google/callback?state=missing-state-1234567890abcdef&code=test-code", nil)
	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("unexpected status code: got %d want %d; body=%s", rr.Code, http.StatusConflict, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "Authentication successful") {
		t.Fatalf("unexpected success page for missing oauth session: %s", rr.Body.String())
	}
}

func TestOAuthBrowserCallback_PersistsPendingCallbackBeforeShowingSuccess(t *testing.T) {
	server := newTestServer(t)
	state := "pending-state-1234567890abcdef"
	managementHandlers.RegisterOAuthSession(state, "gemini")
	defer managementHandlers.CompleteOAuthSession(state)

	req := httptest.NewRequest(http.MethodGet, "/google/callback?state="+state+"&code=test-code", nil)
	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Authentication successful") {
		t.Fatalf("expected success page, got %s", rr.Body.String())
	}

	callbackFile := filepath.Join(server.cfg.AuthDir, ".oauth-gemini-"+state+".oauth")
	if _, err := os.Stat(callbackFile); err != nil {
		t.Fatalf("expected oauth callback file to be created: %v", err)
	}
}

func TestRequestGeminiCLIToken_ReturnsRandomState(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "test-mgmt")
	server := newTestServer(t)

	seen := make(map[string]struct{}, 2)
	statePattern := regexp.MustCompile(`^[a-f0-9]{32}$`)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v0/management/gemini-cli-auth-url", nil)
		req.Header.Set("Authorization", "Bearer test-mgmt")
		rr := httptest.NewRecorder()
		server.engine.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("unexpected status code: got %d want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
		}

		var payload map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatalf("failed to parse response JSON: %v; body=%s", err, rr.Body.String())
		}
		state, _ := payload["state"].(string)
		if !statePattern.MatchString(state) {
			t.Fatalf("state = %q, want 32-char random hex string", state)
		}
		if _, exists := seen[state]; exists {
			t.Fatalf("expected distinct states, got duplicate %q", state)
		}
		seen[state] = struct{}{}
		managementHandlers.CompleteOAuthSession(state)
	}
}

func TestManagementOAuthAuthURLs_UseOverriddenCallbackPort(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "test-mgmt")
	callbackPort := 41234
	server := newTestServerWithOptions(t, WithOAuthCallbackPort(callbackPort))

	testCases := []struct {
		name      string
		path      string
		paramName string
		wantValue string
		stateKey  string
	}{
		{
			name:      "claude",
			path:      "/v0/management/anthropic-auth-url",
			paramName: "redirect_uri",
			wantValue: fmt.Sprintf("http://localhost:%d/callback", callbackPort),
			stateKey:  "state",
		},
		{
			name:      "codex",
			path:      "/v0/management/codex-auth-url",
			paramName: "redirect_uri",
			wantValue: fmt.Sprintf("http://localhost:%d/auth/callback", callbackPort),
			stateKey:  "state",
		},
		{
			name:      "gemini",
			path:      "/v0/management/gemini-cli-auth-url",
			paramName: "redirect_uri",
			wantValue: fmt.Sprintf("http://localhost:%d/oauth2callback", callbackPort),
			stateKey:  "state",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", "Bearer test-mgmt")
			rr := httptest.NewRecorder()
			server.engine.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("unexpected status code: got %d want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
			}

			var payload map[string]any
			if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
				t.Fatalf("failed to parse response JSON: %v; body=%s", err, rr.Body.String())
			}
			rawURL, _ := payload["url"].(string)
			parsedURL, err := url.Parse(rawURL)
			if err != nil {
				t.Fatalf("url.Parse(authURL) error = %v", err)
			}
			if got := parsedURL.Query().Get(tc.paramName); got != tc.wantValue {
				t.Fatalf("%s %s = %q, want %q", tc.name, tc.paramName, got, tc.wantValue)
			}

			state, _ := payload[tc.stateKey].(string)
			managementHandlers.CompleteOAuthSession(state)
		})
	}
}

>>>>>>> 27c1428b (feat: add core proxy server implementation)
func TestAmpProviderModelRoutes(t *testing.T) {
	testCases := []struct {
		name         string
		path         string
		wantStatus   int
		wantContains string
	}{
		{
			name:         "openai root models",
			path:         "/api/provider/openai/models",
			wantStatus:   http.StatusOK,
			wantContains: `"object":"list"`,
		},
		{
			name:         "groq root models",
			path:         "/api/provider/groq/models",
			wantStatus:   http.StatusOK,
			wantContains: `"object":"list"`,
		},
		{
			name:         "openai models",
			path:         "/api/provider/openai/v1/models",
			wantStatus:   http.StatusOK,
			wantContains: `"object":"list"`,
		},
		{
			name:         "anthropic models",
			path:         "/api/provider/anthropic/v1/models",
			wantStatus:   http.StatusOK,
			wantContains: `"data"`,
		},
		{
			name:         "google models v1",
			path:         "/api/provider/google/v1/models",
			wantStatus:   http.StatusOK,
			wantContains: `"models"`,
		},
		{
			name:         "google models v1beta",
			path:         "/api/provider/google/v1beta/models",
			wantStatus:   http.StatusOK,
			wantContains: `"models"`,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			server := newTestServer(t)

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", "Bearer test-key")

			rr := httptest.NewRecorder()
			server.engine.ServeHTTP(rr, req)

			if rr.Code != tc.wantStatus {
				t.Fatalf("unexpected status code for %s: got %d want %d; body=%s", tc.path, rr.Code, tc.wantStatus, rr.Body.String())
			}
			if body := rr.Body.String(); !strings.Contains(body, tc.wantContains) {
				t.Fatalf("response body for %s missing %q: %s", tc.path, tc.wantContains, body)
			}
		})
	}
}

func TestDefaultRequestLoggerFactory_UsesResolvedLogDirectory(t *testing.T) {
	t.Setenv("WRITABLE_PATH", "")
	t.Setenv("writable_path", "")

	originalWD, errGetwd := os.Getwd()
	if errGetwd != nil {
		t.Fatalf("failed to get current working directory: %v", errGetwd)
	}

	tmpDir := t.TempDir()
	if errChdir := os.Chdir(tmpDir); errChdir != nil {
		t.Fatalf("failed to switch working directory: %v", errChdir)
	}
	defer func() {
		if errChdirBack := os.Chdir(originalWD); errChdirBack != nil {
			t.Fatalf("failed to restore working directory: %v", errChdirBack)
		}
	}()

	// Force ResolveLogDirectory to fallback to auth-dir/logs by making ./logs not a writable directory.
	if errWriteFile := os.WriteFile(filepath.Join(tmpDir, "logs"), []byte("not-a-directory"), 0o644); errWriteFile != nil {
		t.Fatalf("failed to create blocking logs file: %v", errWriteFile)
	}

	configDir := filepath.Join(tmpDir, "config")
	if errMkdirConfig := os.MkdirAll(configDir, 0o755); errMkdirConfig != nil {
		t.Fatalf("failed to create config dir: %v", errMkdirConfig)
	}
	configPath := filepath.Join(configDir, "config.yaml")

	authDir := filepath.Join(tmpDir, "auth")
	if errMkdirAuth := os.MkdirAll(authDir, 0o700); errMkdirAuth != nil {
		t.Fatalf("failed to create auth dir: %v", errMkdirAuth)
	}

	cfg := &proxyconfig.Config{
		SDKConfig: proxyconfig.SDKConfig{
			RequestLog: false,
		},
		AuthDir:           authDir,
		ErrorLogsMaxFiles: 10,
	}

	logger := defaultRequestLoggerFactory(cfg, configPath)
	fileLogger, ok := logger.(*internallogging.FileRequestLogger)
	if !ok {
		t.Fatalf("expected *FileRequestLogger, got %T", logger)
	}

	errLog := fileLogger.LogRequestWithOptions(
		"/v1/chat/completions",
		http.MethodPost,
		map[string][]string{"Content-Type": []string{"application/json"}},
		[]byte(`{"input":"hello"}`),
		http.StatusBadGateway,
		map[string][]string{"Content-Type": []string{"application/json"}},
		[]byte(`{"error":"upstream failure"}`),
		nil,
		nil,
		nil,
		nil,
		nil,
		true,
		"issue-1711",
		time.Now(),
		time.Now(),
	)
	if errLog != nil {
		t.Fatalf("failed to write forced error request log: %v", errLog)
	}

	authLogsDir := filepath.Join(authDir, "logs")
	authEntries, errReadAuthDir := os.ReadDir(authLogsDir)
	if errReadAuthDir != nil {
		t.Fatalf("failed to read auth logs dir %s: %v", authLogsDir, errReadAuthDir)
	}
	foundErrorLogInAuthDir := false
	for _, entry := range authEntries {
		if strings.HasPrefix(entry.Name(), "error-") && strings.HasSuffix(entry.Name(), ".log") {
			foundErrorLogInAuthDir = true
			break
		}
	}
	if !foundErrorLogInAuthDir {
		t.Fatalf("expected forced error log in auth fallback dir %s, got entries: %+v", authLogsDir, authEntries)
	}

	configLogsDir := filepath.Join(configDir, "logs")
	configEntries, errReadConfigDir := os.ReadDir(configLogsDir)
	if errReadConfigDir != nil && !os.IsNotExist(errReadConfigDir) {
		t.Fatalf("failed to inspect config logs dir %s: %v", configLogsDir, errReadConfigDir)
	}
	for _, entry := range configEntries {
		if strings.HasPrefix(entry.Name(), "error-") && strings.HasSuffix(entry.Name(), ".log") {
			t.Fatalf("unexpected forced error log in config dir %s", configLogsDir)
		}
	}
}
