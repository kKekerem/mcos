// Package files provides safe file operations inside a server's data directory.
// Every path is validated so it cannot escape the server root via ".." or
// absolute paths.
package files

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mcos/internal/log"
	"mcos/internal/model"
	"mcos/internal/store"
)

// Manager owns file operations for all servers.
type Manager struct {
	store *store.Store
	log   *log.Logger
}

// NewManager constructs the file manager.
func NewManager(st *store.Store, lg *log.Logger) *Manager {
	return &Manager{store: st, log: lg}
}

// resolve returns the absolute path inside the server's data directory,
// rejecting traversal outside the root.
func (m *Manager) resolve(serverID, rel string) (string, error) {
	root := m.store.Paths.ServerData(serverID)
	clean := filepath.Clean(filepath.Join(root, rel))
	if !strings.HasPrefix(clean+string(filepath.Separator), root+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes server root")
	}
	return clean, nil
}

// List returns directory entries for a path inside a server's data tree.
func (m *Manager) List(serverID, rel string) ([]model.FileEntry, error) {
	path, err := m.resolve(serverID, rel)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var out []model.FileEntry
	for _, e := range entries {
		info, _ := e.Info()
		fe := model.FileEntry{
			Name:  e.Name(),
			Path:  filepath.ToSlash(filepath.Join(rel, e.Name())),
			IsDir: e.IsDir(),
		}
		if info != nil {
			fe.Size = info.Size()
			fe.ModTime = info.ModTime()
			fe.Mode = fmt.Sprintf("%04o", info.Mode().Perm())
		}
		out = append(out, fe)
	}
	return out, nil
}

// Read returns the base64-encoded contents of a file.
func (m *Manager) Read(serverID, rel string) (string, int64, error) {
	path, err := m.resolve(serverID, rel)
	if err != nil {
		return "", 0, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	if fi.IsDir() {
		return "", 0, fmt.Errorf("cannot read a directory")
	}
	if fi.Size() > 4<<20 { // 4 MiB limit for safety
		return "", 0, fmt.Errorf("file too large (>4MiB)")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	return base64.StdEncoding.EncodeToString(data), fi.Size(), nil
}

// Write decodes base64 content and writes it to a file inside the server tree.
func (m *Manager) Write(serverID, rel, contentBase64 string) error {
	path, err := m.resolve(serverID, rel)
	if err != nil {
		return err
	}
	data, err := base64.StdEncoding.DecodeString(contentBase64)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	if m.log != nil {
		m.log.Infof("files: wrote %s (%d bytes)", path, len(data))
	}
	return nil
}

// Delete removes a file or an empty directory inside the server tree.
func (m *Manager) Delete(serverID, rel string) error {
	path, err := m.resolve(serverID, rel)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return nil
}

// Size recursively calculates the size of a path inside the server tree.
func (m *Manager) Size(serverID, rel string) (int64, error) {
	path, err := m.resolve(serverID, rel)
	if err != nil {
		return 0, err
	}
	var total int64
	walk := func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		return nil
	}
	if err := filepath.Walk(path, walk); err != nil {
		return 0, err
	}
	return total, nil
}
