package management

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

type blockingReadCloser struct {
	reader      *strings.Reader
	readStarted chan struct{}
	releaseRead chan struct{}
	once        sync.Once
}

func (b *blockingReadCloser) Read(p []byte) (int, error) {
	b.once.Do(func() {
		close(b.readStarted)
		<-b.releaseRead
	})
	return b.reader.Read(p)
}

func (b *blockingReadCloser) Close() error {
	return nil
}

func writeTestConfigFile(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if errWrite := os.WriteFile(path, []byte("{}\n"), 0o600); errWrite != nil {
		t.Fatalf("failed to write test config: %v", errWrite)
	}
	return path
}

func TestDeleteGeminiKey_RequiresBaseURLWhenAPIKeyDuplicated(t *testing.T) {
	t.Parallel()

	h := &Handler{
		cfg: &config.Config{
			GeminiKey: []config.GeminiKey{
				{APIKey: "shared-key", BaseURL: "https://a.example.com"},
				{APIKey: "shared-key", BaseURL: "https://b.example.com"},
			},
		},
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodDelete, "/v0/management/gemini-api-key?api-key=shared-key", nil)

	h.DeleteGeminiKey(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if got := len(h.cfg.GeminiKey); got != 2 {
		t.Fatalf("gemini keys len = %d, want 2", got)
	}
}

func TestPatchAPIKeysUsesCurrentConfigAfterSetConfig(t *testing.T) {
	t.Parallel()

	oldCfg := &config.Config{SDKConfig: config.SDKConfig{APIKeys: []string{"old-key"}}}
	newCfg := &config.Config{SDKConfig: config.SDKConfig{APIKeys: []string{"new-key"}}}
	h := &Handler{
		cfg:            oldCfg,
		configFilePath: writeTestConfigFile(t),
	}

	readStarted := make(chan struct{})
	releaseRead := make(chan struct{})
	body := &blockingReadCloser{
		reader:      strings.NewReader(`{"index":0,"value":"patched-key"}`),
		readStarted: readStarted,
		releaseRead: releaseRead,
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/api-keys", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Body = body

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.PatchAPIKeys(c)
	}()

	select {
	case <-readStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for PatchAPIKeys to read request body")
	}
	h.SetConfig(newCfg)
	close(releaseRead)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("PatchAPIKeys did not finish")
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := oldCfg.APIKeys[0]; got != "old-key" {
		t.Fatalf("old config API key = %q, want unchanged old-key", got)
	}
	if got := newCfg.APIKeys[0]; got != "patched-key" {
		t.Fatalf("new config API key = %q, want patched-key", got)
	}
}

func TestDeleteAPIKeysUsesCurrentConfigAfterSetConfig(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousProcs)

	oldCfg := &config.Config{SDKConfig: config.SDKConfig{APIKeys: []string{"old-key"}}}
	newCfg := &config.Config{SDKConfig: config.SDKConfig{APIKeys: []string{"new-key"}}}
	h := &Handler{
		cfg:            oldCfg,
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodDelete, "/v0/management/api-keys?index=0", nil)

	h.mu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.DeleteAPIKeys(c)
	}()
	runtime.Gosched()
	h.cfg = newCfg
	h.mu.Unlock()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("DeleteAPIKeys did not finish")
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := len(oldCfg.APIKeys); got != 1 {
		t.Fatalf("old config API keys len = %d, want 1", got)
	}
	if got := len(newCfg.APIKeys); got != 0 {
		t.Fatalf("new config API keys len = %d, want 0", got)
	}
}

func TestDeleteGeminiKey_DeletesOnlyMatchingBaseURL(t *testing.T) {
	t.Parallel()

	h := &Handler{
		cfg: &config.Config{
			GeminiKey: []config.GeminiKey{
				{APIKey: "shared-key", BaseURL: "https://a.example.com"},
				{APIKey: "shared-key", BaseURL: "https://b.example.com"},
			},
		},
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodDelete, "/v0/management/gemini-api-key?api-key=shared-key&base-url=https://a.example.com", nil)

	h.DeleteGeminiKey(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := len(h.cfg.GeminiKey); got != 1 {
		t.Fatalf("gemini keys len = %d, want 1", got)
	}
	if got := h.cfg.GeminiKey[0].BaseURL; got != "https://b.example.com" {
		t.Fatalf("remaining base-url = %q, want %q", got, "https://b.example.com")
	}
}

func TestDeleteClaudeKey_DeletesEmptyBaseURLWhenExplicitlyProvided(t *testing.T) {
	t.Parallel()

	h := &Handler{
		cfg: &config.Config{
			ClaudeKey: []config.ClaudeKey{
				{APIKey: "shared-key", BaseURL: ""},
				{APIKey: "shared-key", BaseURL: "https://claude.example.com"},
			},
		},
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodDelete, "/v0/management/claude-api-key?api-key=shared-key&base-url=", nil)

	h.DeleteClaudeKey(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := len(h.cfg.ClaudeKey); got != 1 {
		t.Fatalf("claude keys len = %d, want 1", got)
	}
	if got := h.cfg.ClaudeKey[0].BaseURL; got != "https://claude.example.com" {
		t.Fatalf("remaining base-url = %q, want %q", got, "https://claude.example.com")
	}
}

func TestDeleteVertexCompatKey_DeletesOnlyMatchingBaseURL(t *testing.T) {
	t.Parallel()

	h := &Handler{
		cfg: &config.Config{
			VertexCompatAPIKey: []config.VertexCompatKey{
				{APIKey: "shared-key", BaseURL: "https://a.example.com"},
				{APIKey: "shared-key", BaseURL: "https://b.example.com"},
			},
		},
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodDelete, "/v0/management/vertex-api-key?api-key=shared-key&base-url=https://b.example.com", nil)

	h.DeleteVertexCompatKey(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := len(h.cfg.VertexCompatAPIKey); got != 1 {
		t.Fatalf("vertex keys len = %d, want 1", got)
	}
	if got := h.cfg.VertexCompatAPIKey[0].BaseURL; got != "https://a.example.com" {
		t.Fatalf("remaining base-url = %q, want %q", got, "https://a.example.com")
	}
}

func TestDeleteXAIKey_RequiresBaseURLWhenAPIKeyDuplicated(t *testing.T) {
	t.Parallel()

	h := &Handler{
		cfg: &config.Config{
			XAIKey: []config.XAIKey{
				{APIKey: "shared-key", BaseURL: "https://a.example.com"},
				{APIKey: "shared-key", BaseURL: "https://b.example.com"},
			},
		},
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodDelete, "/v0/management/xai-api-key?api-key=shared-key", nil)

	h.DeleteXAIKey(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if got := len(h.cfg.XAIKey); got != 2 {
		t.Fatalf("xAI keys len = %d, want 2", got)
	}
}

func TestDeleteCodexKey_RequiresBaseURLWhenAPIKeyDuplicated(t *testing.T) {
	t.Parallel()

	h := &Handler{
		cfg: &config.Config{
			CodexKey: []config.CodexKey{
				{APIKey: "shared-key", BaseURL: "https://a.example.com"},
				{APIKey: "shared-key", BaseURL: "https://b.example.com"},
			},
		},
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodDelete, "/v0/management/codex-api-key?api-key=shared-key", nil)

	h.DeleteCodexKey(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if got := len(h.cfg.CodexKey); got != 2 {
		t.Fatalf("codex keys len = %d, want 2", got)
	}
}
