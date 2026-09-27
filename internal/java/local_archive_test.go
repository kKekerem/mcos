package java

import (
	"archive/tar"
	"compress/gzip"
	"crypto/rand"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// yerelArsiv, Adoptium düzeninde (tek üst dizin + bin/java + release) küçük
// bir tar.gz üretir. bin/java, sürüm yoklamasına cevap veren bir betiktir.
func yerelArsiv(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	ekle := func(name, body string, mode int64) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	ekle("jdk-25.0.4.1+1-jre/bin/java", "#!/bin/sh\necho 'openjdk version \"25.0.4.1\" 2026-07-21' >&2\n", 0o755)
	ekle("jdk-25.0.4.1+1-jre/release", "JAVA_VERSION=\"25.0.4.1\"\n", 0o644)
	// Arşiv 1 MB'tan küçükse yarım inmiş sayılır (localArchive). Dolgu
	// rastgele: sıfırlar gzip ile birkaç KB'a iner ve eşiği geçmezdi.
	dolgu := make([]byte, 2<<20)
	if _, err := rand.Read(dolgu); err != nil {
		t.Fatal(err)
	}
	ekle("jdk-25.0.4.1+1-jre/lib/modules", string(dolgu), 0o644)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

// 26.x sunucusu Java 25 ister; çevrimdışı paketteki arşiv varsa internete
// HİÇ çıkılmadan kurulmalı ve tohumlanmış arşiv silinmemeli.
func TestJava25YerelArsivdenKurulur(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("arşiv adı linux yapısına göre")
	}
	m, net, _ := newTestManager(t)
	dir := LocalArchiveDirs[0]
	arsiv := filepath.Join(dir, "0123456789abcdef-OpenJDK25U-jre_x64_linux_hotspot_25.0.4.1_1.tar.gz")
	yerelArsiv(t, arsiv)
	// Başka işletim sisteminin arşivi seçilmemeli.
	if err := os.WriteFile(filepath.Join(dir, "OpenJDK25U-jre_x64_windows_hotspot_25.0.4.1_1.zip"), make([]byte, 2<<20), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := m.Install(25)
	if err != nil {
		t.Fatalf("Install(25) yerel arşivle başarısız: %v", err)
	}
	if c := net.calls(); len(c) != 0 {
		t.Fatalf("yerel arşiv varken ağa çıkıldı: %v", c)
	}
	if _, err := os.Stat(rt.JavaBin); err != nil {
		t.Fatalf("java açılmadı: %v", err)
	}
	if _, err := os.Stat(arsiv); err != nil {
		t.Fatalf("tohumlanmış arşiv silindi: %v", err)
	}
	if got, ok, _ := m.Get(25); !ok || got.JavaBin != rt.JavaBin {
		t.Fatalf("Java 25 kayda geçmedi: %+v %v", got, ok)
	}
}
