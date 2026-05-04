package management

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/redisqueue"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func (h *Handler) GetUsage(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler not initialized"})
		return
	}

	if !redisqueue.Enabled() || !redisqueue.UsageStatisticsEnabled() {
		c.JSON(http.StatusOK, usage.UsageSnapshot{APIs: map[string]usage.APIEntry{}})
		return
	}

	aggregator := usage.NewAggregator(usage.DefaultMaxEvents)
	snapshot := aggregator.GetSnapshot()

	c.JSON(http.StatusOK, snapshot)
}
