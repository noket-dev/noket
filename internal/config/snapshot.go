package config

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/noket-dev/noket/internal/types"
)

// Snapshot is an immutable view of the config used by request routing and by
// GetNodeUpstreams.
type Snapshot struct {
	Config     *types.Config
	Upstreams  []types.Upstream
	CapturedAt time.Time

	byModel map[string]*types.Upstream
	byName  map[string]*types.Upstream
	modTime time.Time
	size    int64
}

func newSnapshot(cfg *types.Config, capturedAt time.Time, modTime time.Time, size int64) *Snapshot {
	s := &Snapshot{
		Config:     cfg,
		Upstreams:  cfg.Upstreams,
		CapturedAt: capturedAt,
		byModel:    make(map[string]*types.Upstream),
		byName:     make(map[string]*types.Upstream),
		modTime:    modTime,
		size:       size,
	}
	for i := range s.Upstreams {
		up := &s.Upstreams[i]
		s.byName[up.Name] = up
		for _, model := range up.Models {
			s.byModel[model] = up
		}
	}
	return s
}

// UpstreamByModel returns the single upstream exposing model id.
func (s *Snapshot) UpstreamByModel(id string) (*types.Upstream, bool) {
	up, ok := s.byModel[id]
	return up, ok
}

func (s *Snapshot) UpstreamByName(name string) (*types.Upstream, bool) {
	up, ok := s.byName[name]
	return up, ok
}

// Store owns the live config snapshot. All writes go through Mutate so they
// are serialized, validated and atomically replaced.
type Store struct {
	mu   sync.Mutex
	path string
	snap *Snapshot
}

// NewStore loads the config at path and returns a ready store.
func NewStore(path string) (*Store, error) {
	store := &Store{path: path}
	snap, err := loadSnapshot(path)
	if err != nil {
		return nil, err
	}
	store.snap = snap
	return store, nil
}

func loadSnapshot(path string) (*Snapshot, error) {
	cfg, err := LoadFile(path)
	if err != nil {
		return nil, err
	}
	info, statErr := os.Stat(path)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("stat %s: %w", path, statErr)
	}
	var modTime time.Time
	var size int64
	if statErr == nil {
		modTime = info.ModTime()
		size = info.Size()
	}
	return newSnapshot(cfg, time.Now().UTC(), modTime, size), nil
}

func (s *Store) Path() string { return s.path }

// Snapshot returns the current immutable snapshot.
func (s *Store) Snapshot() *Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap
}

// ReloadIfChanged reloads config.toml when its mtime or size changed. On a
// validation or parse error the previous snapshot is kept and the error is
// returned; callers should log it and continue serving the old config.
func (s *Store) ReloadIfChanged() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, err := os.Stat(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", s.path, err)
	}
	if info.ModTime().Equal(s.snap.modTime) && info.Size() == s.snap.size {
		return false, nil
	}
	next, err := loadSnapshot(s.path)
	if err != nil {
		return false, err
	}
	s.snap = next
	return true, nil
}

// Mutate applies fn to the latest config from disk, validates it, writes it
// atomically and replaces the snapshot. The mutation is serialized inside the
// noketd process; the atomic rename keeps the CLI and noketd from observing
// a partially written file.
func (s *Store) Mutate(fn func(cfg *types.Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := LoadFile(s.path)
	if err != nil {
		return err
	}
	if err := fn(cfg); err != nil {
		return err
	}
	ApplyDefaults(cfg)
	Normalize(cfg)
	if err := Validate(cfg); err != nil {
		return err
	}
	if err := SaveFile(s.path, cfg); err != nil {
		return err
	}
	info, err := os.Stat(s.path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", s.path, err)
	}
	s.snap = newSnapshot(cfg, time.Now().UTC(), info.ModTime(), info.Size())
	return nil
}
