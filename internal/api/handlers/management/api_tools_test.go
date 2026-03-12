package management

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	kimiauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/kimi"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

type memoryAuthStore struct {
	mu    sync.Mutex
	items map[string]*coreauth.Auth
}

func (s *memoryAuthStore) List(ctx context.Context) ([]*coreauth.Auth, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*coreauth.Auth, 0, len(s.items))
	for _, a := range s.items {
		out = append(out, a.Clone())
	}
	return out, nil
}

func (s *memoryAuthStore) Save(ctx context.Context, auth *coreauth.Auth) (string, error) {
	_ = ctx
	if auth == nil {
		return "", nil
	}
	s.mu.Lock()
	if s.items == nil {
		s.items = make(map[string]*coreauth.Auth)
	}
	s.items[auth.ID] = auth.Clone()
	s.mu.Unlock()
	return auth.ID, nil
}

func (s *memoryAuthStore) Delete(ctx context.Context, id string) error {
	_ = ctx
	s.mu.Lock()
	delete(s.items, id)
	s.mu.Unlock()
	return nil
}

func TestResolveTokenForAuth_Antigravity_RefreshesExpiredToken(t *testing.T) {
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
			t.Fatalf("unexpected content-type: %s", ct)
		}
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		values, err := url.ParseQuery(string(bodyBytes))
		if err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if values.Get("grant_type") != "refresh_token" {
			t.Fatalf("unexpected grant_type: %s", values.Get("grant_type"))
		}
		if values.Get("refresh_token") != "rt" {
			t.Fatalf("unexpected refresh_token: %s", values.Get("refresh_token"))
		}
		if values.Get("client_id") != antigravityOAuthClientID {
			t.Fatalf("unexpected client_id: %s", values.Get("client_id"))
		}
		if values.Get("client_secret") != antigravityOAuthClientSecret {
			t.Fatalf("unexpected client_secret")
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-token",
			"refresh_token": "rt2",
			"expires_in":    int64(3600),
			"token_type":    "Bearer",
		})
	}))
	t.Cleanup(srv.Close)

	originalURL := antigravityOAuthTokenURL
	antigravityOAuthTokenURL = srv.URL
	t.Cleanup(func() { antigravityOAuthTokenURL = originalURL })

	store := &memoryAuthStore{}
	manager := coreauth.NewManager(store, nil, nil)

	auth := &coreauth.Auth{
		ID:       "antigravity-test.json",
		FileName: "antigravity-test.json",
		Provider: "antigravity",
		Metadata: map[string]any{
			"type":          "antigravity",
			"access_token":  "old-token",
			"refresh_token": "rt",
			"expires_in":    int64(3600),
			"timestamp":     time.Now().Add(-2 * time.Hour).UnixMilli(),
			"expired":       time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	h := &Handler{authManager: manager}
	token, err := h.resolveTokenForAuth(context.Background(), auth)
	if err != nil {
		t.Fatalf("resolveTokenForAuth: %v", err)
	}
	if token != "new-token" {
		t.Fatalf("expected refreshed token, got %q", token)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 refresh call, got %d", callCount)
	}

	updated, ok := manager.GetByID(auth.ID)
	if !ok || updated == nil {
		t.Fatalf("expected auth in manager after update")
	}
	if got := tokenValueFromMetadata(updated.Metadata); got != "new-token" {
		t.Fatalf("expected manager metadata updated, got %q", got)
	}
}

func TestResolveTokenForAuth_Antigravity_SkipsRefreshWhenTokenValid(t *testing.T) {
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	originalURL := antigravityOAuthTokenURL
	antigravityOAuthTokenURL = srv.URL
	t.Cleanup(func() { antigravityOAuthTokenURL = originalURL })

	auth := &coreauth.Auth{
		ID:       "antigravity-valid.json",
		FileName: "antigravity-valid.json",
		Provider: "antigravity",
		Metadata: map[string]any{
			"type":         "antigravity",
			"access_token": "ok-token",
			"expired":      time.Now().Add(30 * time.Minute).Format(time.RFC3339),
		},
	}
	h := &Handler{}
	token, err := h.resolveTokenForAuth(context.Background(), auth)
	if err != nil {
		t.Fatalf("resolveTokenForAuth: %v", err)
	}
	if token != "ok-token" {
		t.Fatalf("expected existing token, got %q", token)
	}
	if callCount != 0 {
		t.Fatalf("expected no refresh calls, got %d", callCount)
	}
}

func TestResolveTokenForAuth_Kimi_RefreshesExpiredToken(t *testing.T) {
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("X-Msh-Device-Id"); got != "device-1" {
			t.Fatalf("unexpected X-Msh-Device-Id: %q", got)
		}
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		values, err := url.ParseQuery(string(bodyBytes))
		if err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if values.Get("grant_type") != "refresh_token" {
			t.Fatalf("unexpected grant_type: %s", values.Get("grant_type"))
		}
		if values.Get("refresh_token") != "rt" {
			t.Fatalf("unexpected refresh_token: %s", values.Get("refresh_token"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "kimi-new-token",
			"refresh_token": "kimi-rt2",
			"expires_in":    3600,
			"token_type":    "Bearer",
			"scope":         "coding",
		})
	}))
	t.Cleanup(srv.Close)

	originalTransport := http.DefaultTransport
	http.DefaultTransport = rewriteHostTransport(t, srv.URL)
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	store := &memoryAuthStore{}
	manager := coreauth.NewManager(store, nil, nil)
	auth := &coreauth.Auth{
		ID:       "kimi-test.json",
		FileName: "kimi-test.json",
		Provider: "kimi",
		Metadata: map[string]any{
			"type":          "kimi",
			"access_token":  "old-token",
			"refresh_token": "rt",
			"expired":       time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			"device_id":     "device-1",
		},
		Storage: &kimiauth.KimiTokenStorage{
			RefreshToken: "rt",
			DeviceID:     "device-1",
			Type:         "kimi",
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	h := &Handler{authManager: manager}
	token, err := h.resolveTokenForAuth(context.Background(), auth)
	if err != nil {
		t.Fatalf("resolveTokenForAuth: %v", err)
	}
	if token != "kimi-new-token" {
		t.Fatalf("expected refreshed token, got %q", token)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 refresh call, got %d", callCount)
	}

	updated, ok := manager.GetByID(auth.ID)
	if !ok || updated == nil {
		t.Fatalf("expected auth in manager after update")
	}
	if got := tokenValueFromMetadata(updated.Metadata); got != "kimi-new-token" {
		t.Fatalf("expected manager metadata updated, got %q", got)
	}
	if got := stringValue(updated.Metadata, "device_id"); got != "device-1" {
		t.Fatalf("expected device_id to persist, got %q", got)
	}
}

func TestKimiRefreshConfigForAuth_PrefersAuthProxy(t *testing.T) {
	cfg := &config.Config{}
	auth := &coreauth.Auth{ProxyURL: "http://127.0.0.1:8080"}

	cloned := kimiRefreshConfigForAuth(cfg, auth)
	if cloned == nil {
		t.Fatalf("expected config clone")
	}
	if cloned == cfg {
		t.Fatalf("expected cloned config when auth proxy is present")
	}
	if got := cloned.SDKConfig.ProxyURL; got != auth.ProxyURL {
		t.Fatalf("ProxyURL = %q, want %q", got, auth.ProxyURL)
	}
}

func TestAPICall_KimiAutoInjectsDeviceHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var gotAuth string
	var gotDeviceID string
	var gotUserAgent string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotDeviceID = r.Header.Get("X-Msh-Device-Id")
		gotUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	store := &memoryAuthStore{}
	manager := coreauth.NewManager(store, nil, nil)
	auth := &coreauth.Auth{
		ID:       "kimi-api-call.json",
		FileName: "kimi-api-call.json",
		Provider: "kimi",
		Metadata: map[string]any{
			"type":         "kimi",
			"access_token": "kimi-token",
			"device_id":    "device-42",
			"expired":      time.Now().Add(30 * time.Minute).Format(time.RFC3339),
		},
		Storage: &kimiauth.KimiTokenStorage{
			AccessToken: "kimi-token",
			DeviceID:    "device-42",
			Type:        "kimi",
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	h := &Handler{authManager: manager}

	body := `{"auth_index":"` + auth.EnsureIndex() + `","method":"GET","url":"` + upstream.URL + `","header":{"Authorization":"Bearer $TOKEN$"}}`
	req := httptest.NewRequest(http.MethodPost, "/v0/management/api-call", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = req

	h.APICall(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotAuth != "Bearer kimi-token" {
		t.Fatalf("unexpected Authorization header: %q", gotAuth)
	}
	if gotDeviceID != "device-42" {
		t.Fatalf("unexpected X-Msh-Device-Id: %q", gotDeviceID)
	}
	if gotUserAgent != "KimiCLI/1.10.6" {
		t.Fatalf("unexpected User-Agent: %q", gotUserAgent)
	}
}

func TestAPICall_KimiLocalDeviceIDFallbackIsNotPersisted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	setKimiHomeEnv(t, tempDir)
	deviceDir := kimiDeviceDirForTest(tempDir)
	if err := os.MkdirAll(deviceDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(deviceDir, "device_id"), []byte("local-device\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var gotDeviceID string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDeviceID = r.Header.Get("X-Msh-Device-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	store := &memoryAuthStore{}
	manager := coreauth.NewManager(store, nil, nil)
	auth := &coreauth.Auth{
		ID:       "kimi-local-fallback.json",
		FileName: "kimi-local-fallback.json",
		Provider: "kimi",
		Metadata: map[string]any{
			"type":         "kimi",
			"access_token": "kimi-token",
			"expired":      time.Now().Add(30 * time.Minute).Format(time.RFC3339),
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	h := &Handler{authManager: manager}
	body := `{"auth_index":"` + auth.EnsureIndex() + `","method":"GET","url":"` + upstream.URL + `","header":{"Authorization":"Bearer $TOKEN$"}}`
	req := httptest.NewRequest(http.MethodPost, "/v0/management/api-call", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = req

	h.APICall(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotDeviceID != "local-device" {
		t.Fatalf("unexpected X-Msh-Device-Id: %q", gotDeviceID)
	}
	updated, ok := manager.GetByID(auth.ID)
	if !ok || updated == nil {
		t.Fatalf("expected auth in manager after update")
	}
	if got := stringValue(updated.Metadata, "device_id"); got != "" {
		t.Fatalf("local fallback device_id should not persist, got %q", got)
	}
}

func rewriteHostTransport(t *testing.T, target string) http.RoundTripper {
	t.Helper()

	targetURL, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parse target url: %v", err)
	}

	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok || base == nil {
		t.Fatalf("expected default transport to be *http.Transport")
	}
	clone := base.Clone()
	clone.Proxy = nil
	clone.DialContext = nil

	return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		cloned := req.Clone(req.Context())
		cloned.URL.Scheme = targetURL.Scheme
		cloned.URL.Host = targetURL.Host
		cloned.Host = targetURL.Host
		return clone.RoundTrip(cloned)
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestLocalKimiDeviceID_ReadsPlatformPath(t *testing.T) {
	tempDir := t.TempDir()
	setKimiHomeEnv(t, tempDir)
	deviceDir := kimiDeviceDirForTest(tempDir)
	if err := os.MkdirAll(deviceDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(deviceDir, "device_id"), []byte("local-device\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if got := localKimiDeviceID(); got != "local-device" {
		t.Fatalf("localKimiDeviceID() = %q, want %q", got, "local-device")
	}
}

func kimiDeviceDirForTest(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "kimi")
	case "windows":
		return filepath.Join(home, "AppData", "Roaming", "kimi")
	default:
		return filepath.Join(home, ".local", "share", "kimi")
	}
}

func setKimiHomeEnv(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	}
}
