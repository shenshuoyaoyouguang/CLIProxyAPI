package auth

import (
	"testing"
	"time"
)

func TestAccountHealth_IsBlockedPermanent(t *testing.T) {
	t.Parallel()

	now := time.Now()
	state := &AccountHealthState{
		Degraded:        true,
		DegradedReason:  AccountHealthReasonUnauthorized,
		CooldownUntil:   nil,
		DegradedMessage: "401 unauthorized",
	}

	blocked, cooldown, next := state.IsBlocked(now)
	if !blocked {
		t.Fatalf("blocked = false, want true")
	}
	if cooldown {
		t.Fatalf("cooldown = true, want false")
	}
	if !next.IsZero() {
		t.Fatalf("next = %v, want zero", next)
	}
}
