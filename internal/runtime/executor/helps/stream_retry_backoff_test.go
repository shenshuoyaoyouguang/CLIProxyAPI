package helps

import (
	"testing"
	"time"
)

func TestDefaultStreamRetryConfig(t *testing.T) {
	cfg := DefaultStreamRetryConfig()
	if cfg.MaxAttempts != 2 {
		t.Fatalf("MaxAttempts = %d, want 2", cfg.MaxAttempts)
	}
	if cfg.BaseDelay != 1*time.Second {
		t.Fatalf("BaseDelay = %v, want 1s", cfg.BaseDelay)
	}
	if cfg.MaxDelay != 30*time.Second {
		t.Fatalf("MaxDelay = %v, want 30s", cfg.MaxDelay)
	}
	if cfg.BackoffFactor != 2.0 {
		t.Fatalf("BackoffFactor = %f, want 2.0", cfg.BackoffFactor)
	}
	if cfg.JitterFraction != 0.2 {
		t.Fatalf("JitterFraction = %f, want 0.2", cfg.JitterFraction)
	}
	if cfg.DegradeAfterAttempts != 1 {
		t.Fatalf("DegradeAfterAttempts = %d, want 1", cfg.DegradeAfterAttempts)
	}
}

func TestShouldDegradeReasoning(t *testing.T) {
	// Table-driven cases covering the two dimensions that matter:
	//   threshold (DegradeAfterAttempts) x attempt (0-based retry index)
	// plus negative-input defensiveness.
	cases := []struct {
		name      string
		threshold int
		attempt   int
		want      bool
	}{
		// threshold=0 (legacy: degrade from the very first retry)
		{"threshold=0 first retry degrades", 0, 0, true},
		{"threshold=0 second retry degrades", 0, 1, true},
		{"threshold=0 tenth retry degrades", 0, 10, true},

		// threshold=1 (default: first retry same-param, later retries degrade)
		{"threshold=1 first retry stays original", 1, 0, false},
		{"threshold=1 second retry degrades", 1, 1, true},
		{"threshold=1 third retry degrades", 1, 2, true},

		// threshold=N (first N retries stay original)
		{"threshold=2 first retry stays original", 2, 0, false},
		{"threshold=2 second retry stays original", 2, 1, false},
		{"threshold=2 third retry degrades", 2, 2, true},
		{"threshold=3 boundary before", 3, 2, false},
		{"threshold=3 boundary at", 3, 3, true},

		// Very large threshold: effectively disables degrade under any realistic MaxAttempts.
		{"threshold=100 attempt 5 stays original", 100, 5, false},

		// Negative inputs must be normalised, not panic or flip logic.
		{"negative attempt clamps to 0 with threshold=0", 0, -3, true},
		{"negative attempt clamps to 0 with threshold=1", 1, -3, false},
		{"negative threshold treated as 0 (always degrade)", -5, 0, true},
		{"negative threshold and negative attempt: attempt=0, threshold=0", -5, -1, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := StreamRetryConfig{DegradeAfterAttempts: tc.threshold}
			got := ShouldDegradeReasoning(tc.attempt, cfg)
			if got != tc.want {
				t.Fatalf("ShouldDegradeReasoning(attempt=%d, threshold=%d) = %v, want %v",
					tc.attempt, tc.threshold, got, tc.want)
			}
		})
	}
}

// TestShouldDegradeReasoning_MonotonicSwitch documents the intended contract:
// for a fixed threshold, the return value is a monotonic step function of
// attempt — once it flips to true it must stay true. This guards against any
// future refactor that would accidentally re-enable original-body retries
// after a degrade has already occurred (which would break idempotency).
func TestShouldDegradeReasoning_MonotonicSwitch(t *testing.T) {
	for threshold := 0; threshold <= 5; threshold++ {
		cfg := StreamRetryConfig{DegradeAfterAttempts: threshold}
		flipped := false
		for attempt := 0; attempt < 20; attempt++ {
			got := ShouldDegradeReasoning(attempt, cfg)
			if got {
				flipped = true
			} else if flipped {
				t.Fatalf("threshold=%d attempt=%d: degrade flipped back to false after being true", threshold, attempt)
			}
		}
	}
}

func TestExponentialBackoffWithJitter(t *testing.T) {
	cfg := StreamRetryConfig{
		MaxAttempts:    5,
		BaseDelay:      1 * time.Second,
		MaxDelay:       30 * time.Second,
		BackoffFactor:  2.0,
		JitterFraction: 0.2,
	}

	t.Run("delay increases with attempts", func(t *testing.T) {
		prev := time.Duration(0)
		for attempt := 0; attempt < 5; attempt++ {
			delay := ExponentialBackoffWithJitter(attempt, cfg)
			// With jitter, the delay should be within [base*0.8, base*1.2]
			base := float64(cfg.BaseDelay)
			for i := 0; i < attempt; i++ {
				base *= cfg.BackoffFactor
			}
			minDelay := time.Duration(base * (1 - cfg.JitterFraction))
			maxDelay := time.Duration(base * (1 + cfg.JitterFraction))
			if delay < minDelay || delay > maxDelay {
				t.Fatalf("attempt %d: delay %v not in [%v, %v]", attempt, delay, minDelay, maxDelay)
			}
			if delay < prev {
				t.Fatalf("attempt %d: delay %v < previous %v (should be monotonic on average)", attempt, delay, prev)
			}
			prev = delay
		}
	})

	t.Run("delay is capped at MaxDelay", func(t *testing.T) {
		delay := ExponentialBackoffWithJitter(10, cfg)
		if delay > cfg.MaxDelay {
			t.Fatalf("delay %v exceeds MaxDelay %v", delay, cfg.MaxDelay)
		}
	})

	t.Run("negative attempt treated as 0", func(t *testing.T) {
		delay := ExponentialBackoffWithJitter(-1, cfg)
		if delay <= 0 {
			t.Fatalf("delay should be positive, got %v", delay)
		}
	})

	t.Run("zero jitter produces deterministic delays", func(t *testing.T) {
		noJitter := cfg
		noJitter.JitterFraction = 0
		d1 := ExponentialBackoffWithJitter(2, noJitter)
		d2 := ExponentialBackoffWithJitter(2, noJitter)
		if d1 != d2 {
			t.Fatalf("zero jitter should produce deterministic delays: %v != %v", d1, d2)
		}
		expected := 4 * time.Second // 1s * 2^2
		if d1 != expected {
			t.Fatalf("expected %v, got %v", expected, d1)
		}
	})
}
