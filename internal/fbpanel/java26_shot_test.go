package fbpanel

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"mcos/internal/java"
	"mcos/internal/model"
)

// ── Minecraft 26.x ve Java 25 ───────────────────────────────────────────────
//
// Yazılım ekranı "Java 21 — Minecraft 1.20.5 ve sonrası" diyordu. 26.x
// Java 25 ister (Mojang manifesti, javaVersion.majorVersion = 25); o yazıya
// güvenen kullanıcı 26.3 sunucusu için Java 21 kurup UnsupportedClassVersion
// hatasıyla karşılaşırdı ve listede Java 25 hiç yoktu.

// Her satırın notu, o Java'yı gerçekten İSTEYEN sürümlerle çelişmemeli:
// not "X ve sonrası" diyorsa X'ten yeni hiçbir sürüm başka bir Java istememeli.
func TestJavaTeklifleri26ileTutarli(t *testing.T) {
	var var25 bool
	for _, o := range javaOffer {
		if o.major == 25 {
			var25 = true
			if !strings.Contains(o.note, "26.1") {
				t.Errorf("Java 25 notu 26.x'i söylemeli: %q", o.note)
			}
		}
		if strings.Contains(o.note, "ve sonrası") && o.major != java.RequiredJavaMajor("26.3") {
			t.Errorf("Java %d notu %q; ama 26.3 Java %d istiyor", o.major, o.note, java.RequiredJavaMajor("26.3"))
		}
	}
	if !var25 {
		t.Fatal("Yazılım ekranında Java 25 yok; 26.x sunucuları onu istiyor")
	}
}

// Yazılım ekranı çizilir; MCOS_SHOT_DIR verilirse PNG oraya yazılır.
// Satırlar ekrana sığmalı: en uzun satırın genişliği içerik panelini aşmamalı.
func TestJavaYazilimEkraniShot(t *testing.T) {
	a, img := newTestApp(t)
	a.mu.Lock()
	a.javaRuntimes = []model.JavaRuntime{
		{Major: 21, Version: "21.0.8+9", Vendor: "temurin", Builtin: true},
		{Major: 25, Version: "25.0.4.1+1", Vendor: "temurin"},
	}
	a.dirty = true
	a.mu.Unlock()
	a.gotoSection(SecSoftware)
	a.setFocus(FocusContent)
	a.Draw()
	if ink := countInk(img); ink < 20000 {
		t.Fatalf("Yazılım ekranı neredeyse boş (%d piksel)", ink)
	}
	u := a.ui
	for _, o := range javaOffer {
		// Satır: radyo + "Java NN" + boşluk + not + sağda "gömülü/kurulu".
		w := u.F.CellW*2 + u.TextWidth("Java 25") + u.M.Gap*3 + u.TextWidth(o.note) + u.TextWidth("gömülü")
		if w > 1280-360 { // kenar çubuğu + kenar boşlukları için pay
			t.Errorf("Java %d satırı sığmıyor (%d piksel): %q", o.major, w, o.note)
		}
	}
	writePNG(t, filepath.Join(peersShotDir(t), "yazilim-java25.png"), img)
}

// Kurulum sihirbazının Java adımı (gömülü Java 21) çizilir; iki ekran
// boyutunda, gerçek panelin o boyutta seçtiği yazı tipiyle.
func TestKurulumJavaAdimiShot(t *testing.T) {
	for _, b := range []struct{ w, h int }{{1280, 800}, {1024, 768}} {
		a, img := newSizedApp(t, b.w, b.h)
		a.StartSetup()
		s := a.setupState()
		a.mu.Lock()
		s.step = stepJava
		s.javaOK = true
		s.javaBuiltin = true
		s.javaNote = "Java 21 gömülü, kurulu"
		a.dirty = true
		a.mu.Unlock()
		a.Draw()
		writePNG(t, filepath.Join(peersShotDir(t), fmt.Sprintf("kurulum-java-%dx%d.png", b.w, b.h)), img)
	}
}
