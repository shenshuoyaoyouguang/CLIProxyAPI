package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

// CooldownStateRecord is a persisted runtime cooldown snapshot for one auth/model pair.
type CooldownStateRecord struct {
	Provider       string     `json:"provider,omitempty"`
	AuthID         string     `json:"auth_id"`
	AuthFile       string     `json:"-"`
	Model          string     `json:"model,omitempty"`
	Status         string     `json:"status,omitempty"`
	NextRetryAfter time.Time  `json:"next_retry_after"`
	Reason         string     `json:"reason,omitempty"`
	Quota          QuotaState `json:"quota,omitempty"`
	LastError      *Error     `json:"last_error,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// CooldownStateStore persists runtime cooldown state independently from auth tokens.
type CooldownStateStore interface {
	Load(context.Context) ([]CooldownStateRecord, error)
	Save(context.Context, []CooldownStateRecord) error
}

// CooldownStateStoreProvider exposes a backend-specific cooldown state store.
type CooldownStateStoreProvider interface {
	CooldownStateStore() CooldownStateStore
}

type cooldownStateFile struct {
	Version   int                   `json:"version"`
	UpdatedAt time.Time             `json:"updated_at"`
	Records   []CooldownStateRecord `json:"records"`
}

// cooldownStateFileName is the single shared file holding every cooldown record.
//
// The .cds extension is deliberate: the auth loaders treat every *.json file
// under the auth directory as a credential, so a .json name here would
// register a phantom auth with an empty type.
const cooldownStateFileName = "cooldown-state.cds"

// FileCooldownStateStore stores cooldown state in a single shared file.
type FileCooldownStateStore struct {
	mu   sync.Mutex
	dir  string
	path string
	// legacyCleaned guards the one-time removal of per-auth .cds files left by
	// the previous layout. It uses CAS rather than sync.Once so the cleanup —
	// which walks the whole auth directory and hits disk — never runs while mu
	// is held, and never blocks concurrent writers.
	legacyCleaned atomic.Bool
}

// NewFileCooldownStateStoreWithAuthDir creates a store rooted at dir.
//
// authDir is retained for API compatibility only. Cooldown state lives in one
// shared file and no longer derives per-auth paths from credential files.
func NewFileCooldownStateStoreWithAuthDir(dir, authDir string) *FileCooldownStateStore {
	cleaned := strings.TrimSpace(dir)
	return &FileCooldownStateStore{
		dir:  cleaned,
		path: filepath.Join(cleaned, cooldownStateFileName),
	}
}

// Load reads the shared cooldown state file. A missing file is treated as empty state.
func (s *FileCooldownStateStore) Load(ctx context.Context) ([]CooldownStateRecord, error) {
	if s == nil || s.dir == "" {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if errCtx := ctx.Err(); errCtx != nil {
		return nil, errCtx
	}
	data, errRead := os.ReadFile(s.path)
	if errRead != nil {
		if errors.Is(errRead, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read cooldown state %s: %w", s.path, errRead)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var envelope cooldownStateFile
	if errUnmarshal := json.Unmarshal(data, &envelope); errUnmarshal != nil {
		return nil, fmt.Errorf("parse cooldown state %s: %w", s.path, errUnmarshal)
	}
	return envelope.Records, nil
}

// Save atomically replaces the shared cooldown state file, or removes it when
// no records remain.
func (s *FileCooldownStateStore) Save(ctx context.Context, records []CooldownStateRecord) error {
	if s == nil || s.dir == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if errCtx := ctx.Err(); errCtx != nil {
		return errCtx
	}

	// The previous layout wrote one .cds file per auth. Those files are no
	// longer read, so drop them the first time we write the shared file. This
	// walks the directory, so it runs before taking mu.
	if s.legacyCleaned.CompareAndSwap(false, true) {
		s.removeLegacyStateFiles()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	kept := make([]CooldownStateRecord, 0, len(records))
	for _, record := range records {
		if strings.TrimSpace(record.AuthID) == "" {
			continue
		}
		kept = append(kept, record)
	}
	if len(kept) == 0 {
		if errRemove := os.Remove(s.path); errRemove != nil && !errors.Is(errRemove, os.ErrNotExist) {
			return fmt.Errorf("remove cooldown state %s: %w", s.path, errRemove)
		}
		return nil
	}

	sort.Slice(kept, func(i, j int) bool { return kept[i].Model < kept[j].Model })
	envelope := cooldownStateFile{
		Version:   1,
		UpdatedAt: time.Now().UTC(),
		Records:   kept,
	}
	data, errMarshal := json.MarshalIndent(envelope, "", "  ")
	if errMarshal != nil {
		return fmt.Errorf("marshal cooldown state: %w", errMarshal)
	}
	data = append(data, '\n')
	return s.writeStateFile(data)
}

// writeStateFile writes data through a temp file plus rename so a concurrent
// reader never observes a partially written state file.
func (s *FileCooldownStateStore) writeStateFile(data []byte) error {
	if errMkdir := os.MkdirAll(s.dir, 0o700); errMkdir != nil {
		return fmt.Errorf("create cooldown state directory: %w", errMkdir)
	}
	tmpFile, errCreate := os.CreateTemp(s.dir, cooldownStateFileName+".*.tmp")
	if errCreate != nil {
		return fmt.Errorf("create cooldown state temp file: %w", errCreate)
	}
	tmp := tmpFile.Name()
	if _, errWrite := tmpFile.Write(data); errWrite != nil {
		if errClose := tmpFile.Close(); errClose != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("write cooldown state temp file: %w; close temp file: %v", errWrite, errClose)
		}
		_ = os.Remove(tmp)
		return fmt.Errorf("write cooldown state temp file: %w", errWrite)
	}
	if errClose := tmpFile.Close(); errClose != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close cooldown state temp file: %w", errClose)
	}
	if errRename := os.Rename(tmp, s.path); errRename != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace cooldown state file: %w", errRename)
	}
	return nil
}

// removeLegacyStateFiles deletes the per-auth .cds files written by the
// previous on-disk layout, leaving the shared file untouched.
func (s *FileCooldownStateStore) removeLegacyStateFiles() {
	shared := filepath.Clean(s.path)
	errWalk := filepath.WalkDir(s.dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry == nil || entry.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".cds") {
			return nil
		}
		if filepath.Clean(path) == shared {
			return nil
		}
		if errRemove := os.Remove(path); errRemove != nil && !errors.Is(errRemove, os.ErrNotExist) {
			log.Warnf("remove legacy cooldown state %s: %v", path, errRemove)
		}
		return nil
	})
	if errWalk != nil && !errors.Is(errWalk, os.ErrNotExist) {
		log.Warnf("clean legacy cooldown state directory %s: %v", s.dir, errWalk)
	}
}

func cooldownAuthFile(auth *Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Attributes != nil {
		if path := strings.TrimSpace(auth.Attributes["path"]); path != "" {
			return path
		}
	}
	if fileName := strings.TrimSpace(auth.FileName); fileName != "" {
		return fileName
	}
	return ""
}
