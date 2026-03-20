package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gin "github.com/gin-gonic/gin"
	managementHandlers "github.com/router-for-me/CLIProxyAPI/v6/internal/api/handlers/management"
	proxyconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	internallogging "github.com/router-for-me/CLIProxyAPI/v6/internal/logging"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v6/sdk/access"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()

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
	return NewServer(cfg, authManager, accessManager, configPath)
}

func TestAuthMiddleware_NilManagerRejectsRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AuthMiddleware(nil))
	r.GET("/protected", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status: got %d body=%s", w.Code, w.Body.String())
	}
}

func TestNewServer_CreatesAccessManagerWhenNil(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}

	cfg := &proxyconfig.Config{
		SDKConfig: sdkconfig.SDKConfig{APIKeys: []string{"test-key"}},
		Port:      0,
		AuthDir:   authDir,
	}

	server := NewServer(cfg, auth.NewManager(nil, nil, nil), nil, filepath.Join(tmpDir, "config.yaml"))

	unauthorized := httptest.NewRecorder()
	server.engine.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without credentials, got %d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	authorizedReq := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	authorizedReq.Header.Set("Authorization", "Bearer test-key")
	authorized := httptest.NewRecorder()
	server.engine.ServeHTTP(authorized, authorizedReq)
	if authorized.Code != http.StatusOK {
		t.Fatalf("expected authorized request to succeed, got %d body=%s", authorized.Code, authorized.Body.String())
	}
}
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

func TestOAuthCallbackRoute_UsesSessionAuthDir(t *testing.T) {
	server := newTestServer(t)

	sessionAuthDir := filepath.Join(t.TempDir(), "session-auth")
	if err := os.MkdirAll(sessionAuthDir, 0o700); err != nil {
		t.Fatalf("failed to create session auth dir: %v", err)
	}

	state := "codex-route-session-state"
	managementHandlers.RegisterOAuthSession(state, "codex", sessionAuthDir)

	req := httptest.NewRequest(http.MethodGet, "/codex/callback?state="+state+"&code=route-code", nil)
	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	sessionPath := filepath.Join(sessionAuthDir, ".oauth-codex-"+state+".oauth")
	configPath := filepath.Join(server.cfg.AuthDir, ".oauth-codex-"+state+".oauth")
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("expected callback file in session auth dir, stat err: %v", err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected no callback file in config auth dir, stat err: %v", err)
	}
}

func TestOAuthCallbackRoute_ReturnsErrorWhenCallbackFileWriteFails(t *testing.T) {
	server := newTestServer(t)

	blockedAuthDir := filepath.Join(t.TempDir(), "blocked-auth-dir")
	if err := os.WriteFile(blockedAuthDir, []byte("blocked"), 0o600); err != nil {
		t.Fatalf("failed to create blocking auth path: %v", err)
	}

	state := "codex-route-write-failure"
	managementHandlers.RegisterOAuthSession(state, "codex", blockedAuthDir)

	req := httptest.NewRequest(http.MethodGet, "/codex/callback?state="+state+"&code=route-code", nil)
	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusInternalServerError, rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "Authentication successful") {
		t.Fatalf("expected failure response, got %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Authentication failed") {
		t.Fatalf("expected failure response body, got %s", rr.Body.String())
	}

	sessionPath := filepath.Join(blockedAuthDir, ".oauth-codex-"+state+".oauth")
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("expected no callback file when write fails, stat err: %v", err)
	}
	if managementHandlers.IsOAuthSessionPending(state, "codex") {
		t.Fatal("expected callback write failure to end pending oauth session")
	}
	provider, status, ok := managementHandlers.GetOAuthSession(state)
	if !ok {
		t.Fatal("expected oauth session to remain available with error status")
	}
	if provider != "codex" {
		t.Fatalf("expected provider codex, got %q", provider)
	}
	if strings.TrimSpace(status) == "" {
		t.Fatal("expected oauth session error status to be recorded")
	}
}

func TestManagementPutWebsocketAuth_AppliesCommittedConfigAndUpdatesRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("MANAGEMENT_PASSWORD", "local-secret")

	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}

	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("api-keys:\n  - test-key\nws-auth: false\n"), 0o600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg := &proxyconfig.Config{
		SDKConfig: sdkconfig.SDKConfig{APIKeys: []string{"test-key"}},
		AuthDir:   authDir,
	}
	server := NewServer(cfg, auth.NewManager(nil, nil, nil), nil, configPath, WithLocalManagementPassword("local-secret"))
	server.AttachWebsocketRoute("/live-ws", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	beforeReq := httptest.NewRequest(http.MethodGet, "/live-ws", nil)
	beforeReq.RemoteAddr = "127.0.0.1:12345"
	beforeResp := httptest.NewRecorder()
	server.engine.ServeHTTP(beforeResp, beforeReq)
	if beforeResp.Code != http.StatusNoContent {
		t.Fatalf("expected websocket route to be open before ws-auth update, got %d body=%s", beforeResp.Code, beforeResp.Body.String())
	}

	mgmtReq := httptest.NewRequest(http.MethodPut, "/v0/management/ws-auth", strings.NewReader(`{"value":true}`))
	mgmtReq.RemoteAddr = "127.0.0.1:12345"
	mgmtReq.Header.Set("Authorization", "Bearer local-secret")
	mgmtReq.Header.Set("Content-Type", "application/json")
	mgmtResp := httptest.NewRecorder()
	server.engine.ServeHTTP(mgmtResp, mgmtReq)
	if mgmtResp.Code != http.StatusOK {
		t.Fatalf("expected management update to succeed, got %d body=%s", mgmtResp.Code, mgmtResp.Body.String())
	}

	if server.cfg == nil || !server.cfg.WebsocketAuth {
		t.Fatalf("expected server config to reflect committed ws-auth update, got %+v", server.cfg)
	}

	persisted, err := proxyconfig.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load persisted config: %v", err)
	}
	if !persisted.WebsocketAuth {
		t.Fatalf("expected persisted config to enable ws-auth, got %+v", persisted)
	}

	afterReq := httptest.NewRequest(http.MethodGet, "/live-ws", nil)
	afterReq.RemoteAddr = "127.0.0.1:12345"
	afterResp := httptest.NewRecorder()
	server.engine.ServeHTTP(afterResp, afterReq)
	if afterResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected websocket route to require auth after ws-auth update, got %d body=%s", afterResp.Code, afterResp.Body.String())
	}

	authorizedReq := httptest.NewRequest(http.MethodGet, "/live-ws", nil)
	authorizedReq.RemoteAddr = "127.0.0.1:12345"
	authorizedReq.Header.Set("Authorization", "Bearer test-key")
	authorizedResp := httptest.NewRecorder()
	server.engine.ServeHTTP(authorizedResp, authorizedReq)
	if authorizedResp.Code != http.StatusNoContent {
		t.Fatalf("expected authorized websocket route to succeed after ws-auth update, got %d body=%s", authorizedResp.Code, authorizedResp.Body.String())
	}
}

func TestManagementPutConfigYAML_AppliesCommittedConfigAndEnablesRequestLogging(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("MANAGEMENT_PASSWORD", "local-secret")

	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}

	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("api-keys:\n  - test-key\nrequest-log: false\n"), 0o600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg := &proxyconfig.Config{
		SDKConfig: sdkconfig.SDKConfig{
			APIKeys:    []string{"test-key"},
			RequestLog: false,
		},
		AuthDir: authDir,
	}
	server := NewServer(cfg, auth.NewManager(nil, nil, nil), nil, configPath, WithLocalManagementPassword("local-secret"))
	if server.requestLogger == nil {
		t.Fatal("expected request logger to be configured")
	}
	if server.requestLogger.IsEnabled() {
		t.Fatal("expected request logger to start disabled")
	}

	mgmtReq := httptest.NewRequest(http.MethodPut, "/v0/management/config.yaml", strings.NewReader("api-keys:\n  - test-key\nrequest-log: true\n"))
	mgmtReq.RemoteAddr = "127.0.0.1:12345"
	mgmtReq.Header.Set("Authorization", "Bearer local-secret")
	mgmtReq.Header.Set("Content-Type", "application/yaml")
	mgmtResp := httptest.NewRecorder()
	server.engine.ServeHTTP(mgmtResp, mgmtReq)
	if mgmtResp.Code != http.StatusOK {
		t.Fatalf("expected config.yaml update to succeed, got %d body=%s", mgmtResp.Code, mgmtResp.Body.String())
	}

	if server.cfg == nil || !server.cfg.RequestLog {
		t.Fatalf("expected server config to reflect committed request-log update, got %+v", server.cfg)
	}
	if !server.requestLogger.IsEnabled() {
		t.Fatal("expected request logger to be enabled after config.yaml update")
	}

	persisted, err := proxyconfig.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load persisted config: %v", err)
	}
	if !persisted.RequestLog {
		t.Fatalf("expected persisted config to enable request-log, got %+v", persisted)
	}
}
