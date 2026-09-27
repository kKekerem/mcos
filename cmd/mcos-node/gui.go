package main

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mcos/internal/cluster"
	"mcos/internal/deskgui"
	"mcos/internal/version"
)

// Bu dosya düğümün PENCERELİ arayüzünü sunar.
//
// ════════════════════════════════════════════════════════════════════════════
// NEDEN
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının isteği: "Windows exe'sini derle, basit bir GUI ekle,
// eşleştirme GUI'si yap." Eskiden çift tıklama siyah bir konsol açıyordu:
// anahtar oraya yapıştırılıyor, durum her saniye yeniden çiziliyordu. Konsol
// çalışıyordu ama Windows kullanıcısı için yabancıydı ve "eşleşti mi,
// sunucu nerede, hangi bölge bende" sorularının yanıtı ekranda yoktu.
//
// ════════════════════════════════════════════════════════════════════════════
// PENCERE DÜĞÜMÜN KENDİSİ DEĞİL, DENETLEYİCİSİDİR
// ════════════════════════════════════════════════════════════════════════════
//
// Düğüm bir SERVİS gibi çalışır (--arkaplan: oturum açılışında penceresiz).
// Pencere o servisi YÖNETİR:
//
//   - durumu, servisin zaten yazdığı durum.json'dan okur (status.go),
//   - durdurmayı, servisin zaten izlediği durdur.istek dosyasıyla ister,
//   - başlatmayı startBackground ile yapar (konsol akışıyla AYNI yol).
//
// Böylece pencereyi kapatmak düğümü durdurmaz ve iki süreç arasında yeni bir
// ağ kanalı açılmaz. Arka planda başlatılamazsa (Linux'ta systemd birimi yok)
// düğüm bu süreçte "gömülü" çalışır ve arayüz bunu açıkça söyler: o durumda
// pencereyi kapatmak düğümü durdurur.

//go:embed gui.html
var guiHTML string

//go:embed gui.js
var guiJS string

//go:embed gui.css
var guiCSS string

// statusFresh: durum.json bundan eskiyse düğüm çalışmıyor sayılır.
//
// Düğüm dosyayı 2 saniyede bir yeniler (statusLoop). requestStop ile aynı
// eşik: iki yer "çalışıyor" konusunda farklı karar vermesin.
const statusFresh = 10 * time.Second

// guiPlatform is everything the window does to the machine.
//
// NEDEN ARAYÜZ: sınamalar gerçek bir düğüm süreci başlatmamalı, kayıt
// defterine (oturum açılışı) yazmamalı, güvenlik duvarı için UAC
// istememeli. Üretimde her zaman realPlatform'dur.
type guiPlatform interface {
	setupNeeded(s nodeSettings) bool
	runSetup(dataRoot string, port int) (string, error)
	installedExe() string
	selfExe() string
	startBackground(exe string, args []string) error
	autostart() autostartState
	setAutostart(dataRoot string, port int, on bool) error
	openFolder(path string) error
}

// autostartState is the "start at logon" switch as the window shows it.
type autostartState struct {
	Supported bool   `json:"destek"`
	Enabled   bool   `json:"acik"`
	Detail    string `json:"not,omitempty"`
}

type realPlatform struct{}

func (realPlatform) setupNeeded(s nodeSettings) bool { return setupNeeded(s) }
func (realPlatform) runSetup(d string, p int) (string, error) {
	return runSetup(d, p)
}
func (realPlatform) installedExe() string { return installedExe() }
func (realPlatform) selfExe() string {
	exe, _ := os.Executable()
	return exe
}
func (realPlatform) startBackground(exe string, args []string) error {
	return startBackground(exe, args)
}
func (realPlatform) autostart() autostartState { return autostartStatus() }
func (realPlatform) setAutostart(d string, p int, on bool) error {
	return setAutostart(d, p, on)
}
func (realPlatform) openFolder(path string) error { return openFolder(path) }

// nodeGUI is the controller behind the window.
type nodeGUI struct {
	o    options
	plat guiPlatform

	// serveFn runs the node in THIS process (gömülü kip). Sınamalar
	// değiştirir; üretimde serve'dür.
	serveFn func(o options, s nodeSettings) error
	// waitFor bounds how long a start waits for the node to answer.
	waitFor time.Duration

	mu       sync.Mutex
	busy     string // sürmekte olan iş; boşsa yok
	lastErr  string
	lastInfo string
	embedded bool
	embedEnd chan struct{} // gömülü düğüm bitince kapanır
}

func newNodeGUI(o options) *nodeGUI {
	return &nodeGUI{o: o, plat: realPlatform{}, serveFn: serve, waitFor: 15 * time.Second}
}

// ── Durum ───────────────────────────────────────────────────────────────────

// guiState is what /api/durum returns.
type guiState struct {
	Version      string          `json:"surum"`
	Name         string          `json:"ad"`
	Addr         string          `json:"adres"`
	Port         int             `json:"port"`
	DataRoot     string          `json:"veri"`
	LogPath      string          `json:"gunluk"`
	KeySet       bool            `json:"anahtarVar"`
	KeyHint      string          `json:"anahtarIpucu,omitempty"`
	Running      bool            `json:"calisiyor"`
	Embedded     bool            `json:"gomulu"`
	Node         *nodeStatusFile `json:"dugum,omitempty"`
	Busy         string          `json:"islem,omitempty"`
	Err          string          `json:"hata,omitempty"`
	Info         string          `json:"bilgi,omitempty"`
	Autostart    autostartState  `json:"otobaslat"`
	Portable     bool            `json:"tasinabilir"`
	VersionDrift string          `json:"surumFarki,omitempty"`
}

// running reports whether a node serves this data folder right now.
func (g *nodeGUI) running() (nodeStatusFile, bool) {
	st, ok := readStatus(g.o.dataRoot)
	if !ok || time.Since(st.Updated) > statusFresh {
		return st, false
	}
	return st, true
}

func (g *nodeGUI) state() guiState {
	s, _ := loadSettings(g.o.dataRoot)
	name := s.Name
	if name == "" {
		name = nodeName()
	}
	addr := cluster.PrimaryIPv4()
	if ip := net.ParseIP(g.o.bind); ip != nil && !ip.IsUnspecified() {
		addr = g.o.bind
	}
	out := guiState{
		Version:  version.Display(),
		Name:     name,
		Addr:     addr,
		Port:     g.o.port,
		DataRoot: g.o.dataRoot,
		LogPath:  filepath.Join(g.o.dataRoot, "mcos-node.log"),
		KeySet:   s.Key != "",
		KeyHint:  keyHint(s.Key),
		Portable: g.o.noSetup,
	}
	if st, ok := g.running(); ok {
		out.Running = true
		out.Node = &st
		if st.Version != "" && st.Version != version.Display() {
			out.VersionDrift = st.Version
		}
	}
	out.Autostart = g.plat.autostart()
	if g.o.noSetup {
		out.Autostart.Detail = "Taşınabilir kip (--kurma): oturum açılışı kaydı değiştirilmez."
	}
	g.mu.Lock()
	out.Busy, out.Err, out.Info, out.Embedded = g.busy, g.lastErr, g.lastInfo, g.embedded
	g.mu.Unlock()
	return out
}

// keyHint shows enough of the key to recognise it, not enough to copy it.
//
// Anahtar bir sırdır; pencerenin ekran görüntüsü paylaşılırsa (destek
// isterken sık olur) tamamı görünmemeli.
func keyHint(k string) string {
	r := []rune(k)
	if len(r) == 0 {
		return ""
	}
	if len(r) <= 6 {
		return strings.Repeat("•", len(r))
	}
	return string(r[:4]) + strings.Repeat("•", 6) + string(r[len(r)-2:])
}

// ── İş yönetimi ─────────────────────────────────────────────────────────────

// begin claims the single "operation" slot.
//
// Tek iş: "Başlat"a iki kez basmak iki düğüm başlatmaya, başlatma sürerken
// "Durdur"a basmak yarım kalmış bir durdurmaya yol açıyordu.
func (g *nodeGUI) begin(what string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.busy != "" {
		return deskgui.Fail(http.StatusConflict, "Başka bir işlem sürüyor: %s", g.busy)
	}
	g.busy, g.lastErr, g.lastInfo = what, "", ""
	return nil
}

func (g *nodeGUI) progress(what string) {
	g.mu.Lock()
	g.busy = what
	g.mu.Unlock()
}

func (g *nodeGUI) end(info string, err error) {
	g.mu.Lock()
	g.busy = ""
	if err != nil {
		g.lastErr = err.Error()
	} else {
		g.lastInfo = info
	}
	g.mu.Unlock()
	if err != nil {
		appendLog(g.o.dataRoot, "arayüz: "+err.Error())
	}
}

// startAsync starts (or restarts) the node without blocking the request.
//
// Kurulum UAC bekler, arka plan kopyası 15 saniyeye kadar yanıt vermeyebilir;
// HTTP isteği bunları beklerse pencere donmuş görünür.
func (g *nodeGUI) startAsync(restart, reinstall bool) error {
	// Anahtar ŞART DEĞİL: anahtarsız düğüm ağda görünür ve MCOS'un kodla
	// eşleştirme teklifini bekler (pairing.go). Eskiden burada "önce
	// anahtarı girin" deniyordu ve düğüm hiç başlamıyordu.
	if _, err := loadSettings(g.o.dataRoot); err != nil {
		return err
	}
	what := "Düğüm başlatılıyor…"
	if restart {
		what = "Düğüm yeniden başlatılıyor…"
	}
	if err := g.begin(what); err != nil {
		return err
	}
	go func() {
		if restart {
			if err := g.stopAndWait(); err != nil {
				g.end("", err)
				return
			}
		}
		info, err := g.startNode(reinstall)
		g.end(info, err)
	}()
	return nil
}

// startNode does what a double-click used to do, minus the console.
func (g *nodeGUI) startNode(reinstall bool) (string, error) {
	s, err := loadSettings(g.o.dataRoot)
	if err != nil {
		return "", err
	}
	var warn string
	// İlk kurulum (Windows): program kopyası, güvenlik duvarı (bir kez UAC),
	// oturum açılışı kaydı. Konsol akışındaki VARSAYILANLAR aynen korunur;
	// --kurma bunu atlar.
	if !g.o.noSetup && (reinstall || g.plat.setupNeeded(s)) {
		g.progress("Kurulum yapılıyor — güvenlik duvarı için bir kez yönetici izni istenebilir…")
		exe, serr := g.plat.runSetup(g.o.dataRoot, g.o.port)
		if serr != nil {
			warn = "Kurulum eksik kaldı: " + serr.Error()
			appendLog(g.o.dataRoot, "kurulum: "+serr.Error())
		}
		if exe != "" {
			s.SetupDone = true
			_ = saveSettings(g.o.dataRoot, s)
		}
	}

	// Taşınabilir kipte KURULU kopya kullanılmaz: kullanıcının başka bir
	// yerde kurulu (belki eski) bir düğümü, bu klasörün verisiyle
	// başlatılırdı.
	exe := g.plat.installedExe()
	if g.o.noSetup {
		exe = g.plat.selfExe()
	}
	g.progress("Düğüm arka planda başlatılıyor…")
	if err := g.plat.startBackground(exe, backgroundArgs(g.o)); err == nil && g.waitRunning(g.waitFor) {
		return joinInfo("Düğüm arka planda çalışıyor.", warn), nil
	} else if err != nil {
		appendLog(g.o.dataRoot, "arayüz: arka planda başlatılamadı: "+err.Error())
	}

	// Arka plan yok (Linux'ta systemd birimi kurulmamış olabilir): konsol
	// akışındaki gibi düğüm bu süreçte çalışır.
	g.progress("Arka planda başlatılamadı; düğüm bu pencerede başlatılıyor…")
	if err := g.startEmbedded(s); err != nil {
		return "", err
	}
	if !g.waitRunning(g.waitFor) {
		return "", errors.New("düğüm başlatılamadı — ayrıntı için günlüğe bakın")
	}
	return joinInfo("Düğüm bu pencerede çalışıyor; pencereyi kapatınca durur.", warn), nil
}

func joinInfo(info, warn string) string {
	if warn == "" {
		return info
	}
	return info + " " + warn
}

func (g *nodeGUI) waitRunning(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if _, ok := g.running(); ok {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// startEmbedded runs the node inside this process.
func (g *nodeGUI) startEmbedded(s nodeSettings) error {
	g.mu.Lock()
	if g.embedded {
		g.mu.Unlock()
		return nil
	}
	g.embedded = true
	done := make(chan struct{})
	g.embedEnd = done
	g.mu.Unlock()

	eo := g.o
	// background: serve() konsol durum ekranını ÇİZMESİN; pencere zaten
	// durum.json'dan okuyor.
	eo.background, eo.quiet = true, false
	go func() {
		defer close(done)
		err := g.serveFn(eo, s)
		g.mu.Lock()
		g.embedded = false
		if err != nil {
			g.lastErr = "Düğüm durdu: " + err.Error()
		}
		g.mu.Unlock()
	}()
	return nil
}

// stopAndWait asks the node to stop and waits until it has saved and gone.
func (g *nodeGUI) stopAndWait() error {
	if _, ok := g.running(); !ok {
		return nil
	}
	if err := os.WriteFile(filepath.Join(g.o.dataRoot, stopRequestFile), []byte("dur\n"), 0o644); err != nil {
		return fmt.Errorf("durdurma isteği yazılamadı: %w", err)
	}
	g.progress("Düğüm durduruluyor — sunucu dünyayı kaydediyor…")
	// Düğüm, sunucular DURDUKTAN SONRA durum.json'u siler (main.go serve):
	// dosyanın kaybolması "dünya kaydedildi" demektir.
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := readStatus(g.o.dataRoot); !ok {
			g.waitEmbedded(5 * time.Second)
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return errors.New("düğüm 2 dakikada durmadı — günlüğe bakın")
}

func (g *nodeGUI) waitEmbedded(d time.Duration) {
	g.mu.Lock()
	ch := g.embedEnd
	emb := g.embedded
	g.mu.Unlock()
	if !emb || ch == nil {
		return
	}
	select {
	case <-ch:
	case <-time.After(d):
	}
}

// shutdown runs when the window closes.
//
// Arka plandaki düğüme DOKUNULMAZ (bilinçli: servis olarak kuruldu). Gömülü
// düğüm ise bu süreçle birlikte ölür; önce dünyasını kaydetmesi beklenir.
func (g *nodeGUI) shutdown() {
	g.mu.Lock()
	emb := g.embedded
	g.mu.Unlock()
	if !emb {
		return
	}
	_ = os.WriteFile(filepath.Join(g.o.dataRoot, stopRequestFile), []byte("dur\n"), 0o644)
	g.waitEmbedded(120 * time.Second)
}

// ── API ─────────────────────────────────────────────────────────────────────

func (g *nodeGUI) register(s *deskgui.Server) {
	s.Get("/api/durum", func(*http.Request) (any, error) { return g.state(), nil })
	s.Get("/api/gunluk", func(*http.Request) (any, error) {
		return map[string]any{"satirlar": tailLines(filepath.Join(g.o.dataRoot, "mcos-node.log"), 200)}, nil
	})
	s.Post("/api/anahtar", g.handleKey)
	s.Post("/api/karar", g.handleDecision)
	s.Post("/api/baslat", func(*http.Request) (any, error) {
		if _, ok := g.running(); ok {
			return map[string]any{"ileti": "Düğüm zaten çalışıyor."}, nil
		}
		return nil, g.startAsync(false, false)
	})
	s.Post("/api/guncelle", func(*http.Request) (any, error) {
		_, ok := g.running()
		return nil, g.startAsync(ok, true)
	})
	s.Post("/api/durdur", func(*http.Request) (any, error) {
		if _, ok := g.running(); !ok {
			return map[string]any{"ileti": "Düğüm zaten durmuş."}, nil
		}
		if err := g.begin("Düğüm durduruluyor…"); err != nil {
			return nil, err
		}
		go func() {
			err := g.stopAndWait()
			g.end("Düğüm durduruldu.", err)
		}()
		return nil, nil
	})
	s.Post("/api/otobaslat", func(r *http.Request) (any, error) {
		var req struct {
			On *bool `json:"acik"`
		}
		if err := deskgui.ReadJSON(r, &req); err != nil {
			return nil, err
		}
		if req.On == nil {
			return nil, deskgui.Fail(http.StatusBadRequest, "\"acik\" alanı eksik")
		}
		if g.o.noSetup {
			return nil, deskgui.Fail(http.StatusConflict,
				"Taşınabilir kipte (--kurma) oturum açılışı kaydı değiştirilmez.")
		}
		if st := g.plat.autostart(); !st.Supported {
			return nil, deskgui.Fail(http.StatusConflict, "%s", st.Detail)
		}
		if err := g.plat.setAutostart(g.o.dataRoot, g.o.port, *req.On); err != nil {
			return nil, fmt.Errorf("oturum açılışı ayarlanamadı: %w", err)
		}
		return g.plat.autostart(), nil
	})
	s.Post("/api/klasor", func(*http.Request) (any, error) {
		if err := g.plat.openFolder(g.o.dataRoot); err != nil {
			return nil, fmt.Errorf("klasör açılamadı: %w", err)
		}
		return nil, nil
	})
}

// handleKey saves the pairing key (and name) and (re)starts the node.
func (g *nodeGUI) handleKey(r *http.Request) (any, error) {
	var req struct {
		Key  string `json:"anahtar"`
		Name string `json:"ad"`
	}
	if err := deskgui.ReadJSON(r, &req); err != nil {
		return nil, err
	}
	key := strings.TrimSpace(req.Key)
	name := strings.TrimSpace(req.Name)
	if key == "" && name == "" {
		return nil, deskgui.Fail(http.StatusBadRequest, "Anahtar boş.")
	}
	// Konsol akışıyla aynı sınır (main.go run): kısa bir anahtar hemen
	// hiçbir zaman gerçek anahtar değildir, yarım kopyalanmıştır.
	if key != "" && len(key) < 8 {
		return nil, deskgui.Fail(http.StatusBadRequest,
			"Anahtar çok kısa — MCOS'taki anahtarın TAMAMINI yapıştırın.")
	}
	s, err := loadSettings(g.o.dataRoot)
	if err != nil {
		return nil, err
	}
	changed := false
	if key != "" && key != s.Key {
		s.Key, changed = key, true
	}
	if name != "" && name != s.Name {
		s.Name, changed = name, true
	}
	if !changed {
		return map[string]any{"ileti": "Ayarlar zaten böyle."}, nil
	}
	if err := saveSettings(g.o.dataRoot, s); err != nil {
		return nil, err
	}
	// Çalışan düğüm eski anahtarla sürerdi: konsol akışında yakalanan hata
	// (main.go run, "yeni anahtarı sessizce YOK SAYIYORDUK") burada da
	// geçerli. Yeniden başlatılır.
	_, running := g.running()
	if err := g.startAsync(running, false); err != nil {
		return nil, err
	}
	return map[string]any{"ileti": "Kaydedildi."}, nil
}

// handleDecision is the "Kabul et" / "Reddet" button of an offer card.
//
// Karar, çalışan düğüme karar dosyasıyla gider (pairing.go). Pencere
// kararın ALINDIĞINI da bekler (dosya silinir): düğüm yanıt vermiyorsa
// kullanıcı "bastım, bir şey olmadı" yerine nedeni görür.
func (g *nodeGUI) handleDecision(r *http.Request) (any, error) {
	var req struct {
		ID     string `json:"id"`
		Code   string `json:"kod"`
		Accept *bool  `json:"kabul"`
	}
	if err := deskgui.ReadJSON(r, &req); err != nil {
		return nil, err
	}
	if req.Accept == nil || !validOfferID(req.ID) {
		return nil, deskgui.Fail(http.StatusBadRequest, "Geçersiz istek.")
	}
	st, ok := g.running()
	if !ok {
		return nil, deskgui.Fail(http.StatusConflict, "Düğüm çalışmıyor; istek düştü.")
	}
	o, found := findOfferByID(st.Offers, req.ID)
	if !found {
		return nil, deskgui.Fail(http.StatusGone,
			"Bu eşleştirme isteğinin süresi doldu — MCOS'ta yeniden eşleştirin.")
	}
	// Kullanıcının GÖRDÜĞÜ kod, düğümdeki kodla aynı olmalı: kart eski
	// bir durumla çizildiyse (teklif yenilendi) yanlış teklif onaylanmasın.
	if digitsOnly(o.Code) != digitsOnly(req.Code) {
		return nil, deskgui.Fail(http.StatusConflict,
			"Kod değişti — ekrandaki yeni kodu MCOS'takiyle karşılaştırın.")
	}
	if err := writePairDecision(g.o.dataRoot, o.ID, o.Code, *req.Accept); err != nil {
		return nil, err
	}
	path := filepath.Join(g.o.dataRoot, decisionFileName(o.ID))
	deadline := time.Now().Add(g.decisionWait())
	for {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			return nil, deskgui.Fail(http.StatusGatewayTimeout,
				"Düğüm kararı almadı — yanıt vermiyor olabilir, günlüğe bakın.")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !*req.Accept {
		return map[string]any{"ileti": o.Name + " isteği reddedildi."}, nil
	}
	return map[string]any{"ileti": "Kabul edildi. Şimdi MCOS ekranında \"Kodlar aynı, onayla\"ya basın."}, nil
}

func (g *nodeGUI) decisionWait() time.Duration {
	if g.waitFor > 0 && g.waitFor < 5*time.Second {
		return g.waitFor
	}
	return 5 * time.Second
}

// tailLines returns the last n lines of a file (reads at most 64 KB).
func tailLines(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	const maxRead = 64 << 10
	if st, err := f.Stat(); err == nil && st.Size() > maxRead {
		_, _ = f.Seek(st.Size()-maxRead, io.SeekStart)
	}
	b, _ := io.ReadAll(io.LimitReader(f, maxRead))
	b = bytes.TrimRight(b, "\r\n")
	if len(b) == 0 {
		return []string{}
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// ── Giriş noktası ───────────────────────────────────────────────────────────

// wantGUI decides between the window and the console screen.
//
// Windows'ta çift tıklama PENCERE açar (kullanıcının isteği). Konsola özgü
// her bayrak konsolda kalır; --konsol eski ekranı zorlar. Linux'ta pencere
// yalnızca --gui ile açılır (orada düğüm çoğunlukla systemd ile ve
// terminalden yönetiliyor).
func wantGUI(o options, goos string) bool {
	if o.console || o.quiet || o.background || o.stop || o.remove || o.showVer ||
		o.saveOnly || o.firewall != "" || o.duration > 0 || o.accept != "" || o.reject != "" {
		return false
	}
	return o.gui || goos == "windows"
}

// runGUI opens the window and returns when it closes.
func runGUI(o options) error {
	if o.name != "" {
		nameOnce.Do(func() { nameVal = o.name })
	}
	if err := os.MkdirAll(o.dataRoot, 0o755); err != nil {
		return fmt.Errorf("veri klasörü oluşturulamadı (%s): %w", o.dataRoot, err)
	}
	g := newNodeGUI(o)

	// Komut satırından gelen ayarlar (--key/--name/--ram/--cpu) konsol
	// akışındaki gibi kaydedilir.
	changed, err := applyFlagSettings(o)
	if err != nil {
		return err
	}

	srv, err := deskgui.New(deskgui.Page{Title: "MCOS Düğüm", Body: guiHTML, Script: guiJS, Style: guiCSS})
	if err != nil {
		return err
	}
	g.register(srv)
	srv.Start()
	defer srv.Close()

	// Açılışta: düğüm çalışmıyorsa başlat — anahtar olsun olmasın. Çift
	// tıklamanın anlamı "düğümü çalıştır"; anahtarsız düğüm de ağda görünür
	// ve MCOS'un kodla eşleştirme isteğini bekler. Eskiden anahtar yoksa
	// hiçbir şey başlamıyordu ve MCOS taramada bu PC'yi bulamıyordu. Ayar
	// değiştiyse çalışan düğüm yeniden başlatılır.
	if _, running := g.running(); !running || changed {
		_ = g.startAsync(running, false)
	}

	_, err = srv.Run(deskgui.RunOptions{
		Title:     "MCOS Düğüm",
		Width:     980,
		Height:    760,
		MinWidth:  700,
		MinHeight: 520,
		IconID:    1,
		DataDir:   filepath.Join(o.dataRoot, "arayuz"),
		Browser:   o.browser,
		NoOpen:    o.noOpen,
		Duration:  o.guiDuration,
	})
	g.shutdown()
	return err
}

// applyFlagSettings saves --key/--name/--ram/--cpu; reports whether any changed.
func applyFlagSettings(o options) (bool, error) {
	if o.key == "" && o.name == "" && o.ramMB <= 0 && o.cpuPct <= 0 {
		return false, nil
	}
	s, err := loadSettings(o.dataRoot)
	if err != nil {
		return false, err
	}
	before := s
	if k := strings.TrimSpace(o.key); k != "" {
		if len(k) < 8 {
			return false, fmt.Errorf("eşleştirme anahtarı çok kısa — MCOS panelindeki " +
				"MCOS Paylaşım ekranından tam anahtarı kopyalayın")
		}
		s.Key = k
	}
	if o.name != "" {
		s.Name = o.name
	}
	if o.ramMB > 0 {
		s.RAMMB = o.ramMB
	}
	if o.cpuPct > 0 {
		s.CPUPercent = o.cpuPct
	}
	if s == before {
		return false, nil
	}
	return true, saveSettings(o.dataRoot, s)
}
