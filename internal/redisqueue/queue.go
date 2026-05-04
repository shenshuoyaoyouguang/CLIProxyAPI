package redisqueue

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage/types"
)

const (
	defaultRetentionSeconds int64 = 60
	maxRetentionSeconds     int64 = 3600
)

type queueItem struct {
	enqueuedAt time.Time
	payload    []byte
}

type queue struct {
	mu    sync.Mutex
	items []queueItem
	head  int
}

var (
	enabled          atomic.Bool
	retentionSeconds atomic.Int64
	global           queue
)

func init() {
	retentionSeconds.Store(defaultRetentionSeconds)
}

func SetEnabled(value bool) {
	enabled.Store(value)
	if !value {
		global.clear()
	}
}

func Enabled() bool {
	return enabled.Load()
}

func SetRetentionSeconds(value int) {
	normalized := int64(value)
	if normalized <= 0 {
		normalized = defaultRetentionSeconds
	} else if normalized > maxRetentionSeconds {
		normalized = maxRetentionSeconds
	}
	retentionSeconds.Store(normalized)
}

func Enqueue(payload []byte) {
	if !Enabled() {
		return
	}
	if len(payload) == 0 {
		return
	}
	global.mu.Lock()
	defer global.mu.Unlock()
	global.enqueue(payload)
}

// Clear empties the entire usage queue. Use with caution — intended for recovery from snapshots.
func Clear() {
	global.clear()
}

// RestoreFromSnapshot repopulates the queue with usage events from a UsageSnapshot.
// It enqueues all UsageDetail events from all API endpoints and models.
// If the queue already has events, no action is taken (safe to call multiple times).
func RestoreFromSnapshot(snapshot *types.UsageSnapshot) {
	if snapshot == nil {
		return
	}
	if !Enabled() || !UsageStatisticsEnabled() {
		return
	}
	global.mu.Lock()
	defer global.mu.Unlock()
	if len(global.items)-global.head > 0 {
		return
	}
	for endpoint, apiEntry := range snapshot.APIs {
		for model, modelEntry := range apiEntry.Models {
			for _, detail := range modelEntry.Details {
				timestamp := detail.Timestamp
				if timestamp == "" {
					timestamp = time.Now().UTC().Format(time.RFC3339)
				}
				parsed, _ := time.Parse(time.RFC3339, timestamp)
				if parsed.IsZero() {
					parsed = time.Now().UTC()
				}
				queued := queuedUsageDetail{
					requestDetail: requestDetail{
						Timestamp: parsed,
						Source:    detail.Source,
						AuthIndex: detail.AuthIndex,
						Tokens: tokenStats{
							InputTokens:     detail.Tokens.InputTokens,
							OutputTokens:    detail.Tokens.OutputTokens,
							ReasoningTokens: detail.Tokens.ReasoningTokens,
							CachedTokens:    detail.Tokens.CachedTokens,
							TotalTokens:     detail.Tokens.TotalTokens,
						},
						Failed: detail.Failed,
					},
					Provider: "unknown",
					Model:    model,
					Endpoint: endpoint,
					AuthType: "unknown",
				}
				payload, err := json.Marshal(queued)
				if err != nil {
					continue
				}
				global.enqueue(payload)
			}
		}
	}
}

func PopOldest(count int) [][]byte {
	if !Enabled() || !UsageStatisticsEnabled() {
		return nil
	}
	if count <= 0 {
		return nil
	}
	return global.popOldest(count)
}

func PeekOldest(count int) [][]byte {
	if !Enabled() || !UsageStatisticsEnabled() {
		return nil
	}
	if count <= 0 {
		return nil
	}
	return global.peekOldest(count)
}

func (q *queue) clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = nil
	q.head = 0
}

func (q *queue) enqueue(payload []byte) {
	now := time.Now()
	q.pruneLocked(now)
	q.items = append(q.items, queueItem{
		enqueuedAt: now,
		payload:    append([]byte(nil), payload...),
	})
	q.maybeCompactLocked()
}

func (q *queue) popOldest(count int) [][]byte {
	now := time.Now()

	q.mu.Lock()
	defer q.mu.Unlock()

	q.pruneLocked(now)
	available := len(q.items) - q.head
	if available <= 0 {
		q.items = nil
		q.head = 0
		return nil
	}
	if count > available {
		count = available
	}

	out := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		item := q.items[q.head+i]
		out = append(out, item.payload)
	}
	q.head += count
	q.maybeCompactLocked()
	return out
}

func (q *queue) peekOldest(count int) [][]byte {
	now := time.Now()

	q.mu.Lock()
	defer q.mu.Unlock()

	q.pruneLocked(now)
	available := len(q.items) - q.head
	if available <= 0 {
		return nil
	}
	if count > available {
		count = available
	}

	out := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		item := q.items[q.head+i]
		out = append(out, item.payload)
	}
	return out
}

func (q *queue) pruneLocked(now time.Time) {
	if q.head >= len(q.items) {
		q.items = nil
		q.head = 0
		return
	}

	windowSeconds := retentionSeconds.Load()
	if windowSeconds <= 0 {
		windowSeconds = defaultRetentionSeconds
	}
	cutoff := now.Add(-time.Duration(windowSeconds) * time.Second)
	for q.head < len(q.items) && q.items[q.head].enqueuedAt.Before(cutoff) {
		q.head++
	}
}

func (q *queue) maybeCompactLocked() {
	if q.head == 0 {
		return
	}
	if q.head >= len(q.items) {
		q.items = nil
		q.head = 0
		return
	}
	if q.head < 1024 && q.head*2 < len(q.items) {
		return
	}
	q.items = append([]queueItem(nil), q.items[q.head:]...)
	q.head = 0
}

func (q *queue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items) - q.head
}
