package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"golang.org/x/crypto/bcrypt"
)

func TestStateMiddleware_DoesNotBlockConcurrentSetConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("generate hash: %v", err)
	}

	h := NewHandler(&config.Config{RemoteManagement: config.RemoteManagement{SecretKey: string(hash)}}, filepath.Join(t.TempDir(), "config.yaml"), nil)
	r := gin.New()
	release := make(chan struct{})
	reached := make(chan struct{})
	r.GET("/guarded", h.StateMiddleware(), func(c *gin.Context) {
		close(reached)
		<-release
		c.Status(http.StatusNoContent)
	})

	go func() {
		req := httptest.NewRequest(http.MethodGet, "/guarded", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
	}()

	select {
	case <-reached:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not enter guarded handler")
	}

	updated := make(chan struct{})
	go func() {
		h.SetConfig(&config.Config{})
		close(updated)
	}()

	select {
	case <-updated:
	case <-time.After(150 * time.Millisecond):
		t.Fatal("SetConfig should not wait for in-flight management request")
	}

	close(release)

	select {
	case <-updated:
	case <-time.After(2 * time.Second):
		t.Fatal("SetConfig did not complete after request finished")
	}
}

func TestPutLogsMaxTotalSizeMB_RejectsOversizedValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("logging-to-file: false\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	h := NewHandler(&config.Config{}, configPath, nil)
	r := gin.New()
	r.PUT("/logs-max-total-size-mb", h.StateMiddleware(), h.PutLogsMaxTotalSizeMB)

	body := strings.NewReader(`{"value":1048577}`)
	req := httptest.NewRequest(http.MethodPut, "/logs-max-total-size-mb", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !strings.Contains(resp["error"], "exceeds allowed maximum") {
		t.Fatalf("unexpected error response: %v", resp)
	}
}

func TestStateMiddleware_DoesNotDeadlockRegisterOAuthSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewHandler(&config.Config{AuthDir: t.TempDir()}, filepath.Join(t.TempDir(), "config.yaml"), nil)
	r := gin.New()
	r.GET("/oauth", h.StateMiddleware(), func(c *gin.Context) {
		h.registerOAuthSession("state-for-deadlock-check", "codex")
		c.Status(http.StatusNoContent)
	})

	done := make(chan struct{})
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/oauth", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("registerOAuthSession should not block while management request is in flight")
	}
}

func TestApplyConfigMutation_AppliesPersistedConfigViaRuntimeApplier(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("request-log: false\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	h := NewHandler(&config.Config{}, configPath, nil)
	applied := 0
	applierSawPersisted := false
	h.SetRuntimeApplier(func(cfg *config.Config) error {
		applied++
		if cfg != nil && cfg.RequestLog {
			persisted, err := os.ReadFile(configPath)
			if err == nil && strings.Contains(string(persisted), "request-log: true") {
				applierSawPersisted = true
			}
		}
		return nil
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	if !h.applyConfigMutation(ctx, func(cfg *config.Config) error {
		cfg.RequestLog = true
		return nil
	}) {
		t.Fatalf("expected applyConfigMutation to succeed, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if applied != 1 {
		t.Fatalf("expected runtime applier to be called once, got %d", applied)
	}
	if !applierSawPersisted {
		t.Fatal("expected runtime applier to observe committed config on disk")
	}

	snapshot, err := h.runtimeSnapshot()
	if err != nil {
		t.Fatalf("runtime snapshot: %v", err)
	}
	if snapshot.cfg == nil || !snapshot.cfg.RequestLog {
		t.Fatalf("expected runtime snapshot config to include request-log=true, got %+v", snapshot.cfg)
	}
}

func TestPutConfigYAML_ClampsOversizedLogLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("logging-to-file: false\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	h := NewHandler(&config.Config{}, configPath, nil)
	applied := 0
	applierSawClamped := false
	applierSawPersisted := false
	h.SetRuntimeApplier(func(cfg *config.Config) error {
		applied++
		if cfg != nil && cfg.LogsMaxTotalSizeMB == config.MaxLogsMaxTotalSizeMB {
			applierSawClamped = true
		}
		persisted, err := os.ReadFile(configPath)
		if err == nil && strings.Contains(string(persisted), "logs-max-total-size-mb: 1024") {
			applierSawPersisted = true
		}
		return nil
	})

	r := gin.New()
	r.PUT("/config.yaml", h.PutConfigYAML)

	req := httptest.NewRequest(http.MethodPut, "/config.yaml", strings.NewReader("logs-max-total-size-mb: 1048577\n"))
	req.Header.Set("Content-Type", "application/yaml")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected oversized config to be clamped, got %d body=%s", w.Code, w.Body.String())
	}
	if applied != 1 {
		t.Fatalf("expected runtime applier to be called once, got %d", applied)
	}
	if !applierSawClamped {
		t.Fatal("expected runtime applier to receive clamped committed config")
	}
	if !applierSawPersisted {
		t.Fatal("expected runtime applier to observe committed config on disk")
	}

	snapshot, err := h.runtimeSnapshot()
	if err != nil {
		t.Fatalf("runtime snapshot: %v", err)
	}
	if snapshot.cfg == nil {
		t.Fatal("expected runtime snapshot config to be available")
	}
	if snapshot.cfg.LogsMaxTotalSizeMB != config.MaxLogsMaxTotalSizeMB {
		t.Fatalf("expected logs-max-total-size-mb to be clamped to %d, got %d", config.MaxLogsMaxTotalSizeMB, snapshot.cfg.LogsMaxTotalSizeMB)
	}

	persisted, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read persisted config: %v", err)
	}
	persistedText := string(persisted)
	if strings.Contains(persistedText, "1048577") {
		t.Fatalf("expected persisted config to remove oversized value, got %s", persistedText)
	}
	if !strings.Contains(persistedText, "logs-max-total-size-mb: 1024") {
		t.Fatalf("expected persisted config to contain clamped value, got %s", persistedText)
	}
}

func TestApplyConfigMutation_RuntimeApplierMayUpdateHandlerState(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("request-log: false\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	h := NewHandler(&config.Config{}, configPath, nil)
	done := make(chan struct{})
	h.SetRuntimeApplier(func(cfg *config.Config) error {
		h.SetConfig(cfg)
		close(done)
		return nil
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		if !h.applyConfigMutation(ctx, func(cfg *config.Config) error {
			cfg.RequestLog = true
			return nil
		}) {
			t.Errorf("expected applyConfigMutation to succeed, got %d body=%s", recorder.Code, recorder.Body.String())
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runtime applier should be able to update handler state without deadlocking")
	}

	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("applyConfigMutation should finish after runtime applier updates handler state")
	}
}
