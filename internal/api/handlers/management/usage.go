package management

import (
	"encoding/json"
	"net/http"
<<<<<<< HEAD
=======
	"strings"
>>>>>>> 27c1428b (feat: add core proxy server implementation)
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
<<<<<<< HEAD
=======
	log "github.com/sirupsen/logrus"
>>>>>>> 27c1428b (feat: add core proxy server implementation)
)

type usageExportPayload struct {
	Version    int                      `json:"version"`
	ExportedAt time.Time                `json:"exported_at"`
	Usage      usage.StatisticsSnapshot `json:"usage"`
}

type usageImportPayload struct {
<<<<<<< HEAD
	Version int                      `json:"version"`
	Usage   usage.StatisticsSnapshot `json:"usage"`
=======
	Version   int                      `json:"version"`
	Usage     usage.StatisticsSnapshot `json:"usage"`
	Origin    string                   `json:"origin"`
	SessionID string                   `json:"session_id"`
>>>>>>> 27c1428b (feat: add core proxy server implementation)
}

// GetUsageStatistics returns the in-memory request statistics snapshot.
func (h *Handler) GetUsageStatistics(c *gin.Context) {
	var snapshot usage.StatisticsSnapshot
	if h != nil && h.usageStats != nil {
		snapshot = h.usageStats.Snapshot()
	}
	c.JSON(http.StatusOK, gin.H{
		"usage":           snapshot,
		"failed_requests": snapshot.FailureCount,
	})
}

// ExportUsageStatistics returns a complete usage snapshot for backup/migration.
func (h *Handler) ExportUsageStatistics(c *gin.Context) {
	var snapshot usage.StatisticsSnapshot
	if h != nil && h.usageStats != nil {
		snapshot = h.usageStats.Snapshot()
	}
	c.JSON(http.StatusOK, usageExportPayload{
		Version:    1,
		ExportedAt: time.Now().UTC(),
		Usage:      snapshot,
	})
}

// ImportUsageStatistics merges a previously exported usage snapshot into memory.
func (h *Handler) ImportUsageStatistics(c *gin.Context) {
	if h == nil || h.usageStats == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "usage statistics unavailable"})
		return
	}

	data, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	var payload usageImportPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if payload.Version != 0 && payload.Version != 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported version"})
		return
	}

	result := h.usageStats.MergeSnapshot(payload.Usage)
<<<<<<< HEAD
	snapshot := h.usageStats.Snapshot()
=======
	if err := h.usageStats.PersistNow(); err != nil {
		log.WithError(err).Warn("usage: failed to persist imported snapshot")
	}
	snapshot := h.usageStats.Snapshot()
	log.WithFields(log.Fields{
		"origin":     strings.TrimSpace(payload.Origin),
		"session_id": strings.TrimSpace(payload.SessionID),
		"added":      result.Added,
		"skipped":    result.Skipped,
	}).Info("usage snapshot imported")
>>>>>>> 27c1428b (feat: add core proxy server implementation)
	c.JSON(http.StatusOK, gin.H{
		"added":           result.Added,
		"skipped":         result.Skipped,
		"total_requests":  snapshot.TotalRequests,
		"failed_requests": snapshot.FailureCount,
	})
}
