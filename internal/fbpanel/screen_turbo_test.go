package fbpanel

import (
	"strings"
	"testing"

	"mcos/internal/model"
)

// Gerçek PC'de turbo "4,4 yerine 2,4 GHz" verdi ve panel sebebi hiçbir yerde
// göstermiyordu. Performans bölümünde "d" tuşu, daemon'un ölçtüğü tanıyı
// (sınırlayan, RAPL, fan geri okuması) tam ekran bir pencerede açmalı ve
// pencere ÇİZİLEBİLMELİ (kullanıcı ekran görüntüsünü bundan alacak).
func TestTurboDiagModalShowsDiagnosis(t *testing.T) {
	a, img := newTestApp(t)
	st, _, _ := a.Snapshot()
	cp := *st
	cp.Turbo = &model.TurboStatus{
		Active: true, Supported: true, DiagAt: 1_700_000_000,
		Summary: "performans kipi, hedef 4,4 GHz (azami), şu an 2,4 GHz, sınırlayan: PL1 güç sınırı (15,0 W)",
		Limiter: "PL1 güç sınırı (15,0 W)",
		Items:   []model.TurboItem{{Name: "Güç sınırı (RAPL)", State: model.TurboPartial, Detail: "package-0 (MMIO): PL1: BIOS kilitli"}},
		Diag: []model.TurboItem{
			{Name: "RAPL package-0 (MSR)", State: model.TurboPartial, Detail: "PL1 15,0 W, tüketim 14,9 W, TÜKETİM PL1'E DAYANMIŞ"},
			{Name: "Fan", State: model.TurboUnsupported, Detail: "hiçbir fan yolu yok"},
		},
	}
	a.SetStatus(&cp)
	a.gotoSection(SecPerformance)
	a.setFocus(FocusContent)
	a.Key("d")
	m, ok := a.ActiveModal().(*turboDiagModal)
	if !ok {
		t.Fatalf("Performans'ta d tuşu turbo tanısını açmadı (etkin pencere: %T)", a.ActiveModal())
	}
	var all []string
	for _, l := range a.turboDiagLines(1200) {
		all = append(all, l.s)
	}
	s := strings.Join(all, "\n")
	for _, w := range []string{"sınırlayan: PL1 güç sınırı", "TÜKETİM PL1'E DAYANMIŞ", "! Fan: hiçbir fan yolu yok", "BIOS kilitli"} {
		if !strings.Contains(s, w) {
			t.Errorf("tanı penceresinde %q yok:\n%s", w, s)
		}
	}
	for i := range img.Pix {
		img.Pix[i] = 0
	}
	a.Draw()
	if ink := countInk(img); ink < 2000 {
		t.Errorf("tanı penceresi çizilmedi (%d piksel)", ink)
	}
	if !m.Key(a, "esc") {
		t.Error("Esc tanı penceresini kapatmıyor")
	}
}
