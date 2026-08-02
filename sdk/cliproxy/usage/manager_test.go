package usage

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// recordFunc adapts a function to the Plugin interface for tests.
type recordFunc func(context.Context, Record)

func (f recordFunc) HandleUsage(ctx context.Context, record Record) { f(ctx, record) }

func TestGenerateEnabledDefaultsNilToTrue(t *testing.T) {
	if !GenerateEnabled(nil) {
		t.Fatalf("GenerateEnabled(nil) = false, want true")
	}
}

func TestGenerateEnabledHonorsExplicitFalse(t *testing.T) {
	if GenerateEnabled(GenerateFlag(false)) {
		t.Fatalf("GenerateEnabled(false) = true, want false")
	}
}

func TestGenerateEnabledHonorsExplicitTrue(t *testing.T) {
	if !GenerateEnabled(GenerateFlag(true)) {
		t.Fatalf("GenerateEnabled(true) = false, want true")
	}
}

func TestGenerateFromContextDefaultsMissingToTrue(t *testing.T) {
	if !GenerateFromContext(context.Background()) {
		t.Fatalf("GenerateFromContext(background) = false, want true")
	}
}

func TestGenerateFromContextHonorsExplicitFalse(t *testing.T) {
	ctx := WithGenerate(context.Background(), false)
	if GenerateFromContext(ctx) {
		t.Fatalf("GenerateFromContext(false) = true, want false")
	}
}

func TestRecordOmittedGenerateIsEnabled(t *testing.T) {
	// Existing callers construct Record without setting Generate.
	// Omission must remain distinguishable from explicit false and default to true.
	record := Record{
		Provider: "openai",
		Model:    "gpt-5.4",
	}
	if record.Generate != nil {
		t.Fatalf("Record.Generate = %v, want nil for omitted field", record.Generate)
	}
	if !GenerateEnabled(record.Generate) {
		t.Fatalf("GenerateEnabled(omitted) = false, want true")
	}
}

func TestPublishDropsWhenQueueFull(t *testing.T) {
	const capacity = 4
	m := NewManager(capacity)

	entered := make(chan struct{})
	release := make(chan struct{})
	var dispatched atomic.Int64
	m.Register(recordFunc(func(ctx context.Context, _ Record) {
		dispatched.Add(1)
		if dispatched.Load() == 1 {
			close(entered)
			<-release
		}
	}))

	// First record is picked up by the dispatcher, which then blocks inside the
	// plugin so the bounded queue fills up.
	m.Publish(context.Background(), Record{Provider: "p"})
	<-entered
	for i := 0; i < capacity; i++ {
		m.Publish(context.Background(), Record{Provider: "p"})
	}
	// The queue is now full; this record must be dropped.
	m.Publish(context.Background(), Record{Provider: "p"})

	close(release)
	m.Stop()

	// Exactly one (the first) plus the capacity buffered records are dispatched;
	// the overflow record was dropped.
	if got := dispatched.Load(); got != capacity+1 {
		t.Fatalf("dispatched = %d, want %d (overflow record should have been dropped)", got, capacity+1)
	}
}

func TestStopWaitsForInFlightDispatch(t *testing.T) {
	m := NewManager(8)

	entered := make(chan struct{})
	done := make(chan struct{})
	m.Register(recordFunc(func(ctx context.Context, _ Record) {
		close(entered)
		<-done
	}))
	m.Publish(context.Background(), Record{Provider: "p"})
	<-entered // dispatcher is now mid-dispatch inside the plugin

	stopped := make(chan struct{})
	go func() {
		m.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("Stop returned while a dispatch was still in progress")
	case <-time.After(50 * time.Millisecond):
	}

	close(done) // let the in-flight dispatch finish
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after the in-flight dispatch completed")
	}
}
