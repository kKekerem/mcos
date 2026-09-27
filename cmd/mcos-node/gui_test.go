package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"mcos/internal/deskgui"
	"mcos/internal/model"
	"mcos/internal/version"
)

// ════════════════════════════════════════════════════════════════════════════
// PENCERE, DÜĞÜMÜ KONSOL AKIŞIYLA AYNI KURALLARLA YÖNETMELİ
// ════════════════════════════════════════════════════════════════════════════
//
// Pencere yeni bir yol açtı: anahtar artık bir HTTP isteğiyle kaydediliyor ve
// düğüm bir düğmeyle başlatılıyor. Sınamalar gerçek süreç başlatmaz, kayıt
// defterine yazmaz, UAC istemez: platform sahte (fakePlat), "arka plandaki
// düğüm" ise durum.json yazan bir işlevdir — gerçek düğümün pencereye
// gösterdiği tek yüz de zaten o dosyadır.

type fakePlat struct {
	mu         sync.Mutex
	dataRoot   string
	setupNeed  bool
	setupCalls int
	startExe   []string
	startArgs  [][]string
	startErr   error
	auto       autostartState
	autoSet    []bool
	opened     []string
	noStatus   bool // başlatılan "düğüm" durum yazmasın (yanıt vermeyen düğüm)
}

func (f *fakePlat) setupNeeded(s nodeSettings) bool { return f.setupNeed && !s.SetupDone }
func (f *fakePlat) runSetup(string, int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setupCalls++
	return `C:\kurulu\mcos-node.exe`, nil
}
func (f *fakePlat) installedExe() string { return `C:\kurulu\mcos-node.exe` }
func (f *fakePlat) selfExe() string      { return `C:\indirilenler\mcos-node.exe` }
func (f *fakePlat) startBackground(exe string, args []string) error {
	f.mu.Lock()
	f.startExe = append(f.startExe, exe)
	f.startArgs = append(f.startArgs, append([]string(nil), args...))
	err, quiet := f.startErr, f.noStatus
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if !quiet {
		writeFreshStatus(f.dataRoot, version.Display())
	}
	return nil
}
func (f *fakePlat) autostart() autostartState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.auto
}
func (f *fakePlat) setAutostart(_ string, _ int, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.autoSet = append(f.autoSet, on)
	f.auto.Enabled = on
	return nil
}
func (f *fakePlat) openFolder(p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, p)
	return nil
}

func writeFreshStatus(dataRoot, ver string) {
	writeStatus(dataRoot, nodeStatusFile{PID: 1, Name: "sınama", Version: ver, Updated: time.Now(),
		Note: "eşleştirme bekleniyor"})
}

type guiRig struct {
	g     *nodeGUI
	f     *fakePlat
	base  string
	token string
	o     options
}

func newGUIRig(t *testing.T, portable bool) *guiRig {
	t.Helper()
	dir := t.TempDir()
	o := options{dataRoot: dir, port: 22422, bind: "127.0.0.1", noSetup: portable}
	f := &fakePlat{dataRoot: dir, auto: autostartState{Supported: true}}
	g := newNodeGUI(o)
	g.plat = f
	g.waitFor = 3 * time.Second
	g.serveFn = func(options, nodeSettings) error {
		t.Error("gömülü düğüm beklenmedik biçimde başlatıldı")
		return nil
	}
	srv, err := deskgui.New(deskgui.Page{Title: "sınama"})
	if err != nil {
		t.Fatal(err)
	}
	g.register(srv)
	srv.Start()
	t.Cleanup(srv.Close)
	base := strings.SplitN(srv.URL(), "/?", 2)[0]
	return &guiRig{g: g, f: f, base: base, token: srv.Token(), o: o}
}

func (r *guiRig) call(t *testing.T, method, path, body string, withToken bool) (int, map[string]any, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, r.base+path, rd)
	if withToken {
		req.Header.Set(deskgui.TokenHeader, r.token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var v map[string]any
	_ = json.Unmarshal(raw, &v)
	return resp.StatusCode, v, string(raw)
}

// idle waits until the controller has no operation running.
func (r *guiRig) idle(t *testing.T) guiState {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st := r.g.state()
		if st.Busy == "" {
			return st
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("işlem bitmedi: %q", r.g.state().Busy)
	return guiState{}
}

func TestGUIBelirtecsizAnahtarYazilamaz(t *testing.T) {
	r := newGUIRig(t, true)
	code, _, _ := r.call(t, http.MethodPost, "/api/anahtar", `{"anahtar":"cok-gizli-anahtar-1"}`, false)
	if code != http.StatusForbidden {
		t.Fatalf("belirteçsiz anahtar yazma: kod %d, 403 bekleniyordu", code)
	}
	if _, err := os.Stat(settingsPath(r.o.dataRoot)); !os.IsNotExist(err) {
		t.Fatal("belirteçsiz istek ayar dosyasını oluşturdu")
	}
	if len(r.f.startExe) != 0 {
		t.Fatal("belirteçsiz istek düğüm başlattı")
	}
}

func TestGUIKisaAnahtarReddedilir(t *testing.T) {
	r := newGUIRig(t, true)
	code, v, _ := r.call(t, http.MethodPost, "/api/anahtar", `{"anahtar":"  abc  "}`, true)
	if code != http.StatusBadRequest || !strings.Contains(v["hata"].(string), "kısa") {
		t.Fatalf("kısa anahtar: kod %d yanıt %v", code, v)
	}
	if s, _ := loadSettings(r.o.dataRoot); s.Key != "" {
		t.Fatalf("kısa anahtar kaydedildi: %q", s.Key)
	}
	if len(r.f.startExe) != 0 {
		t.Fatal("kısa anahtarla düğüm başlatıldı")
	}
}

// Taşınabilir kip (--kurma): kurulum YOK, başlatılan program BU program.
// Kurulu kopya kullanılsaydı kullanıcının başka yerde kurulu (belki eski)
// düğümü bu klasörün verisiyle başlardı.
func TestGUIAnahtarKaydedilirVeBaslatirTasinabilir(t *testing.T) {
	r := newGUIRig(t, true)
	code, v, _ := r.call(t, http.MethodPost, "/api/anahtar", `{"anahtar":" yeni-anahtar-123 ","ad":"Salon PC"}`, true)
	if code != http.StatusOK {
		t.Fatalf("kod %d: %v", code, v)
	}
	st := r.idle(t)
	if st.Err != "" {
		t.Fatalf("hata: %s", st.Err)
	}
	s, _ := loadSettings(r.o.dataRoot)
	if s.Key != "yeni-anahtar-123" || s.Name != "Salon PC" {
		t.Fatalf("ayar kaydedilmedi/kırpılmadı: %+v", s)
	}
	if r.f.setupCalls != 0 {
		t.Fatal("taşınabilir kipte kurulum yapıldı")
	}
	if len(r.f.startExe) != 1 || r.f.startExe[0] != r.f.selfExe() {
		t.Fatalf("başlatılan program %v; bu program (%s) olmalıydı", r.f.startExe, r.f.selfExe())
	}
	if !reflect.DeepEqual(r.f.startArgs[0], backgroundArgs(r.o)) {
		t.Fatalf("arka plan bayrakları %v, beklenen %v", r.f.startArgs[0], backgroundArgs(r.o))
	}
	if !st.Running || st.Embedded {
		t.Fatalf("durum: çalışıyor=%v gömülü=%v", st.Running, st.Embedded)
	}
}

// Kurulu kipte ilk başlatma, konsol akışındaki kurulumu (program kopyası,
// güvenlik duvarı, oturum açılışı) BİR KEZ yapar ve kurulu kopyayı başlatır.
func TestGUIIlkKurulumBirKez(t *testing.T) {
	r := newGUIRig(t, false)
	r.f.setupNeed = true
	if code, v, _ := r.call(t, http.MethodPost, "/api/anahtar", `{"anahtar":"yeni-anahtar-123"}`, true); code != http.StatusOK {
		t.Fatalf("kod %d: %v", code, v)
	}
	r.idle(t)
	if r.f.setupCalls != 1 {
		t.Fatalf("kurulum %d kez yapıldı, 1 bekleniyordu", r.f.setupCalls)
	}
	if s, _ := loadSettings(r.o.dataRoot); !s.SetupDone {
		t.Fatal("SetupDone kaydedilmedi: her açılışta UAC sorulurdu")
	}
	if r.f.startExe[0] != r.f.installedExe() {
		t.Fatalf("başlatılan %s; kurulu kopya olmalıydı", r.f.startExe[0])
	}

	// Düğüm durur, yeniden başlatılır: kurulum TEKRARLANMAZ.
	_ = os.Remove(statusPath(r.o.dataRoot))
	if code, v, _ := r.call(t, http.MethodPost, "/api/baslat", `{}`, true); code != http.StatusOK {
		t.Fatalf("başlat: %d %v", code, v)
	}
	r.idle(t)
	if r.f.setupCalls != 1 {
		t.Fatalf("ikinci başlatmada kurulum yeniden yapıldı (%d)", r.f.setupCalls)
	}
}

// Anahtar değişince ÇALIŞAN düğüm yeniden başlatılmalı: konsol akışında
// yakalanan hata (yeni anahtar sessizce yok sayılıyordu) pencerede de
// tekrarlanmasın.
func TestGUIAnahtarDegisinceYenidenBaslatir(t *testing.T) {
	r := newGUIRig(t, true)
	_ = saveSettings(r.o.dataRoot, nodeSettings{Key: "eski-anahtar-123", Name: "PC"})
	writeFreshStatus(r.o.dataRoot, version.Display())
	// "Düğüm" durdurma isteğini görünce durum dosyasını silsin.
	stopSeen := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			if _, err := os.Stat(filepath.Join(r.o.dataRoot, stopRequestFile)); err == nil {
				_ = os.Remove(filepath.Join(r.o.dataRoot, stopRequestFile))
				_ = os.Remove(statusPath(r.o.dataRoot))
				close(stopSeen)
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
	}()
	if code, v, _ := r.call(t, http.MethodPost, "/api/anahtar", `{"anahtar":"yeni-anahtar-456"}`, true); code != http.StatusOK {
		t.Fatalf("kod %d: %v", code, v)
	}
	select {
	case <-stopSeen:
	case <-time.After(5 * time.Second):
		t.Fatal("çalışan düğüme durdurma isteği gönderilmedi (eski anahtarla sürerdi)")
	}
	st := r.idle(t)
	if len(r.f.startExe) != 1 || !st.Running {
		t.Fatalf("düğüm yeniden başlatılmadı: başlatma=%d çalışıyor=%v hata=%q", len(r.f.startExe), st.Running, st.Err)
	}
}

// Arka planda başlatılamazsa (Linux'ta systemd birimi yok) düğüm BU
// süreçte çalışır ve durum bunu söyler; pencere kapanınca dünyasını kaydedip
// durur.
func TestGUIGomuluYedek(t *testing.T) {
	r := newGUIRig(t, true)
	r.f.startErr = errors.New("mcos-node.service kurulu değil")
	_ = saveSettings(r.o.dataRoot, nodeSettings{Key: "anahtar-12345678"})

	served := make(chan options, 1)
	returned := make(chan struct{})
	r.g.serveFn = func(o options, s nodeSettings) error {
		served <- o
		defer close(returned)
		writeFreshStatus(o.dataRoot, version.Display())
		for {
			if _, err := os.Stat(filepath.Join(o.dataRoot, stopRequestFile)); err == nil {
				_ = os.Remove(statusPath(o.dataRoot))
				return nil
			}
			writeFreshStatus(o.dataRoot, version.Display())
			time.Sleep(20 * time.Millisecond)
		}
	}
	if code, v, _ := r.call(t, http.MethodPost, "/api/baslat", `{}`, true); code != http.StatusOK {
		t.Fatalf("başlat: %d %v", code, v)
	}
	var eo options
	select {
	case eo = <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("arka plan başarısız olunca gömülü düğüm başlatılmadı")
	}
	if !eo.background || eo.quiet {
		t.Fatalf("gömülü düğüm konsol ekranı çizecek bayraklarla başladı: background=%v quiet=%v", eo.background, eo.quiet)
	}
	st := r.idle(t)
	if !st.Embedded || !st.Running {
		t.Fatalf("durum gömülü=%v çalışıyor=%v olmalıydı (hata %q)", st.Embedded, st.Running, st.Err)
	}
	r.g.shutdown()
	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		t.Fatal("pencere kapanırken gömülü düğüm durdurulmadı")
	}
}

// Anahtar bir sırdır: durum yanıtında (ve dolayısıyla ekran görüntüsünde)
// tamamı görünmemeli.
func TestGUIDurumAnahtariSizdirmaz(t *testing.T) {
	r := newGUIRig(t, true)
	const key = "gizli-anahtar-987654"
	_ = saveSettings(r.o.dataRoot, nodeSettings{Key: key, Name: "PC"})
	code, v, raw := r.call(t, http.MethodGet, "/api/durum", "", true)
	if code != http.StatusOK {
		t.Fatalf("kod %d", code)
	}
	if strings.Contains(raw, key) {
		t.Fatalf("durum yanıtı anahtarın tamamını içeriyor: %s", raw)
	}
	if v["anahtarVar"] != true || v["anahtarIpucu"] != "gizl••••••54" {
		t.Fatalf("anahtar ipucu beklenmedik: var=%v ipucu=%v", v["anahtarVar"], v["anahtarIpucu"])
	}
}

func TestGUICalisiyorTazelige(t *testing.T) {
	r := newGUIRig(t, true)
	writeFreshStatus(r.o.dataRoot, "v0.9.0")
	st := r.g.state()
	if !st.Running || st.Node == nil {
		t.Fatal("taze durum.json çalışıyor sayılmadı")
	}
	if st.VersionDrift != "v0.9.0" {
		t.Fatalf("sürüm farkı gösterilmedi: %q", st.VersionDrift)
	}
	writeStatus(r.o.dataRoot, nodeStatusFile{Updated: time.Now().Add(-30 * time.Second)})
	if st := r.g.state(); st.Running {
		t.Fatal("30 sn eski durum.json çalışıyor sayıldı (çökmüş düğüm 'çalışıyor' görünürdü)")
	}
}

func TestGUIOtobaslat(t *testing.T) {
	r := newGUIRig(t, true)
	code, _, _ := r.call(t, http.MethodPost, "/api/otobaslat", `{"acik":true}`, true)
	if code != http.StatusConflict || len(r.f.autoSet) != 0 {
		t.Fatalf("taşınabilir kipte oturum açılışı değiştirildi: kod %d çağrı %v", code, r.f.autoSet)
	}

	r = newGUIRig(t, false)
	if code, _, _ := r.call(t, http.MethodPost, "/api/otobaslat", `{}`, true); code != http.StatusBadRequest {
		t.Fatalf("alan eksikken kod %d, 400 bekleniyordu", code)
	}
	code, v, _ := r.call(t, http.MethodPost, "/api/otobaslat", `{"acik":false}`, true)
	if code != http.StatusOK || !reflect.DeepEqual(r.f.autoSet, []bool{false}) || v["acik"] != false {
		t.Fatalf("kapatma: kod %d çağrı %v yanıt %v", code, r.f.autoSet, v)
	}
	r.f.auto = autostartState{Detail: "systemd yok"}
	if code, _, _ := r.call(t, http.MethodPost, "/api/otobaslat", `{"acik":true}`, true); code != http.StatusConflict {
		t.Fatalf("desteklenmeyen sistemde kod %d, 409 bekleniyordu", code)
	}
}

func TestGUIDurdurIstegi(t *testing.T) {
	r := newGUIRig(t, true)
	_ = saveSettings(r.o.dataRoot, nodeSettings{Key: "anahtar-12345678"})
	if code, v, _ := r.call(t, http.MethodPost, "/api/durdur", `{}`, true); code != http.StatusOK || v["ileti"] == nil {
		t.Fatalf("durmuş düğümde durdur: %d %v", code, v)
	}
	writeFreshStatus(r.o.dataRoot, version.Display())
	if code, v, _ := r.call(t, http.MethodPost, "/api/durdur", `{}`, true); code != http.StatusOK {
		t.Fatalf("durdur: %d %v", code, v)
	}
	req := filepath.Join(r.o.dataRoot, stopRequestFile)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(req); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("durdurma isteği dosyası yazılmadı")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Başlatma, durdurma sürerken reddedilir (tek iş kuralı).
	if code, _, _ := r.call(t, http.MethodPost, "/api/guncelle", `{}`, true); code != http.StatusConflict {
		t.Fatalf("durdurma sürerken ikinci iş kabul edildi: %d", code)
	}
	_ = os.Remove(statusPath(r.o.dataRoot)) // düğüm dünyayı kaydedip çıktı
	st := r.idle(t)
	if st.Info != "Düğüm durduruldu." || st.Err != "" {
		t.Fatalf("bitiş durumu: bilgi %q hata %q", st.Info, st.Err)
	}
}

func TestGUIKlasorAc(t *testing.T) {
	r := newGUIRig(t, true)
	if code, _, _ := r.call(t, http.MethodPost, "/api/klasor", `{}`, true); code != http.StatusOK {
		t.Fatalf("kod %d", code)
	}
	if !reflect.DeepEqual(r.f.opened, []string{r.o.dataRoot}) {
		t.Fatalf("açılan klasör %v, veri klasörü %s olmalıydı", r.f.opened, r.o.dataRoot)
	}
}

func TestGUIGunlukKuyrugu(t *testing.T) {
	r := newGUIRig(t, true)
	var b strings.Builder
	for i := 0; i < 500; i++ {
		b.WriteString("satır ")
		b.WriteString(string(rune('a' + i%26)))
		b.WriteString("\r\n")
	}
	b.WriteString("SON SATIR\n")
	_ = os.WriteFile(filepath.Join(r.o.dataRoot, "mcos-node.log"), []byte(b.String()), 0o644)
	code, v, _ := r.call(t, http.MethodGet, "/api/gunluk", "", true)
	lines, _ := v["satirlar"].([]any)
	if code != http.StatusOK || len(lines) != 200 || lines[199] != "SON SATIR" {
		t.Fatalf("kod %d, %d satır, son %v", code, len(lines), lines[len(lines)-1])
	}
	if strings.Contains(lines[0].(string), "\r") {
		t.Fatal("Windows satır sonu (\\r) temizlenmedi")
	}
}

func TestWantGUI(t *testing.T) {
	for _, c := range []struct {
		ad   string
		o    options
		goos string
		want bool
	}{
		{"Windows çift tıklama", options{}, "windows", true},
		{"Windows --key ile", options{key: "x"}, "windows", true},
		{"Windows --konsol", options{console: true}, "windows", false},
		{"Windows --arkaplan (oturum açılışı)", options{background: true}, "windows", false},
		{"Windows --durdur", options{stop: true}, "windows", false},
		{"Windows --sure (konsol sınaması)", options{duration: time.Second}, "windows", false},
		{"Windows --quiet", options{quiet: true}, "windows", false},
		{"Windows güvenlik duvarı alt süreci", options{firewall: "2222"}, "windows", false},
		// --kabul çıktısını komut isteminde yazar; pencere açsaydı kullanıcı
		// "kabul edildi mi" sorusunun yanıtını göremezdi.
		{"Windows --kabul", options{accept: "482913"}, "windows", false},
		{"Windows --reddet", options{reject: "482913"}, "windows", false},
		{"Linux varsayılan", options{}, "linux", false},
		{"Linux --gui", options{gui: true}, "linux", true},
		{"Linux --gui --konsol", options{gui: true, console: true}, "linux", false},
	} {
		if got := wantGUI(c.o, c.goos); got != c.want {
			t.Errorf("%s: %v, beklenen %v", c.ad, got, c.want)
		}
	}
}

// Bölge sütunu MCOS panelindeki dilim çubuğuyla aynı şeyi söylemeli.
func TestSelfRegion(t *testing.T) {
	areas := model.Territories([]string{"mcos", "salon-pc"}, 0)
	if got := selfRegion(true, "salon-pc", areas, ""); got != "x ≥ 0" {
		t.Fatalf("ikinci düğüm: %q", got)
	}
	if got := selfRegion(true, "mcos", areas, ""); got != "x < 0" {
		t.Fatalf("ilk düğüm: %q", got)
	}
	three := model.Territories([]string{"a", "b", "c"}, 64)
	if got := selfRegion(true, "b", three, ""); got != "-512 ≤ x < 512" {
		t.Fatalf("orta düğüm: %q", got)
	}
	if got := selfRegion(false, "b", nil, "ortak dünya için en az iki eşleşmiş cihaz gerekir"); !strings.Contains(got, "en az iki") {
		t.Fatalf("dilim yokken neden gösterilmedi: %q", got)
	}
}
