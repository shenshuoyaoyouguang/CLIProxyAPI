package ratelimit

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

// DeepSeekGatewayHook applies gateway-level DeepSeek concurrency governance.
type DeepSeekGatewayHook struct {
	limiterMgr     *DeepSeekLimiterManager
	userIDResolver *UserIDResolver
	retryConfig    RetryConfig
	enabled        bool
	metrics        *DeepSeekGatewayMetrics
}

// DeepSeekGatewayMetrics holds Prometheus series for the gateway.
type DeepSeekGatewayMetrics struct {
	ActiveRequests    *prometheus.GaugeVec
	QueueDepth        *prometheus.GaugeVec
	Total429          *prometheus.CounterVec
	RequestLatency    *prometheus.HistogramVec
	ShardUtilization  *prometheus.GaugeVec
	AcquireDuration   *prometheus.HistogramVec
	GlobalUtilization prometheus.Gauge
}

var (
	deepSeekMetricsOnce sync.Once
	deepSeekMetrics     *DeepSeekGatewayMetrics
)

func newDeepSeekGatewayMetrics() *DeepSeekGatewayMetrics {
	// Register Prometheus collectors once per process so config hot-reload
	// that rebuilds the gateway hook does not panic on duplicate registration.
	deepSeekMetricsOnce.Do(func() {
		deepSeekMetrics = &DeepSeekGatewayMetrics{
			ActiveRequests: promauto.NewGaugeVec(prometheus.GaugeOpts{
				Name: "deepseek_gateway_active_requests",
				Help: "Number of currently active requests per user_id",
			}, []string{"user_id"}),

			QueueDepth: promauto.NewGaugeVec(prometheus.GaugeOpts{
				Name: "deepseek_gateway_queue_depth",
				Help: "Number of requests waiting in queue per user_id",
			}, []string{"user_id"}),

			Total429: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "deepseek_gateway_429_total",
				Help: "Total number of 429 rate limit errors per user_id",
			}, []string{"user_id"}),

			RequestLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "deepseek_gateway_request_latency_seconds",
				Help:    "Request latency in seconds per user_id and phase",
				Buckets: prometheus.DefBuckets,
			}, []string{"user_id", "phase"}),

			ShardUtilization: promauto.NewGaugeVec(prometheus.GaugeOpts{
				Name: "deepseek_gateway_shard_utilization",
				Help: "Shard utilization ratio (active/max) per user_id",
			}, []string{"user_id"}),

			AcquireDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "deepseek_gateway_acquire_duration_seconds",
				Help:    "Time spent acquiring semaphore slot per user_id",
				Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
			}, []string{"user_id"}),

			GlobalUtilization: promauto.NewGauge(prometheus.GaugeOpts{
				Name: "deepseek_gateway_global_utilization",
				Help: "Global semaphore utilization ratio",
			}),
		}
	})
	return deepSeekMetrics
}

// NewDeepSeekGatewayHook creates a gateway hook from config.
func NewDeepSeekGatewayHook(
	cfg config.DeepSeekGatewayConfig,
	limiterMgr *DeepSeekLimiterManager,
	userIDResolver *UserIDResolver,
) *DeepSeekGatewayHook {
	if !cfg.Enabled {
		return &DeepSeekGatewayHook{enabled: false}
	}

	baseDelay := parseDurationOrDefault(cfg.RetryBaseDelay, 500*time.Millisecond)
	maxDelay := parseDurationOrDefault(cfg.RetryMaxDelay, 30*time.Second)

	retryCfg := RetryConfig{
		MaxAttempts:       cfg.RetryMaxAttempts,
		BaseDelay:         baseDelay,
		MaxDelay:          maxDelay,
		RespectRetryAfter: cfg.RespectRetryAfter,
		JitterFactor:      0.25,
	}
	if retryCfg.MaxAttempts <= 0 {
		retryCfg.MaxAttempts = 3
	}
	if retryCfg.BaseDelay <= 0 {
		retryCfg.BaseDelay = 500 * time.Millisecond
	}
	if retryCfg.MaxDelay <= 0 {
		retryCfg.MaxDelay = 30 * time.Second
	}

	var metrics *DeepSeekGatewayMetrics
	if cfg.MetricsEnabled {
		metrics = newDeepSeekGatewayMetrics()
		if limiterMgr != nil {
			limiterMgr.SetMetrics(metrics)
		}
	}

	return &DeepSeekGatewayHook{
		limiterMgr:     limiterMgr,
		userIDResolver: userIDResolver,
		retryConfig:    retryCfg,
		enabled:        true,
		metrics:        metrics,
	}
}

func parseDurationOrDefault(s string, def time.Duration) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	return def
}

// IsDeepSeekURL reports whether baseURL targets DeepSeek's API host.
func IsDeepSeekURL(baseURL string) bool {
	host := strings.ToLower(strings.TrimSpace(baseURL))
	return strings.Contains(host, "api.deepseek.com") || strings.Contains(host, "deepseek.com")
}

// ResolveUserID resolves the DeepSeek "user" field for a request.
// header may be the inbound client headers (for strategy=header).
func (h *DeepSeekGatewayHook) ResolveUserID(ctx context.Context, header http.Header, cred *auth.Auth) string {
	if h == nil || h.userIDResolver == nil {
		return ""
	}
	credentialID := ""
	apiKey := ""
	if cred != nil {
		credentialID = cred.ID
		if cred.Attributes != nil {
			apiKey = cred.Attributes["api_key"]
		}
	}
	return h.userIDResolver.Resolve(ctx, nil, header, credentialID, apiKey)
}

// AcquireSlot obtains a concurrency slot. The returned ReleaseFunc is always non-nil.
func (h *DeepSeekGatewayHook) AcquireSlot(ctx context.Context, userID string) (ReleaseFunc, error) {
	if h == nil || !h.enabled || h.limiterMgr == nil {
		return func() {}, nil
	}
	release, err := h.limiterMgr.Acquire(ctx, userID)
	if err != nil {
		return func() {}, err
	}
	// Metrics for active/queue are owned by the shard to avoid double-counting.
	return release, nil
}

// ExecuteWithRetry runs fn with gateway retry policy.
func (h *DeepSeekGatewayHook) ExecuteWithRetry(
	ctx context.Context,
	userID string,
	fn func(context.Context) (*http.Response, error),
) (*http.Response, error) {
	if h == nil || !h.enabled {
		return fn(ctx)
	}

	start := time.Now()
	resp, err := DoWithRetry(ctx, h.retryConfig, fn)
	if h.metrics != nil {
		h.metrics.RequestLatency.WithLabelValues(userID, "total").Observe(time.Since(start).Seconds())
	}

	if err != nil {
		var re *RetryableError
		if errors.As(err, &re) && re != nil && re.HTTPStatus == http.StatusTooManyRequests {
			if h.limiterMgr != nil {
				h.limiterMgr.Record429(userID)
			}
		}
		return resp, err
	}
	return resp, nil
}

// LimiterManager exposes the underlying manager for management endpoints.
func (h *DeepSeekGatewayHook) LimiterManager() *DeepSeekLimiterManager {
	if h == nil {
		return nil
	}
	return h.limiterMgr
}

// Enabled reports whether the hook is active.
func (h *DeepSeekGatewayHook) Enabled() bool {
	return h != nil && h.enabled
}
