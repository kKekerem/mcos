package fbpanel

import (
	"testing"
	"time"

	"mcos/internal/model"
)

// Enter'a art arda basmak bütün kademeleri gezip başa dönmeli.
func TestAnimasyonHiziDongusu(t *testing.T) {
	ui := model.DefaultUI()
	var gorulen []string
	for i := 0; i < 4; i++ {
		on, speed := nextAnimSpeed(ui)
		ui.Animations = on
		if on {
			ui.AnimSpeed = speed
		}
		ui = ui.Normalize()
		if !ui.Animations {
			gorulen = append(gorulen, "kapalı")
		} else {
			gorulen = append(gorulen, animSpeedLabel(ui.AnimSpeed))
		}
	}
	want := []string{"hızlı", "kapalı", "yavaş", "normal"}
	for i := range want {
		if gorulen[i] != want[i] {
			t.Fatalf("kademe sırası %v olmalı, %v geldi", want, gorulen)
		}
	}
}

func TestAnimasyonCarpani(t *testing.T) {
	ui := model.DefaultUI()
	cases := []struct {
		on    bool
		speed string
		want  time.Duration
	}{
		{true, "", 180 * time.Millisecond},
		{true, model.AnimSlow, 270 * time.Millisecond},
		{true, model.AnimFast, 108 * time.Millisecond},
		{true, "bozuk-değer", 180 * time.Millisecond}, // Normalize → normal
		{false, model.AnimSlow, 0},                     // kapalı = anında
	}
	for _, c := range cases {
		ui.Animations, ui.AnimSpeed = c.on, c.speed
		got := scaleDur(transDuration, ui.Normalize().AnimScale())
		if got != c.want {
			t.Errorf("açık=%v hız=%q: %v bekleniyordu, %v geldi", c.on, c.speed, c.want, got)
		}
	}
}

// Şerit kayması da çarpana uymalı: süresi verilmiş bir kayma o sürede biter.
func TestSeritKaymasiHizaUyuyor(t *testing.T) {
	h := &highlightAnim{dur: 40 * time.Millisecond}
	if h.slideDur() != 40*time.Millisecond {
		t.Fatal("verilen süre kullanılmalı")
	}
	if (&highlightAnim{}).slideDur() != stripeSlideDur {
		t.Fatal("süre verilmemişse varsayılan kullanılmalı")
	}
}
