// Package worlds manages Minecraft world directories inside a server's data
// tree. A "world" is any subdirectory containing a level.dat file.
package worlds

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mcos/internal/log"
	"mcos/internal/model"
	"mcos/internal/store"
)

// Manager owns world operations for all servers.
type Manager struct {
	store *store.Store
	log   *log.Logger
}

// NewManager constructs the worlds manager.
func NewManager(st *store.Store, lg *log.Logger) *Manager {
	return &Manager{store: st, log: lg}
}

func (m *Manager) dataDir(id string) string {
	return m.store.Paths.ServerData(id)
}

// List returns every directory under the server data tree that contains a
// level.dat file.
func (m *Manager) List(serverID string) ([]model.World, error) {
	root := m.dataDir(serverID)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []model.World
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		wpath := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(wpath, "level.dat")); err != nil {
			continue
		}
		size := dirSize(wpath)
		out = append(out, model.World{Name: e.Name(), Path: e.Name(), SizeBytes: size})
	}
	return out, nil
}

// Rename renames a world directory inside the server data tree.
func (m *Manager) Rename(serverID, oldName, newName string) error {
	root := m.dataDir(serverID)
	src := filepath.Join(root, oldName)
	dst := filepath.Join(root, newName)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("world not found: %w", err)
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("destination already exists")
	}
	if strings.Contains(newName, "..") || filepath.IsAbs(newName) {
		return fmt.Errorf("invalid name")
	}
	return os.Rename(src, dst)
}

// Delete removes a world directory.
func (m *Manager) Delete(serverID, name string) error {
	root := m.dataDir(serverID)
	path := filepath.Join(root, name)
	if _, err := os.Stat(filepath.Join(path, "level.dat")); err != nil {
		return fmt.Errorf("not a recognised world")
	}
	return os.RemoveAll(path)
}

func dirSize(path string) int64 {
	var total int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}
