package usage

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/redisqueue"
)

func TestNewAggregator_DefaultMaxEvents(t *testing.T) {
	a := NewAggregator(0)
	if a.maxEvents != DefaultMaxEvents {
		t.Fatalf("NewAggregator(0).maxEvents = %d, want %d", a.maxEvents, DefaultMaxEvents)
	}

	a = NewAggregator(-1)
	if a.maxEvents != DefaultMaxEvents {
		t.Fatalf("NewAggregator(-1).maxEvents = %d, want %d", a.maxEvents, DefaultMaxEvents)
	}

	a = NewAggregator(500)
	if a.maxEvents != 500 {
		t.Fatalf("NewAggregator(500).maxEvents = %d, want 500", a.maxEvents)
	}
}

func TestGetSnapshot_ReturnsEmptyWhenDisabled(t *testing.T) {
	prevQueueEnabled := redisqueue.Enabled()
	prevUsageEnabled := redisqueue.UsageStatisticsEnabled()
	redisqueue.SetEnabled(false)
	redisqueue.SetUsageStatisticsEnabled(false)
	t.Cleanup(func() {
		redisqueue.SetEnabled(prevQueueEnabled)
		redisqueue.SetUsageStatisticsEnabled(prevUsageEnabled)
	})

	a := NewAggregator(100)
	snapshot := a.GetSnapshot()

	if snapshot == nil {
		t.Fatal("GetSnapshot() returned nil")
	}
	if len(snapshot.APIs) != 0 {
		t.Fatalf("snapshot.APIs len = %d, want 0", len(snapshot.APIs))
	}
}

func TestGetSnapshot_AggregatesEvents(t *testing.T) {
	prevQueueEnabled := redisqueue.Enabled()
	prevUsageEnabled := redisqueue.UsageStatisticsEnabled()
	redisqueue.SetEnabled(true)
	redisqueue.SetUsageStatisticsEnabled(true)
	t.Cleanup(func() {
		redisqueue.SetEnabled(prevQueueEnabled)
		redisqueue.SetUsageStatisticsEnabled(prevUsageEnabled)
	})

	now := time.Now().UTC()
	event1 := QueuedUsageDetail{
		Timestamp: now,
		Provider:  "openai",
		Model:     "gpt-4",
		Endpoint:  "POST /v1/chat/completions",
		Source:    "user1",
		AuthIndex: "idx1",
		Tokens: TokenStats{
			InputTokens:  100,
			OutputTokens: 200,
			TotalTokens:  300,
		},
		Failed: false,
	}
	event2 := QueuedUsageDetail{
		Timestamp: now,
		Provider:  "openai",
		Model:     "gpt-4",
		Endpoint:  "POST /v1/chat/completions",
		Source:    "user2",
		AuthIndex: "idx2",
		Tokens: TokenStats{
			InputTokens:  50,
			OutputTokens: 150,
			TotalTokens:  200,
		},
		Failed: true,
	}
	event3 := QueuedUsageDetail{
		Timestamp: now,
		Provider:  "anthropic",
		Model:     "claude-3",
		Endpoint:  "POST /v1/messages",
		Source:    "user3",
		AuthIndex: "idx3",
		Tokens: TokenStats{
			InputTokens:  80,
			OutputTokens: 120,
			TotalTokens:  200,
		},
		Failed: false,
	}

	data1, _ := json.Marshal(event1)
	data2, _ := json.Marshal(event2)
	data3, _ := json.Marshal(event3)
	redisqueue.Enqueue(data1)
	redisqueue.Enqueue(data2)
	redisqueue.Enqueue(data3)

	a := NewAggregator(100)
	snapshot := a.GetSnapshot()

	if len(snapshot.APIs) != 2 {
		t.Fatalf("snapshot.APIs len = %d, want 2", len(snapshot.APIs))
	}

	chatEndpoint, ok := snapshot.APIs["POST /v1/chat/completions"]
	if !ok {
		t.Fatal("missing endpoint POST /v1/chat/completions")
	}
	if chatEndpoint.TotalRequests != 2 {
		t.Fatalf("chatEndpoint.TotalRequests = %d, want 2", chatEndpoint.TotalRequests)
	}
	if chatEndpoint.SuccessCount != 1 {
		t.Fatalf("chatEndpoint.SuccessCount = %d, want 1", chatEndpoint.SuccessCount)
	}
	if chatEndpoint.FailureCount != 1 {
		t.Fatalf("chatEndpoint.FailureCount = %d, want 1", chatEndpoint.FailureCount)
	}
	if chatEndpoint.TotalTokens != 500 {
		t.Fatalf("chatEndpoint.TotalTokens = %d, want 500", chatEndpoint.TotalTokens)
	}

	gpt4Model, ok := chatEndpoint.Models["gpt-4"]
	if !ok {
		t.Fatal("missing model gpt-4")
	}
	if len(gpt4Model.Details) != 2 {
		t.Fatalf("gpt4Model.Details len = %d, want 2", len(gpt4Model.Details))
	}

	messagesEndpoint, ok := snapshot.APIs["POST /v1/messages"]
	if !ok {
		t.Fatal("missing endpoint POST /v1/messages")
	}
	if messagesEndpoint.TotalRequests != 1 {
		t.Fatalf("messagesEndpoint.TotalRequests = %d, want 1", messagesEndpoint.TotalRequests)
	}
}

func TestGetSnapshot_HandlesUnknownEndpointAndModel(t *testing.T) {
	prevQueueEnabled := redisqueue.Enabled()
	prevUsageEnabled := redisqueue.UsageStatisticsEnabled()
	redisqueue.SetEnabled(true)
	redisqueue.SetUsageStatisticsEnabled(true)
	t.Cleanup(func() {
		redisqueue.SetEnabled(prevQueueEnabled)
		redisqueue.SetUsageStatisticsEnabled(prevUsageEnabled)
	})

	event := QueuedUsageDetail{
		Timestamp: time.Now().UTC(),
		Provider:  "test",
		Tokens:    TokenStats{TotalTokens: 100},
	}
	data, _ := json.Marshal(event)
	redisqueue.Enqueue(data)

	a := NewAggregator(100)
	snapshot := a.GetSnapshot()

	unknownEndpoint, ok := snapshot.APIs["unknown"]
	if !ok {
		t.Fatal("missing endpoint 'unknown'")
	}
	if _, ok := unknownEndpoint.Models["unknown"]; !ok {
		t.Fatal("missing model 'unknown'")
	}
}

func TestGetSnapshot_SkipsInvalidJSON(t *testing.T) {
	prevQueueEnabled := redisqueue.Enabled()
	prevUsageEnabled := redisqueue.UsageStatisticsEnabled()
	redisqueue.SetEnabled(true)
	redisqueue.SetUsageStatisticsEnabled(true)
	t.Cleanup(func() {
		redisqueue.SetEnabled(prevQueueEnabled)
		redisqueue.SetUsageStatisticsEnabled(prevUsageEnabled)
	})

	redisqueue.Enqueue([]byte(`{invalid json`))

	event := QueuedUsageDetail{
		Timestamp: time.Now().UTC(),
		Endpoint:  "test",
		Model:     "test-model",
		Tokens:    TokenStats{TotalTokens: 100},
	}
	data, _ := json.Marshal(event)
	redisqueue.Enqueue(data)

	a := NewAggregator(100)
	snapshot := a.GetSnapshot()

	if len(snapshot.APIs) != 1 {
		t.Fatalf("snapshot.APIs len = %d, want 1 (invalid JSON should be skipped)", len(snapshot.APIs))
	}
}
