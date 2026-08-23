package cache

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	homekv "github.com/router-for-me/CLIProxyAPI/v7/internal/home"
)

// Tests for the shared generation-fenced replay base (fenced_value_cache.go)
// exercised through a standalone []byte-valued store, independent of the
// kimi/claude wrappers.

func newFencedTestCache() (*sync.Mutex, map[string]fencedReplayEntry[[]byte], *int) {
	total := 0
	return &sync.Mutex{}, make(map[string]fencedReplayEntry[[]byte]), &total
}

var fencedTestLimits = fencedReplayLimits{TTL: time.Hour, MaxEntries: 8, EvictBatch: 2, MaxTotalBytes: 1 << 20}

func fencedTestSize(v []byte) int { return len(v) }

func TestFencedLookupReservesTombstoneForAbsentKey(t *testing.T) {
	mu, entries, total := newFencedTestCache()
	now := time.Now()

	entry := lookupFencedReplayLocal(mu, entries, total, fencedTestLimits, fencedTestSize, "k", now)
	if !entry.Deleted || entry.Generation == "" || entry.Timestamp != now {
		t.Fatalf("absent lookup did not reserve tombstone: %+v", entry)
	}
	if _, ok := entries["k"]; !ok {
		t.Fatal("reservation not stored")
	}
}

func TestFencedLookupExpiresAndReserves(t *testing.T) {
	mu, entries, total := newFencedTestCache()
	now := time.Now()
	storeFencedReplayLocal(mu, entries, total, fencedTestLimits, fencedTestSize, "k", []byte("v"), "gen-1", false, now.Add(-2*time.Hour))
	before := *total

	entry := lookupFencedReplayLocal(mu, entries, total, fencedTestLimits, fencedTestSize, "k", now)
	if !entry.Deleted {
		t.Fatal("expired entry not replaced by tombstone")
	}
	if *total != before-len("v") {
		t.Fatalf("total bytes after expiry = %d, want %d", *total, before-len("v"))
	}
}

func TestFencedSnapshotGuardRejectsGenerationMismatch(t *testing.T) {
	mu, entries, total := newFencedTestCache()
	now := time.Now()
	storeFencedReplayLocal(mu, entries, total, fencedTestLimits, fencedTestSize, "k", []byte("v"), "gen-1", false, now)

	// A stale snapshot (different generation) must be rejected.
	mu.Lock()
	_, current := fencedReplaySnapshotCurrentLocked(entries, "k", true, "gen-stale")
	mu.Unlock()
	if current {
		t.Fatal("stale generation accepted by CAS guard")
	}

	// found-mismatch must be rejected too.
	mu.Lock()
	if _, current := fencedReplaySnapshotCurrentLocked(entries, "missing", true, "gen-1"); current {
		t.Fatal("found mismatch accepted by CAS guard")
	}
	mu.Unlock()
}

func TestFencedCommitReplaceAccountsBytes(t *testing.T) {
	mu, entries, total := newFencedTestCache()
	now := time.Now()
	storeFencedReplayLocal(mu, entries, total, fencedTestLimits, fencedTestSize, "k", []byte("vvvv"), "gen-1", false, now)
	want := *total - len("vvvv") + len("short")

	mu.Lock()
	generation := commitFencedReplayReplaceLocked(entries, total, fencedTestLimits, fencedTestSize, "k", []byte("short"), now)
	mu.Unlock()

	if generation == "" || generation == "gen-1" {
		t.Fatalf("replace did not mint fresh generation: %q", generation)
	}
	if *total != want {
		t.Fatalf("total bytes after replace = %d, want %d", *total, want)
	}
	if got := entries["k"].Value; string(got) != "short" {
		t.Fatalf("replaced value %q", got)
	}
}

func TestFencedTombstoneRequiresCurrentSnapshot(t *testing.T) {
	mu, entries, total := newFencedTestCache()
	now := time.Now()
	storeFencedReplayLocal(mu, entries, total, fencedTestLimits, fencedTestSize, "k", []byte("v"), "gen-1", false, now)

	if tombstoneFencedReplayIfUnchanged(mu, entries, total, fencedTestSize, "k", true, "gen-stale", now) {
		t.Fatal("tombstone accepted stale snapshot")
	}
	if entries["k"].Deleted {
		t.Fatal("stale tombstone overwrote live entry")
	}
	if !tombstoneFencedReplayIfUnchanged(mu, entries, total, fencedTestSize, "k", true, "gen-1", now) {
		t.Fatal("tombstone rejected current snapshot")
	}
	if !entries["k"].Deleted || entries["k"].Value != nil && len(entries["k"].Value) != 0 {
		t.Fatalf("entry not tombstoned: %+v", entries["k"])
	}
}

func TestFencedEnforceEvictsUntilTotalBytesCap(t *testing.T) {
	mu, entries, total := newFencedTestCache()
	limits := fencedReplayLimits{TTL: time.Hour, MaxEntries: 100, EvictBatch: 1, MaxTotalBytes: 10}
	now := time.Now()
	for i := 0; i < 5; i++ {
		storeFencedReplayLocal(mu, entries, total, limits, fencedTestSize, string(rune('a'+i)), []byte(strings.Repeat("x", 8)), uuid.NewString(), false, now.Add(time.Duration(i)*time.Minute))
	}
	if *total > limits.MaxTotalBytes {
		t.Fatalf("total bytes %d exceeds cap %d after enforcement", *total, limits.MaxTotalBytes)
	}
	if _, ok := entries["a"]; ok {
		t.Fatal("oldest entry survived byte-cap eviction")
	}
}

type fakeFencedKV struct {
	values    map[string][]byte
	casWrites int
	failFirst bool
	attempts  int
}

func (c *fakeFencedKV) KVGet(_ context.Context, key string) ([]byte, bool, error) {
	raw, ok := c.values[key]
	return raw, ok, nil
}

func (c *fakeFencedKV) KVSet(_ context.Context, key string, value []byte, _ homekv.KVSetOptions) (bool, error) {
	c.values[key] = value
	return true, nil
}

func (c *fakeFencedKV) KVDel(_ context.Context, keys ...string) (int64, error) { return 0, nil }

func (c *fakeFencedKV) KVCompareAndSwap(_ context.Context, key string, expected []byte, expectedExists bool, value []byte, _ time.Duration) (bool, error) {
	c.attempts++
	current, exists := c.values[key]
	if exists != expectedExists || (exists && string(current) != string(expected)) {
		return false, nil
	}
	if c.failFirst && c.attempts == 1 {
		// Transient contention: reservation lost without a visible write.
		return false, nil
	}
	c.values[key] = value
	c.casWrites++
	return true, nil
}

func (c *fakeFencedKV) KVExpire(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return true, nil
}

func TestFencedReadOrReserveHomeReservation(t *testing.T) {
	client := &fakeFencedKV{values: make(map[string][]byte)}
	tombstone := []byte(`{"generation":"t","deleted":true}`)
	raw, err := readOrReserveFencedReplayHome(context.Background(), client, "k", time.Hour, 1024, "test", tombstone)
	if err != nil || string(raw) != string(tombstone) {
		t.Fatalf("reserve raw=%s err=%v", raw, err)
	}
	if client.values["k"] == nil {
		t.Fatal("tombstone not persisted")
	}
}

func TestFencedReadOrReserveHomeRetriesAfterRace(t *testing.T) {
	client := &fakeFencedKV{values: make(map[string][]byte), failFirst: true}
	tombstone := []byte(`{"generation":"t","deleted":true}`)
	raw, err := readOrReserveFencedReplayHome(context.Background(), client, "k", time.Hour, 1024, "test", tombstone)
	if err != nil || client.attempts < 2 {
		t.Fatalf("race retry attempts=%d err=%v", client.attempts, err)
	}
	if string(raw) != string(tombstone) {
		t.Fatalf("returned raw %s != tombstone", raw)
	}
}

func TestFencedReadOrReserveHomeReturnsExisting(t *testing.T) {
	client := &fakeFencedKV{values: map[string][]byte{"k": []byte(`{"generation":"g"}`)}}
	raw, err := readOrReserveFencedReplayHome(context.Background(), client, "k", time.Hour, 1024, "test", []byte("unused"))
	if err != nil || string(raw) != `{"generation":"g"}` {
		t.Fatalf("existing raw=%s err=%v", raw, err)
	}
}
