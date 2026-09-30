package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

// Eklenti plugins/ altına kopyalanır, aynıysa yeniden yazılmaz; kaynak yoksa
// önceki kopya kullanılmaya devam eder; bozuk dosya eklenti sayılmaz.
func TestInstallPlugin(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(t.TempDir(), PluginJar)
	if ok, _ := InstallPlugin(dir, ""); ok {
		t.Fatal("hiç eklenti yokken true")
	}
	if err := os.WriteFile(src, []byte("PK\x03\x04bir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := InstallPlugin(dir, src); !ok || err != nil {
		t.Fatalf("kopyalanmadı: %v %v", ok, err)
	}
	dst := filepath.Join(dir, "plugins", PluginJar)
	st1, _ := os.Stat(dst)
	if ok, _ := InstallPlugin(dir, src); !ok {
		t.Fatal("ikinci tur false")
	}
	if st2, _ := os.Stat(dst); !os.SameFile(st1, st2) {
		t.Error("aynı jar yeniden yazıldı")
	}
	if ok, _ := InstallPlugin(dir, ""); !ok {
		t.Error("kaynak yokken önceki kopya sayılmadı")
	}
	bad := filepath.Join(t.TempDir(), "bos.jar")
	_ = os.WriteFile(bad, nil, 0o644)
	if ok, _ := InstallPlugin(t.TempDir(), bad); ok {
		t.Error("boş dosya eklenti sayıldı")
	}
}
