package management

import "testing"

func TestCompleteOAuthSession_DoesNotClearOtherPendingSessionsForProvider(t *testing.T) {
	stateA := "state-a-1234567890abcdef"
	stateB := "state-b-1234567890abcdef"

	RegisterOAuthSession(stateA, "anthropic")
	RegisterOAuthSession(stateB, "anthropic")
	defer CompleteOAuthSession(stateA)
	defer CompleteOAuthSession(stateB)

	CompleteOAuthSession(stateA)

	if IsOAuthSessionPending(stateA, "anthropic") {
		t.Fatalf("expected first session to be completed")
	}
	if !IsOAuthSessionPending(stateB, "anthropic") {
		t.Fatalf("expected second session to remain pending")
	}
}
