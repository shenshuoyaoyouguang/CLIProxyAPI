package management

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/redisqueue"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	"github.com/sirupsen/logrus"
)

// UsageEvent represents a usage event for API responses.
// SECURITY: This struct is safe for API exposure as it does NOT contain
// sensitive fields like APIKey. When converting from QueuedUsageDetail,
// ensure all sensitive fields are explicitly excluded.
type UsageEvent struct {
	Timestamp       string `json:"timestamp"`
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	Source          string `json:"source"`
	AuthIndex       string `json:"auth_index"`
	AuthType        string `json:"auth_type"`
	Endpoint        string `json:"endpoint"`
	RequestID       string `json:"request_id"`
	LatencyMs       int64  `json:"latency_ms"`
	Failed          bool   `json:"failed"`
	InputTokens     int64  `json:"input_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	ReasoningTokens int64  `json:"reasoning_tokens"`
	CachedTokens    int64  `json:"cached_tokens"`
	TotalTokens     int64  `json:"total_tokens"`
}

type UsageEventsResponse struct {
	Events []UsageEvent `json:"events"`
}

func (h *Handler) GetUsageEvents(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler not initialized"})
		return
	}

	if !redisqueue.Enabled() || !redisqueue.UsageStatisticsEnabled() {
		c.JSON(http.StatusOK, UsageEventsResponse{Events: []UsageEvent{}})
		return
	}

	limit := usage.UsageEventsLimit
	if limitStr := c.Query("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
			if limit > usage.UsageEventsMaxLimit {
				limit = usage.UsageEventsMaxLimit
			}
		}
	}

	items := redisqueue.PeekOldest(limit)
	if len(items) == 0 {
		c.JSON(http.StatusOK, UsageEventsResponse{Events: []UsageEvent{}})
		return
	}

	events := make([]UsageEvent, 0, len(items))
	for i, item := range items {
		var detail usage.QueuedUsageDetail
		if err := json.Unmarshal(item, &detail); err != nil {
			logrus.WithError(err).WithField("index", i).WithField("raw_len", len(item)).Debug("failed to unmarshal usage event from queue")
			continue
		}
		// SECURITY: Explicitly map only non-sensitive fields.
		// APIKey is intentionally excluded to prevent sensitive data exposure.
		events = append(events, UsageEvent{
			Timestamp:       detail.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
			Provider:        detail.Provider,
			Model:           detail.Model,
			Source:          detail.Source,
			AuthIndex:       detail.AuthIndex,
			AuthType:        detail.AuthType,
			Endpoint:        detail.Endpoint,
			RequestID:       detail.RequestID,
			LatencyMs:       detail.LatencyMs,
			Failed:          detail.Failed,
			InputTokens:     detail.Tokens.InputTokens,
			OutputTokens:    detail.Tokens.OutputTokens,
			ReasoningTokens: detail.Tokens.ReasoningTokens,
			CachedTokens:    detail.Tokens.CachedTokens,
			TotalTokens:     detail.Tokens.TotalTokens,
		})
	}

	c.JSON(http.StatusOK, UsageEventsResponse{Events: events})
}
