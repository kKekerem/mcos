package fbpanel

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"net"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/ipcclient"
	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// Sihirbaz: sürüm listesi YAZILIMA göre
// ════════════════════════════════════════════════════════════════════════════
//
// Daemon artık her yazılım için o yazılımın kendi listesini veriyor
// (2026-09-27 gerçek listeleri: Paper'da düz 26.1 yok, Folia'nın en yenisi
// 26.2). Eskiden bütün listeler aynı Mojang listesiydi ve sihirbazın
// "seçimi yeni listede ara" mantığı hep bulurdu; artık bulamayabilir.

var (
	listePaper = []string{"26.3", "26.2", "26.1.2", "26.1.1", "1.21.11", "1.21.10",
		"1.21.8", "1.21.4", "1.20.6"}
	listeFolia = []string{"26.2", "26.1.2", "1.21.11", "1.21.8", "1.21.6", "1.21.5",
		"1.21.4", "1.20.6", "1.20.4", "1.20.2", "1.20.1", "1.19.4"}
	listeVanilla = []string{"26.3", "26.2", "26.1.2", "26.1.1", "26.1", "1.21.11",
		"1.21.10", "1.21.8", "1.12.2"}
)

func TestYakinSurum(t *testing.T) {
	for _, tc := range []struct {
		liste []string
		want  string
		sec   string
		tam   bool
	}{
		{listePaper, "1.21.10", "1.21.10", true},
		// Aynı çizgide en yakın YENİ düzeltme: 26.1 -> 26.1.1 (en yeni
		// 26.3 değil, çizginin en yenisi 26.1.2 de değil).
		{listePaper, "26.1", "26.1.1", false},
		// Aynı çizgi yoksa en yakın ESKİ: Folia'da 26.3 yok -> 26.2.
		{listeFolia, "26.3", "26.2", false},
		{listeFolia, "1.21.7", "1.21.8", false},
		{listeFolia, "1.20.3", "1.20.4", false},
		// Listedeki her şeyden eski: en eskisi.
		{listeFolia, "1.12.2", "1.19.4", false},
		// mcver'e göre aynı sürüm.
		{[]string{"1.21.1", "1.21"}, "1.21.0", "1.21", true},
	} {
		i, tam := yakinSurum(tc.liste, tc.want)
		if tc.liste[i] != tc.sec || tam != tc.tam {
			t.Errorf("%s: %s (tam=%v) seçildi, beklenen %s (tam=%v)",
				tc.want, tc.liste[i], tam, tc.sec, tc.tam)
		}
	}
}

// yazilimSec moves the wizard to sw the way the "Altyapı" row does, and
// delivers sw's list as the daemon would.
func yazilimSec(t *testing.T, a *App, w *Wizard, sw model.Software, res ipc.ServerVersionsResult) {
	t.Helper()
	a.mu.Lock()
	for i, s := range model.AllSoftware {
		if s == sw {
			w.softwareIdx = i
		}
	}
	w.clearVersionsLocked()
	gen := w.verGen
	a.mu.Unlock()
	a.applyVersions(w, gen, sw, res, nil)
}

func surumSihirbazi(t *testing.T) (*App, *Wizard) {
	t.Helper()
	a, _ := newTestApp(t)
	a.StartWizard()
	w := a.wizardState()
	if w == nil {
		t.Fatal("sihirbaz açılmadı")
	}
	a.applyVersions(w, w.verGen, model.SoftwarePaper,
		ipc.ServerVersionsResult{Versions: listePaper}, nil)
	return a, w
}

// Hiç seçim yapılmadıysa her yazılımda O YAZILIMIN en yenisi: Paper 26.3,
// Folia 26.2, Paper'a dönünce yine 26.3. Eski clearVersionsLocked ekrandaki
// varsayılanı açık seçime çeviriyordu ve Paper'a dönen kullanıcı hiç
// seçmediği 26.2'de kalıyordu.
func TestSihirbazVarsayilanYaziliminEnYenisi(t *testing.T) {
	a, w := surumSihirbazi(t)
	if v := a.wizardVersion(w); v != "26.3" {
		t.Fatalf("paper varsayılanı %q", v)
	}
	yazilimSec(t, a, w, model.SoftwareFolia, ipc.ServerVersionsResult{Versions: listeFolia})
	if v := a.wizardVersion(w); v != "26.2" || w.verSubst != "" {
		t.Fatalf("folia: %q (not %q); en yenisi 26.2 olmalı, uyarısız", v, w.verSubst)
	}
	yazilimSec(t, a, w, model.SoftwarePaper, ipc.ServerVersionsResult{Versions: listePaper})
	if v := a.wizardVersion(w); v != "26.3" {
		t.Fatalf("paper'a dönünce %q; paper'ın en yenisi 26.3 olmalı", v)
	}
}

// Açık seçim yeni yazılımda yoksa EN YAKIN sürüm seçilir ve söylenir; açık
// seçim silinmez, geri dönünce geri gelir.
func TestSihirbazAcikSecimYakinaDuser(t *testing.T) {
	a, w := surumSihirbazi(t)
	yazilimSec(t, a, w, model.SoftwareVanilla, ipc.ServerVersionsResult{Versions: listeVanilla})
	a.mu.Lock()
	w.verWant = "26.1" // listeden seçim (wizardPick'in yaptığı)
	w.verIdx = 4
	a.mu.Unlock()

	yazilimSec(t, a, w, model.SoftwarePaper, ipc.ServerVersionsResult{Versions: listePaper})
	if v := a.wizardVersion(w); v != "26.1.1" {
		t.Fatalf("paper'da 26.1 yok: en yakın 26.1.1 seçilmeliydi, %q seçildi", v)
	}
	if want := "paper için 26.1 yok; en yakın 26.1.1 seçildi"; w.verSubst != want {
		t.Fatalf("uyarı %q, beklenen %q", w.verSubst, want)
	}

	yazilimSec(t, a, w, model.SoftwareVanilla, ipc.ServerVersionsResult{Versions: listeVanilla})
	if v := a.wizardVersion(w); v != "26.1" || w.verSubst != "" {
		t.Fatalf("vanilla'ya dönünce açık seçim 26.1 geri gelmeliydi: %q (not %q)", v, w.verSubst)
	}
}

// Yedek liste (Fallback) işaretlenir ve yazılım değişince sıfırlanır.
func TestSihirbazYedekListeIsareti(t *testing.T) {
	a, w := surumSihirbazi(t)
	yazilimSec(t, a, w, model.SoftwareFolia,
		ipc.ServerVersionsResult{Versions: listeVanilla, Fallback: true, Source: "launchermeta.mojang.com"})
	if v := a.wizVersionView(w); !v.fallback {
		t.Fatal("yedek liste işaretlenmedi")
	}
	yazilimSec(t, a, w, model.SoftwarePaper, ipc.ServerVersionsResult{Versions: listePaper})
	if v := a.wizVersionView(w); v.fallback {
		t.Fatal("gerçek liste gelince yedek işareti kalktı olmalıydı")
	}
}

// Sürüm, yazılım ve özet sayfaları uyarılarla çizilir. Üç ekran boyutunda
// ÇİZİLEN uyarı metinleri (wizVersionWarnings) gövdeye sığmalı ve uyarılar
// gerçekten ekrana çıkmalı. MCOS_SHOT_DIR verilirse PNG'ler oraya.
//
// Neden uyarı renginde piksel sayılıyor: countInk arka planı da mürekkep
// sayıyor (Pal.Bg = 0x0F1216, eşik 8), yani "sayfa boş değil" denetimi
// her sayfada geçer; uyarılar hiç çizilmese de. Aynı sayfa uyarısız
// çizildiğinde Pal.Warn renginde belirgin şekilde AZ piksel kalmalı.
// Uyarılar varken çizilen PNG'ler (üç sayfa × üç boyut) MCOS_SHOT_DIR'e.
func TestSihirbazSurumUyarilariShot(t *testing.T) {
	for _, b := range []struct{ w, h int }{{1280, 800}, {1024, 768}, {800, 600}} {
		a, img := newSizedApp(t, b.w, b.h)
		a.StartWizard()
		w := a.wizardState()
		a.applyVersions(w, w.verGen, model.SoftwarePaper,
			ipc.ServerVersionsResult{Versions: listePaper}, nil)
		a.mu.Lock()
		w.verWant = "1.21.11"
		a.mu.Unlock()
		// En uzun metin: en uzun yazılım adı (craftbukkit) ve iki yedi
		// karakterli sürüm. Liste yedekten geliyor.
		yazilimSec(t, a, w, model.SoftwareCraftBukkit, ipc.ServerVersionsResult{
			Versions: []string{"26.2", "1.21.10"}, Fallback: true, Source: "yerleşik"})
		v := a.wizVersionView(w)
		uyarilar := wizVersionWarnings(v)
		if want := "craftbukkit için 1.21.11 yok; en yakın 1.21.10 seçildi"; v.subst != want ||
			!v.fallback || len(uyarilar) != 2 {
			t.Fatalf("uyarılar kurulmadı: %+v (beklenen not %q)", v, want)
		}

		u := a.ui
		govde := min(setupMaxCols*u.F.CellW, b.w-u.M.PadX*4)
		metinler := []string{wizVersionCount(999, model.SoftwareCraftBukkit)}
		for _, uy := range uyarilar {
			metinler = append(metinler, uy.ana, uy.alt)
		}
		for _, s := range metinler {
			if gen := u.F.CellW + u.M.Gap + u.TextWidth(s); gen > govde {
				t.Errorf("%dx%d: %q sığmıyor (%d > %d piksel)", b.w, b.h, s, gen, govde)
			}
		}

		ciz := func() int {
			a.mu.Lock()
			a.dirty = true
			a.mu.Unlock()
			a.Draw()
			return renkliPiksel(img, u.Pal.Warn)
		}
		for _, sayfa := range []struct {
			step wizStep
			ad   string
		}{{wizVersion, "surum"}, {wizSoftware, "yazilim"}, {wizConfirm, "ozet"}} {
			w.step, w.cursor = sayfa.step, 0
			uyarili := ciz()
			writePNG(t, filepath.Join(peersShotDir(t),
				fmt.Sprintf("sihirbaz-%s-%dx%d.png", sayfa.ad, b.w, b.h)), img)

			a.mu.Lock()
			subst, yedek := w.verSubst, w.verFallback
			w.verSubst, w.verFallback = "", false
			a.mu.Unlock()
			uyarisiz := ciz()
			a.mu.Lock()
			w.verSubst, w.verFallback = subst, yedek
			a.mu.Unlock()

			// Eşik: 800x600'deki en küçük yazıda TEK uyarı satırı ~160 piksel
			// bırakıyor (ölçüldü); uyarısız sayfada bu renk hiç yok.
			t.Logf("%dx%d %s: uyarı rengi %d piksel, uyarısız %d", b.w, b.h, sayfa.ad, uyarili, uyarisiz)
			if uyarili-uyarisiz < 100 {
				t.Errorf("%dx%d %s: uyarılar çizilmemiş (uyarı rengi %d piksel, uyarısız %d)",
					b.w, b.h, sayfa.ad, uyarili, uyarisiz)
			}
		}
	}
}

// renkliPiksel counts pixels within a small distance of c (kenar yumuşatma
// payı; metnin dolu pikselleri tam renktedir).
func renkliPiksel(img *image.RGBA, c color.RGBA) int {
	yakin := func(x, y uint8) bool { d := int(x) - int(y); return d > -24 && d < 24 }
	n := 0
	for i := 0; i+3 < len(img.Pix); i += 4 {
		if yakin(img.Pix[i], c.R) && yakin(img.Pix[i+1], c.G) && yakin(img.Pix[i+2], c.B) {
			n++
		}
	}
	return n
}

// sahteSurumDaemonu serves server.versions over a REAL ipc connection and
// records which software every request asked for.
type sahteSurumDaemonu struct {
	mu    sync.Mutex
	istek []model.Software
	kapi  chan struct{} // nil değilse yanıt, kapı kapanana (açılana) kadar bekler
	liste map[model.Software][]string
}

func (d *sahteSurumDaemonu) istekler() []model.Software {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.istek)
}

func (d *sahteSurumDaemonu) baslat(t *testing.T) *ipcclient.Client {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("dinleme: %v", err)
	}
	srv := ipc.NewServer(ln, nil)
	srv.Handle(ipc.MethodServerVersions, func(_ context.Context, raw json.RawMessage) (any, error) {
		var p ipc.ServerVersionsParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		d.mu.Lock()
		d.istek = append(d.istek, p.Software)
		kapi, liste := d.kapi, d.liste[p.Software]
		d.mu.Unlock()
		if kapi != nil {
			<-kapi
		}
		if len(liste) == 0 {
			return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: "liste yok: " + string(p.Software)}
		}
		return ipc.ServerVersionsResult{Versions: liste, Latest: liste[0]}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Serve(ctx) }()
	t.Cleanup(cancel)
	cl, err := ipcclient.Dial("tcp://" + ln.Addr().String())
	if err != nil {
		t.Fatalf("bağlantı: %v", err)
	}
	return cl
}

// listeBekle waits until the wizard shows list (arka plan goroutine'i
// yanıtı a.mu altında uygular; ana döngü gerekmez).
func listeBekle(t *testing.T, a *App, w *Wizard, list []string) {
	t.Helper()
	son := time.Now().Add(5 * time.Second)
	for time.Now().Before(son) {
		a.mu.Lock()
		ok := w.verLoaded && !w.verFetching && slices.Equal(w.versions, list)
		a.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	t.Fatalf("liste gelmedi: gösterilen %v (yükleniyor=%v, not %q), beklenen %v",
		w.versions, w.verFetching, w.verNote, list)
}

// Yazılım DEĞİŞİNCE liste yeniden istenir, istek YENİ yazılımı taşır ve
// ekrana o yazılımın listesi gelir. Gerçek ipc bağlantısıyla: istek
// gövdesindeki "software" alanı da sınanıyor (daemon yalnızca onu okur).
//
// Kapı, Altyapı satırında sağ oka yedi kez basılırken (paper -> folia)
// yanıtları bekletir: tek uçuş kuralı araya giren altı yazılım için istek
// AÇMAMALI, bayat purpur yanıtı UYGULANMAMALI ve son yazılım (folia) için
// tam bir kez daha istenmeli.
func TestSihirbazYazilimDegisinceListeYenidenIstenir(t *testing.T) {
	d := &sahteSurumDaemonu{liste: map[model.Software][]string{
		model.SoftwarePaper:  listePaper,
		model.SoftwarePurpur: {"26.3", "26.2", "26.1.2", "1.21.11"},
		model.SoftwareFolia:  listeFolia,
	}}
	a, _ := newTestApp(t)
	a.cl = d.baslat(t)
	a.StartWizard()
	w := a.wizardState()
	listeBekle(t, a, w, listePaper)

	d.mu.Lock()
	kapi := make(chan struct{})
	d.kapi = kapi
	d.mu.Unlock()
	a.mu.Lock()
	w.step, w.cursor = wizSoftware, 0
	a.mu.Unlock()
	adim := slices.Index(model.AllSoftware, model.SoftwareFolia) -
		slices.Index(model.AllSoftware, model.SoftwarePaper)
	for range adim {
		a.wizardKey("right")
	}
	if sw := w.software(); sw != model.SoftwareFolia {
		t.Fatalf("sağ ok %d kez: %s seçili, folia beklenirdi", adim, sw)
	}
	close(kapi)

	listeBekle(t, a, w, listeFolia)
	if v := a.wizardVersion(w); v != "26.2" {
		t.Fatalf("folia'nın en yenisi 26.2 seçilmeliydi: %q", v)
	}
	want := []model.Software{model.SoftwarePaper, model.SoftwarePurpur, model.SoftwareFolia}
	if got := d.istekler(); !slices.Equal(got, want) {
		t.Fatalf("istekler %v, beklenen %v", got, want)
	}
}
