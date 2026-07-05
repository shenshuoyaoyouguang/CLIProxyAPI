// Package thinking provides unified thinking configuration processing.
package thinking

// DegradeThinkingLevel lowers a thinking level by one notch along the canonical chain.
//
// Degradation chain:
//   - max/xhigh → high
//   - high       → medium
//   - medium     → low
//   - low        → minimal
//   - minimal    → empty string (signals field removal)
//   - auto/none  → returned unchanged
//   - unknown    → empty string
func DegradeThinkingLevel(level ThinkingLevel) ThinkingLevel {
	switch level {
	case LevelMax, LevelXHigh:
		return LevelHigh
	case LevelHigh:
		return LevelMedium
	case LevelMedium:
		return LevelLow
	case LevelLow:
		return LevelMinimal
	case LevelMinimal, LevelNone, LevelAuto:
		// minimal → empty (field removal); none/auto → no-op
		return ""
	default:
		return ""
	}
}

// DegradeThinkingConfig lowers thinking effort by one notch on the canonical config.
//
// Mode handling:
//   - ModeLevel: degrades the Level field via DegradeThinkingLevel.
//   - ModeBudget: reduces Budget to the next lower level threshold.
//     e.g. 32768 (xhigh) → 24576 (high) → 8192 (medium) → 1024 (low) → 512 (minimal) → 0 (none)
//   - ModeNone / ModeAuto: no-op, returns cfg unchanged.
func DegradeThinkingConfig(cfg ThinkingConfig) ThinkingConfig {
	switch cfg.Mode {
	case ModeLevel:
		cfg.Level = DegradeThinkingLevel(cfg.Level)
		if cfg.Level == "" {
			cfg.Mode = ModeNone
			cfg.Budget = 0
		}
		return cfg

	case ModeBudget:
		if cfg.Budget <= 0 {
			return cfg
		}
		level, ok := ConvertBudgetToLevel(cfg.Budget)
		if !ok {
			return cfg
		}
		degraded := DegradeThinkingLevel(ThinkingLevel(level))
		if degraded == "" {
			cfg.Mode = ModeNone
			cfg.Budget = 0
			cfg.Level = ""
			return cfg
		}
		budget, ok := ConvertLevelToBudget(string(degraded))
		if !ok {
			cfg.Mode = ModeNone
			cfg.Budget = 0
			cfg.Level = ""
			return cfg
		}
		cfg.Budget = budget
		cfg.Level = ""
		return cfg

	default:
		// ModeNone, ModeAuto: no-op
		return cfg
	}
}
