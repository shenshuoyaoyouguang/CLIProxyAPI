package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestSetMaxConcurrencyWakesWaiters(t *testing.T) {
	t.Parallel()
	mgr := NewDeepSeekLimiterManager(DeepSeekLimiterConfig{
		GlobalMaxConcurrency:    0,
		PerUserIDMaxConcurrency: 1,
	})
	release1, err := mgr.Acquire(context.Background(), "u1")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	var secondOK atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		rel, errAcq := mgr.Acquire(ctx, "u1")
		if errAcq != nil {
			t.Errorf("second acquire: %v", errAcq)
			return
		}
		secondOK.Store(true)
		rel()
	}()

	// Give waiter time to block.
	time.Sleep(50 * time.Millisecond)
	if !mgr.SetShardConcurrency("u1", 2) {
		t.Fatal("shard not found")
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter was not woken by SetMaxConcurrency")
	}
	if !secondOK.Load() {
		t.Fatal("second acquire did not succeed after tune")
	}
	release1()
}

func TestAcquireRespectsPerUserCap(t *testing.T) {
	t.Parallel()
	mgr := NewDeepSeekLimiterManager(DeepSeekLimiterConfig{
		PerUserIDMaxConcurrency: 1,
	})
	rel, err := mgr.Acquire(context.Background(), "cap")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer rel()

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, err = mgr.Acquire(ctx, "cap")
	if err == nil {
		t.Fatal("expected second acquire to block until timeout")
	}
}

func TestRecord429EmptyUserIDHitsDefaultShard(t *testing.T) {
	t.Parallel()
	mgr := NewDeepSeekLimiterManager(DeepSeekLimiterConfig{
		PerUserIDMaxConcurrency: 10,
	})
	shard := mgr.GetShard("")
	if shard.userID != "default" {
		t.Fatalf("GetShard(\"\") returned shard with userID=%q, want %q", shard.userID, "default")
	}
	mgr.Record429("")
	stats := mgr.AllStats()
	if len(stats) != 1 {
		t.Fatalf("AllStats() len = %d, want 1", len(stats))
	}
	if stats[0].UserID != "default" {
		t.Fatalf("stats[0].UserID = %q, want %q", stats[0].UserID, "default")
	}
	if stats[0].Total429 != 1 {
		t.Fatalf("stats[0].Total429 = %d, want 1", stats[0].Total429)
	}
}

func TestSetMetricsPropagatesToExistingShards(t *testing.T) {
	t.Parallel()
	mgr := NewDeepSeekLimiterManager(DeepSeekLimiterConfig{
		PerUserIDMaxConcurrency: 5,
	})
	shard := mgr.GetShard("user1")
	if shard.metrics != nil {
		t.Fatal("expected shard metrics to be nil before SetMetrics")
	}
	reg := prometheus.NewRegistry()
	metrics := newTestGatewayMetrics(reg)
	mgr.SetMetrics(metrics)
	if shard.metrics != metrics {
		t.Fatal("expected existing shard metrics to be updated after SetMetrics")
	}
	shard2 := mgr.GetShard("user2")
	if shard2.metrics != metrics {
		t.Fatal("expected newly created shard to use metrics from SetMetrics")
	}
	_, err := mgr.Acquire(context.Background(), "user1")
	if err != nil {
		t.Fatalf("acquire with propagated metrics: %v", err)
	}
	mfs, _ := reg.Gather()
	activeVal := getGaugeValue(mfs, "test_active_requests")
	if activeVal != 1 {
		t.Fatalf("active_requests for user1 = %f, want 1", activeVal)
	}
}

func TestGlobalUtilizationGaugeUpdatedOnAcquireRelease(t *testing.T) {
	t.Parallel()
	mgr := NewDeepSeekLimiterManager(DeepSeekLimiterConfig{
		GlobalMaxConcurrency:    2,
		PerUserIDMaxConcurrency: 10,
	})
	reg := prometheus.NewRegistry()
	metrics := newTestGatewayMetrics(reg)
	mgr.SetMetrics(metrics)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	utilVal := getGaugeValue(mfs, "test_global_util")
	if utilVal != 0 {
		t.Fatalf("initial global utilization = %f, want 0", utilVal)
	}

	rel1, err := mgr.Acquire(context.Background(), "u1")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	mfs, _ = reg.Gather()
	utilVal = getGaugeValue(mfs, "test_global_util")
	if utilVal != 0.5 {
		t.Fatalf("after 1 acquire global utilization = %f, want 0.5", utilVal)
	}

	rel2, err := mgr.Acquire(context.Background(), "u2")
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	mfs, _ = reg.Gather()
	utilVal = getGaugeValue(mfs, "test_global_util")
	if utilVal != 1.0 {
		t.Fatalf("after 2 acquires global utilization = %f, want 1.0", utilVal)
	}

	rel1()
	mfs, _ = reg.Gather()
	utilVal = getGaugeValue(mfs, "test_global_util")
	if utilVal != 0.5 {
		t.Fatalf("after 1 release global utilization = %f, want 0.5", utilVal)
	}

	rel2()
	mfs, _ = reg.Gather()
	utilVal = getGaugeValue(mfs, "test_global_util")
	if utilVal != 0.0 {
		t.Fatalf("after all releases global utilization = %f, want 0.0", utilVal)
	}
}

func newTestGatewayMetrics(reg *prometheus.Registry) *DeepSeekGatewayMetrics {
	m := &DeepSeekGatewayMetrics{
		ActiveRequests: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "test_active_requests"}, []string{"user_id"}),
		QueueDepth:     prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "test_queue_depth"}, []string{"user_id"}),
		Total429:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_total_429"}, []string{"user_id"}),
		RequestLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "test_request_latency", Buckets: prometheus.DefBuckets}, []string{"user_id", "phase"}),
		ShardUtilization: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "test_shard_utilization"}, []string{"user_id"}),
		AcquireDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "test_acquire_duration", Buckets: []float64{0.001, 0.01, 0.1, 1}}, []string{"user_id"}),
		GlobalUtilization: prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_global_util"}),
	}
	reg.MustRegister(
		m.ActiveRequests, m.QueueDepth, m.Total429, m.RequestLatency,
		m.ShardUtilization, m.AcquireDuration, m.GlobalUtilization,
	)
	return m
}

func getGaugeValue(mfs []*dto.MetricFamily, name string) float64 {
	for _, mf := range mfs {
		if mf.GetName() == name && len(mf.GetMetric()) == 1 {
			return mf.GetMetric()[0].GetGauge().GetValue()
		}
	}
	return -1
}
