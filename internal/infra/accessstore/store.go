// Package accessstore is a JSON-file-backed implementation of
// domain.AccessStore. The project has no database, so persistence is a single
// JSON file kept in sync with an in-memory map under an RWMutex. Writes are
// atomic (temp file + rename) so a crash mid-write can never leave corrupt JSON
// on disk.
package accessstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"admin-bot/internal/domain"
)

// Store is a concurrency-safe, file-backed AccessStore.
type Store struct {
	path string

	mu   sync.RWMutex
	reqs map[int64]domain.AccessRequest
}

// New creates a store bound to path and loads any existing data. If the file
// does not exist an empty store is created (and its parent directory made). A
// load error is returned rather than swallowed so callers can fail closed
// instead of silently starting with an empty allowlist.
func New(path string) (*Store, error) {
	s := &Store{
		path: path,
		reqs: make(map[int64]domain.AccessRequest),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// load reads the backing file into memory. A missing file is not an error: it
// means a fresh install, so we ensure the parent directory exists and keep the
// empty map.
func (s *Store) load() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create access store dir: %w", err)
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read access store %q: %w", s.path, err)
	}
	if len(data) == 0 {
		return nil
	}

	var reqs []domain.AccessRequest
	if err := json.Unmarshal(data, &reqs); err != nil {
		return fmt.Errorf("parse access store %q: %w", s.path, err)
	}

	s.reqs = make(map[int64]domain.AccessRequest, len(reqs))
	for _, r := range reqs {
		s.reqs[r.UserID] = r
	}
	return nil
}

// Get returns the stored request for userID and whether it exists.
func (s *Store) Get(userID int64) (domain.AccessRequest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.reqs[userID]
	return r, ok
}

// Save upserts req in memory and rewrites the file atomically. The in-memory
// map is updated only after the durable write succeeds, so a failed write
// leaves memory and disk consistent.
func (s *Store) Save(req domain.AccessRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Snapshot including the new record, then persist before mutating the map.
	next := make(map[int64]domain.AccessRequest, len(s.reqs)+1)
	for k, v := range s.reqs {
		next[k] = v
	}
	next[req.UserID] = req

	if err := s.persist(next); err != nil {
		return err
	}
	s.reqs = next
	return nil
}

// ListPending returns pending requests ordered by RequestedAt (oldest first).
func (s *Store) ListPending() ([]domain.AccessRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var pending []domain.AccessRequest
	for _, r := range s.reqs {
		if r.Status == domain.AccessPending {
			pending = append(pending, r)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].RequestedAt.Before(pending[j].RequestedAt)
	})
	return pending, nil
}

// persist writes the given snapshot to a temp file in the same directory and
// atomically renames it over the target, so readers never observe a partial
// write. Records are sorted by UserID for a stable, diff-friendly file.
func (s *Store) persist(reqs map[int64]domain.AccessRequest) error {
	list := make([]domain.AccessRequest, 0, len(reqs))
	for _, r := range reqs {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].UserID < list[j].UserID })

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal access store: %w", err)
	}

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".access-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp access store: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we bail out before the rename succeeds.
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp access store: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp access store: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp access store: %w", err)
	}

	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("rename access store into place: %w", err)
	}
	return nil
}
