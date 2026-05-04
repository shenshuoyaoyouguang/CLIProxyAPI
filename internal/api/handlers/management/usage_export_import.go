package management

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/redisqueue"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

// ExportUsage returns a snapshot with metadata for download.
// It retrieves the current usage statistics from the redisqueue and wraps them
// in an ExportPayload with version and timestamp information.
func (h *Handler) ExportUsage(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler not initialized"})
		return
	}

	if !redisqueue.Enabled() || !redisqueue.UsageStatisticsEnabled() {
		c.JSON(http.StatusOK, usage.ExportPayload{
			Version:    1,
			ExportedAt: time.Now().Format(time.RFC3339),
			Usage:      usage.UsageSnapshot{APIs: make(map[string]usage.APIEntry)},
		})
		return
	}

	aggregator := usage.NewAggregator(usage.ExportMaxEvents)
	snapshot := aggregator.GetSnapshot()

	payload := usage.ExportPayload{
		Version:    1,
		ExportedAt: time.Now().Format(time.RFC3339),
		Usage:      *snapshot,
	}

	c.JSON(http.StatusOK, payload)
}

// ImportUsage accepts a payload and validates it.
// For now, it just parses the data and returns counts.
// Actual merging into redisqueue is complex and can be a future enhancement.
func (h *Handler) ImportUsage(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler not initialized"})
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	var exportPayload usage.ExportPayload
	if err := json.Unmarshal(body, &exportPayload); err == nil && exportPayload.Version != 0 {
		response := countUsageSnapshot(&exportPayload.Usage)
		c.JSON(http.StatusOK, response)
		return
	}

	var snapshot usage.UsageSnapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: expected ExportPayload or UsageSnapshot"})
		return
	}

	response := countUsageSnapshot(&snapshot)
	c.JSON(http.StatusOK, response)
}

func countUsageSnapshot(snapshot *usage.UsageSnapshot) usage.ImportResponse {
	if snapshot == nil || snapshot.APIs == nil {
		return usage.ImportResponse{}
	}

	var totalRequests, failedRequests int64

	for _, apiEntry := range snapshot.APIs {
		totalRequests += apiEntry.TotalRequests
		failedRequests += apiEntry.FailureCount
	}

	return usage.ImportResponse{
		TotalRequests:  totalRequests,
		FailedRequests: failedRequests,
	}
}
