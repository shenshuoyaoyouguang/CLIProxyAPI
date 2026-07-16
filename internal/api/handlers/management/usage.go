package management

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/redisqueue"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const maxUsageQueueCount = 1000

// usageSensitiveFields lists the top-level JSON fields whose values may carry
// upstream credentials and must be masked before being returned to clients.
var usageSensitiveFields = []string{"source", "api_key", "apiKey"}

type usageQueueRecord []byte

func (r usageQueueRecord) MarshalJSON() ([]byte, error) {
	if json.Valid(r) {
		return append([]byte(nil), r...), nil
	}
	return json.Marshal(string(r))
}

// GetUsageQueue pops queued usage records from the usage queue.
func (h *Handler) GetUsageQueue(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler unavailable"})
		return
	}

	count, errCount := parseUsageQueueCount(c.Query("count"))
	if errCount != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errCount.Error()})
		return
	}

	items := redisqueue.PopOldest(count)
	records := make([]usageQueueRecord, 0, len(items))
	for _, item := range items {
		redacted := append([]byte(nil), item...)
		for _, field := range usageSensitiveFields {
			if v := gjson.GetBytes(redacted, field); v.Exists() && v.Type == gjson.String {
				if masked := maskForLog(v.String()); masked != v.String() {
					redacted, _ = sjson.SetBytes(redacted, field, masked)
				}
			}
		}
		records = append(records, usageQueueRecord(redacted))
	}

	c.JSON(http.StatusOK, records)
}

func parseUsageQueueCount(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 1, nil
	}
	count, errCount := strconv.Atoi(value)
	if errCount != nil || count <= 0 {
		return 0, errors.New("count must be a positive integer")
	}
	if count > maxUsageQueueCount {
		count = maxUsageQueueCount
	}
	return count, nil
}

// maskForLog returns a masked representation of a sensitive string for logging
// or API responses. Strings up to 8 characters are fully hidden; longer ones
// keep only the first and last 4 characters.
func maskForLog(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "..." + s[len(s)-4:]
}
