package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"mcos/internal/deskgui"
	"mcos/internal/flash"
	"mcos/internal/version"
)

// Bu dosya USB kurucusunun PENCERELİ arayüzünü sunar.
//
// ════════════════════════════════════════════════════════════════════════════
// NEDEN, VE "SUNUCU YOK" KARARI NE OLDU
// ════════════════════════════════════════════════════════════════════════════
//
// main.go başındaki not, eski tarayıcı arayüzünün üç sorununu anlatıyor:
// tarayıcı gerekiyordu, güvenlik duvarı uyarısı çıkıyordu, çift tıklama
// beklentisini karşılamıyordu. Kullanıcı şimdi açıkça pencere istiyor
// ("basit bir GUI ekle"). Yeni arayüz o üç sorunu şöyle kapatıyor:
//
//   - Tarayıcı gerekmez: sayfa WebView2 PENCERESİNDE açılır (Windows 10/11'de
//     hazır gelen Edge motoru). Yoksa varsayılan tarayıcıya düşülür.
//   - Güvenlik duvarı uyarısı yok: dinleyici YALNIZCA 127.0.0.1'e bağlanır;
//     Windows geri döngü için uyarı göstermez (gerçek Windows'ta ölçüldü).
//   - Çift tıklama pencereyi açar; konsol kipi --konsol ile korunur.
//
// Konsol akışı (main.go) HİÇ DEĞİŞMEDİ; ikisi aynı güvenlik kurallarını
// uygular ve bu dosya onları TEKRAR denetler (bkz. handleWrite).
//
// ════════════════════════════════════════════════════════════════════════════
// GÜVENLİK KURALLARI (konsolla aynı, pencerede)
// ════════════════════════════════════════════════════════════════════════════
//
//  1. Yalnızca ÇIKARILABİLİR aygıtlar listelenir; sistem diski ASLA. Pencere
//     --all-disks'i hiç tanımaz: dahili disk isteyen konsolu kullanır.
//  2. Yazmadan önce kullanıcı aygıt yolunu ELLE yazar (onay alanı).
//  3. Yazmadan hemen önce aygıt listesi YENİDEN çekilir; aynı yol artık farklı
//     boyutta bir diski gösteriyorsa yazma reddedilir.
//  4. Yalnızca listelenen imajlar yazılabilir; istemci keyfi bir yol veremez.

//go:embed gui.html
var guiHTML string

//go:embed gui.js
var guiJS string

// Pencereye özgü bayraklar. Paket düzeyinde tanımlanır ki main.go'ya yalnızca
// tek satırlık bir kanca (guiHook) eklensin: main.go'nun hata yollarını başka
// bir çalışma düzenliyor.
var (
	guiConsole  = flag.Bool("konsol", false, "pencere yerine konsol (metin) arayüzünü kullan")
	guiForce    = flag.Bool("gui", false, "pencereli arayüzü aç (Windows'ta varsayılan; Linux'ta adres yazdırılır)")
	guiDuration = flag.Duration("gui-sure", 0, "pencereyi bu süre sonunda kapat (ör. 15s; sınama için)")
	guiBrowser  = flag.Bool("tarayici", false, "arayüzü pencere yerine varsayılan tarayıcıda aç")
	guiNoOpen   = flag.Bool("gui-acma", false, "arayüz için hiçbir şey açma, yalnızca adresini yaz")
)

// consoleFlags are switches that only make sense in the text interface.
//
// --device/--yes betiklerin, --list sorgunun, --all-disks ise dahili diske
// bilerek yazmak isteyen uzmanın bayrağıdır: pencere bunların hiçbirini
// sunmaz, o yüzden verildiklerinde konsolda kalınır.
var consoleFlags = []string{"list", "device", "yes", "all-disks", "version", "konsol"}

// wantGUI decides between the window and the console.
func wantGUI(set map[string]bool, goos string) bool {
	for _, f := range consoleFlags {
		if set[f] {
			return false
		}
	}
	return set["gui"] || goos == "windows"
}

// guiHook is the only line main.go calls: it either runs the window (and
// returns true) or prepares the console and returns false.
func guiHook() bool {
	set := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if !wantGUI(set, runtime.GOOS) {
		// Windows'ta GUI alt sistemiyle derleniyoruz (make flash-windows):
		// konsol kipinde metnin görünmesi için konsol bağlanır/açılır.
		attachConsole()
		return false
	}
	if err := runGUI(guiConfigFromFlags()); err != nil {
		guiFatal(err)
		os.Exit(1)
	}
	return true
}

// guiConfig is what the window needs from the command line.
type guiConfig struct {
	image    string // --image: listede en üste konur
	imageDir string // --images
	dryRun   bool
	verify   bool
}

func guiConfigFromFlags() guiConfig {
	get := func(name string) string {
		if f := flag.Lookup(name); f != nil {
			return f.Value.String()
		}
		return ""
	}
	return guiConfig{
		image:    get("image"),
		imageDir: get("images"),
		dryRun:   get("dry-run") == "true",
		verify:   get("no-verify") != "true",
	}
}

// ── Denetleyici ─────────────────────────────────────────────────────────────

// flashGUI is the controller behind the window.
type flashGUI struct {
	cfg guiConfig

	// Aşağıdakiler sınamalarda değiştirilir: gerçek bir diske DOKUNMADAN
	// bütün akış (onay, yeniden doğrulama, ilerleme) sınanabilsin.
	enumerate func(includeInternal bool) ([]flash.Device, error)
	write     func(ctx context.Context, w flash.Writer, p func(flash.Progress)) error
	elevated  func() bool
	relaunch  func(args []string) error
	imageDirs func() []string
	quit      chan struct{}
	quitOnce  sync.Once

	mu      sync.Mutex
	writing bool
	cancel  context.CancelFunc
	prog    writeProgress
}

// writeProgress is the last write's state as the window shows it.
type writeProgress struct {
	Active  bool    `json:"etkin"`
	Stage   string  `json:"asama,omitempty"`
	Percent int     `json:"yuzde"`
	Done    uint64  `json:"yazilan"`
	Total   uint64  `json:"toplam"`
	Rate    float64 `json:"hiz"`
	Remain  string  `json:"kalan,omitempty"`
	Err     string  `json:"hata,omitempty"`
	OK      bool    `json:"basari"`
	Device  string  `json:"aygit,omitempty"`
	Image   string  `json:"imaj,omitempty"`
	Took    string  `json:"sure,omitempty"`
	DryRun  bool    `json:"deneme"`
	Verify  bool    `json:"dogrulama"`

	stageStart time.Time
	started    time.Time
}

func newFlashGUI(cfg guiConfig) *flashGUI {
	return &flashGUI{
		cfg:       cfg,
		enumerate: flash.Enumerate,
		write: func(ctx context.Context, w flash.Writer, p func(flash.Progress)) error {
			return w.Write(ctx, p)
		},
		elevated:  isElevated,
		relaunch:  relaunchElevated,
		imageDirs: flash.DefaultImageDirs,
		quit:      make(chan struct{}),
	}
}

// devices lists what the window may offer: removable, never the system disk.
//
// Enumerate(false) zaten ikisini de eler; burada YİNE bakılır. Konsoldaki
// kural (main.go reverify + flash.Validate) gibi: tek bir yerde unutulan
// denetim, silinmiş bir disk demektir.
func (g *flashGUI) devices() ([]flash.Device, error) {
	list, err := g.enumerate(false)
	if err != nil {
		return nil, err
	}
	out := list[:0:0]
	for _, d := range list {
		if d.System || !d.Removable {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

// images lists writable images, newest first (main.go findImages).
func (g *flashGUI) images() ([]imageFile, []string) {
	dirs := g.imageDirs()
	if g.cfg.imageDir != "" {
		dirs = append([]string{g.cfg.imageDir}, dirs...)
	}
	if g.cfg.image != "" {
		// --image ile verilen dosya, bulunduğu klasörle birlikte aranır ki
		// listede (ve izinli yollar arasında) yer alsın.
		dirs = append([]string{filepath.Dir(g.cfg.image)}, dirs...)
	}
	return findImages(dirs), dirs
}

type imageJSON struct {
	Path   string `json:"yol"`
	Name   string `json:"ad"`
	Size   uint64 `json:"boyut"`
	Date   string `json:"tarih"`
	Newest bool   `json:"enYeni"`
	Stale  string `json:"bayat,omitempty"`
}

type deviceJSON struct {
	Path     string   `json:"yol"`
	Name     string   `json:"ad"`
	Model    string   `json:"model"`
	Size     uint64   `json:"boyut"`
	Bus      string   `json:"veriyolu,omitempty"`
	Warnings []string `json:"uyarilar,omitempty"`
	TooSmall bool     `json:"kucuk"`
}

func (g *flashGUI) register(s *deskgui.Server) {
	s.Get("/api/durum", func(*http.Request) (any, error) {
		g.mu.Lock()
		p := g.prog
		g.mu.Unlock()
		return map[string]any{
			"surum":     version.Display(),
			"deneme":    g.cfg.dryRun,
			"dogrulama": g.cfg.verify,
			"yonetici":  g.elevated(),
			"windows":   runtime.GOOS == "windows",
			"ilerleme":  p,
			"enKucuk":   flash.MinSizeBytes,
		}, nil
	})
	s.Get("/api/imajlar", func(*http.Request) (any, error) {
		list, dirs := g.images()
		out := []imageJSON{}
		for i, im := range list {
			j := imageJSON{Path: im.path, Name: filepath.Base(im.path), Size: im.size,
				Date: im.mod.Format("02.01.2006 15:04"), Newest: i == 0}
			if i > 0 && bayatMi(im, list[0]) {
				j.Stale = eskilik(list[0].mod.Sub(im.mod))
			}
			out = append(out, j)
		}
		return map[string]any{"imajlar": out, "klasorler": dirs}, nil
	})
	s.Get("/api/aygitlar", func(*http.Request) (any, error) {
		list, err := g.devices()
		if err != nil {
			// Hata (ör. "yönetici olarak çalıştırın") listeyle birlikte
			// gösterilir; boş bir liste "USB takılı değil" gibi okunurdu.
			return map[string]any{"aygitlar": []deviceJSON{}, "hata": err.Error()}, nil
		}
		out := []deviceJSON{}
		for _, d := range list {
			out = append(out, deviceJSON{Path: d.Path, Name: d.Name, Model: d.Model,
				Size: d.SizeBytes, Bus: d.Bus, Warnings: d.Warnings(),
				TooSmall: d.SizeBytes < flash.MinSizeBytes})
		}
		return map[string]any{"aygitlar": out}, nil
	})
	s.Post("/api/yaz", g.handleWrite)
	s.Post("/api/iptal", func(*http.Request) (any, error) {
		g.mu.Lock()
		c := g.cancel
		g.mu.Unlock()
		if c == nil {
			return map[string]any{"ileti": "Süren bir yazma yok."}, nil
		}
		c()
		return map[string]any{"ileti": "Durduruluyor… (yarım yazılmış bir bellek açılmaz — baştan yazın)"}, nil
	})
	s.Post("/api/yonetici", func(*http.Request) (any, error) {
		if g.elevated() {
			return map[string]any{"ileti": "Zaten yönetici olarak çalışıyor."}, nil
		}
		g.mu.Lock()
		busy := g.writing
		g.mu.Unlock()
		if busy {
			return nil, deskgui.Fail(http.StatusConflict, "Yazma sürerken yeniden başlatılamaz.")
		}
		if err := g.relaunch(g.relaunchArgs()); err != nil {
			return nil, fmt.Errorf("yönetici olarak başlatılamadı: %w", err)
		}
		// Yükseltilmiş kopya kendi penceresini açar; bu pencere kapanır.
		go func() {
			time.Sleep(time.Second)
			g.quitOnce.Do(func() { close(g.quit) })
		}()
		return map[string]any{"ileti": "Yönetici olarak yeniden açılıyor…"}, nil
	})
}

// relaunchArgs keeps the user's switches when restarting as administrator.
//
// mcos-flash.bat'ta yakalanan hata (argümanlar yükseltmede DÜŞÜYORDU,
// --dry-run diyen kullanıcı gerçek yazma akışında buluyordu kendini) burada
// tekrarlanmasın: deneme kipi yükseltilmiş kopyaya da geçer.
func (g *flashGUI) relaunchArgs() []string {
	a := []string{"--gui"}
	if g.cfg.dryRun {
		a = append(a, "--dry-run")
	}
	if !g.cfg.verify {
		a = append(a, "--no-verify")
	}
	if g.cfg.image != "" {
		a = append(a, "--image", g.cfg.image)
	}
	if g.cfg.imageDir != "" {
		a = append(a, "--images", g.cfg.imageDir)
	}
	return a
}

// handleWrite validates everything again and starts the write.
func (g *flashGUI) handleWrite(r *http.Request) (any, error) {
	var req struct {
		Image   string `json:"imaj"`
		Device  string `json:"aygit"`
		Size    uint64 `json:"boyut"`
		Confirm string `json:"onay"`
	}
	if err := deskgui.ReadJSON(r, &req); err != nil {
		return nil, err
	}

	// ── Onay: kullanıcı aygıt yolunu ELLE yazdı mı ──────────────────────
	// Konsoldaki kuralın aynısı (main.go confirm): "Evet" düğmesi alışkanlık
	// olur, yolu yazmak hangi diske baktığını görmeyi zorunlu kılar.
	if req.Device == "" || strings.TrimSpace(req.Confirm) != req.Device {
		return nil, deskgui.Fail(http.StatusBadRequest,
			"Onay eşleşmedi — aygıt yolunu tam olarak yazın. Hiçbir şey yazılmadı.")
	}

	// ── İmaj: yalnızca listelenenler ────────────────────────────────────
	list, _ := g.images()
	var img *imageFile
	for i := range list {
		if list[i].path == req.Image {
			img = &list[i]
			break
		}
	}
	if img == nil {
		return nil, deskgui.Fail(http.StatusBadRequest,
			"İmaj listede yok (silinmiş ya da taşınmış olabilir). Listeyi yenileyin.")
	}

	// ── Yönetici hakkı ──────────────────────────────────────────────────
	if !g.cfg.dryRun && !g.elevated() {
		return nil, deskgui.Fail(http.StatusForbidden,
			"USB'ye yazmak yönetici hakkı ister: \"Yönetici olarak yeniden aç\" düğmesine basın.")
	}

	// ── SON DENETİM: aygıt hâlâ aynı mı ─────────────────────────────────
	// Kullanıcı seçim yaparken belleği çıkarıp başkasını takmış olabilir;
	// aynı yol artık BAŞKA bir diski gösterebilir (main.go reverify).
	fresh, err := g.devices()
	if err != nil {
		return nil, err
	}
	var dev *flash.Device
	for i := range fresh {
		if fresh[i].Path == req.Device {
			dev = &fresh[i]
			break
		}
	}
	if dev == nil {
		return nil, deskgui.Fail(http.StatusConflict,
			"%s artık listede yok — çıkarıldı mı? Hiçbir şey yazılmadı.", req.Device)
	}
	if dev.SizeBytes != req.Size {
		return nil, deskgui.Fail(http.StatusConflict,
			"%s değişmiş görünüyor (%s → %s) — bellek çıkarılıp başkası mı takıldı? Hiçbir şey yazılmadı.",
			dev.Path, flash.HumanBytes(req.Size), flash.HumanBytes(dev.SizeBytes))
	}
	if err := flash.Validate(*dev); err != nil {
		return nil, deskgui.Fail(http.StatusBadRequest, "%v", err)
	}
	if img.size > dev.SizeBytes {
		return nil, deskgui.Fail(http.StatusBadRequest, "İmaj (%s) belleğe (%s) sığmıyor.",
			flash.HumanBytes(img.size), flash.HumanBytes(dev.SizeBytes))
	}

	g.mu.Lock()
	if g.writing {
		g.mu.Unlock()
		return nil, deskgui.Fail(http.StatusConflict, "Bir yazma zaten sürüyor.")
	}
	ctx, cancel := context.WithCancel(context.Background())
	g.writing, g.cancel = true, cancel
	now := time.Now()
	g.prog = writeProgress{Active: true, Stage: "hazırlanıyor", Device: dev.Path,
		Image: img.path, Total: img.size, DryRun: g.cfg.dryRun,
		Verify: g.cfg.verify && !g.cfg.dryRun, started: now, stageStart: now}
	g.mu.Unlock()

	w := flash.Writer{ImagePath: img.path, Device: *dev,
		Verify: g.cfg.verify && !g.cfg.dryRun, DryRun: g.cfg.dryRun}
	go func() {
		err := g.write(ctx, w, g.onProgress)
		g.mu.Lock()
		defer g.mu.Unlock()
		g.writing, g.cancel = false, nil
		cancel()
		g.prog.Active = false
		g.prog.Took = took(time.Since(g.prog.started))
		if err != nil {
			g.prog.Err = err.Error()
			if ctx.Err() != nil {
				g.prog.Err = "Durduruldu — yarım yazılmış bir bellek açılmaz, baştan yazın."
			}
			return
		}
		g.prog.OK = true
		g.prog.Percent = 100
	}()
	return map[string]any{"ileti": "Yazma başladı."}, nil
}

// onProgress stores the writer's progress (flash.Writer callback).
func (g *flashGUI) onProgress(p flash.Progress) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if p.Stage != "" && p.Stage != g.prog.Stage {
		// Yeni aşama: hız sıfırlanır; yoksa "yazılıyor" hızı "doğrulanıyor"
		// aşamasına taşınır ve kalan süre yanlış çıkar (progress.go ile aynı).
		g.prog.Stage = p.Stage
		g.prog.stageStart = now
		g.prog.Rate = 0
	}
	g.prog.Done, g.prog.Percent = p.BytesDone, p.Percent()
	if p.BytesTotal > 0 {
		g.prog.Total = p.BytesTotal
	}
	if el := now.Sub(g.prog.stageStart).Seconds(); el > 0.5 && p.BytesDone > 0 {
		g.prog.Rate = float64(p.BytesDone) / el
	}
	g.prog.Remain = remaining(p, g.prog.Rate)
}

// runGUI opens the window and returns when it closes.
func runGUI(cfg guiConfig) error {
	g := newFlashGUI(cfg)
	srv, err := deskgui.New(deskgui.Page{Title: "MCOS USB Kurucu", Body: guiHTML, Script: guiJS})
	if err != nil {
		return err
	}
	g.register(srv)
	srv.Start()
	defer srv.Close()

	_, err = srv.Run(deskgui.RunOptions{
		Title:     "MCOS USB Kurucu",
		Width:     900,
		Height:    760,
		MinWidth:  680,
		MinHeight: 520,
		IconID:    1,
		// Kurucu bir kez kullanılır: WebView2 profili geçici klasörde durur,
		// kullanıcının sisteminde kalıcı iz bırakmaz.
		DataDir:  filepath.Join(os.TempDir(), "mcos-flash-arayuz"),
		Browser:  *guiBrowser,
		NoOpen:   *guiNoOpen,
		Duration: *guiDuration,
		Done:     g.quit,
	})

	// Pencere yazma sürerken kapatıldıysa yazmayı temiz biçimde durdur.
	g.mu.Lock()
	c := g.cancel
	g.mu.Unlock()
	if c != nil {
		c()
		for i := 0; i < 100; i++ {
			g.mu.Lock()
			w := g.writing
			g.mu.Unlock()
			if !w {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	return err
}
