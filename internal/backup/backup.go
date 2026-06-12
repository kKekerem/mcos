// Package backup manages server snapshots as ZIP archives stored under each
// server's backups/ directory. A small JSON manifest inside the ZIP records
// metadata (id, name, description, createdAt, size) so listing does not need
// to decompress anything.
package backup

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mcos/internal/log"
	"mcos/internal/model"
	"mcos/internal/server"
	"mcos/internal/store"
)

// Manager coordinates backup creation, listing, restore, and deletion.
type Manager struct {
	store   *store.Store
	servers *server.Manager
	log     *log.Logger
}

// NewManager constructs a backup manager tied to the daemon's store and server
// lifecycle manager.
func NewManager(st *store.Store, sm *server.Manager, lg *log.Logger) *Manager {
	return &Manager{store: st, servers: sm, log: lg}
}

const manifestName = "backup.json"

// List returns all backups for a server, ordered newest-first.
func (m *Manager) List(serverID string) ([]model.Backup, error) {
	dir := m.store.Paths.ServerBackups(serverID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []model.Backup
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		b, err := m.readManifest(filepath.Join(dir, e.Name()))
		if err != nil {
			continue // skip corrupted archives
		}
		out = append(out, b)
	}
	// newest first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// Create archives the server's data directory (or only worlds) into a new ZIP
// backup. If the server is running, a best-effort "save-all" is issued first.
func (m *Manager) Create(serverID, name, description string, worldOnly bool) (model.Backup, error) {
	var zero model.Backup
	if name == "" {
		name = time.Now().Format("2006-01-02_15-04-05")
	}
	if err := m.servers.Command(serverID, "save-all"); err != nil {
		// ignore — server may be stopped
	}
	// brief pause so the save finishes before we read files
	time.Sleep(500 * time.Millisecond)

	id := generateID()
	backupPath := filepath.Join(m.store.Paths.ServerBackups(serverID), id+".zip")
	dataDir := m.store.Paths.ServerData(serverID)

	if err := os.MkdirAll(m.store.Paths.ServerBackups(serverID), 0o755); err != nil {
		return zero, err
	}

	zipFile, err := os.Create(backupPath)
	if err != nil {
		return zero, err
	}
	defer zipFile.Close()

	zw := zip.NewWriter(zipFile)
	defer zw.Close()

	root := dataDir
	srcPrefix := dataDir
	if worldOnly {
		root = filepath.Join(dataDir, "world")
		srcPrefix = dataDir
	}

	var totalSize int64
	walkFn := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcPrefix, path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rc, err := os.Open(path)
		if err != nil {
			return err
		}
		defer rc.Close()
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		n, err := io.Copy(w, rc)
		if err != nil {
			return err
		}
		totalSize += n
		return nil
	}
	if err := filepath.Walk(root, walkFn); err != nil {
		return zero, fmt.Errorf("backup walk: %w", err)
	}

	b := model.Backup{
		ID:          id,
		ServerID:    serverID,
		Name:        name,
		Description: description,
		CreatedAt:   time.Now(),
		SizeBytes:   totalSize,
		Type:        "manual",
		WorldOnly:   worldOnly,
	}

	// embed manifest inside the ZIP
	mf, err := zw.Create(manifestName)
	if err != nil {
		return zero, err
	}
	if err := json.NewEncoder(mf).Encode(b); err != nil {
		return zero, err
	}
	zw.Close()
	zipFile.Close()

	// update size to actual archive size
	if st, err := os.Stat(backupPath); err == nil {
		b.SizeBytes = st.Size()
	}

	if m.log != nil {
		m.log.Infof("backup: created %s (%s, %d bytes)", b.ID, b.Name, b.SizeBytes)
	}
	return b, nil
}

// Restore extracts a backup ZIP over the server's data directory. The server
// must be stopped first.
func (m *Manager) Restore(serverID, backupID string) error {
	if s := m.servers.State(serverID); s == model.StateRunning || s == model.StateStarting {
		return fmt.Errorf("stop the server before restoring")
	}
	backupPath := filepath.Join(m.store.Paths.ServerBackups(serverID), backupID+".zip")
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("backup not found: %w", err)
	}
	dataDir := m.store.Paths.ServerData(serverID)

	// preserve current data as a pre-restore snapshot
	snapshotDir := dataDir + "-pre-restore-" + time.Now().Format("20060102-150405")
	if err := os.Rename(dataDir, snapshotDir); err != nil {
		return fmt.Errorf("rename pre-restore: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}

	zr, err := zip.OpenReader(backupPath)
	if err != nil {
		// roll back
		os.RemoveAll(dataDir)
		os.Rename(snapshotDir, dataDir)
		return fmt.Errorf("open backup: %w", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name == manifestName || strings.HasPrefix(f.Name, ".") {
			continue
		}
		dest := filepath.Join(dataDir, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(dest)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}

	if m.log != nil {
		m.log.Infof("backup: restored %s into %s", backupID, serverID)
	}
	return nil
}

// Delete removes a backup archive.
func (m *Manager) Delete(serverID, backupID string) error {
	path := filepath.Join(m.store.Paths.ServerBackups(serverID), backupID+".zip")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("backup not found")
		}
		return err
	}
	return nil
}

func (m *Manager) readManifest(zipPath string) (model.Backup, error) {
	var b model.Backup
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return b, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == manifestName {
			rc, err := f.Open()
			if err != nil {
				return b, err
			}
			defer rc.Close()
			if err := json.NewDecoder(rc).Decode(&b); err != nil {
				return b, err
			}
			// ensure path-derived fields are current
			if b.ID == "" {
				b.ID = strings.TrimSuffix(filepath.Base(zipPath), ".zip")
			}
			return b, nil
		}
	}
	return b, fmt.Errorf("manifest not found")
}

func generateID() string {
	b := make([]byte, 4)
	for i := range b {
		b[i] = byte('a' + time.Now().UnixNano()%26)
	}
	return "bak_" + fmt.Sprintf("%x", time.Now().UnixNano())[:8]
}
