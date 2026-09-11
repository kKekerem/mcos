package files

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mcos/internal/store"
)

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	st, err := store.New(root)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	return NewManager(st, nil), root
}

func TestValidServerID(t *testing.T) {
	ok := []string{"srv_1a2b3c", "abc", "A-Z_0-9", strings.Repeat("a", 64)}
	for _, id := range ok {
		if err := ValidServerID(id); err != nil {
			t.Errorf("ValidServerID(%q) = %v, geçerli olmalı", id, err)
		}
	}

	bad := []string{
		"",                      // boş
		"..",                    // üst dizin
		"../..",                 // veri kökünden çıkış
		"a/b",                   // ayırıcı
		`a\b`,                   // Windows ayırıcı
		"srv id",                // boşluk
		"srv;rm",                // kabuk metakarakteri
		"../../../etc",          // klasik traversal
		strings.Repeat("a", 65), // çok uzun
		"srv\x00",               // NUL
	}
	for _, id := range bad {
		if err := ValidServerID(id); err == nil {
			t.Errorf("ValidServerID(%q) = nil, reddedilmeliydi", id)
		}
	}
}

// TestResolveRejectsEscape, D2 güvenlik açığının regresyon testidir: kayıtlı
// olmayan ve ".." içeren bir serverID veri kökünün dışına çıkabiliyordu.
func TestResolveRejectsEscape(t *testing.T) {
	m, _ := newTestManager(t)

	cases := []struct{ serverID, rel string }{
		{"../../..", "etc/passwd"},        // kimlik üzerinden kaçış
		{"..", "x"},                       // kimlik üzerinden kaçış
		{"../srv_other", "x"},             // komşu sunucuya sızma
		{"srv_ok", "../../../etc/passwd"}, // göreli yol üzerinden kaçış
		{"srv_ok", "a/../../../../tmp/x"}, // karışık
	}
	for _, c := range cases {
		if got, err := m.resolve(c.serverID, c.rel); err == nil {
			t.Errorf("resolve(%q, %q) = %q, hata beklenmişti", c.serverID, c.rel, got)
		}
	}
}

// TestResolveRerootsAbsolutePaths belgelendirir: MUTLAK bir göreli yol kaçış
// DEĞİLDİR. filepath.Join baştaki "/" işaretini temizler, yani "/etc/passwd"
// sunucu klasörünün altına köklenir. Bu güvenli ve kasıtlı davranıştır;
// önceden bunun bir açık olduğu sanılmıştı.
func TestResolveRerootsAbsolutePaths(t *testing.T) {
	m, _ := newTestManager(t)
	dataDir := m.store.Paths.ServerData("srv_ok")

	got, err := m.resolve("srv_ok", "/etc/passwd")
	if err != nil {
		t.Fatalf("resolve mutlak yolu reddetti: %v", err)
	}
	want := filepath.Join(dataDir, "etc", "passwd")
	if got != want {
		t.Errorf("resolve(/etc/passwd) = %q, beklenen %q", got, want)
	}
	if !strings.HasPrefix(got, dataDir) {
		t.Errorf("mutlak yol sunucu klasörünün dışına köklendi: %q", got)
	}
}

func TestResolveAllowsInsidePaths(t *testing.T) {
	m, root := newTestManager(t)
	dataDir := m.store.Paths.ServerData("srv_ok")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct{ rel, want string }{
		{"", dataDir},
		{".", dataDir},
		{"server.properties", filepath.Join(dataDir, "server.properties")},
		{"plugins/ViaVersion.jar", filepath.Join(dataDir, "plugins", "ViaVersion.jar")},
		{"world/../plugins/x.jar", filepath.Join(dataDir, "plugins", "x.jar")},
	}
	for _, c := range cases {
		got, err := m.resolve("srv_ok", c.rel)
		if err != nil {
			t.Errorf("resolve(%q) beklenmedik hata: %v", c.rel, err)
			continue
		}
		// t.TempDir() macOS'ta /var -> /private/var symlink'i verebilir; kökün
		// altında kalması yeterli.
		if !strings.HasPrefix(got, root) {
			t.Errorf("resolve(%q) = %q, %q altında olmalı", c.rel, got, root)
		}
		if got != c.want {
			t.Errorf("resolve(%q) = %q, beklenen %q", c.rel, got, c.want)
		}
	}
}

// TestResolveRejectsSymlinkEscape, sunucunun kendi veri klasörüne dışarıyı
// gösteren bir sembolik bağ bırakması durumunu kapsar. Yalnızca sözdizimsel
// ön ek kontrolü bunu yakalamıyordu.
func TestResolveRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows'ta symlink oluşturmak yönetici hakkı ister")
	}
	m, _ := newTestManager(t)
	dataDir := m.store.Paths.ServerData("srv_ok")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("gizli"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Veri klasörünün içinden dışarıya bağ.
	if err := os.Symlink(outside, filepath.Join(dataDir, "kacis")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Var olan dosyaya bağ üzerinden erişim reddedilmeli.
	if got, err := m.resolve("srv_ok", "kacis/secret"); err == nil {
		t.Errorf("resolve(kacis/secret) = %q, symlink kaçışı reddedilmeliydi", got)
	}
	// Henüz var olmayan dosya için de (Write yolu) reddedilmeli.
	if got, err := m.resolve("srv_ok", "kacis/yeni.txt"); err == nil {
		t.Errorf("resolve(kacis/yeni.txt) = %q, symlink kaçışı reddedilmeliydi", got)
	}
}

// TestWriteStaysInsideRoot, uçtan uca doğrulama: Write yalnızca sunucu
// klasörünün içine yazabilir.
func TestWriteStaysInsideRoot(t *testing.T) {
	m, root := newTestManager(t)
	if err := os.MkdirAll(m.store.Paths.ServerData("srv_ok"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Geçerli yazma: "merhaba" base64 = bWVyaGFiYQ==
	if err := m.Write("srv_ok", "plugins/not.txt", "bWVyaGFiYQ=="); err != nil {
		t.Fatalf("geçerli Write başarısız: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(m.store.Paths.ServerData("srv_ok"), "plugins", "not.txt"))
	if err != nil || string(b) != "merhaba" {
		t.Fatalf("yazılan içerik = %q, %v", b, err)
	}

	// Kaçış denemesi hiçbir dosya oluşturmamalı.
	if err := m.Write("srv_ok", "../../../kacti.txt", "bWVyaGFiYQ=="); err == nil {
		t.Error("Write kaçış yolunu kabul etti")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "kacti.txt")); err == nil {
		t.Error("kaçış dosyası veri kökünün dışında oluşturuldu")
	}
}
