package cache

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	homekv "github.com/router-for-me/CLIProxyAPI/v7/internal/home"
)

// Tests for the shared item-list replay engine section of replay_engine.go
// exercised through a standalone store, independent of the codex/xai wrappers.

func newItemReplayTestStore() (*sync.Mutex, map[string]itemReplayEntry) {
	return &sync.Mutex{}, make(map[string]itemReplayEntry)
}

func TestItemReplayLookupExpiresAfterTTL(t *testing.T) {
	mu, entries := newItemReplayTestStore()
	now := time.Now()
	storeItemReplayLocal(mu, entries, 8, 2, "k", [][]byte{[]byte("a")}, now)

	if items, ok := lookupItemReplayLocal(mu, entries, time.Hour, "k", now.Add(time.Minute)); !ok || len(items) != 1 {
		t.Fatalf("live lookup ok=%v len=%d, want ok with 1 item", ok, len(items))
	}
	if _, ok := lookupItemReplayLocal(mu, entries, time.Hour, "k", now.Add(2*time.Hour)); ok {
		t.Fatal("expired entry still returned")
	}
	mu.Lock()
	_, present := entries["k"]
	mu.Unlock()
	if present {
		t.Fatal("expired entry not deleted on lookup")
	}
}

func TestItemReplayLookupClonesStoredItems(t *testing.T) {
	mu, entries := newItemReplayTestStore()
	now := time.Now()
	original := [][]byte{[]byte("a")}
	storeItemReplayLocal(mu, entries, 8, 2, "k", original, now)

	items, _ := lookupItemReplayLocal(mu, entries, time.Hour, "k", now)
	items[0][0] = 'z'
	if original[0][0] != 'a' {
		t.Fatal("lookup returned internal slice without cloning")
	}
}

func TestItemReplayUpdateResetsExpiredBeforeMerge(t *testing.T) {
	mu, entries := newItemReplayTestStore()
	now := time.Now()
	storeItemReplayLocal(mu, entries, 8, 2, "k", [][]byte{[]byte("old")}, now)

	updateItemReplayLocal(mu, entries, time.Hour, 8, 2, "k",
		func(existing [][]byte) [][]byte { return append(existing, []byte("new")) }, now.Add(2*time.Hour))

	items, ok := lookupItemReplayLocal(mu, entries, time.Hour, "k", now.Add(2*time.Hour))
	if !ok || len(items) != 1 || string(items[0]) != "new" {
		t.Fatalf("expired state not reset before merge: ok=%v items=%v", ok, items)
	}
}

func TestItemReplayEvictionRemovesOldest(t *testing.T) {
	mu, entries := newItemReplayTestStore()
	now := time.Now()
	for i := 0; i < 4; i++ {
		storeItemReplayLocal(mu, entries, 3, 2, string(rune('a'+i)), [][]byte{}, now.Add(time.Duration(i)*time.Minute))
	}
	mu.Lock()
	gotLen := len(entries)
	mu.Unlock()
	if gotLen > 3 {
		t.Fatalf("entries=%d, want eviction to cap at 3", gotLen)
	}
	if _, ok := entries["a"]; ok {
		t.Fatal("oldest entry survived eviction")
	}
}

func TestItemReplayPurgeExpiredOnly(t *testing.T) {
	mu, entries := newItemReplayTestStore()
	now := time.Now()
	storeItemReplayLocal(mu, entries, 8, 2, "old", [][]byte{}, now.Add(-2*time.Hour))
	storeItemReplayLocal(mu, entries, 8, 2, "live", [][]byte{}, now)

	purgeExpiredItemReplay(mu, entries, time.Hour, now)

	mu.Lock()
	_, oldPresent := entries["old"]
	_, livePresent := entries["live"]
	mu.Unlock()
	if oldPresent || !livePresent {
		t.Fatalf("purge result old=%v live=%v, want old gone live kept", oldPresent, livePresent)
	}
}

type fakeItemReplayKV struct {
	values    map[string][]byte
	getErr    error
	setErr    error
	expireErr error
	expired   int
}

func (c *fakeItemReplayKV) KVGet(_ context.Context, key string) ([]byte, bool, error) {
	if c.getErr != nil {
		return nil, false, c.getErr
	}
	raw, ok := c.values[key]
	return raw, ok, nil
}

func (c *fakeItemReplayKV) KVSet(_ context.Context, key string, value []byte, _ homekv.KVSetOptions) (bool, error) {
	if c.setErr != nil {
		return false, c.setErr
	}
	c.values[key] = value
	return true, nil
}

func (c *fakeItemReplayKV) KVDel(_ context.Context, keys ...string) (int64, error) {
	var removed int64
	for _, key := range keys {
		if _, ok := c.values[key]; ok {
			delete(c.values, key)
			removed++
		}
	}
	return removed, nil
}

func (c *fakeItemReplayKV) KVExpire(_ context.Context, _ string, _ time.Duration) (bool, error) {
	if c.expireErr != nil {
		return false, c.expireErr
	}
	c.expired++
	return true, nil
}

func TestItemReplayHomeGetFatalExpireError(t *testing.T) {
	client := &fakeItemReplayKV{values: map[string][]byte{"k": []byte(`["YQ=="]`)}, expireErr: errors.New("expire failed")}
	opts := itemReplayHomeOptions{KVKey: "k", TTL: time.Hour, Label: "codex", LogPrefix: "cpa:codex:*", ExpireErrFatal: true}
	if _, _, err := getItemReplayHome(context.Background(), client, opts); err == nil {
		t.Fatal("fatal expire error not surfaced")
	}
}

func TestItemReplayHomeGetTolerantExpireError(t *testing.T) {
	client := &fakeItemReplayKV{values: map[string][]byte{"k": []byte(`["YQ=="]`)}, expireErr: errors.New("expire failed")}
	opts := itemReplayHomeOptions{KVKey: "k", TTL: time.Hour, Label: "xai", LogPrefix: "cpa:xai:*"}
	items, ok, err := getItemReplayHome(context.Background(), client, opts)
	if err != nil || !ok || len(items) != 1 {
		t.Fatalf("tolerant expire failed: err=%v ok=%v items=%d", err, ok, len(items))
	}
}

func TestItemReplayHomePutThenGet(t *testing.T) {
	client := &fakeItemReplayKV{values: make(map[string][]byte)}
	opts := itemReplayHomeOptions{KVKey: "k", TTL: time.Hour}
	written, err := putItemReplayHome(context.Background(), client, opts, [][]byte{[]byte("a")})
	if err != nil || !written {
		t.Fatalf("put written=%v err=%v", written, err)
	}
	items, ok, err := getItemReplayHome(context.Background(), client, opts)
	if err != nil || !ok || len(items) != 1 || string(items[0]) != "a" {
		t.Fatalf("get err=%v ok=%v items=%v", err, ok, items)
	}
	if client.expired != 1 {
		t.Fatalf("TTL refresh count=%d, want 1", client.expired)
	}
}

func TestItemReplayHomeGetMissingKey(t *testing.T) {
	client := &fakeItemReplayKV{values: make(map[string][]byte)}
	items, ok, err := getItemReplayHome(context.Background(), client, itemReplayHomeOptions{KVKey: "k", TTL: time.Hour})
	if err != nil || ok || items != nil {
		t.Fatalf("missing key err=%v ok=%v items=%v, want clean miss", err, ok, items)
	}
}
