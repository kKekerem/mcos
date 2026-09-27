package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Tek örnek kilidinin testi.
//
// ── Neden gerekli ───────────────────────────────────────────────────────────
// Bu kilit, GERÇEKTEN olmuş bir arızayı kapatıyor: QEMU'da iki mcosd süreci
// aynı /data kökünde çalışıyordu (mcos-launch'taki koruma `pgrep`'e
// dayanıyordu ve pgrep imajda yok). İki daemon aynı Minecraft sunucusunu iki
// kez başlatabilir — iki JVM aynı dünya klasörüne yazar.
//
// Kilit sessizce kaybolursa hiçbir test kırılmazdı; bu yüzden burada.

func TestSecondInstanceRefused(t *testing.T) {
	dir := t.TempDir()

	first, err := lockDataRoot(dir)
	if err != nil {
		t.Fatalf("ilk örnek kilidi alamadı: %v", err)
	}
	defer first.Close()

	if _, err := lockDataRoot(dir); err == nil {
		t.Fatal("İKİNCİ örnek de kilidi aldı — aynı sunucu iki kez başlatılabilirdi")
	}
}

// Kilit BIRAKILINCA yeni bir örnek açılabilmeli: yoksa daemon bir kez
// çöktüğünde makine bir daha açılmazdı.
func TestLockIsReleasedOnClose(t *testing.T) {
	dir := t.TempDir()

	first, err := lockDataRoot(dir)
	if err != nil {
		t.Fatalf("ilk kilit: %v", err)
	}
	first.Close()

	second, err := lockDataRoot(dir)
	if err != nil {
		t.Fatalf("kilit bırakıldıktan sonra ikinci örnek açılamadı: %v", err)
	}
	second.Close()
}

// Kilit dosyası PID yazmalı: ikinci örnek "kim tutuyor" diyebilmeli.
func TestLockFileNamesHolder(t *testing.T) {
	dir := t.TempDir()
	f, err := lockDataRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	b, err := os.ReadFile(filepath.Join(dir, "mcosd.lock"))
	if err != nil {
		t.Fatalf("kilit dosyası okunamadı: %v", err)
	}
	if len(b) == 0 {
		t.Error("kilit dosyası boş — hata mesajı sahibini söyleyemez")
	}
}
