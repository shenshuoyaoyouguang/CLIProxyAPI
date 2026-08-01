package thinking

import "testing"

// TestExtractDeepSeekConfig_EnabledWithEffortNone verifies issue #2:
// thinking.type=enabled + reasoning_effort=none must NOT close thinking.
// The function returns an empty config (passthrough) so the upstream sees
// thinking.type=enabled + reasoning_effort=none unchanged.
func TestExtractDeepSeekConfig_EnabledWithEffortNone(t *testing.T) {
	body := []byte(`{"thinking":{"type":"enabled"},"reasoning_effort":"none"}`)
	cfg := extractDeepSeekConfig(body)
	if hasThinkingConfig(cfg) {
		t.Errorf("thinking.type=enabled + effort=none: expected passthrough (empty config), got Mode=%v Level=%q", cfg.Mode, cfg.Level)
	}
}

// TestExtractDeepSeekConfig_EffortNoneWithoutEnabled verifies that
// reasoning_effort=none without thinking.type=enabled still returns ModeNone
// (hard off) so chat-mode requests don't get polluted.
func TestExtractDeepSeekConfig_EffortNoneWithoutEnabled(t *testing.T) {
	body := []byte(`{"reasoning_effort":"none"}`)
	cfg := extractDeepSeekConfig(body)
	if cfg.Mode != ModeNone {
		t.Errorf("effort=none without enabled: expected ModeNone, got Mode=%v", cfg.Mode)
	}
}

// TestExtractDeepSeekConfig_EnabledWithoutEffort returns empty config (passthrough).
func TestExtractDeepSeekConfig_EnabledWithoutEffort(t *testing.T) {
	body := []byte(`{"thinking":{"type":"enabled"}}`)
	cfg := extractDeepSeekConfig(body)
	if hasThinkingConfig(cfg) {
		t.Errorf("thinking.type=enabled without effort: expected passthrough, got Mode=%v", cfg.Mode)
	}
}

// TestExtractDeepSeekConfig_DisabledReturnsModeNone verifies hard off.
func TestExtractDeepSeekConfig_DisabledReturnsModeNone(t *testing.T) {
	body := []byte(`{"thinking":{"type":"disabled"},"reasoning_effort":"high"}`)
	cfg := extractDeepSeekConfig(body)
	if cfg.Mode != ModeNone {
		t.Errorf("thinking.type=disabled: expected ModeNone, got Mode=%v", cfg.Mode)
	}
}

// TestExtractDeepSeekConfig_EffortHigh returns ModeLevel.
func TestExtractDeepSeekConfig_EffortHigh(t *testing.T) {
	body := []byte(`{"reasoning_effort":"high"}`)
	cfg := extractDeepSeekConfig(body)
	if cfg.Mode != ModeLevel || cfg.Level != LevelHigh {
		t.Errorf("effort=high: expected ModeLevel+LevelHigh, got Mode=%v Level=%q", cfg.Mode, cfg.Level)
	}
}

// TestExtractDeepSeekConfig_EnabledWithEffortHigh returns ModeLevel (enabled takes
// precedence but effort is honored as the level).
func TestExtractDeepSeekConfig_EnabledWithEffortHigh(t *testing.T) {
	body := []byte(`{"thinking":{"type":"enabled"},"reasoning_effort":"high"}`)
	cfg := extractDeepSeekConfig(body)
	if cfg.Mode != ModeLevel || cfg.Level != LevelHigh {
		t.Errorf("enabled+effort=high: expected ModeLevel+LevelHigh, got Mode=%v Level=%q", cfg.Mode, cfg.Level)
	}
}

// TestExtractDeepSeekConfig_EnabledWithEffortAuto returns ModeAuto.
func TestExtractDeepSeekConfig_EnabledWithEffortAuto(t *testing.T) {
	body := []byte(`{"thinking":{"type":"enabled"},"reasoning_effort":"auto"}`)
	cfg := extractDeepSeekConfig(body)
	if cfg.Mode != ModeAuto {
		t.Errorf("enabled+effort=auto: expected ModeAuto, got Mode=%v", cfg.Mode)
	}
}

// TestExtractDeepSeekConfig_NullReasoningEffort verifies that a JSON null
// reasoning_effort is treated exactly like a missing field (passthrough),
// symmetric with the reasoning_content null standardization: gjson.Exists()
// reports true for null, so the type must be checked explicitly.
func TestExtractDeepSeekConfig_NullReasoningEffort(t *testing.T) {
	body := []byte(`{"reasoning_effort":null}`)
	cfg := extractDeepSeekConfig(body)
	if hasThinkingConfig(cfg) {
		t.Errorf("reasoning_effort=null: expected passthrough (empty config), got Mode=%v Level=%q", cfg.Mode, cfg.Level)
	}
}

// TestExtractDeepSeekConfig_EnabledWithNullEffort verifies thinking.type=enabled
// with a null effort behaves like enabled without effort: passthrough.
func TestExtractDeepSeekConfig_EnabledWithNullEffort(t *testing.T) {
	body := []byte(`{"thinking":{"type":"enabled"},"reasoning_effort":null}`)
	cfg := extractDeepSeekConfig(body)
	if hasThinkingConfig(cfg) {
		t.Errorf("enabled + effort=null: expected passthrough, got Mode=%v Level=%q", cfg.Mode, cfg.Level)
	}
}
