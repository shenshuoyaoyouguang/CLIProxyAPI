package usage

import "time"

const (
	DefaultMaxEvents   = 1000
	ExportMaxEvents    = 10000
	UsageEventsLimit   = 100
	UsageEventsMaxLimit = 1000
)

type TokenStats struct {
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens"`
	CachedTokens    int64 `json:"cached_tokens"`
	TotalTokens     int64 `json:"total_tokens"`
}

type UsageDetail struct {
	Timestamp string     `json:"timestamp"`
	Source    string     `json:"source"`
	AuthIndex string     `json:"auth_index"`
	Tokens    TokenStats `json:"tokens"`
	Failed    bool       `json:"failed"`
}

type ModelEntry struct {
	TotalRequests int64         `json:"total_requests"`
	SuccessCount  int64         `json:"success_count"`
	FailureCount  int64         `json:"failure_count"`
	TotalTokens   int64         `json:"total_tokens"`
	Details       []UsageDetail `json:"details"`
}

type APIEntry struct {
	TotalRequests int64                 `json:"total_requests"`
	SuccessCount  int64                 `json:"success_count"`
	FailureCount  int64                 `json:"failure_count"`
	TotalTokens   int64                 `json:"total_tokens"`
	Models        map[string]ModelEntry `json:"models"`
}

type UsageSnapshot struct {
	APIs map[string]APIEntry `json:"apis"`
}

type ExportPayload struct {
	Version    int           `json:"version"`
	ExportedAt string        `json:"exported_at"`
	Usage      UsageSnapshot `json:"usage"`
}

type ImportResponse struct {
	TotalRequests  int64 `json:"total_requests"`
	FailedRequests int64 `json:"failed_requests"`
}

// QueuedUsageDetail represents a usage event stored in the queue.
// SECURITY WARNING: This struct contains sensitive fields (APIKey) that must
// NEVER be directly exposed in API responses. Always use ToUsageDetail() or
// ToUsageEvent() methods to convert to safe API response types.
type QueuedUsageDetail struct {
	Timestamp time.Time  `json:"timestamp"`
	LatencyMs int64      `json:"latency_ms"`
	Source    string     `json:"source"`
	AuthIndex string     `json:"auth_index"`
	Tokens    TokenStats `json:"tokens"`
	Failed    bool       `json:"failed"`
	Provider  string     `json:"provider"`
	Model     string     `json:"model"`
	Endpoint  string     `json:"endpoint"`
	AuthType  string     `json:"auth_type"`
	APIKey    string     `json:"api_key"` // SENSITIVE: Never expose in API responses
	RequestID string     `json:"request_id"`
}

// ToUsageDetail converts QueuedUsageDetail to UsageDetail for API responses.
// SECURITY: This method explicitly excludes sensitive fields (APIKey).
func (q *QueuedUsageDetail) ToUsageDetail() UsageDetail {
	return UsageDetail{
		Timestamp: q.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
		Source:    q.Source,
		AuthIndex: q.AuthIndex,
		Tokens:    q.Tokens,
		Failed:    q.Failed,
	}
}
