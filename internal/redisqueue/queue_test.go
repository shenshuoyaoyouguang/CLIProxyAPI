package redisqueue

import (
	"testing"
)

func TestPeekOldest_DoesNotRemoveItems(t *testing.T) {
	prevQueueEnabled := Enabled()
	prevUsageEnabled := UsageStatisticsEnabled()
	SetEnabled(true)
	SetUsageStatisticsEnabled(true)
	t.Cleanup(func() {
		SetEnabled(prevQueueEnabled)
		SetUsageStatisticsEnabled(prevUsageEnabled)
	})

	Enqueue([]byte(`{"test": "item1"}`))
	Enqueue([]byte(`{"test": "item2"}`))

	items1 := PeekOldest(10)
	if len(items1) != 2 {
		t.Fatalf("first PeekOldest returned %d items, want 2", len(items1))
	}

	items2 := PeekOldest(10)
	if len(items2) != 2 {
		t.Fatalf("second PeekOldest returned %d items, want 2 (items should still be in queue)", len(items2))
	}

	items3 := PeekOldest(1)
	if len(items3) != 1 {
		t.Fatalf("PeekOldest(1) returned %d items, want 1", len(items3))
	}
}

func TestPopOldest_RemovesItems(t *testing.T) {
	prevQueueEnabled := Enabled()
	prevUsageEnabled := UsageStatisticsEnabled()
	SetEnabled(true)
	SetUsageStatisticsEnabled(true)
	t.Cleanup(func() {
		SetEnabled(prevQueueEnabled)
		SetUsageStatisticsEnabled(prevUsageEnabled)
	})

	Enqueue([]byte(`{"test": "item1"}`))
	Enqueue([]byte(`{"test": "item2"}`))

	items1 := PopOldest(1)
	if len(items1) != 1 {
		t.Fatalf("first PopOldest returned %d items, want 1", len(items1))
	}

	items2 := PopOldest(10)
	if len(items2) != 1 {
		t.Fatalf("second PopOldest returned %d items, want 1 (one item should remain)", len(items2))
	}

	items3 := PopOldest(10)
	if len(items3) != 0 {
		t.Fatalf("third PopOldest returned %d items, want 0 (queue should be empty)", len(items3))
	}
}

func TestPeekOldest_ReturnsNilWhenDisabled(t *testing.T) {
	prevQueueEnabled := Enabled()
	SetEnabled(false)
	t.Cleanup(func() {
		SetEnabled(prevQueueEnabled)
	})

	items := PeekOldest(10)
	if items != nil {
		t.Fatalf("PeekOldest when disabled returned %v, want nil", items)
	}
}

func TestPeekOldest_ReturnsNilForInvalidCount(t *testing.T) {
	prevQueueEnabled := Enabled()
	prevUsageEnabled := UsageStatisticsEnabled()
	SetEnabled(true)
	SetUsageStatisticsEnabled(true)
	t.Cleanup(func() {
		SetEnabled(prevQueueEnabled)
		SetUsageStatisticsEnabled(prevUsageEnabled)
	})

	Enqueue([]byte(`{"test": "item1"}`))

	items := PeekOldest(0)
	if items != nil {
		t.Fatalf("PeekOldest(0) returned %v, want nil", items)
	}

	items = PeekOldest(-1)
	if items != nil {
		t.Fatalf("PeekOldest(-1) returned %v, want nil", items)
	}
}

func TestPeekOldest_RespectsLimit(t *testing.T) {
	prevQueueEnabled := Enabled()
	prevUsageEnabled := UsageStatisticsEnabled()
	SetEnabled(true)
	SetUsageStatisticsEnabled(true)
	t.Cleanup(func() {
		SetEnabled(prevQueueEnabled)
		SetUsageStatisticsEnabled(prevUsageEnabled)
	})

	Enqueue([]byte(`{"test": "item1"}`))
	Enqueue([]byte(`{"test": "item2"}`))
	Enqueue([]byte(`{"test": "item3"}`))

	items := PeekOldest(2)
	if len(items) != 2 {
		t.Fatalf("PeekOldest(2) returned %d items, want 2", len(items))
	}

	allItems := PeekOldest(10)
	if len(allItems) != 3 {
		t.Fatalf("PeekOldest(10) returned %d items, want 3", len(allItems))
	}
}
