package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// isQuitCmd checks whether a tea.Cmd produces a tea.QuitMsg.
func isQuitCmd(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	_, ok := msg.(tea.QuitMsg)
	return ok
}

func newStandaloneApp() App {
	hook := NewLogHook(10)
	return NewApp(9999, "test-key", hook)
}

func newAuthRequiredApp() App {
	return NewApp(9999, "test-key", nil)
}

func TestNewApp_Standalone(t *testing.T) {
	app := newStandaloneApp()

	if !app.authenticated {
		t.Error("standalone app should be authenticated")
	}
	if !app.standalone {
		t.Error("standalone app should have standalone=true")
	}
	if !app.logsEnabled {
		t.Error("standalone app should have logsEnabled=true")
	}
}

func TestNewApp_AuthRequired(t *testing.T) {
	app := newAuthRequiredApp()

	if app.authenticated {
		t.Error("auth-required app should not be authenticated")
	}
	if app.standalone {
		t.Error("auth-required app should have standalone=false")
	}
}

func TestApp_WindowSizeMsg(t *testing.T) {
	app := newStandaloneApp()

	msg := tea.WindowSizeMsg{Width: 120, Height: 40}
	model, _ := app.Update(msg)
	updated := model.(App)

	if updated.width != 120 {
		t.Errorf("width = %d, want 120", updated.width)
	}
	if updated.height != 40 {
		t.Errorf("height = %d, want 40", updated.height)
	}
	if !updated.ready {
		t.Error("ready should be true after WindowSizeMsg")
	}
}

func TestApp_TabSwitch_Forward(t *testing.T) {
	app := newStandaloneApp()
	if app.activeTab != 0 {
		t.Fatalf("initial activeTab = %d, want 0", app.activeTab)
	}

	tabCount := len(app.tabs)
	if tabCount != 6 {
		t.Fatalf("expected 6 tabs, got %d", tabCount)
	}

	// Press tab once: 0 -> 1
	msg := tea.KeyMsg{Type: tea.KeyTab}
	model, _ := app.Update(msg)
	updated := model.(App)
	if updated.activeTab != 1 {
		t.Errorf("after one tab press: activeTab = %d, want 1", updated.activeTab)
	}

	// Press tab repeatedly to wrap around
	current := updated
	for i := 1; i < tabCount; i++ {
		m, _ := current.Update(msg)
		current = m.(App)
	}
	if current.activeTab != 0 {
		t.Errorf("after wrapping: activeTab = %d, want 0", current.activeTab)
	}
}

func TestApp_TabSwitch_Backward(t *testing.T) {
	app := newStandaloneApp()
	tabCount := len(app.tabs)

	// From 0, shift+tab should wrap to last tab
	msg := tea.KeyMsg{Type: tea.KeyShiftTab}
	model, _ := app.Update(msg)
	updated := model.(App)
	if updated.activeTab != tabCount-1 {
		t.Errorf("shift+tab from 0: activeTab = %d, want %d", updated.activeTab, tabCount-1)
	}

	// One more shift+tab should go to tabCount-2
	model, _ = updated.Update(msg)
	updated = model.(App)
	if updated.activeTab != tabCount-2 {
		t.Errorf("second shift+tab: activeTab = %d, want %d", updated.activeTab, tabCount-2)
	}
}

func TestApp_QuitOnCtrlC(t *testing.T) {
	app := newStandaloneApp()

	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	_, cmd := app.Update(msg)
	if !isQuitCmd(cmd) {
		t.Error("ctrl+c should return tea.Quit")
	}
}

func TestApp_QuitOnQ_NotLogsTab(t *testing.T) {
	app := newStandaloneApp()
	// Ensure we are on dashboard (not logs tab)
	app.activeTab = tabDashboard

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	_, cmd := app.Update(msg)
	if !isQuitCmd(cmd) {
		t.Error("'q' on non-logs tab should return tea.Quit")
	}
}

func TestApp_QuitOnQ_LogsTab(t *testing.T) {
	app := newStandaloneApp()
	app.activeTab = tabLogs

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	_, cmd := app.Update(msg)
	if isQuitCmd(cmd) {
		t.Error("'q' on logs tab should NOT return tea.Quit")
	}
}

func TestApp_LocaleToggle(t *testing.T) {
	// Reset locale to a known state
	SetLocale("en")
	app := newStandaloneApp()

	initialTabs := make([]string, len(app.tabs))
	copy(initialTabs, app.tabs)

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}}
	model, _ := app.Update(msg)
	updated := model.(App)

	// After toggle from "en", locale should be "zh" and tab names should differ
	if updated.tabs[0] == initialTabs[0] {
		t.Error("tabs should change after locale toggle")
	}

	// Toggle back
	SetLocale("en")
}

func TestApp_AuthGate_EmptyPassword(t *testing.T) {
	app := newAuthRequiredApp()
	// Clear the pre-filled secret key to simulate empty input
	app.authInput.SetValue("")

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	model, _ := app.Update(msg)
	updated := model.(App)

	if updated.authError == "" {
		t.Error("entering with empty password should set authError")
	}
	if updated.authenticated {
		t.Error("should not be authenticated after empty password")
	}
}

func TestApp_AuthConnect_Success(t *testing.T) {
	app := newAuthRequiredApp()

	msg := authConnectMsg{cfg: map[string]any{"some": "config"}, err: nil}
	model, _ := app.Update(msg)
	updated := model.(App)

	if !updated.authenticated {
		t.Error("authenticated should be true after successful authConnectMsg")
	}
	if updated.authError != "" {
		t.Errorf("authError should be empty, got %q", updated.authError)
	}
}

func TestIsLogsEnabledFromConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  map[string]any
		want bool
	}{
		{"nil config", nil, true},
		{"missing key", map[string]any{"other": "value"}, true},
		{"true value", map[string]any{"logging-to-file": true}, true},
		{"false value", map[string]any{"logging-to-file": false}, false},
		{"non-bool value", map[string]any{"logging-to-file": "yes"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLogsEnabledFromConfig(tt.cfg)
			if got != tt.want {
				t.Errorf("isLogsEnabledFromConfig(%v) = %v, want %v", tt.cfg, got, tt.want)
			}
		})
	}
}

func TestFitStringWidth(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxWidth int
		want     string
	}{
		{"zero width", "hello", 0, ""},
		{"negative width", "hello", -1, ""},
		{"short text fits", "hi", 10, "hi"},
		{"exact fit", "hello", 5, "hello"},
		{"truncated", "hello world this is long", 5, "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fitStringWidth(tt.text, tt.maxWidth)
			if tt.maxWidth <= 0 {
				if got != "" {
					t.Errorf("fitStringWidth(%q, %d) = %q, want empty", tt.text, tt.maxWidth, got)
				}
				return
			}
			if tt.text == tt.want {
				// Short text case: should be unchanged
				if got != tt.want {
					t.Errorf("fitStringWidth(%q, %d) = %q, want %q", tt.text, tt.maxWidth, got, tt.want)
				}
			} else if tt.maxWidth > 0 && got == "" && tt.text != "" {
				t.Errorf("fitStringWidth(%q, %d) returned empty string unexpectedly", tt.text, tt.maxWidth)
			}
			// For truncation, verify output is within maxWidth
			if len(got) > tt.maxWidth && tt.maxWidth > 0 {
				// lipgloss width may differ from len for wide chars,
				// but for ASCII this is a reasonable check
				t.Errorf("fitStringWidth(%q, %d) = %q (len %d), exceeds maxWidth", tt.text, tt.maxWidth, got, len(got))
			}
		})
	}
}
