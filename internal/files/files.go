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

// ValidServerID reports whether id is safe to embed in a filesystem path.
//
// Sunucu kimlikleri daemon tarafından üretilir (srv_<onaltılık>), bu yüzden
// beyaz liste uygulanır: harf, rakam, "_" ve "-". Ayırıcı veya ".." içeren bir
// kimlik yazım hatası değil, saldırı girdisidir.
//
// Bu kontrol OLMADAN serverID="../../.." verildiğinde Paths.ServerData() veri
// kökünün dışına çıkıyor ve resolve()'un ön ek kontrolü AYNI kaçmış kökle
// yapıldığı için kontrol geçiyordu.
func ValidServerID(id string) error {
	if id == "" {
		return fmt.Errorf("sunucu kimliği boş")
	}
	if len(id) > 64 {
		return fmt.Errorf("sunucu kimliği çok uzun")
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return fmt.Errorf("sunucu kimliğinde geçersiz karakter: %q", r)
		}
	}
	return nil
}

// withinPath reports whether p is root itself or lives under it.
func withinPath(root, p string) bool {
	if p == root {
		return true
	}
	return strings.HasPrefix(p, root+string(filepath.Separator))
}

// resolve returns the absolute path inside the server's data directory,
// rejecting traversal outside the root — both syntactically (".." segments,
// absolute rel) and through symlinks that point out of the tree.
func (m *Manager) resolve(serverID, rel string) (string, error) {
	if err := ValidServerID(serverID); err != nil {
		return "", err
	}
	root, err := filepath.Abs(m.store.Paths.ServerData(serverID))
	if err != nil {
		return "", fmt.Errorf("sunucu klasörü çözümlenemedi: %w", err)
	}
	clean := filepath.Clean(filepath.Join(root, rel))
	if !withinPath(root, clean) {
		return "", fmt.Errorf("yol sunucu klasörünün dışına çıkıyor")
	}
	if err := m.checkSymlinkEscape(root, clean); err != nil {
		return "", err
	}
	return clean, nil
}

// checkSymlinkEscape verifies that following symlinks does not lead outside
// root. Yalnızca sözdizimsel ön ek kontrolü yeterli değildi: sunucu, veri
// klasörünün içine "/" gösteren bir symlink bırakırsa files.write ile tüm
// dosya sistemine yazılabiliyordu.
//
// Write yeni dosya oluşturabilmeli, bu yüzden yolun VAR OLAN en derin atası
// çözülür; henüz var olmayan son bileşenler olduğu gibi eklenir.
func (m *Manager) checkSymlinkEscape(root, p string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = root // kök henüz oluşturulmadı
	}

	cur := p
	var missing []string
	for {
		if _, err := os.Lstat(cur); err == nil {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		missing = append([]string{filepath.Base(cur)}, missing...)
		cur = parent
	}

	realCur, err := filepath.EvalSymlinks(cur)
	if err != nil {
		realCur = cur
	}
	full := filepath.Join(append([]string{realCur}, missing...)...)
	if !withinPath(realRoot, full) {
		return fmt.Errorf("yol bir sembolik bağ üzerinden sunucu klasörünün dışına çıkıyor")
	}
	return nil
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
