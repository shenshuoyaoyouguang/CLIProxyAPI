package helps

import (
	"math/rand"
	"time"
)

// StreamRetryConfig controls the retry behavior for streaming requests.
type StreamRetryConfig struct {
	MaxAttempts    int           // Maximum total attempts (including initial), default 2
	BaseDelay      time.Duration // Initial backoff delay, default 1s
	MaxDelay       time.Duration // Maximum backoff delay, default 30s
	BackoffFactor  float64       // Backoff multiplier, default 2.0
	JitterFraction float64       // Jitter fraction (0-1), default 0.2

	// DegradeAfterAttempts is the number of retry attempts to perform with the
	// original request body (same params, backoff only) before starting to
	// degrade reasoning_effort. 0 means degrade on the first retry (legacy
	// behavior); 1 means the first retry uses the original body, later retries
	// degrade; N means the first N retries stay at the original body.
	//
	// This gives transient upstream disconnects (network glitches, LB timeouts,
	// 5xx expressed via connection close) a fair chance to recover before the
	// caller's declared reasoning_effort is silently lowered.
	DegradeAfterAttempts int
}

// DefaultStreamRetryConfig returns the default retry configuration.
func DefaultStreamRetryConfig() StreamRetryConfig {
	return StreamRetryConfig{
		MaxAttempts:          2,
		BaseDelay:            1 * time.Second,
		MaxDelay:             30 * time.Second,
		BackoffFactor:        2.0,
		JitterFraction:       0.2,
		DegradeAfterAttempts: 1,
	}
}

// ShouldDegradeReasoning reports whether the retry indexed by attempt (0-based:
// 0 = first retry) should degrade reasoning_effort or stay at the original body.
//
// Rule: degrade only when attempt >= DegradeAfterAttempts. A negative attempt is
// treated as 0. A negative DegradeAfterAttempts is treated as 0 (legacy: always
// degrade).
func ShouldDegradeReasoning(attempt int, cfg StreamRetryConfig) bool {
	if attempt < 0 {
		attempt = 0
	}
	threshold := cfg.DegradeAfterAttempts
	if threshold < 0 {
		threshold = 0
	}
	return attempt >= threshold
}

// ExponentialBackoffWithJitter calculates the backoff duration for a given attempt.
// attempt is 0-indexed (0 = first retry).
// Formula: delay = baseDelay * factor^attempt, then apply jitter, then cap at maxDelay.
// Jitter: delay * (1 - jitterFraction + 2*jitterFraction*random)
func ExponentialBackoffWithJitter(attempt int, cfg StreamRetryConfig) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	// Calculate base delay with exponential growth
	delay := float64(cfg.BaseDelay)
	for i := 0; i < attempt; i++ {
		delay *= cfg.BackoffFactor
	}
	// Apply jitter
	if cfg.JitterFraction > 0 {
		jitter := 2 * cfg.JitterFraction * rand.Float64()
		multiplier := 1 - cfg.JitterFraction + jitter
		delay *= multiplier
	}
	// Cap at max delay
	if delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}
	if delay < 0 {
		delay = 0
	}
	return time.Duration(delay)
}
