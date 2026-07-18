package cache

import (
	"testing"
	"time"
)

func TestLRUCache_SetGetAndOverwrite(t *testing.T) {
	c := NewLRUCache[string, int](8, time.Hour, nil)
	c.Set("a", 1)
	c.Set("b", 2)
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("Get(a) = %v,%v want 1,true", v, ok)
	}
	// Overwrite refreshes value.
	c.Set("a", 11)
	if v, ok := c.Get("a"); !ok || v != 11 {
		t.Fatalf("Get(a) after overwrite = %v,%v want 11,true", v, ok)
	}
	if c.Len() != 2 {
		t.Fatalf("Len() = %d want 2", c.Len())
	}
}

func TestLRUCache_MaxEntriesEviction(t *testing.T) {
	max := 4
	c := NewLRUCache[int, int](max, time.Hour, nil)
	for i := 0; i < max+6; i++ {
		c.Set(i, i)
	}
	if c.Len() != max {
		t.Fatalf("Len() = %d want %d (capacity must be bounded)", c.Len(), max)
	}
}

func TestLRUCache_AbsoluteTTLDoesNotRefreshOnRead(t *testing.T) {
	c := NewLRUCache[string, int](8, 30*time.Millisecond, nil)
	c.Set("k", 1)
	time.Sleep(10 * time.Millisecond)
	// Absolute TTL: reading must not extend the entry's age.
	if _, ok := c.Get("k"); !ok {
		t.Fatalf("Get before expiry should succeed")
	}
	time.Sleep(30 * time.Millisecond)
	if _, ok := c.Get("k"); ok {
		t.Fatalf("Get after absolute TTL should miss")
	}
}

func TestLRUCache_SlidingTTLRefreshesOnRead(t *testing.T) {
	c := NewLRUCache[string, int](8, 40*time.Millisecond, nil)
	c.SetSliding(true)
	c.Set("k", 1)
	// Repeated reads within the window keep the entry alive.
	for i := 0; i < 5; i++ {
		time.Sleep(15 * time.Millisecond)
		if _, ok := c.Get("k"); !ok {
			t.Fatalf("sliding Get should refresh age; miss at iteration %d", i)
		}
	}
}

func TestLRUCache_PurgeExpired(t *testing.T) {
	c := NewLRUCache[string, int](8, 50*time.Millisecond, nil)
	c.Set("fresh", 1)
	c.Set("stale", 2)
	now := time.Now().Add(100 * time.Millisecond)
	removed := c.PurgeExpired(now)
	if removed != 2 {
		t.Fatalf("PurgeExpired removed %d want 2", removed)
	}
	if c.Len() != 0 {
		t.Fatalf("Len after purge = %d want 0", c.Len())
	}
}

func TestLRUCache_DeleteAndClear(t *testing.T) {
	c := NewLRUCache[string, int](8, time.Hour, nil)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Delete("a")
	if _, ok := c.Get("a"); ok {
		t.Fatalf("Get(a) after Delete should miss")
	}
	if v, ok := c.Get("b"); !ok || v != 2 {
		t.Fatalf("Get(b) = %v,%v want 2,true", v, ok)
	}
	c.Clear()
	if c.Len() != 0 {
		t.Fatalf("Len after Clear = %d want 0", c.Len())
	}
}

func TestLRUCache_OnEvictCallback(t *testing.T) {
	var evicted []int
	c := NewLRUCache[int, int](2, time.Hour, func(_ int, v int) {
		evicted = append(evicted, v)
	})
	c.Set(1, 10)
	c.Set(2, 20)
	c.Set(3, 30) // evicts the oldest (1 -> 10)
	if len(evicted) != 1 || evicted[0] != 10 {
		t.Fatalf("onEvict got %v want [10]", evicted)
	}
}
