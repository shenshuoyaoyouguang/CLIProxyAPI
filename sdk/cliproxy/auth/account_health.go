package auth

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

const accountHealthMetadataKey = "account_health"

// AccountHealthReason describes why a credential was degraded.
type AccountHealthReason string

const (
	AccountHealthReasonUnauthorized AccountHealthReason = "401_unauthorized"
	AccountHealthReasonForbidden    AccountHealthReason = "403_forbidden"
	AccountHealthReasonRateLimited  AccountHealthReason = "429_rate_limited"
	AccountHealthReasonServerError  AccountHealthReason = "server_error"
	AccountHealthReasonTimeout      AccountHealthReason = "timeout"
	AccountHealthReasonManual       AccountHealthReason = "manual"
)

// AccountHealthState stores persisted auth degradation details.
type AccountHealthState struct {
	Degraded            bool                `json:"degraded"`
	DegradedReason      AccountHealthReason `json:"degraded_reason,omitempty"`
	DegradedStatus      int                 `json:"degraded_status,omitempty"`
	DegradedMessage     string              `json:"degraded_message,omitempty"`
	ConsecutiveFailures int                 `json:"consecutive_failures,omitempty"`
	FailureStatuses     []int               `json:"failure_statuses,omitempty"`
	DegradedAt          int64               `json:"degraded_at,omitempty"`
	CooldownUntil       *int64              `json:"cooldown_until"`
	ManualDegraded      bool                `json:"manual_degraded,omitempty"`
	Stale               bool                `json:"stale,omitempty"`
}

// Clone duplicates the health state so callers can mutate safely.
func (s *AccountHealthState) Clone() *AccountHealthState {
	if s == nil {
		return nil
	}
	clone := *s
	if len(s.FailureStatuses) > 0 {
		clone.FailureStatuses = append([]int(nil), s.FailureStatuses...)
	}
	if s.CooldownUntil != nil {
		next := *s.CooldownUntil
		clone.CooldownUntil = &next
	}
	return &clone
}

// IsStale reports whether the cooldown has expired and the auth is waiting for recheck.
func (s *AccountHealthState) IsStale(now time.Time) bool {
	if s == nil || !s.Degraded || s.CooldownUntil == nil {
		return false
	}
	return !time.UnixMilli(*s.CooldownUntil).After(now)
}

// IsBlocked reports whether the degraded state should block scheduling.
func (s *AccountHealthState) IsBlocked(now time.Time) (blocked bool, cooldown bool, nextRetry time.Time) {
	if s == nil || !s.Degraded {
		return false, false, time.Time{}
	}
	if s.CooldownUntil == nil {
		return true, false, time.Time{}
	}
	nextRetry = time.UnixMilli(*s.CooldownUntil)
	if nextRetry.After(now) {
		return true, true, nextRetry
	}
	return false, false, time.Time{}
}

// AccountHealth reads the persisted health state from auth metadata.
func (a *Auth) AccountHealth() (*AccountHealthState, bool) {
	if a == nil || a.Metadata == nil {
		return nil, false
	}

	for _, key := range []string{accountHealthMetadataKey, "accountHealth", "health"} {
		raw, ok := a.Metadata[key]
		if !ok {
			continue
		}
		state, okParse := ParseAccountHealthState(raw)
		if okParse {
			return state, true
		}
	}
	return nil, false
}

// SetAccountHealth persists the provided health state into metadata.
func (a *Auth) SetAccountHealth(state *AccountHealthState) {
	if a == nil {
		return
	}
	if a.Metadata == nil {
		a.Metadata = make(map[string]any)
	}
	if state == nil {
		delete(a.Metadata, accountHealthMetadataKey)
		delete(a.Metadata, "accountHealth")
		delete(a.Metadata, "health")
		return
	}
	a.Metadata[accountHealthMetadataKey] = state.Clone()
	delete(a.Metadata, "accountHealth")
	delete(a.Metadata, "health")
}

// ClearAccountHealth removes any persisted health state from metadata.
func (a *Auth) ClearAccountHealth() {
	if a == nil || a.Metadata == nil {
		return
	}
	delete(a.Metadata, accountHealthMetadataKey)
	delete(a.Metadata, "accountHealth")
	delete(a.Metadata, "health")
}

// ApplyPersistedAccountHealth restores runtime availability fields from metadata.
func (a *Auth) ApplyPersistedAccountHealth(now time.Time) {
	if a == nil {
		return
	}

	state, ok := a.AccountHealth()
	if !ok || state == nil || !state.Degraded {
		return
	}

	state = state.Clone()
	state.Stale = state.IsStale(now)
	a.SetAccountHealth(state)

	if a.Disabled || a.Status == StatusDisabled {
		return
	}

	if blocked, _, nextRetry := state.IsBlocked(now); blocked {
		a.Unavailable = true
		a.NextRetryAfter = nextRetry
		if message := strings.TrimSpace(state.DegradedMessage); message != "" {
			a.StatusMessage = message
		} else if state.DegradedReason != "" {
			a.StatusMessage = string(state.DegradedReason)
		}
		return
	}

	a.Unavailable = false
	a.NextRetryAfter = time.Time{}
}

// ParseAccountHealthState normalises a raw JSON-compatible value into AccountHealthState.
func ParseAccountHealthState(raw any) (*AccountHealthState, bool) {
	switch value := raw.(type) {
	case nil:
		return nil, false
	case *AccountHealthState:
		return value.Clone(), true
	case AccountHealthState:
		return value.Clone(), true
	case map[string]any:
		return parseAccountHealthMap(value)
	default:
		payload, err := json.Marshal(value)
		if err != nil {
			return nil, false
		}
		var decoded map[string]any
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return nil, false
		}
		return parseAccountHealthMap(decoded)
	}
}

func parseAccountHealthMap(raw map[string]any) (*AccountHealthState, bool) {
	if len(raw) == 0 {
		return nil, false
	}

	state := &AccountHealthState{
		Degraded:            lookupBool(raw, "degraded"),
		DegradedReason:      AccountHealthReason(lookupString(raw, "degraded_reason", "degradedReason")),
		DegradedStatus:      int(lookupInt64(raw, "degraded_status", "degradedStatus")),
		DegradedMessage:     lookupString(raw, "degraded_message", "degradedMessage"),
		ConsecutiveFailures: int(lookupInt64(raw, "consecutive_failures", "consecutiveFailures")),
		FailureStatuses:     lookupIntSlice(raw, "failure_statuses", "failureStatuses"),
		DegradedAt:          lookupInt64(raw, "degraded_at", "degradedAt"),
		ManualDegraded:      lookupBool(raw, "manual_degraded", "manualDegraded"),
		Stale:               lookupBool(raw, "stale"),
	}

	if value, found, isNull := lookupNullableInt64(raw, "cooldown_until", "cooldownUntil"); found {
		if isNull {
			state.CooldownUntil = nil
		} else {
			state.CooldownUntil = &value
		}
	}

	return state, true
}

func lookupBool(raw map[string]any, keys ...string) bool {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
			if err == nil {
				return parsed
			}
		case float64:
			return typed != 0
		case int:
			return typed != 0
		case int64:
			return typed != 0
		}
	}
	return false
}

func lookupString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok || value == nil {
			continue
		}
		if text := strings.TrimSpace(anyString(value)); text != "" {
			return text
		}
	}
	return ""
}

func lookupInt64(raw map[string]any, keys ...string) int64 {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok || value == nil {
			continue
		}
		if parsed, okParse := anyInt64(value); okParse {
			return parsed
		}
	}
	return 0
}

func lookupNullableInt64(raw map[string]any, keys ...string) (int64, bool, bool) {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok {
			continue
		}
		if value == nil {
			return 0, true, true
		}
		parsed, okParse := anyInt64(value)
		if okParse {
			return parsed, true, false
		}
	}
	return 0, false, false
}

func lookupIntSlice(raw map[string]any, keys ...string) []int {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok || value == nil {
			continue
		}
		values, okParse := anyIntSlice(value)
		if okParse {
			return values
		}
	}
	return nil
}

func anyString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		payload, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return strings.Trim(string(payload), `"`)
	}
}

func anyInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		return int64(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return parsed, true
		}
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func anyIntSlice(value any) ([]int, bool) {
	raw, ok := value.([]any)
	if !ok {
		return nil, false
	}
	items := make([]int, 0, len(raw))
	for _, entry := range raw {
		parsed, okParse := anyInt64(entry)
		if !okParse {
			continue
		}
		items = append(items, int(parsed))
	}
	return items, true
}
