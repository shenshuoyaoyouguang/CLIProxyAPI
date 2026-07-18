package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// DeepSeekLimiterConfig holds concurrency limits for the DeepSeek gateway limiter.
type DeepSeekLimiterConfig struct {
	GlobalMaxConcurrency    int
	PerUserIDMaxConcurrency int
}

// DeepSeekShard is a per-user_id concurrency shard.
// Capacity is enforced with a condition variable so SetMaxConcurrency never
// swaps channels (which would strand waiters on the old channel).
type DeepSeekShard struct {
	userID         string
	maxConcurrency int
	active         int
	waiting        int
	total429       int64
	mu             sync.Mutex
	cond           *sync.Cond
	metrics        *DeepSeekGatewayMetrics
}

// NewDeepSeekShard creates a shard.
func NewDeepSeekShard(userID string, maxConcurrency int, metrics *DeepSeekGatewayMetrics) *DeepSeekShard {
	if maxConcurrency <= 0 {
		maxConcurrency = 40
	}
	s := &DeepSeekShard{
		userID:         userID,
		maxConcurrency: maxConcurrency,
		metrics:        metrics,
	}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// Acquire obtains a concurrency slot, waiting until one is free or ctx is done.
func (s *DeepSeekShard) Acquire(ctx context.Context) error {
	if s == nil {
		return nil
	}
	start := time.Now()
	s.mu.Lock()
	s.waiting++
	if s.metrics != nil {
		s.metrics.QueueDepth.WithLabelValues(s.userID).Inc()
	}
	defer func() {
		s.waiting--
		if s.metrics != nil {
			s.metrics.QueueDepth.WithLabelValues(s.userID).Dec()
			s.metrics.AcquireDuration.WithLabelValues(s.userID).Observe(time.Since(start).Seconds())
		}
		s.mu.Unlock()
	}()

	for s.active >= s.maxConcurrency {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Wake when ctx cancels so Wait does not block forever.
		stop := context.AfterFunc(ctx, func() {
			s.mu.Lock()
			s.cond.Broadcast()
			s.mu.Unlock()
		})
		s.cond.Wait()
		stop()
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	s.active++
	if s.metrics != nil {
		s.metrics.ActiveRequests.WithLabelValues(s.userID).Inc()
		s.updateUtilizationLocked()
	}
	return nil
}

// Release frees a concurrency slot.
func (s *DeepSeekShard) Release() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active <= 0 {
		return
	}
	s.active--
	if s.metrics != nil {
		s.metrics.ActiveRequests.WithLabelValues(s.userID).Dec()
		s.updateUtilizationLocked()
	}
	s.cond.Signal()
}

// Record429 records a 429 rate-limit error for this shard.
func (s *DeepSeekShard) Record429() {
	if s == nil {
		return
	}
	atomic.AddInt64(&s.total429, 1)
	if s.metrics != nil {
		s.metrics.Total429.WithLabelValues(s.userID).Inc()
	}
}

// Stats returns a snapshot of shard statistics.
func (s *DeepSeekShard) Stats() ShardStats {
	if s == nil {
		return ShardStats{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	util := 0.0
	if s.maxConcurrency > 0 {
		util = float64(s.active) / float64(s.maxConcurrency)
	}
	return ShardStats{
		UserID:         s.userID,
		MaxConcurrency: s.maxConcurrency,
		Active:         s.active,
		Waiting:        s.waiting,
		Total429:       atomic.LoadInt64(&s.total429),
		Utilization:    util,
	}
}

// SetMaxConcurrency updates the concurrency cap and wakes waiters.
// Waiters re-check active < max and proceed when capacity allows.
func (s *DeepSeekShard) SetMaxConcurrency(newMax int) {
	if s == nil || newMax <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if newMax == s.maxConcurrency {
		return
	}
	s.maxConcurrency = newMax
	if s.metrics != nil {
		s.updateUtilizationLocked()
	}
	s.cond.Broadcast()
}

func (s *DeepSeekShard) updateUtilizationLocked() {
	if s.metrics != nil && s.maxConcurrency > 0 {
		s.metrics.ShardUtilization.WithLabelValues(s.userID).Set(float64(s.active) / float64(s.maxConcurrency))
	}
}

// ShardStats is a snapshot of per-shard counters.
type ShardStats struct {
	UserID         string  `json:"user_id"`
	MaxConcurrency int     `json:"max_concurrency"`
	Active         int     `json:"active"`
	Waiting        int     `json:"waiting"`
	Total429       int64   `json:"total_429"`
	Utilization    float64 `json:"utilization"`
}

// DeepSeekLimiterManager manages global + per-user concurrency.
type DeepSeekLimiterManager struct {
	config       DeepSeekLimiterConfig
	shards       sync.Map // userID -> *DeepSeekShard
	metrics      *DeepSeekGatewayMetrics
	globalMax    int
	globalActive int
	globalMu     sync.Mutex
	globalCond   *sync.Cond
	metricsMu    sync.RWMutex
}

// NewDeepSeekLimiterManager creates a manager.
func NewDeepSeekLimiterManager(config DeepSeekLimiterConfig) *DeepSeekLimiterManager {
	m := &DeepSeekLimiterManager{
		config:    config,
		globalMax: config.GlobalMaxConcurrency,
	}
	m.globalCond = sync.NewCond(&m.globalMu)
	return m
}

// SetMetrics injects a shared metrics collector (created once by the gateway hook).
func (m *DeepSeekLimiterManager) SetMetrics(metrics *DeepSeekGatewayMetrics) {
	if m == nil {
		return
	}
	m.metricsMu.Lock()
	m.metrics = metrics
	m.metricsMu.Unlock()
}

func (m *DeepSeekLimiterManager) metricsOrNil() *DeepSeekGatewayMetrics {
	if m == nil {
		return nil
	}
	m.metricsMu.RLock()
	defer m.metricsMu.RUnlock()
	return m.metrics
}

// GetShard returns (or creates) the shard for userID.
func (m *DeepSeekLimiterManager) GetShard(userID string) *DeepSeekShard {
	if userID == "" {
		userID = "default"
	}
	if val, ok := m.shards.Load(userID); ok {
		return val.(*DeepSeekShard)
	}
	maxConcurrency := m.config.PerUserIDMaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 40
	}
	shard := NewDeepSeekShard(userID, maxConcurrency, m.metricsOrNil())
	actual, loaded := m.shards.LoadOrStore(userID, shard)
	if loaded {
		return actual.(*DeepSeekShard)
	}
	return shard
}

// Acquire obtains global + per-user slots. ReleaseFunc must be called once.
func (m *DeepSeekLimiterManager) Acquire(ctx context.Context, userID string) (ReleaseFunc, error) {
	if m == nil {
		return func() {}, nil
	}
	shard := m.GetShard(userID)

	if err := m.acquireGlobal(ctx); err != nil {
		return nil, err
	}
	if err := shard.Acquire(ctx); err != nil {
		m.releaseGlobal()
		return nil, err
	}
	return func() {
		shard.Release()
		m.releaseGlobal()
	}, nil
}

func (m *DeepSeekLimiterManager) acquireGlobal(ctx context.Context) error {
	m.globalMu.Lock()
	defer m.globalMu.Unlock()
	if m.globalMax <= 0 {
		return nil
	}
	for m.globalActive >= m.globalMax {
		if err := ctx.Err(); err != nil {
			return err
		}
		stop := context.AfterFunc(ctx, func() {
			m.globalMu.Lock()
			m.globalCond.Broadcast()
			m.globalMu.Unlock()
		})
		m.globalCond.Wait()
		stop()
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	m.globalActive++
	return nil
}

func (m *DeepSeekLimiterManager) releaseGlobal() {
	m.globalMu.Lock()
	defer m.globalMu.Unlock()
	if m.globalMax <= 0 {
		return
	}
	if m.globalActive <= 0 {
		return
	}
	m.globalActive--
	m.globalCond.Signal()
}

// ReleaseFunc releases previously acquired slots.
type ReleaseFunc func()

// Record429 records a 429 against the user shard.
func (m *DeepSeekLimiterManager) Record429(userID string) {
	if m == nil {
		return
	}
	if shard, ok := m.shards.Load(userID); ok {
		shard.(*DeepSeekShard).Record429()
	}
}

// GlobalSemaphoreCap returns the configured global concurrency cap (0 if disabled).
func (m *DeepSeekLimiterManager) GlobalSemaphoreCap() int {
	if m == nil {
		return 0
	}
	m.globalMu.Lock()
	defer m.globalMu.Unlock()
	if m.globalMax <= 0 {
		return 0
	}
	return m.globalMax
}

// AllStats returns stats for every shard.
func (m *DeepSeekLimiterManager) AllStats() []ShardStats {
	if m == nil {
		return nil
	}
	var stats []ShardStats
	m.shards.Range(func(_, value interface{}) bool {
		stats = append(stats, value.(*DeepSeekShard).Stats())
		return true
	})
	return stats
}

// GetShardStats returns stats for one user_id shard.
func (m *DeepSeekLimiterManager) GetShardStats(userID string) (ShardStats, bool) {
	if m == nil {
		return ShardStats{}, false
	}
	if val, ok := m.shards.Load(userID); ok {
		return val.(*DeepSeekShard).Stats(), true
	}
	return ShardStats{}, false
}

// SetShardConcurrency updates a shard cap and wakes its waiters.
func (m *DeepSeekLimiterManager) SetShardConcurrency(userID string, newMax int) bool {
	if m == nil {
		return false
	}
	if val, ok := m.shards.Load(userID); ok {
		val.(*DeepSeekShard).SetMaxConcurrency(newMax)
		return true
	}
	return false
}

// SetGlobalConcurrency updates the global cap and wakes waiters.
func (m *DeepSeekLimiterManager) SetGlobalConcurrency(newMax int) {
	if m == nil || newMax <= 0 {
		return
	}
	m.globalMu.Lock()
	defer m.globalMu.Unlock()
	m.globalMax = newMax
	m.globalCond.Broadcast()
}
