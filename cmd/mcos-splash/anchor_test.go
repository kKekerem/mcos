package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Kök değişse de (switch_root) dosyalar çalışma dizininden görülmeli: burada
// dizinin ADINI değiştirerek aynı durumu taklit ediyoruz — mutlak yol artık
// çözülmüyor, çalışma dizini ise hâlâ aynı dizini gösteriyor.
func TestCalismaDiziniYolDegisinceDeGoruyor(t *testing.T) {
	eski, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(eski) })

	kok := t.TempDir()
	dir := filepath.Join(kok, ".mcos")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	until, stage, save := anchorRunDir(filepath.Join(dir, "ready"),
		filepath.Join(dir, "stage"), filepath.Join(dir, "frame.rgba"))
	if until != "ready" || stage != "stage" || save != "frame.rgba" {
		t.Fatalf("göreli adlar bekleniyordu: %q %q %q", until, stage, save)
	}

	// Dizin "taşındı": eski mutlak yol artık yok.
	if err := os.Rename(dir, filepath.Join(kok, "tasindi")); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(dir, "ready")) {
		t.Fatal("test kurgusu hatalı: eski yol hâlâ çözülüyor")
	}
	if err := os.WriteFile(filepath.Join(kok, "tasindi", "stage"), []byte("60|Sunucu yöneticisi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(kok, "tasindi", "ready"), nil, 0o644)

	if !fileExists(until) {
		t.Fatal("hazır bayrağı çalışma dizininden görülmeliydi")
	}
	if st := readStageInfo(stage); st.pct != 60 {
		t.Fatalf("aşama okunamadı: %+v", st)
	}
}

func TestFarkliDizinlerDokunulmuyor(t *testing.T) {
	eski, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(eski) })
	a, b, c := anchorRunDir("/a/ready", "/b/stage", "")
	if a != "/a/ready" || b != "/b/stage" || c != "" {
		t.Fatalf("farklı dizinlerde yollar korunmalı: %q %q %q", a, b, c)
	}
}
