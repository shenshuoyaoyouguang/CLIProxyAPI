package tui

import (
	"fmt"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

func TestNewLogHook(t *testing.T) {
	h := NewLogHook(10)
	if h == nil {
		t.Fatal("NewLogHook returned nil")
	}
	if cap(h.ch) != 10 {
		t.Errorf("channel capacity = %d, want 10", cap(h.ch))
	}
}

func TestLogHook_Levels(t *testing.T) {
	h := NewLogHook(5)
	levels := h.Levels()
	if len(levels) != len(log.AllLevels) {
		t.Errorf("Levels() returned %d levels, want %d", len(levels), len(log.AllLevels))
	}
}

func TestLogHook_Fire(t *testing.T) {
	h := NewLogHook(10)
	entry := &log.Entry{
		Level:   log.InfoLevel,
		Message: "hello world",
	}

	err := h.Fire(entry)
	if err != nil {
		t.Fatalf("Fire() returned error: %v", err)
	}

	select {
	case line := <-h.Chan():
		if line == "" {
			t.Error("Fire() sent empty string to channel")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message on channel")
	}
}

func TestLogHook_Fire_ChannelOverflow(t *testing.T) {
	h := NewLogHook(2)

	// Fill the buffer
	for i := 0; i < 2; i++ {
		entry := &log.Entry{
			Level:   log.InfoLevel,
			Message: fmt.Sprintf("msg-%d", i),
		}
		if err := h.Fire(entry); err != nil {
			t.Fatalf("Fire() error on msg-%d: %v", i, err)
		}
	}

	// Buffer is full; fire another message — should drop oldest and add new
	entry := &log.Entry{
		Level:   log.InfoLevel,
		Message: "msg-overflow",
	}
	if err := h.Fire(entry); err != nil {
		t.Fatalf("Fire() error on overflow: %v", err)
	}

	// Drain channel and verify the new message is present
	found := false
	for i := 0; i < 2; i++ {
		select {
		case line := <-h.Chan():
			if line != "" && containsSubstring(line, "msg-overflow") {
				found = true
			}
		case <-time.After(time.Second):
			t.Fatal("timeout draining channel")
		}
	}
	if !found {
		t.Error("overflow message not found in channel after drop")
	}
}

func TestLogHook_SetFormatter(t *testing.T) {
	h := NewLogHook(10)

	customFmt := &log.JSONFormatter{}
	h.SetFormatter(customFmt)

	entry := &log.Entry{
		Level:   log.WarnLevel,
		Message: "json test",
		Data:    log.Fields{"key": "value"},
	}

	if err := h.Fire(entry); err != nil {
		t.Fatalf("Fire() error: %v", err)
	}

	select {
	case line := <-h.Chan():
		// JSON formatter produces output with braces
		if len(line) == 0 {
			t.Error("expected non-empty formatted line")
		}
		if !containsSubstring(line, "json test") {
			t.Errorf("formatted line %q does not contain message", line)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for formatted message")
	}
}

func TestLogHook_Fire_NilFormatter(t *testing.T) {
	h := NewLogHook(10)
	// Explicitly set formatter to nil
	h.mu.Lock()
	h.formatter = nil
	h.mu.Unlock()

	entry := &log.Entry{
		Level:   log.ErrorLevel,
		Message: "nil fmt test",
	}

	if err := h.Fire(entry); err != nil {
		t.Fatalf("Fire() error: %v", err)
	}

	select {
	case line := <-h.Chan():
		// Fallback format: [level] message
		want := fmt.Sprintf("[%s] %s", log.ErrorLevel, "nil fmt test")
		if line != want {
			t.Errorf("line = %q, want %q", line, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestLogHook_ConcurrentFire(t *testing.T) {
	h := NewLogHook(100)
	var wg sync.WaitGroup
	numGoroutines := 20
	msgsPerGoroutine := 5

	wg.Add(numGoroutines)
	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < msgsPerGoroutine; i++ {
				entry := &log.Entry{
					Level:   log.InfoLevel,
					Message: fmt.Sprintf("goroutine-%d-msg-%d", id, i),
				}
				if err := h.Fire(entry); err != nil {
					t.Errorf("Fire() error: %v", err)
				}
			}
		}(g)
	}

	wg.Wait()

	// Drain and count messages
	count := 0
	for {
		select {
		case <-h.Chan():
			count++
		default:
			goto done
		}
	}
done:
	if count == 0 {
		t.Error("no messages received from concurrent Fire calls")
	}
	if count > numGoroutines*msgsPerGoroutine {
		t.Errorf("received more messages (%d) than sent (%d)", count, numGoroutines*msgsPerGoroutine)
	}
}

func TestLogHook_Chan(t *testing.T) {
	h := NewLogHook(5)
	ch := h.Chan()
	if ch == nil {
		t.Fatal("Chan() returned nil")
	}
	// Verify it's the same channel
	entry := &log.Entry{Level: log.InfoLevel, Message: "chan test"}
	_ = h.Fire(entry)
	select {
	case <-ch:
		// ok
	case <-time.After(time.Second):
		t.Fatal("could not read from Chan()")
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}