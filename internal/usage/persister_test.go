package usage

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
)

// mockObjectStore implements store.ObjectStorePersistence for testing.
type mockObjectStore struct {
	objects     map[string][]byte
	putErr      error
	listErr     error
	getErr      error
	deleteErr   error
	putCalled   int
	deleteCalls []string
}

func newMockObjectStore() *mockObjectStore {
	return &mockObjectStore{
		objects:     make(map[string][]byte),
		deleteCalls: make([]string, 0),
	}
}

func (m *mockObjectStore) PutObject(_ context.Context, key string, data []byte) error {
	m.putCalled++
	if m.putErr != nil {
		return m.putErr
	}
	m.objects[key] = data
	return nil
}

func (m *mockObjectStore) ListObjects(_ context.Context, prefix string) ([]string, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var keys []string
	for k := range m.objects {
		if prefix == "" || len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (m *mockObjectStore) GetObject(_ context.Context, key string) ([]byte, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if data, ok := m.objects[key]; ok {
		return data, nil
	}
	return nil, errors.New("not found")
}

func (m *mockObjectStore) DeleteObject(_ context.Context, key string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.deleteCalls = append(m.deleteCalls, key)
	delete(m.objects, key)
	return nil
}

// --- Tests ---

func TestPersistNow_UploadsSnapshotToObjectStorage(t *testing.T) {
	store := newMockObjectStore()
	cfg := UsagePersistConfig{
		Enabled:          true,
		ObjectPathPrefix: "snaps/",
	}

	persister := NewSnapshotPersister(store, cfg, nil)
	ctx := context.Background()

	err := persister.PersistNow(ctx)
	if err != nil {
		t.Fatalf("PersistNow() error = %v, want nil", err)
	}

	if store.putCalled != 1 {
		t.Fatalf("putCalled = %d, want 1", store.putCalled)
	}

	// Verify key format: {prefix}usage-snapshot-{unix_ts}.json
	var uploadedKey string
	for k := range store.objects {
		uploadedKey = k
		break
	}
	if uploadedKey == "" {
		t.Fatal("no object was uploaded")
	}

	const wantPrefix = "snaps/usage-snapshot-"
	if len(uploadedKey) < len(wantPrefix) || uploadedKey[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("uploaded key = %q, want prefix %q", uploadedKey, wantPrefix)
	}
	if uploadedKey[len(uploadedKey)-5:] != ".json" {
		t.Fatalf("uploaded key = %q should end with .json", uploadedKey)
	}

	// Verify JSON content is parseable
	data := store.objects[uploadedKey]
	var snap UsageSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("uploaded data is not valid JSON: %v", err)
	}
	if snap.APIs == nil {
		t.Fatal("snapshot.APIs is nil, want empty map")
	}
}

func TestPersistNow_UploadsCorrectTimestampedFilename(t *testing.T) {
	store := newMockObjectStore()
	cfg := UsagePersistConfig{
		Enabled:          true,
		ObjectPathPrefix: "snaps/",
	}

	persister := NewSnapshotPersister(store, cfg, nil)
	ctx := context.Background()

	before := time.Now().Unix()
	err := persister.PersistNow(ctx)
	if err != nil {
		t.Fatalf("PersistNow() error = %v, want nil", err)
	}
	after := time.Now().Unix()

	var key string
	for k := range store.objects {
		key = k
		break
	}

	const prefix = "snaps/usage-snapshot-"
	const suffix = ".json"
	if key[:len(prefix)] != prefix {
		t.Fatalf("key = %q, want prefix %q", key, prefix)
	}
	if key[len(key)-len(suffix):] != suffix {
		t.Fatalf("key = %q, want suffix %q", key, suffix)
	}

	tsStr := key[len(prefix) : len(key)-len(suffix)]
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		t.Fatalf("timestamp in key %q is not a valid int64: %v", key, err)
	}
	if ts < before || ts > after {
		t.Fatalf("timestamp %d not in [%d, %d]", ts, before, after)
	}
}

func TestPersistNow_DisabledDoesNothing(t *testing.T) {
	store := newMockObjectStore()
	cfg := UsagePersistConfig{
		Enabled:          false,
		ObjectPathPrefix: "snaps/",
	}

	persister := NewSnapshotPersister(store, cfg, nil)
	err := persister.PersistNow(context.Background())
	if err != nil {
		t.Fatalf("PersistNow() when disabled: error = %v, want nil", err)
	}
	if store.putCalled > 0 {
		t.Fatalf("putCalled = %d, want 0 (disabled)", store.putCalled)
	}
}

func TestPersistNow_ObjectStoreError(t *testing.T) {
	store := newMockObjectStore()
	store.putErr = errors.New("connection refused")
	cfg := UsagePersistConfig{
		Enabled:          true,
		ObjectPathPrefix: "snaps/",
	}

	persister := NewSnapshotPersister(store, cfg, nil)
	err := persister.PersistNow(context.Background())
	if err == nil {
		t.Fatal("PersistNow() want error, got nil")
	}
	if store.putCalled != 1 {
		t.Fatalf("putCalled = %d, want 1", store.putCalled)
	}
}

func TestRecoverLatest_LoadsLatestSnapshot(t *testing.T) {
	store := newMockObjectStore()
	now := time.Now().Unix()
	// Add older and newer snapshots
	store.objects["snaps/usage-snapshot-"+strconv.FormatInt(now-86400*10, 10)+".json"] =
		[]byte(`{"apis":{}}`)
	store.objects["snaps/usage-snapshot-"+strconv.FormatInt(now-86400, 10)+".json"] =
		[]byte(`{"apis":{"POST /v1/chat/completions":{"total_requests":42,"success_count":40,"failure_count":2,"total_tokens":1000,"models":{}}}}`)

	cfg := UsagePersistConfig{
		Enabled:          true,
		ObjectPathPrefix: "snaps/",
	}

	persister := NewSnapshotPersister(store, cfg, nil)
	ctx := context.Background()

	snapshot, err := persister.RecoverLatest(ctx)
	if err != nil {
		t.Fatalf("RecoverLatest() error = %v, want nil", err)
	}
	if snapshot == nil {
		t.Fatal("RecoverLatest() returned nil")
	}

	entry, ok := snapshot.APIs["POST /v1/chat/completions"]
	if !ok {
		t.Fatal("missing endpoint in recovered snapshot")
	}
	if entry.TotalRequests != 42 {
		t.Fatalf("TotalRequests = %d, want 42", entry.TotalRequests)
	}
}

func TestRecoverLatest_NoSnapshots(t *testing.T) {
	store := newMockObjectStore()
	cfg := UsagePersistConfig{
		Enabled:          true,
		ObjectPathPrefix: "snaps/",
	}

	persister := NewSnapshotPersister(store, cfg, nil)
	ctx := context.Background()

	snapshot, err := persister.RecoverLatest(ctx)
	if err != nil {
		t.Fatalf("RecoverLatest() error = %v, want nil", err)
	}
	if snapshot != nil {
		t.Fatalf("RecoverLatest() with no snapshots: snapshot = %+v, want nil", snapshot)
	}
}

func TestRecoverLatest_CorruptSnapshotFallsBackToNext(t *testing.T) {
	store := newMockObjectStore()
	store.objects["snaps/usage-snapshot-1717000000.json"] = []byte(`{invalid`)
	store.objects["snaps/usage-snapshot-1717100000.json"] =
		[]byte(`{"apis":{"endpoint":{"total_requests":99,"success_count":99,"failure_count":0,"total_tokens":0,"models":{}}}}`)
	store.objects["snaps/usage-snapshot-1717200000.json"] = []byte(`{also invalid`)

	cfg := UsagePersistConfig{
		Enabled:          true,
		ObjectPathPrefix: "snaps/",
	}

	persister := NewSnapshotPersister(store, cfg, nil)
	ctx := context.Background()

	snapshot, err := persister.RecoverLatest(ctx)
	if err != nil {
		t.Fatalf("RecoverLatest() error = %v, want nil (should fallback)", err)
	}
	if snapshot == nil {
		t.Fatal("RecoverLatest() returned nil, want valid fallback snapshot")
	}
	entry, ok := snapshot.APIs["endpoint"]
	if !ok {
		t.Fatal("missing endpoint in recovered snapshot")
	}
	if entry.TotalRequests != 99 {
		t.Fatalf("TotalRequests = %d, want 99", entry.TotalRequests)
	}
}

func TestRecoverLatest_AllCorrupt(t *testing.T) {
	store := newMockObjectStore()
	store.objects["snaps/usage-snapshot-1717000000.json"] = []byte(`{invalid`)
	store.objects["snaps/usage-snapshot-1717100000.json"] = []byte(`{also invalid`)

	cfg := UsagePersistConfig{
		Enabled:          true,
		ObjectPathPrefix: "snaps/",
	}

	persister := NewSnapshotPersister(store, cfg, nil)
	ctx := context.Background()

	snapshot, err := persister.RecoverLatest(ctx)
	if err != nil {
		t.Fatalf("RecoverLatest() error = %v, want nil", err)
	}
	if snapshot != nil {
		t.Fatalf("RecoverLatest() with all corrupt: snapshot = %+v, want nil", snapshot)
	}
}

func TestCleanupExpired_DeletesOldSnapshots(t *testing.T) {
	store := newMockObjectStore()
	now := time.Now().Unix()
	// 31 days ago (expired, >30), 30 days+1h ago (expired), 29 days ago (kept), 1 day ago (kept)
	expired1 := now - (31 * 86400)        // definitely expired
	expired2 := now - (30 * 86400) - 3700 // 30 days + ~1 hour, expired
	kept1 := now - (29 * 86400)           // definitely within 30 days
	kept2 := now - (1 * 86400)            // definitely within 30 days

	store.objects["snaps/usage-snapshot-"+strconv.FormatInt(expired1, 10)+".json"] = []byte(`{}`)
	store.objects["snaps/usage-snapshot-"+strconv.FormatInt(expired2, 10)+".json"] = []byte(`{}`)
	store.objects["snaps/usage-snapshot-"+strconv.FormatInt(kept1, 10)+".json"] = []byte(`{}`)
	store.objects["snaps/usage-snapshot-"+strconv.FormatInt(kept2, 10)+".json"] = []byte(`{}`)

	cfg := UsagePersistConfig{
		Enabled:          true,
		ObjectPathPrefix: "snaps/",
		RetentionDays:    30,
	}

	persister := NewSnapshotPersister(store, cfg, nil)
	ctx := context.Background()

	err := persister.CleanupExpired(ctx)
	if err != nil {
		t.Fatalf("CleanupExpired() error = %v, want nil", err)
	}

	if len(store.deleteCalls) != 2 {
		t.Fatalf("deleted %d snapshots, want 2 (expired)", len(store.deleteCalls))
	}

	// Verify the 2 recent snapshots still exist
	if _, ok := store.objects["snaps/usage-snapshot-"+strconv.FormatInt(kept1, 10)+".json"]; !ok {
		t.Error("snapshot 5 days ago should be kept but was deleted")
	}
	if _, ok := store.objects["snaps/usage-snapshot-"+strconv.FormatInt(kept2, 10)+".json"]; !ok {
		t.Error("snapshot 1 day ago should be kept but was deleted")
	}
}

func TestPersister_NilAggregator(t *testing.T) {
	store := newMockObjectStore()
	cfg := UsagePersistConfig{
		Enabled:          true,
		ObjectPathPrefix: "snaps/",
	}

	persister := NewSnapshotPersister(store, cfg, nil)
	ctx := context.Background()

	// PersistNow with nil aggregator should upload an empty snapshot without panicking
	err := persister.PersistNow(ctx)
	if err != nil {
		t.Fatalf("PersistNow() with nil aggregator: error = %v, want nil", err)
	}

	// RecoverLatest with nil aggregator should not panic and return nil (no store)
	persisterNoStore := NewSnapshotPersister(nil, cfg, nil)
	snapshot, err := persisterNoStore.RecoverLatest(ctx)
	if err != nil {
		t.Fatalf("RecoverLatest() with nil store: error = %v, want nil", err)
	}
	if snapshot != nil {
		t.Fatalf("RecoverLatest() with nil store: snapshot = %+v, want nil", snapshot)
	}
}

func TestStartStop_TimerBehavior(t *testing.T) {
	store := newMockObjectStore()
	cfg := UsagePersistConfig{
		Enabled:          true,
		IntervalSeconds:  1, // 1 second for fast test
		ObjectPathPrefix: "snaps/",
	}

	persister := NewSnapshotPersister(store, cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		persister.Start(ctx)
	}()

	time.Sleep(2500 * time.Millisecond)
	cancel()
	persister.Stop()
	wg.Wait() // ensure goroutine exits before test returns

	if store.putCalled < 1 {
		t.Fatalf("After 2.5s, putCalled = %d, want >= 1", store.putCalled)
	}
}

func TestStart_DisabledDoesNothing(t *testing.T) {
	store := newMockObjectStore()
	cfg := UsagePersistConfig{
		Enabled:          false,
		IntervalSeconds:  1,
		ObjectPathPrefix: "snaps/",
	}

	persister := NewSnapshotPersister(store, cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	persister.Start(ctx)
	time.Sleep(500 * time.Millisecond)
	persister.Stop()

	if store.putCalled > 0 {
		t.Fatalf("Disabled persister: putCalled = %d, want 0", store.putCalled)
	}
}

func TestExtractTimestamp(t *testing.T) {
	tests := []struct {
		key    string
		wantOK bool
	}{
		{"snaps/usage-snapshot-1717000000.json", true},
		{"snaps/usage-snapshot-0.json", true},
		{"snaps/usage-snapshot--1.json", false},
		{"snaps/usage-snapshot-abc.json", false},
		{"snaps/other-file-1717000000.json", false},
		{"", false},
	}

	for _, tt := range tests {
		ts, ok := extractTimestamp(tt.key)
		if ok != tt.wantOK {
			t.Errorf("extractTimestamp(%q) ok = %v, want %v", tt.key, ok, tt.wantOK)
		}
		if ok && ts == 0 && tt.key != "snaps/usage-snapshot-0.json" {
			// only ts=0 is valid for key ending in -0.json
		}
	}
}
