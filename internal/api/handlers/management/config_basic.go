package management

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

const (
	latestReleaseURL       = "https://api.github.com/repos/router-for-me/CLIProxyAPI/releases/latest"
	latestReleaseUserAgent = "CLIProxyAPI"
)

func (h *Handler) GetConfig(c *gin.Context) {
	if h == nil {
		c.JSON(200, gin.H{})
		return
	}
	// Serialize the config to JSON while holding h.mu. Other management
	// handlers mutate fields of *h.cfg under the same lock before persist, and
	// PutConfigYAML replaces h.cfg entirely under the lock. Encoding under the
	// lock guarantees a consistent snapshot. Secrets are redacted so a stolen
	// management session cannot bulk-export every provider API key; dedicated
	// key endpoints remain available for full values when needed.
	h.mu.Lock()
	cfg := h.cfg
	if cfg == nil {
		h.mu.Unlock()
		c.JSON(200, gin.H{})
		return
	}
	rendered, errMarshal := marshalConfigForManagementJSON(cfg)
	h.mu.Unlock()
	if errMarshal != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "encode_failed", "message": "failed to encode config"})
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", rendered)
}

type releaseInfo struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
}

// GetLatestVersion returns the latest release version from GitHub without downloading assets.
func (h *Handler) GetLatestVersion(c *gin.Context) {
	client := &http.Client{Timeout: 10 * time.Second}
	proxyURL := ""
	if h != nil {
		h.mu.Lock()
		if h.cfg != nil {
			proxyURL = strings.TrimSpace(h.cfg.ProxyURL)
		}
		h.mu.Unlock()
	}
	if proxyURL != "" {
		sdkCfg := &sdkconfig.SDKConfig{ProxyURL: proxyURL}
		util.SetProxy(sdkCfg, client)
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "request_create_failed", "message": err.Error()})
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", latestReleaseUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "request_failed", "message": err.Error()})
		return
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.WithError(errClose).Debug("failed to close latest version response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		c.JSON(http.StatusBadGateway, gin.H{"error": "unexpected_status", "message": fmt.Sprintf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))})
		return
	}

	var info releaseInfo
	if errDecode := json.NewDecoder(resp.Body).Decode(&info); errDecode != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "decode_failed", "message": errDecode.Error()})
		return
	}

	version := strings.TrimSpace(info.TagName)
	if version == "" {
		version = strings.TrimSpace(info.Name)
	}
	if version == "" {
		c.JSON(http.StatusBadGateway, gin.H{"error": "invalid_response", "message": "missing release version"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"latest-version": version})
}

func WriteConfig(path string, data []byte) error {
	data = config.NormalizeCommentIndentation(data)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, errWrite := f.Write(data); errWrite != nil {
		_ = f.Close()
		return errWrite
	}
	if errSync := f.Sync(); errSync != nil {
		_ = f.Close()
		return errSync
	}
	return f.Close()
}

func (h *Handler) PutConfigYAML(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_yaml", "message": "cannot read request body"})
		return
	}
	var cfg config.Config
	if err = yaml.Unmarshal(body, &cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_yaml", "message": err.Error()})
		return
	}
	// Validate config using LoadConfigOptional with optional=false to enforce parsing
	tmpDir := filepath.Dir(h.configFilePath)
	tmpFile, err := os.CreateTemp(tmpDir, "config-validate-*.yaml")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "write_failed", "message": err.Error()})
		return
	}
	tempFile := tmpFile.Name()
	if _, errWrite := tmpFile.Write(body); errWrite != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tempFile)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "write_failed", "message": errWrite.Error()})
		return
	}
	if errClose := tmpFile.Close(); errClose != nil {
		_ = os.Remove(tempFile)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "write_failed", "message": errClose.Error()})
		return
	}
	defer func() {
		_ = os.Remove(tempFile)
	}()
	_, err = config.LoadConfigOptional(tempFile, false)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_config", "message": err.Error()})
		return
	}
	h.mu.Lock()
	// Restore redacted secrets before persisting. The management JSON GET
	// endpoint returns masked api-keys/proxy-urls/headers so a stolen
	// management session cannot bulk-export credentials; if that masked
	// snapshot is round-tripped back through PUT /config.yaml we must undo
	// the masking against the live config under the lock or every provider
	// auth on disk gets corrupted. Restoration is structural-preserve and
	// only substitutes values whose redaction matches the live secret.
	restored, errRestore := restoreRedactedSecrets(body, h.cfg)
	if errRestore != nil {
		h.mu.Unlock()
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "redaction_restore_failed", "message": errRestore.Error()})
		return
	}
	body = restored
	if WriteConfig(h.configFilePath, body) != nil {
		h.mu.Unlock()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "write_failed", "message": "failed to write config"})
		return
	}
	// Reload into handler to keep memory in sync
	newCfg, err := config.LoadConfig(h.configFilePath)
	if err != nil {
		h.mu.Unlock()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reload_failed", "message": err.Error()})
		return
	}
	h.cfg = newCfg
	snapshot := h.reloadSnapshotConfigLocked()
	h.mu.Unlock()
	var reqCtx context.Context
	if c != nil && c.Request != nil {
		reqCtx = c.Request.Context()
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "changed": []string{"config"}})
	h.reloadConfigAfterManagementSaveAsync(reqCtx, snapshot)
}

// GetConfigYAML returns the raw config.yaml file bytes without re-encoding.
// It preserves comments and original formatting/styles.
func (h *Handler) GetConfigYAML(c *gin.Context) {
	data, err := os.ReadFile(h.configFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "config file not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "read_failed", "message": err.Error()})
		return
	}
	c.Header("Content-Type", "application/yaml; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	// Write raw bytes as-is
	_, _ = c.Writer.Write(data)
}

// Debug
func (h *Handler) GetDebug(c *gin.Context) {
	h.mu.Lock()
	v := h.cfg != nil && h.cfg.Debug
	h.mu.Unlock()
	c.JSON(200, gin.H{"debug": v})
}
func (h *Handler) PutDebug(c *gin.Context) { h.updateBoolField(c, func(v bool) { h.cfg.Debug = v }) }

// UsageStatisticsEnabled
func (h *Handler) GetUsageStatisticsEnabled(c *gin.Context) {
	h.mu.Lock()
	v := h.cfg != nil && h.cfg.UsageStatisticsEnabled
	h.mu.Unlock()
	c.JSON(200, gin.H{"usage-statistics-enabled": v})
}
func (h *Handler) PutUsageStatisticsEnabled(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.UsageStatisticsEnabled = v })
}

// LoggingToFile
func (h *Handler) GetLoggingToFile(c *gin.Context) {
	h.mu.Lock()
	v := h.cfg != nil && h.cfg.LoggingToFile
	h.mu.Unlock()
	c.JSON(200, gin.H{"logging-to-file": v})
}
func (h *Handler) PutLoggingToFile(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.LoggingToFile = v })
}

// LogsMaxTotalSizeMB
func (h *Handler) GetLogsMaxTotalSizeMB(c *gin.Context) {
	h.mu.Lock()
	v := 0
	if h.cfg != nil {
		v = h.cfg.LogsMaxTotalSizeMB
	}
	h.mu.Unlock()
	c.JSON(200, gin.H{"logs-max-total-size-mb": v})
}
func (h *Handler) PutLogsMaxTotalSizeMB(c *gin.Context) {
	h.updateIntFieldClamped(c, 0, func(v int) { h.cfg.LogsMaxTotalSizeMB = v })
}

// ErrorLogsMaxFiles
func (h *Handler) GetErrorLogsMaxFiles(c *gin.Context) {
	h.mu.Lock()
	v := 0
	if h.cfg != nil {
		v = h.cfg.ErrorLogsMaxFiles
	}
	h.mu.Unlock()
	c.JSON(200, gin.H{"error-logs-max-files": v})
}
func (h *Handler) PutErrorLogsMaxFiles(c *gin.Context) {
	h.updateIntFieldClamped(c, 10, func(v int) { h.cfg.ErrorLogsMaxFiles = v })
}

// Request log
func (h *Handler) GetRequestLog(c *gin.Context) {
	h.mu.Lock()
	v := h.cfg != nil && h.cfg.RequestLog
	h.mu.Unlock()
	c.JSON(200, gin.H{"request-log": v})
}
func (h *Handler) PutRequestLog(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.RequestLog = v })
}

// Websocket auth
func (h *Handler) GetWebsocketAuth(c *gin.Context) {
	h.mu.Lock()
	v := h.cfg != nil && h.cfg.WebsocketAuth
	h.mu.Unlock()
	c.JSON(200, gin.H{"ws-auth": v})
}
func (h *Handler) PutWebsocketAuth(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.WebsocketAuth = v })
}

// Request retry
func (h *Handler) GetRequestRetry(c *gin.Context) {
	h.mu.Lock()
	v := 0
	if h.cfg != nil {
		v = h.cfg.RequestRetry
	}
	h.mu.Unlock()
	c.JSON(200, gin.H{"request-retry": v})
}
func (h *Handler) PutRequestRetry(c *gin.Context) {
	h.updateIntField(c, func(v int) { h.cfg.RequestRetry = v })
}

// Max retry interval
func (h *Handler) GetMaxRetryInterval(c *gin.Context) {
	h.mu.Lock()
	v := 0
	if h.cfg != nil {
		v = h.cfg.MaxRetryInterval
	}
	h.mu.Unlock()
	c.JSON(200, gin.H{"max-retry-interval": v})
}
func (h *Handler) PutMaxRetryInterval(c *gin.Context) {
	h.updateIntField(c, func(v int) { h.cfg.MaxRetryInterval = v })
}

// ForceModelPrefix
func (h *Handler) GetForceModelPrefix(c *gin.Context) {
	h.mu.Lock()
	v := h.cfg != nil && h.cfg.ForceModelPrefix
	h.mu.Unlock()
	c.JSON(200, gin.H{"force-model-prefix": v})
}
func (h *Handler) PutForceModelPrefix(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.ForceModelPrefix = v })
}

func normalizeRoutingStrategy(strategy string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(strategy))
	switch normalized {
	case "", "round-robin", "roundrobin", "rr":
		return "round-robin", true
	case "fill-first", "fillfirst", "ff":
		return "fill-first", true
	default:
		return "", false
	}
}

// RoutingStrategy
func (h *Handler) GetRoutingStrategy(c *gin.Context) {
	h.mu.Lock()
	raw := ""
	if h.cfg != nil {
		raw = h.cfg.Routing.Strategy
	}
	h.mu.Unlock()
	strategy, ok := normalizeRoutingStrategy(raw)
	if !ok {
		c.JSON(200, gin.H{"strategy": strings.TrimSpace(raw)})
		return
	}
	c.JSON(200, gin.H{"strategy": strategy})
}
func (h *Handler) PutRoutingStrategy(c *gin.Context) {
	var body struct {
		Value *string `json:"value"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	normalized, ok := normalizeRoutingStrategy(*body.Value)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid strategy"})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "config_unavailable"})
		return
	}
	h.cfg.Routing.Strategy = normalized
	h.persistLocked(c)
}

// Proxy URL
func (h *Handler) GetProxyURL(c *gin.Context) {
	h.mu.Lock()
	v := ""
	if h.cfg != nil {
		v = h.cfg.ProxyURL
	}
	h.mu.Unlock()
	c.JSON(200, gin.H{"proxy-url": v})
}
func (h *Handler) PutProxyURL(c *gin.Context) {
	var body struct {
		Value *string `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "config_unavailable"})
		return
	}
	incoming := strings.TrimSpace(*body.Value)
	// Restore userinfo when the management UI round-trips a redacted proxy-url.
	h.cfg.ProxyURL = restoreProxyURLString(incoming, h.cfg.ProxyURL, []string{h.cfg.ProxyURL})
	h.persistLocked(c)
}
func (h *Handler) DeleteProxyURL(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "config_unavailable"})
		return
	}
	h.cfg.ProxyURL = ""
	h.persistLocked(c)
}
