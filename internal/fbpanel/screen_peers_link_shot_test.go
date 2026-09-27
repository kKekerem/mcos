package fbpanel

import (
	"errors"
	"image"
	"image/color"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcos/internal/fbfont"
	"mcos/internal/fbui"
	"mcos/internal/model"
)

// Ortak dünya modu kurulamadığında ekran NEDENİ göstermeli. Kullanıcının
// gerçek raporu: "ortak dünyayı açınca 'mcos link kurulu değil' diyor" —
// ekran nedeni hiç söylemiyordu (sunucu 26.3, mod yalnızca 1.21.11 içindi).
// Metinler daemon'un (internal/linkjar) GERÇEKTEN ürettiği cümlelerdir.

const (
	modProblemVersion = "Fabric 1.20.1 için ortak dünya modu yok (desteklenen: 1.20.5–26.3)"
	// En uzun gerçekçi neden: fabric-api yok + indirme zaman aşımı.
	modProblemLong = "fabric-api-0.161.0+26.3.jar bulunamadı ve indirilemedi (Get " +
		"\"https://api.modrinth.com/v2/project/fabric-api/version\": dial tcp: lookup " +
		"api.modrinth.com: no such host); ortak dünya modu kurulmadı — modu fabric-api " +
		"olmadan koymak sunucunun HİÇ açılmamasına yol açardı"
)

// peersShotAppSized is peersShotApp on a canvas of the given size.
func peersShotAppSized(t *testing.T, w, h int) *App {
	t.Helper()
	if w == 1280 && h == 800 {
		return peersShotApp(t)
	}
	f, err := fbfont.Load(16)
	if err != nil {
		t.Fatalf("font: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	a := New(fbui.NewUI(img, f, fbui.DefaultPalette), nil)
	a.SetHeadless(true)
	FillDemo(a)
	a.SetScreenSize(w, h)
	ref := peersShotApp(t)
	ref.mu.Lock()
	a.mu.Lock()
	a.clusterID, a.peers, a.link = ref.clusterID, ref.peers[:2], ref.link
	a.dirty = true
	a.mu.Unlock()
	ref.mu.Unlock()
	a.gotoSection(SecPeers)
	a.setFocus(FocusContent)
	return a
}

func withModProblem(a *App, problem string) {
	a.mu.Lock()
	a.link.ModInstalled = false
	a.link.ModProblem = problem
	a.dirty = true
	a.mu.Unlock()
}

// Neden ekranda: genel "kurulu değil" metni DEĞİL, daemon'un nedeni.
func TestPeersModProblemShowsReason(t *testing.T) {
	a := peersShotApp(t)
	withModProblem(a, modProblemVersion)
	a.mu.Lock()
	warn := linkModWarning(a.link)
	a.mu.Unlock()
	if !strings.HasPrefix(warn, modProblemVersion) {
		t.Errorf("uyarı nedeni taşımıyor: %q", warn)
	}
	if strings.Contains(warn, "mcos-link modu kurulu değil") {
		t.Errorf("hâlâ genel metin: %q", warn)
	}
	// Neden yoksa (eski daemon) genel metin kalır.
	if w := linkModWarning(model.LinkStatus{}); !strings.Contains(w, "kurulu değil") {
		t.Errorf("neden yokken: %q", w)
	}
	// "–" (aralık çizgisi) fontta olmalı; yoksa fitText onu "?" yapar ve
	// "1.20.5?26.3" okunur.
	if a.ui.F.Glyph('–').Missing {
		t.Error("fontta '–' yok")
	}
}

// Uzun neden iki satıra sarılır, her satır sütuna sığar, hiçbiri taşmaz.
func TestFitLinesNeverOverflows(t *testing.T) {
	a := peersShotApp(t)
	for _, cols := range []int{40, 60, 90} {
		for _, s := range []string{modProblemVersion + " — ortak dünya çalışmaz", modProblemLong} {
			lines := fitLines(a.ui.F, s, cols, 2)
			if len(lines) == 0 || len(lines) > 2 {
				t.Errorf("%d sütun: %d satır", cols, len(lines))
			}
			for _, l := range lines {
				if n := len([]rune(l)); n > cols {
					t.Errorf("%d sütun: satır %d karakter: %q", cols, n, l)
				}
			}
		}
	}
	// 60 sütunda sürüm nedeni KIRPILMADAN iki satıra sığar: desteklenen aralık
	// tam da kırpılan kısımdı.
	got := strings.Join(fitLines(a.ui.F, modProblemVersion+" — ortak dünya çalışmaz", 60, 2), " ")
	if !strings.Contains(got, "1.20.5–26.3") || strings.Contains(got, "…") {
		t.Errorf("aralık kayboldu: %q", got)
	}
}

func TestPeersModProblemShot(t *testing.T) {
	for _, sz := range []struct {
		w, h    int
		problem string
		name    string
	}{
		{1280, 800, modProblemVersion, "esleme-mod-sorunu.png"},
		{800, 600, modProblemVersion, "esleme-mod-sorunu-800.png"},
		{800, 600, modProblemLong, "esleme-mod-sorunu-uzun-800.png"},
	} {
		a := peersShotAppSized(t, sz.w, sz.h)
		withModProblem(a, sz.problem)
		a.Draw()
		img := a.ui.Canvas()
		writePNG(t, filepath.Join(peersShotDir(t), sz.name), img)
		// Uyarı rengi panelin İÇ alanının altına taşmamalı. 800x600'de
		// özetin altına konan ikinci satır panelin alt kenarına biniyordu
		// (PNG'de görüldü). İç alt sınır, Draw'daki düzenle aynı hesap.
		u := a.ui
		bottom := u.Bounds().Max.Y - u.StatusBarH() - u.M.PadX - u.M.PadY
		if n := warnPixels(img, u.Pal.Warn, a.sidebarWidth(), bottom, u.Bounds().Max.Y-u.StatusBarH()); n > 0 {
			t.Errorf("%s: uyarı metni panelin altına taştı (%d piksel, y>=%d)", sz.name, n, bottom)
		}
	}
}

// Ortak dünya AÇIK ama dilim yok (eş çevrimdışı): neden yine görünmeli.
// Eskiden bu durumda yalnızca genel ipucu çiziliyordu; sürümü değiştirilip
// modu kaldırılan ortak dünyada ekranın hiçbir yerinde neden yoktu.
func TestPeersModProblemShownWithoutTerritories(t *testing.T) {
	for _, sz := range []struct {
		w, h int
		name string
	}{
		{1280, 800, "esleme-mod-sorunu-dilimsiz.png"},
		{800, 600, "esleme-mod-sorunu-dilimsiz-800.png"},
	} {
		render := func(problem bool) (int, int) {
			a := peersShotAppSized(t, sz.w, sz.h)
			a.mu.Lock()
			a.link.Territories, a.link.Nodes = nil, nil
			a.dirty = true
			a.mu.Unlock()
			if problem {
				withModProblem(a, modProblemVersion)
			}
			a.Draw()
			img := a.ui.Canvas()
			u := a.ui
			bottom := u.Bounds().Max.Y - u.StatusBarH() - u.M.PadX - u.M.PadY
			if problem {
				writePNG(t, filepath.Join(peersShotDir(t), sz.name), img)
			}
			return warnPixels(img, u.Pal.Warn, a.sidebarWidth(), 0, bottom),
				warnPixels(img, u.Pal.Warn, a.sidebarWidth(), bottom, u.Bounds().Max.Y-u.StatusBarH())
		}
		base, _ := render(false)
		got, spill := render(true)
		// Satır başına yüzlerce piksel; 200'ün altı "çizilmedi" demektir.
		if got-base < 200 {
			t.Errorf("%s: dilimsiz ortak dünyada mod nedeni çizilmedi (uyarı pikseli %d -> %d)",
				sz.name, base, got)
		}
		if spill > 0 {
			t.Errorf("%s: uyarı panelin altına taştı (%d piksel)", sz.name, spill)
		}
	}
}

// warnPixels counts pixels close to the warning colour in rows [y0,y1), x>=x0.
func warnPixels(img *image.RGBA, c color.RGBA, x0, y0, y1 int) int {
	n := 0
	near := func(a, b uint8) bool { d := int(a) - int(b); return d > -24 && d < 24 }
	for y := y0; y < y1; y++ {
		for x := x0; x < img.Bounds().Max.X; x++ {
			p := img.RGBAAt(x, y)
			if near(p.R, c.R) && near(p.G, c.G) && near(p.B, c.B) {
				n++
			}
		}
	}
	return n
}

// link.enable reddedince açılan pencere: NEDEN + YAPILACAK ŞEY.
func TestSharedWorldFailureShot(t *testing.T) {
	lines := sharedWorldFailureLines(errors.New(modProblemVersion))
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "1.20.5–26.3") || !strings.Contains(joined, "desteklenen bir Minecraft sürümüyle") {
		t.Errorf("pencere nedeni ya da yapılacak şeyi söylemiyor:\n%s", joined)
	}
	if l := sharedWorldFailureLines(errors.New("Forge ortak dünyayı desteklemiyor; Fabric veya Paper seçin")); !strings.Contains(strings.Join(l, " "), "Fabric ya da Paper") {
		t.Errorf("Forge için yapılacak şey yok: %v", l)
	}
	a := peersShotAppSized(t, 800, 600)
	a.OpenModal(NewInfoModal("Ortak dünya açılamadı", lines))
	for i := 0; i < 10; i++ {
		a.Draw()
		time.Sleep(60 * time.Millisecond)
	}
	a.Draw()
	writePNG(t, filepath.Join(peersShotDir(t), "ortak-dunya-hata-800.png"), a.ui.Canvas())
}
