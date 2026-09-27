package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"mcos/internal/model"
	"mcos/internal/tunnel"
)

// playit imajdayken WAN açık sunucuya Serveo tüneli AÇILMAMALI: panel Serveo'yu
// kaldırmıştı ama daemon her başlatmada serveo.net'e SSH tüneli açıyor,
// sunucunun görünmeyen ikinci bir genel adresi oluyordu.
func TestServeoOnlyWithoutPlayit(t *testing.T) {
	srv := &model.Server{WAN: model.WANConfig{Enabled: true}}

	dir := t.TempDir()
	d, c := filepath.Join(dir, "playitd"), filepath.Join(dir, "playit-cli")
	for _, p := range []string{d, c} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	restore := tunnel.SetPlayitBinsForTest(d, c)
	if serveoFallback(srv) {
		t.Error("playit varken Serveo açılacaktı")
	}
	restore()

	// KARŞI-SINAMA: playit yoksa Serveo yedeği çalışmaya devam eder.
	restore = tunnel.SetPlayitBinsForTest(filepath.Join(dir, "yok-d"), filepath.Join(dir, "yok-c"))
	defer restore()
	if !serveoFallback(srv) {
		t.Error("playit yokken Serveo yedeği açılmadı")
	}
	if serveoFallback(&model.Server{}) {
		t.Error("WAN kapalı sunucuya tünel açılacaktı")
	}
}
