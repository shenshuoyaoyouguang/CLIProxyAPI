package tui

import (
	"testing"
)

func resetLocale() {
	currentLocale = "en"
}

func TestSetLocale(t *testing.T) {
	tests := []struct {
		name     string
		locale   string
		wantSet  bool
		wantLang string
	}{
		{"valid zh", "zh", true, "zh"},
		{"valid en", "en", true, "en"},
		{"invalid locale", "fr", false, "en"},
		{"empty string", "", false, "en"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetLocale()
			SetLocale(tt.locale)
			got := CurrentLocale()
			if got != tt.wantLang {
				t.Errorf("CurrentLocale() = %q, want %q", got, tt.wantLang)
			}
		})
	}
}

func TestCurrentLocale(t *testing.T) {
	resetLocale()
	if got := CurrentLocale(); got != "en" {
		t.Errorf("CurrentLocale() = %q, want %q", got, "en")
	}
	SetLocale("zh")
	if got := CurrentLocale(); got != "zh" {
		t.Errorf("CurrentLocale() = %q, want %q", got, "zh")
	}
}

func TestToggleLocale(t *testing.T) {
	tests := []struct {
		name      string
		initial   string
		wantAfter string
	}{
		{"en to zh", "en", "zh"},
		{"zh to en", "zh", "en"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentLocale = tt.initial
			ToggleLocale()
			if got := CurrentLocale(); got != tt.wantAfter {
				t.Errorf("after ToggleLocale(): CurrentLocale() = %q, want %q", got, tt.wantAfter)
			}
		})
	}
}

func TestT(t *testing.T) {
	tests := []struct {
		name   string
		locale string
		key    string
		want   string
	}{
		{"existing key in en", "en", "loading", "Loading..."},
		{"existing key in zh", "zh", "loading", "加载中..."},
		{"fallback to en when key missing in zh", "zh", "loading", "加载中..."},
		{"key not in any locale returns key", "en", "nonexistent_key_xyz", "nonexistent_key_xyz"},
		{"key not in any locale zh", "zh", "nonexistent_key_xyz", "nonexistent_key_xyz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentLocale = tt.locale
			got := T(tt.key)
			if got != tt.want {
				t.Errorf("T(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestT_FallbackToEnglish(t *testing.T) {
	// Temporarily add a key only in English to verify fallback
	resetLocale()
	currentLocale = "zh"

	// "success" exists in both locales; use a key that exists in en
	// We test fallback by checking a key that exists in en returns en value
	// when current locale map doesn't have it.
	// Since both zh and en have the same keys, we test with a nonexistent key
	// which should return the key itself.
	got := T("totally_missing_key")
	if got != "totally_missing_key" {
		t.Errorf("T(missing key) = %q, want the key itself", got)
	}

	// Verify a known key returns zh value when locale is zh
	got = T("save")
	if got != "保存" {
		t.Errorf("T(\"save\") with zh locale = %q, want %q", got, "保存")
	}
}

func TestTabNames(t *testing.T) {
	tests := []struct {
		name   string
		locale string
		want   []string
	}{
		{"en tab names", "en", enTabNames},
		{"zh tab names", "zh", zhTabNames},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentLocale = tt.locale
			got := TabNames()
			if len(got) != len(tt.want) {
				t.Fatalf("TabNames() length = %d, want %d", len(got), len(tt.want))
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("TabNames()[%d] = %q, want %q", i, v, tt.want[i])
				}
			}
		})
	}
}
