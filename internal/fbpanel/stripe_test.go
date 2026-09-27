package fbpanel

import (
	"image"
	"testing"
	"time"
)

// Seçim şeridi eski satırdan yenisine KAYARAK gitmeli (yarı yolda arada),
// süre dolunca hedefte durmalı.
func TestSeritKayar(t *testing.T) {
	simdi := time.Now()
	h := &highlightAnim{
		from: image.Rect(0, 100, 400, 140),
		to:   image.Rect(0, 200, 400, 240),
		at:   simdi,
	}
	if r := stripeAt(h, simdi); r.Min.Y != 100 {
		t.Fatalf("başta eski satırda olmalı: %v", r)
	}
	yari := stripeAt(h, simdi.Add(stripeSlideDur/2))
	if yari.Min.Y <= 100 || yari.Min.Y >= 200 {
		t.Fatalf("yarı yolda iki satırın ARASINDA olmalı: %v", yari)
	}
	if r := stripeAt(h, simdi.Add(stripeSlideDur)); r != h.to {
		t.Fatalf("süre dolunca hedefte olmalı: %v", r)
	}
}

// Farklı bir liste (bölüm değişti) şeridi kaydırmamalı: yeni yerinde belirir.
func TestSeritBaskaListedeZiplamaz(t *testing.T) {
	a, _ := newTestApp(t)
	r1 := image.Rect(0, 100, 400, 140)
	a.slidingStripe(r1)
	a.mu.Lock()
	a.hl.key = "baska-liste"
	a.mu.Unlock()
	r2 := image.Rect(0, 600, 400, 640)
	if got := a.slidingStripe(r2); got != r2 {
		t.Fatalf("başka listede şerit kaymamalı, yerinde belirmeli: %v", got)
	}
	if a.stripeSliding() {
		t.Fatal("başka listeye geçişte kayma animasyonu başladı")
	}
}

// Kayarken panel kare tikinde çizim istemeli; bitince boşta kalmalı.
func TestSeritKayarkenHizliCizim(t *testing.T) {
	a, _ := newTestApp(t)
	a.slidingStripe(image.Rect(0, 100, 400, 140))
	a.slidingStripe(image.Rect(0, 140, 400, 180))
	if !a.needsFastRedraw() {
		t.Fatal("şerit kayarken kare tikinde çizim istenmiyor")
	}
	a.mu.Lock()
	a.hl.at = time.Now().Add(-2 * stripeSlideDur)
	a.mu.Unlock()
	if a.needsFastRedraw() {
		t.Fatal("kayma bittiği hâlde hâlâ hızlı çizim isteniyor")
	}
}
