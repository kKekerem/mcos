// Package store provides JSON file persistence for all daemon state. Writes are
// atomic (temp file + rename) and serialized through an in-process lock so the
// single daemon never corrupts a file on crash or concurrent access.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("store: not found")

// Store owns the data root and serializes access to it.
type Store struct {
	Paths Paths
	mu    sync.RWMutex
}

// New opens (and creates if needed) a store rooted at dataRoot.
func New(dataRoot string) (*Store, error) {
	s := &Store{Paths: Paths{Root: dataRoot}}
	for _, dir := range []string{
		s.Paths.Root, s.Paths.Servers(), s.Paths.JavaDir(),
		s.Paths.ClusterDir(), s.Paths.TunnelsDir(), s.Paths.LogDir(),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("store: mkdir %s: %w", dir, err)
		}
	}
	return s, nil
}

// readJSON loads and decodes a JSON file into v. Returns ErrNotFound if absent.
func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("store: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("store: decode %s: %w", path, err)
	}
	return nil
}

// writeJSON atomically encodes v to path: it writes a sibling temp file, fsyncs
// it, then renames over the target so a reader never observes a partial file.
func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("store: mkdir for %s: %w", path, err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("store: encode %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("store: tempfile for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("store: write %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("store: sync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("store: close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("store: rename %s -> %s: %w", tmpName, path, err)
	}
	// ── Düzeltilen gerçek hata: DİZİN fsync'lenmiyordu ──────────────────────
	//
	// İçerik fsync'leniyor (yukarıda tmp.Sync) ve rename atomik — ama rename'in
	// KENDİSİ dosya sistemi günlüğüne yazılana kadar kalıcı değildir. Elektrik
	// kesilirse (ya da kullanıcı güç düğmesini basılı tutarsa) dizin girdisi
	// eski hâlinde kalabilir; ext4'ün varsayılan data=ordered kipinde en kötü
	// sonuç, config.json'ın SIFIR BAYT ya da hiç olmaması.
	//
	// Bu, bir sunucu appliance'ında sıradan bir senaryodur: makine kapanmak
	// yerine fişten çekilir. Kaybedilen dosya ise kullanıcının Wi-Fi parolası,
	// teması, düğüm kimliği ve sunucu kayıt defteri.
	//
	// Dizini açıp fsync etmek bunu kapatır. Hata YUTULUYOR: bazı dosya
	// sistemleri (ör. bazı FAT sürücüleri) dizin fsync'ini desteklemez ve
	// orada başarısız olması yazmayı geçersiz kılmaz.
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}
