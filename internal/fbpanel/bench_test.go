package fbpanel

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mcos/internal/fbfont"
	"mcos/internal/fbinput"
	"mcos/internal/fbui"
	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// KARE BÜTÇESİ ÖLÇÜMLERİ
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı: "60fps olmalı ekran destekliyorsa". Bütçe monitörden gelir:
// 60 Hz'de 16,6 ms, 144 Hz'de 6,9 ms. Bir kare bunu aşarsa sayfa çevirme bir
// dikey boşluğu kaçırır ve hareket o anda yarı hıza düşer (takılma).
//
// Ölçülen şey Draw'un TAMAMI: arka plan, kenar çubuğu, içerik, pencere,
// perde, geçiş karışımı. Flip dahil DEĞİL — o, ekran kartına kopyalamadır ve
// DRM yolunda hasar takibiyle ayrıca küçültülüyor (internal/drm).
//
// Çalıştırma:
//
//	go test ./internal/fbpanel -run '^$' -bench 'Olcum' -benchtime 20x
//
// Çözünürlükler: 1920x1080, 2560x1440, 3840x2160. Yazı tipi, gerçek
// paneldeki gibi AutoFontSize ile seçilir; yoksa 4K'da 16 px'lik yazıyla
// ölçülen kare gerçekte çizilenden çok daha ucuz görünürdü.

var olcumCozunurlukler = []struct {
	ad   string
	w, h int
}{
	{"1080p", 1920, 1080},
	{"1440p", 2560, 1440},
	{"2160p", 3840, 2160},
}

// fontCache: her alt ölçümde yazı tipini yeniden yüklemek ölçüm süresini
// ikiye katlıyordu; boyut başına bir kez yüklenir.
var (
	fontCacheMu sync.Mutex
	fontCache   = map[float64]*fbfont.Face{}
)

func olcumFont(tb testing.TB, px float64) *fbfont.Face {
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	if f, ok := fontCache[px]; ok {
		return f
	}
	f, err := fbfont.Load(px)
	if err != nil {
		tb.Fatalf("font: %v", err)
	}
	fontCache[px] = f
	return f
}

// newOlcumApp builds a demo panel at w×h with the real font sizing.
func newOlcumApp(tb testing.TB, w, h int) *App {
	tb.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	ui := fbui.NewUI(img, olcumFont(tb, AutoFontSize(h)), fbui.DefaultPalette)
	a := New(ui, nil)
	a.SetHeadless(true)
	FillDemo(a)
	a.SetScreenSize(w, h)
	cfg := model.DefaultConfig()
	cfg.UI.Animations = true
	// Kurulum tamam: yoksa Run ilk kurulum sihirbazını açar ve sihirbazın
	// nefes alan logosu sürekli kare ister (doğru davranış, ama ölçülen
	// şey ana panel).
	cfg.SetupComplete = true
	a.SetConfig(cfg)
	return a
}

// olcumSenaryo prepares the app once, then `each` runs before every frame.
type olcumSenaryo struct {
	ad    string
	setup func(a *App)
	each  func(a *App)
}

// settle clears any transition the setup requested, so a scenario measures
// the STEADY frame unless it asks for a transition explicitly.
func settle(a *App) {
	a.Draw()
	a.mu.Lock()
	a.trans = nil
	a.mu.Unlock()
	a.Draw()
}

func olcumSenaryolari() []olcumSenaryo {
	var out []olcumSenaryo
	for s := Section(0); s < secCount; s++ {
		s := s
		out = append(out, olcumSenaryo{
			ad: fmt.Sprintf("bolum-%02d", int(s)),
			setup: func(a *App) {
				a.gotoSection(s)
				a.setFocus(FocusContent)
				settle(a)
			},
		})
	}
	openModal := func(a *App) {
		a.gotoSection(SecNetwork)
		a.setFocus(FocusContent)
		settle(a)
		a.OpenModal(NewListModal("Wi-Fi ağı seç", "Bağlanılacak ağı seçin.",
			[]ListItem{
				{Label: "Ev-5G", Detail: "%92", Current: true},
				{Label: "Ev-2.4", Detail: "%80"},
				{Label: "Komşu", Detail: "%41", Badge: "WPA2"},
				{Label: "Kafe", Detail: "%23"},
			}, nil))
	}
	out = append(out,
		olcumSenaryo{
			ad: "pencere-acik",
			setup: func(a *App) {
				openModal(a)
				settle(a)
			},
		},
		olcumSenaryo{
			// Pencerenin İLK karesi: arkadaki ekranın bulanıklığı bu karede
			// hesaplanır. Açılış animasyonunun ilk karesi budur; bütçeyi
			// aşarsa pencere "takılarak" açılır.
			ad: "pencere-ilk-kare",
			setup: func(a *App) {
				openModal(a)
				settle(a)
			},
			each: func(a *App) {
				a.mu.Lock()
				a.scrim.Invalidate()
				a.mu.Unlock()
			},
		},
		olcumSenaryo{
			ad: "pencere-gecis-ortasi",
			setup: func(a *App) {
				openModal(a)
				settle(a)
			},
			each: func(a *App) { midTransition(a, transFade) },
		},
		olcumSenaryo{
			ad: "kilit",
			setup: func(a *App) {
				a.mu.Lock()
				a.locked = true
				a.lockInput = "gizli"
				a.mu.Unlock()
				settle(a)
			},
		},
		olcumSenaryo{
			ad: "bolum-gecis-ortasi",
			setup: func(a *App) {
				a.gotoSection(SecServers)
				a.setFocus(FocusContent)
				settle(a)
			},
			each: func(a *App) { midTransition(a, transSlideDown) },
		},
	)
	return out
}

// midTransition pins a transition at its half-way point for this frame.
//
// Saat gerçek zamandan okunduğu için başlangıç her karede yeniden kurulur;
// yoksa ölçüm ilk 180 ms'den sonra geçişsiz kareyi ölçerdi.
func midTransition(a *App, k transKind) {
	a.mu.Lock()
	a.trans = &transition{kind: k, start: time.Now().Add(-transDuration / 2), dur: transDuration}
	a.dirty = true
	a.mu.Unlock()
}

func BenchmarkOlcum(b *testing.B) {
	for _, c := range olcumCozunurlukler {
		for _, s := range olcumSenaryolari() {
			c, s := c, s
			b.Run(c.ad+"/"+s.ad, func(b *testing.B) {
				a := newOlcumApp(b, c.w, c.h)
				s.setup(a)
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if s.each != nil {
						s.each(a)
					}
					a.Invalidate()
					a.Draw()
				}
				b.StopTimer()
				ms := float64(b.Elapsed().Microseconds()) / 1000 / float64(b.N)
				b.ReportMetric(ms, "ms/kare")
			})
		}
	}
}

// TestOlcumEkranGoruntuleri writes one PNG per scenario for visual review.
//
// Yalnızca MCOS_OLCUM_PNG bir klasör gösteriyorsa çalışır: normal test
// koşusunda diske yazmak istenmez.
func TestOlcumEkranGoruntuleri(t *testing.T) {
	dir := os.Getenv("MCOS_OLCUM_PNG")
	if dir == "" {
		t.Skip("MCOS_OLCUM_PNG ayarlı değil")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range olcumCozunurlukler[:1] {
		for _, s := range olcumSenaryolari() {
			a := newOlcumApp(t, c.w, c.h)
			s.setup(a)
			if s.each != nil {
				s.each(a)
			}
			a.Invalidate()
			a.Draw()
			p := filepath.Join(dir, c.ad+"-"+s.ad+".png")
			f, err := os.Create(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, a.ui.Canvas()); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
	}
}

// ── Boştayken kare üretimi ──────────────────────────────────────────────────

// countingDisplay counts Flips: her Flip'ten önce tam bir Draw çalışır.
type countingDisplay struct {
	mu    sync.Mutex
	flips int
}

func (d *countingDisplay) Flip(*image.RGBA) error {
	d.mu.Lock()
	d.flips++
	d.mu.Unlock()
	return nil
}
func (d *countingDisplay) Blank(bool) bool { return true }
func (d *countingDisplay) n() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.flips
}

// startIdleRun runs the loop on a quiet demo system (nothing spinning).
func startIdleRun(t *testing.T) (*App, *countingDisplay, *fakeHost, chan error) {
	t.Helper()
	a := newOlcumApp(t, 1920, 1080)
	// Demo verisindeki "açılıyor" sunucusu dönen gösterge ister, yani
	// gerçekten bir şey hareket ediyor demektir; boşta ölçümü için durgun
	// bir sistem kuruyoruz.
	_, servers, _ := a.Snapshot()
	for _, s := range servers {
		if s.State == model.StateStarting || s.State == model.StateStopping {
			s.State = model.StateRunning
		}
	}
	a.SetServers(servers)
	a.Emit(fbui.EventInfo, "hazır")

	disp := &countingDisplay{}
	host := newFakeHost()
	done := make(chan error, 1)
	go func() {
		done <- a.Run(a.ui.Canvas(), disp, host, Options{
			Poll: time.Hour, Anim: 80 * time.Millisecond,
			EscTimeout: 10 * time.Millisecond, Frame: 16 * time.Millisecond,
		})
	}()
	time.Sleep(300 * time.Millisecond)
	return a, disp, host, done
}

// TestBostaKareUretmez — nothing moving means NO frames.
//
// Bu makinede Minecraft sunucuları çalışıyor: boştaki bir panelin saniyede
// 60 kare çizmesi, oyunculardan çalınan CPU demek. Yeni animasyonların
// (yumuşak sayılar, kayan vurgu, arka plan ışıkları) hiçbiri bittikten sonra
// kare istememeli.
//
// Süre varsayılan 1,5 sn; MCOS_BOSTA_SN ile uzatılabilir (ölçüm: 5 sn).
func TestBostaKareUretmez(t *testing.T) {
	sure := 1500 * time.Millisecond
	if v := os.Getenv("MCOS_BOSTA_SN"); v != "" {
		var sn int
		fmt.Sscanf(v, "%d", &sn)
		if sn > 0 {
			sure = time.Duration(sn) * time.Second
		}
	}
	_, disp, host, done := startIdleRun(t)
	// Bir bölüm değiştir: geçiş, kayan vurgu ve kademeli giriş çalışsın;
	// hepsi bitince panel susmalı.
	host.keys <- fbinput.Key{Name: "down"}
	time.Sleep(1500 * time.Millisecond)

	basla := disp.n()
	time.Sleep(sure)
	fark := disp.n() - basla
	t.Logf("boşta %v içinde %d kare (Draw+Flip)", sure, fark)
	if fark != 0 {
		t.Errorf("panel boştayken %d kare üretti; 0 bekleniyordu", fark)
	}
	close(host.keys)
	<-done
}

// TestGecisKareHizi — a transition must run at the FRAME rate, not the
// 12 fps spinner tick.
//
// 180 ms'lik bir kayma 80 ms'lik animasyon tikine bağlıysa ekranda yalnızca
// 2-3 ara kare görünür: kullanıcının "akıcı değil" dediği şey. Kare tiki
// 16 ms iken aynı geçiş ~11 kare olmalı.
func TestGecisKareHizi(t *testing.T) {
	_, disp, host, done := startIdleRun(t)
	basla := disp.n()
	host.keys <- fbinput.Key{Name: "down"}
	time.Sleep(transDuration)
	fark := disp.n() - basla
	t.Logf("%v'lik geçişte %d kare", transDuration, fark)
	if fark < 7 {
		t.Errorf("geçiş %d kareyle oynadı; kare tikinde (16 ms) en az 7 bekleniyordu", fark)
	}
	close(host.keys)
	<-done
}
