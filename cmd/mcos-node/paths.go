package main

import (
	"os"
	"path/filepath"
	"runtime"
)

// Bu dosya, düğüm uygulamasının verisini NEREYE koyacağını belirler.
//
// ── Neden MCOS'takinden farklı ──────────────────────────────────────────────
// MCOS işletim sisteminde her şey /var/lib/mcos altındadır, çünkü orası bize
// ait bir makinedir. Burada ise kullanıcının KENDİ bilgisayarındayız: Windows
// ya da Linux, üzerinde başka işler de yapılıyor. O yüzden her platformun
// kendi alışılmış yerini kullanıyoruz ve kullanıcının belgelerine, masaüstüne
// hiçbir şey bırakmıyoruz.

// defaultDataRoot returns the per-user directory for this node's state.
func defaultDataRoot() string {
	if env := os.Getenv("MCOS_NODE_DATA"); env != "" {
		return env
	}

	switch runtime.GOOS {
	case "windows":
		// %LOCALAPPDATA%: dolaşan profile (roaming) DAHİL DEĞİL. Sunucu
		// dünyaları gigabaytlarca olabilir; onları ağ üzerinden senkronlanan
		// bir klasöre koymak, kullanıcının oturum açmasını dakikalarca
		// uzatırdı.
		if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
			return filepath.Join(dir, "MCOS-Node")
		}
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", "MCOS-Node")
		}
	default: // linux ve diğerleri
		if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
			return filepath.Join(dir, "mcos-node")
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".local", "share", "mcos-node")
		}
	}

	// Son çare: çalışma klasörü. Hiçbir yere yazamamaktansa buraya yazmak
	// yeğdir; kullanıcı programı taşıdığında verisi de taşınır.
	return filepath.Join(".", "mcos-node-data")
}

// exeDir returns the directory the program was launched from.
//
// Mod dosyası ve başlatıcı betikler ikilinin YANINDA durur; kullanıcı klasörü
// olduğu gibi kopyalayıp başka bir makinede çalıştırabilmeli.
func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}
