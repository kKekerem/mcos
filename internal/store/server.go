package store

import (
	"fmt"
	"os"
	"sort"
	"time"

	"mcos/internal/model"
)

// ListServers reads every server manifest under the servers directory. Runtime
// fields (state/PID/etc.) are not persisted and come back zero-valued.
func (s *Store) ListServers() ([]*model.Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(s.Paths.Servers())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: list servers: %w", err)
	}
	var out []*model.Server
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var srv model.Server
		if err := readJSON(s.Paths.ServerManifest(e.Name()), &srv); err != nil {
			if err == ErrNotFound {
				continue // directory without a manifest, skip
			}
			return nil, err
		}
		out = append(out, &srv)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// GetServer loads a single server manifest by id.
func (s *Store) GetServer(id string) (*model.Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var srv model.Server
	if err := readJSON(s.Paths.ServerManifest(id), &srv); err != nil {
		return nil, err
	}
	return &srv, nil
}

// SaveServer persists a server manifest, creating its directory tree and
// stamping UpdatedAt (and CreatedAt on first save).
func (s *Store) SaveServer(srv *model.Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if srv.CreatedAt.IsZero() {
		srv.CreatedAt = now
	}
	srv.UpdatedAt = now
	for _, dir := range []string{
		s.Paths.ServerData(srv.ID), s.Paths.ServerLogs(srv.ID), s.Paths.ServerBackups(srv.ID),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("store: mkdir %s: %w", dir, err)
		}
	}
	return writeJSON(s.Paths.ServerManifest(srv.ID), srv)
}

// DeleteServer removes a server's entire directory tree.
func (s *Store) DeleteServer(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.Paths.ServerDir(id)); os.IsNotExist(err) {
		return ErrNotFound
	}
	return os.RemoveAll(s.Paths.ServerDir(id))
}
