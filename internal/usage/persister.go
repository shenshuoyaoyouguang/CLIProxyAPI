package usage

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/store"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
	log "github.com/sirupsen/logrus"
)

type UsagePersistConfig = sdkconfig.UsagePersistConfig

func DefaultUsagePersistConfig() UsagePersistConfig {
	return sdkconfig.UsagePersistConfig{
		Enabled:          false,
		IntervalSeconds:  300,
		RetentionDays:    30,
		ObjectPathPrefix: "usage-snapshots/",
	}
}

// SnapshotPersister periodically snapshots usage statistics and persists them to object storage.
type SnapshotPersister struct {
	cfg        UsagePersistConfig
	store      store.ObjectStorePersistence
	aggregator *Aggregator
	stopCh     chan struct{}
	stopOnce   sync.Once
}

// NewSnapshotPersister creates a new persister. The store parameter must implement
// the ObjectStorePersistence interface. If aggregator is nil, PersistNow and
// RecoverLatest return nil without error.
func NewSnapshotPersister(store store.ObjectStorePersistence, cfg UsagePersistConfig, aggregator *Aggregator) *SnapshotPersister {
	if cfg.IntervalSeconds <= 0 {
		cfg.IntervalSeconds = 300
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 30
	}
	if cfg.ObjectPathPrefix == "" {
		cfg.ObjectPathPrefix = "usage-snapshots/"
	}
	return &SnapshotPersister{
		cfg:        cfg,
		store:      store,
		aggregator: aggregator,
		stopCh:     make(chan struct{}),
	}
}

// Start launches the background persistence loop.
// It persists snapshots at the configured interval and cleans up expired snapshots.
func (p *SnapshotPersister) Start(ctx context.Context) {
	if !p.cfg.Enabled {
		return
	}
	if p.store == nil {
		log.Warn("usage: persister: object store not configured, skipping persistence")
		return
	}
	interval := time.Duration(p.cfg.IntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Infof("usage: persister started (interval=%s, retention=%d days)", interval, p.cfg.RetentionDays)

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.snapshotAndCleanup(ctx)
		}
	}
}

// Stop signals the persister to stop. It is safe to call multiple times.
func (p *SnapshotPersister) Stop() {
	p.stopOnce.Do(func() {
		close(p.stopCh)
	})
}

// PersistNow triggers an immediate snapshot write to object storage.
// It is safe to call even when the persister is disabled.
func (p *SnapshotPersister) PersistNow(ctx context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}
	return p.persistSnapshot(ctx)
}

// RecoverLatest downloads and returns the most recent snapshot from object storage,
// or nil if no snapshot exists. All snapshots are attempted in descending timestamp
// order; the first successfully parsed one is returned.
func (p *SnapshotPersister) RecoverLatest(ctx context.Context) (*UsageSnapshot, error) {
	if !p.cfg.Enabled {
		return nil, nil
	}
	return p.loadLatestSnapshot(ctx)
}

// CleanupExpired deletes snapshot objects older than RetentionDays.
func (p *SnapshotPersister) CleanupExpired(ctx context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}
	return p.cleanupExpiredImpl(ctx)
}

func (p *SnapshotPersister) snapshotAndCleanup(ctx context.Context) {
	if err := p.persistSnapshot(ctx); err != nil {
		log.Errorf("usage: persister: failed to persist snapshot: %v", err)
	}
	if err := p.cleanupExpiredImpl(ctx); err != nil {
		log.Errorf("usage: persister: failed to cleanup expired snapshots: %v", err)
	}
}

func (p *SnapshotPersister) persistSnapshot(ctx context.Context) error {
	if p.store == nil {
		return nil
	}

	var snapshot UsageSnapshot
	if p.aggregator != nil {
		snapshot = *p.aggregator.GetSnapshot()
	} else {
		snapshot = UsageSnapshot{APIs: make(map[string]APIEntry)}
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}

	key := p.snapshotKey(time.Now())
	err = p.store.PutObject(ctx, key, data)
	if err != nil {
		return err
	}

	log.Debugf("usage: persister: snapshot saved to %s", key)
	return nil
}

func (p *SnapshotPersister) loadLatestSnapshot(ctx context.Context) (*UsageSnapshot, error) {
	if p.store == nil {
		return nil, nil
	}

	prefix := p.cfg.ObjectPathPrefix + snapshotFilenamePrefix
	keys, err := p.store.ListObjects(ctx, prefix)
	if err != nil {
		return nil, err
	}

	sortKeysByTimestampDesc(keys)

	for _, key := range keys {
		data, err := p.store.GetObject(ctx, key)
		if err != nil {
			log.Debugf("usage: persister: failed to get %s: %v", key, err)
			continue
		}
		var snapshot UsageSnapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			log.Warnf("usage: persister: failed to parse snapshot %s: %v", key, err)
			continue
		}
		log.Infof("usage: persister: recovered snapshot from %s", key)
		return &snapshot, nil
	}

	return nil, nil
}

func (p *SnapshotPersister) cleanupExpiredImpl(ctx context.Context) error {
	if p.store == nil {
		return nil
	}

	prefix := p.cfg.ObjectPathPrefix + snapshotFilenamePrefix
	keys, err := p.store.ListObjects(ctx, prefix)
	if err != nil {
		return err
	}

	cutoff := time.Now().AddDate(0, 0, -p.cfg.RetentionDays).Unix()

	var deleted int
	for _, key := range keys {
		ts, ok := extractTimestamp(key)
		if !ok {
			continue
		}
		if ts < cutoff {
			if err := p.store.DeleteObject(ctx, key); err != nil {
				log.Errorf("usage: persister: failed to delete %s: %v", key, err)
			} else {
				deleted++
			}
		}
	}

	if deleted > 0 {
		log.Infof("usage: persister: cleaned up %d expired snapshots", deleted)
	}
	return nil
}

const (
	snapshotFilenamePrefix = "usage-snapshot-"
	snapshotFilenameSuffix = ".json"
)

func (p *SnapshotPersister) snapshotKey(t time.Time) string {
	prefix := p.cfg.ObjectPathPrefix
	if !strings.HasSuffix(prefix, "/") && prefix != "" {
		prefix += "/"
	}
	return prefix + snapshotFilenamePrefix + strconv.FormatInt(t.Unix(), 10) + snapshotFilenameSuffix
}

func sortKeysByTimestampDesc(keys []string) {
	sort.Slice(keys, func(i, j int) bool {
		ti, ok1 := extractTimestamp(keys[i])
		tj, ok2 := extractTimestamp(keys[j])
		if !ok1 && !ok2 {
			return false
		}
		if !ok1 {
			return false
		}
		if !ok2 {
			return true
		}
		return ti > tj
	})
}

func extractTimestamp(key string) (int64, bool) {
	idx := strings.LastIndex(key, snapshotFilenamePrefix)
	if idx < 0 {
		return 0, false
	}
	rest := key[idx+len(snapshotFilenamePrefix):]
	if !strings.HasSuffix(rest, snapshotFilenameSuffix) {
		return 0, false
	}
	tsStr := strings.TrimSuffix(rest, snapshotFilenameSuffix)
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil || ts < 0 {
		return 0, false
	}
	return ts, true
}
