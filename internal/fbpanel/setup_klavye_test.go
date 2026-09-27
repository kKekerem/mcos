package fbpanel

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/ipcclient"
	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// SİHİRBAZIN SONU: iptal yollarından sonra KLAVYE ÇALIŞMAYA DEVAM ETMELİ
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı (gerçek PC): "son aşamada iptal falan edince klavye çalışmıyor".
//
// Sınamalar gerçek bir IPC bağlantısı üzerinden (sahte mcosd, TCP loopback)
// App.Key'e tuş dizisi gönderir; her iptal yolundan sonra BİR SONRAKİ tuşun
// ekranda bir şeyi değiştirdiğini ölçer. Sahte daemon'da kalıcılık ve
// kurulum istenildiği kadar "sürer" (kapı kanalı): gerçek PC'de dakikalar
// süren işlerin tuşları yutup yutmadığı böylece yakalanır.

// sahteDaemon, sihirbazın sonunda kullanılan RPC'leri taklit eder.
type sahteDaemon struct {
	mu          sync.Mutex
	persistN    int
	configSetN  int
	persistKapi chan struct{} // nil değilse Persist kapı açılana kadar sürer
	persistOK   bool
	persistMsg  string
	persistHata string // doluysa Persist gerçek bir hata döner (daemon biçiminde)
	disks       []ipc.DiskTarget
}

func (d *sahteDaemon) persistSayisi() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.persistN
}

// sahteDaemonBaslat, d'yi dinleyen bir sunucu açar ve ona bağlı istemciyi
// döndürür. TCP loopback: unix soket yolu 108 bayt sınırına takılmasın.
func sahteDaemonBaslat(t *testing.T, d *sahteDaemon) *ipcclient.Client {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("dinleme: %v", err)
	}
	srv := ipc.NewServer(ln, nil)
	var cfg *model.Config
	srv.Handle(ipc.MethodConfigSet, func(_ context.Context, raw json.RawMessage) (any, error) {
		var c model.Config
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		d.mu.Lock()
		d.configSetN++
		cfg = &c
		d.mu.Unlock()
		return ipc.OKResult{OK: true}, nil
	})
	srv.Handle(ipc.MethodConfigGet, func(_ context.Context, _ json.RawMessage) (any, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		return ipc.ConfigResult{Config: cfg}, nil
	})
	srv.Handle(ipc.MethodSystemDisks, func(_ context.Context, _ json.RawMessage) (any, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		return ipc.DisksResult{Disks: d.disks}, nil
	})
	srv.Handle(ipc.MethodSystemPersist, func(_ context.Context, _ json.RawMessage) (any, error) {
		d.mu.Lock()
		d.persistN++
		kapi := d.persistKapi
		ok, msg, hata := d.persistOK, d.persistMsg, d.persistHata
		d.mu.Unlock()
		if kapi != nil {
			<-kapi
		}
		if hata != "" {
			return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: "kalıcılık başarısız: " + hata}
		}
		return ipc.OKResult{OK: ok, Message: msg}, nil
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

// bagliSihirbaz, sahte daemon'a bağlı bir panel açar ve sihirbazı başlatır.
func bagliSihirbaz(t *testing.T, d *sahteDaemon) (*App, *Setup) {
	t.Helper()
	a, _ := newTestApp(t)
	a.cl = sahteDaemonBaslat(t, d)
	a.StartSetup()
	return a, a.setupState()
}

// bekle, kosul doğru olana kadar ANA DÖNGÜYÜ taklit eder (setupTick + Tick).
func bekle(t *testing.T, a *App, neIcin string, kosul func() bool) {
	t.Helper()
	son := time.Now().Add(5 * time.Second)
	for time.Now().Before(son) {
		a.Tick()
		a.setupTick()
		if kosul() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("zaman aşımı: %s", neIcin)
}

func (a *App) setupAdim(s *Setup) (setupStep, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return s.step, s.cursor
}

// tus, ana döngünün bir tuşa yaptığını yapar: Key, ardından tikler.
func tus(a *App, keys ...string) {
	for _, k := range keys {
		a.Key(k)
		a.Tick()
		a.setupTick()
	}
}

// satirEtiketi, sihirbazın imleçteki satırının etiketini döndürür.
func satirEtiketi(a *App, s *Setup) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	rows := s.rowsLocked()
	if s.cursor < 0 || s.cursor >= len(rows) {
		return ""
	}
	return rows[s.cursor].label
}

// klavyeCanli, BİR SONRAKİ tuşun etki ettiğini ölçer: açık pencere yoksa
// "aşağı" imleci ya da sayfayı değiştirmeli. Tek satırlı bir sayfada ölçüm
// anlamsız olduğundan çağıran en az iki satırlı bir sayfada çağırır.
func klavyeCanli(t *testing.T, a *App, s *Setup, neden string) {
	t.Helper()
	if m := a.ActiveModal(); m != nil {
		t.Fatalf("%s: pencere hâlâ açık (%s) — tuşlar ona gidiyor", neden, m.Title())
	}
	adim, imlec := a.setupAdim(s)
	tus(a, "down")
	adim2, imlec2 := a.setupAdim(s)
	if adim == adim2 && imlec == imlec2 {
		t.Fatalf("%s: 'aşağı' tuşu hiçbir şey değiştirmedi (adım=%s imleç=%d) — klavye ölü",
			neden, setupTitles[adim], imlec)
	}
	tus(a, "up") // imleci geri koy: çağıranın satır seçimi bozulmasın
}

// TestKaydetmeKaliciligiSormadanYapmaz: "Ayarları kaydet" yalnızca
// yapılandırmayı yazar; USB'ye bölüm ekleyen kalıcılık KENDİLİĞİNDEN
// çalışmaz ve klavye kaydetmeden hemen sonra çalışır.
//
// Ölçülen eski hata: kaydetme a.cl.Persist("")'i sormadan çağırıyordu ve
// o çağrı sürdükçe (Ventoy'da dakikalar) s.saving açık kaldığı için "aşağı"
// tuşu hiçbir şey yapmıyordu (adım=Özet, kalıcılık çağrısı=1).
func TestKaydetmeKaliciligiSormadanYapmaz(t *testing.T) {
	d := &sahteDaemon{persistKapi: make(chan struct{}), persistOK: true, persistMsg: "tamam"}
	defer close(d.persistKapi)
	a, s := bagliSihirbaz(t, d)

	a.setupUpdate(s, func(s *Setup) { s.step = stepSummary; s.cursor = 0 })
	tus(a, "enter") // "Ayarları kaydet"
	bekle(t, a, "kalıcılık sayfası", func() bool {
		adim, _ := a.setupAdim(s)
		return adim == stepPersist
	})
	if n := d.persistSayisi(); n != 0 {
		t.Fatalf("kaydetme kalıcılığı SORMADAN çalıştırdı (%d çağrı)", n)
	}
	klavyeCanli(t, a, s, "kaydetmeden sonra")

	// "Şimdilik geç": kalıcılık yapılmadan kurulum sayfasına geçilir.
	for satirEtiketi(a, s) != "Şimdilik geç" {
		tus(a, "down")
	}
	tus(a, "enter")
	if adim, _ := a.setupAdim(s); adim != stepInstall {
		t.Fatalf("'Şimdilik geç' kurulum sayfasına geçmedi: %s", setupTitles[adim])
	}
	if n := d.persistSayisi(); n != 0 {
		t.Fatalf("'Şimdilik geç' kalıcılığı çalıştırdı (%d çağrı)", n)
	}
}

// TestKalicilikIptalVeArkaPlan: kalıcılık onayında İPTAL klavyeyi
// kilitlemez; onaylanınca iş ARKA PLANDA sürer ve tuşlar çalışmaya devam
// eder (sayfadan devam edilebilir); sonuç sayfaya yazılır.
func TestKalicilikIptalVeArkaPlan(t *testing.T) {
	d := &sahteDaemon{persistKapi: make(chan struct{}), persistOK: true,
		persistMsg: "USB kalıcı yapıldı"}
	a, s := bagliSihirbaz(t, d)
	eski := procMounts
	procMounts = "/yok/mounts" // sınama makinesinin /data'sı karışmasın
	defer func() { procMounts = eski }()

	a.setupUpdate(s, func(s *Setup) { s.step = stepPersist; s.cursor = 0 })
	if got := satirEtiketi(a, s); got != "Bu USB'ye kalıcı kaydet" {
		t.Fatalf("ilk satır %q", got)
	}

	// 1) Esc ile iptal.
	tus(a, "enter")
	if a.ActiveModal() == nil {
		t.Fatal("onay penceresi açılmadı")
	}
	tus(a, "esc")
	klavyeCanli(t, a, s, "kalıcılık onayı Esc ile iptal")

	// 2) Odak "Vazgeç"teyken Enter (varsayılan) ile iptal.
	tus(a, "enter", "enter")
	klavyeCanli(t, a, s, "kalıcılık onayı Vazgeç ile iptal")

	// 3) Fareyle Vazgeç düğmesi.
	enablePointer(a)
	tus(a, "enter")
	a.Draw()
	if !tiklaBolge(a, zoneModalCancel) {
		t.Fatal("Vazgeç düğmesinin bölgesi yok")
	}
	klavyeCanli(t, a, s, "kalıcılık onayı fareyle iptal")
	if n := d.persistSayisi(); n != 0 {
		t.Fatalf("iptal edilen kalıcılık çalıştı (%d çağrı)", n)
	}

	// 4) Onayla: Tab odağı "Kalıcı yap"a taşır, Enter onu çalıştırır.
	tus(a, "enter", "tab", "enter")
	bekle(t, a, "kalıcılık çağrısı", func() bool { return d.persistSayisi() == 1 })
	if !a.runningJobs()[persistJobID] {
		t.Fatal("kalıcılık arka plan işi olarak görünmüyor")
	}
	// İş sürerken tuşlar çalışır: "Devam" kurulum sayfasına geçer (iş
	// arka planda sürmeye devam eder), Esc de geri getirir.
	if got := satirEtiketi(a, s); got != "Devam" {
		t.Fatalf("kalıcılık sürerken satır %q", got)
	}
	tus(a, "enter")
	if adim, _ := a.setupAdim(s); adim != stepInstall {
		t.Fatalf("kalıcılık SÜRERKEN Enter (Devam) çalışmadı: %s — klavye ölü", setupTitles[adim])
	}
	tus(a, "esc")
	if adim, _ := a.setupAdim(s); adim != stepPersist {
		t.Fatalf("kalıcılık SÜRERKEN Esc çalışmadı: %s — klavye ölü", setupTitles[adim])
	}
	tus(a, "enter")
	close(d.persistKapi)
	bekle(t, a, "kalıcılık sonucu", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return s.persist == persistTamam && len(a.jobs) == 0
	})
	if e := a.LastEvent(); e == nil || e.Text != "USB kalıcı yapıldı" {
		t.Fatalf("sonuç alt çubuğa yazılmadı: %+v", e)
	}
}

// tiklaBolge, son çizimdeki ilk kind bölgesinin ortasına tıklar.
func tiklaBolge(a *App, kind ZoneKind) bool {
	a.mu.Lock()
	var r image.Rectangle
	bulundu := false
	for _, z := range a.zones {
		if z.kind == kind {
			r, bulundu = z.r, true
			break
		}
	}
	a.mu.Unlock()
	if !bulundu {
		return false
	}
	clickAt(a, (r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2)
	a.Tick()
	a.setupTick()
	return true
}

// sahteKurucu, installRunner'ı kapı dosyası oluşana kadar süren bir betiğe
// bağlar. basarisiz ise betik mcos-install'ın HATA/NEDEN/ÇÖZÜM biçiminde
// yazıp 1 ile çıkar.
func sahteKurucu(t *testing.T, basarisiz bool) (kapi string) {
	t.Helper()
	dir := t.TempDir()
	kapi = filepath.Join(dir, "kapi")
	eskiDurum := installStatusFile
	installStatusFile = filepath.Join(dir, "durum")
	t.Cleanup(func() { installStatusFile = eskiDurum })
	son := `echo "Kurulum tamamlandı: $1"`
	if basarisiz {
		son = `echo "HATA: bölüm tablosu oluşturulamadı"; ` +
			`echo "NEDEN: Error: Partition(s) on $1 are being used."; ` +
			`echo "ÇÖZÜM: Diski çıkarıp yeniden takın; Ventoy kullanıyorsanız ISO dosyası Ventoy bölümünde durmalı, yoksa ISO'yu bir USB'ye yazın."; exit 1`
	}
	betik := `echo "35|[4/7] Çekirdek kopyalanıyor" > "` + installStatusFile + `"; ` +
		`while [ ! -e "` + kapi + `" ]; do sleep 0.02; done; ` + son
	eski := installRunner
	installRunner = func(_ string, args ...string) *exec.Cmd {
		return exec.Command("sh", append([]string{"-c", betik, "sh"}, args...)...)
	}
	t.Cleanup(func() { installRunner = eski })
	return kapi
}

func kapiyiAc(t *testing.T, kapi string) {
	t.Helper()
	if err := os.WriteFile(kapi, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

var sahteDisk = ipc.DiskTarget{Device: "/dev/sdz", Model: "Sahte USB",
	SizeBytes: 32 << 30, Removable: true}

// TestKurulumOnayIptalindenSonraKlavyeYasar: diske kur sayfasında disk
// seçip onay penceresini her yoldan iptal etmek klavyeyi kilitlememeli ve
// kurulum BAŞLAMAMALI.
func TestKurulumOnayIptalindenSonraKlavyeYasar(t *testing.T) {
	kapi := sahteKurucu(t, false)
	defer kapiyiAc(t, kapi)
	d := &sahteDaemon{disks: []ipc.DiskTarget{sahteDisk}}
	a, s := bagliSihirbaz(t, d)
	enablePointer(a)
	a.setupUpdate(s, func(s *Setup) { s.step = stepInstall; s.cursor = 0 })
	a.setupEnter(s)
	bekle(t, a, "disk listesi", func() bool { return satirEtiketi(a, s) == "/dev/sdz" })

	tus(a, "enter", "esc")
	klavyeCanli(t, a, s, "kurulum onayı Esc ile iptal")
	tus(a, "enter", "enter")
	klavyeCanli(t, a, s, "kurulum onayı Vazgeç ile iptal")
	tus(a, "enter")
	a.Draw()
	if !tiklaBolge(a, zoneModalCancel) {
		t.Fatal("Vazgeç düğmesinin bölgesi yok")
	}
	klavyeCanli(t, a, s, "kurulum onayı fareyle iptal")
	if a.runningJobs()[installJobID] {
		t.Fatal("iptal edilen kurulum başladı")
	}
}

// TestKurulumSurerkenTuslarSessizKalmaz: kurulum yarıda bırakılamaz, ama
// sürerken basılan tuş SESSİZCE yutulmamalı — kullanıcı neden beklediğini
// görmeli; bitince klavye normal çalışmalı.
func TestKurulumSurerkenTuslarSessizKalmaz(t *testing.T) {
	kapi := sahteKurucu(t, false)
	d := &sahteDaemon{disks: []ipc.DiskTarget{sahteDisk}}
	a, s := bagliSihirbaz(t, d)
	a.setupUpdate(s, func(s *Setup) { s.step = stepInstall; s.cursor = 0 })
	a.setupEnter(s)
	bekle(t, a, "disk listesi", func() bool { return satirEtiketi(a, s) == "/dev/sdz" })

	tus(a, "enter", "tab", "enter") // disk → onay → "Kur"
	bekle(t, a, "kurulum işi", func() bool { return a.runningJobs()[installJobID] })
	tus(a, "esc")
	if adim, _ := a.setupAdim(s); adim != stepInstall {
		t.Fatalf("kurulum sürerken Esc sayfadan çıkardı: %s", setupTitles[adim])
	}
	if e := a.LastEvent(); e == nil || !strings.Contains(e.Text, "yarıda bırakılamaz") {
		t.Fatalf("kurulum sürerken Esc hiçbir geri bildirim vermedi: %+v", e)
	}
	bekle(t, a, "ilerleme", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return strings.Contains(s.installMsg, "%35")
	})

	kapiyiAc(t, kapi)
	bekle(t, a, "kurulum sonucu", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return s.installDone && len(a.jobs) == 0
	})
	if m := a.ActiveModal(); m != nil {
		t.Fatalf("sonuç sihirbaz sayfasında gösterilmeliydi, pencere açıldı: %s", m.Title())
	}
	klavyeCanli(t, a, s, "kurulum bittikten sonra")
}

// ayarlardaKurulumAc, Ayarlar > "Diske / USB'ye kur"u çalıştırır ve disk
// listesinin açılmasını bekler.
func ayarlardaKurulumAc(t *testing.T, a *App) {
	t.Helper()
	a.gotoSection(SecSettings)
	a.setFocus(FocusContent)
	a.SetCursor(int(setInstall))
	tus(a, "enter")
	bekle(t, a, "disk listesi penceresi", func() bool {
		m, ok := a.ActiveModal().(*ListModal)
		return ok && m.Title() == "Diske / USB'ye kur"
	})
}

// TestAyarlardanDogrudanKurulum: sihirbazı açmadan Ayarlar'dan disk seçip
// kurulum yapılabilmeli; iş arka planda sürer (bölümden çıkılınca da), ilerleme
// alt çubukta görünür, sonuç bir pencerede açılır.
func TestAyarlardanDogrudanKurulum(t *testing.T) {
	kapi := sahteKurucu(t, false)
	d := &sahteDaemon{disks: []ipc.DiskTarget{sahteDisk}}
	// Saat ileri alınabilsin: yeni bir mesaj alt çubukta jobMsgHold kadar
	// süren işin önüne geçer; ilerlemeyi görmek için o süre atlanır.
	var ileri atomic.Int64
	eskiSaat := nowFunc
	nowFunc = func() time.Time { return time.Now().Add(time.Duration(ileri.Load())) }
	t.Cleanup(func() { nowFunc = eskiSaat })
	a, _ := newTestApp(t)
	a.cl = sahteDaemonBaslat(t, d)

	ayarlardaKurulumAc(t, a)
	tus(a, "enter") // /dev/sdz
	if m, ok := a.ActiveModal().(*ConfirmModal); !ok || m.Title() != "MCOS'u diske kur?" {
		t.Fatalf("onay penceresi açılmadı: %v", a.ActiveModal())
	}
	tus(a, "tab", "enter") // "Kur"
	if a.setupState() != nil {
		t.Fatal("Ayarlar'dan kurulum sihirbazı açtı")
	}
	ileri.Store(int64(jobMsgHold + time.Second))
	bekle(t, a, "ilerleme alt çubukta", func() bool {
		e := a.statusEvent()
		return e != nil && strings.Contains(e.Text, "/dev/sdz") && strings.Contains(e.Text, "%35")
	})
	// Bölümden çıkmak işi durdurmaz.
	a.gotoSection(SecServers)
	if !a.runningJobs()[installJobID] {
		t.Fatal("bölümden çıkınca kurulum işi kayboldu")
	}
	kapiyiAc(t, kapi)
	bekle(t, a, "sonuç penceresi", func() bool {
		m, ok := a.ActiveModal().(*ConfirmModal)
		return ok && m.Title() == "Kurulum tamamlandı"
	})
	tus(a, "esc")
	if a.ActiveModal() != nil {
		t.Fatal("sonuç penceresi Esc ile kapanmadı")
	}
	// İş goroutine'i son iletisini (nowFunc'u okuyarak) yazana kadar bekle:
	// t.Cleanup nowFunc'u geri yüklerken okuma sürerse veri yarışı olur.
	bekle(t, a, "son ileti", func() bool {
		e := a.LastEvent()
		return e != nil && strings.HasPrefix(e.Text, "Kurulum tamamlandı")
	})
}

// TestAyarlardanKurulumGercekHatayiGosterir: başarısız kurulumda pencere
// betiğin HATA/NEDEN/ÇÖZÜM satırlarını gösterir ("exit status 1" DEĞİL).
func TestAyarlardanKurulumGercekHatayiGosterir(t *testing.T) {
	kapi := sahteKurucu(t, true)
	kapiyiAc(t, kapi)
	d := &sahteDaemon{disks: []ipc.DiskTarget{sahteDisk}}
	a, _ := newTestApp(t)
	a.cl = sahteDaemonBaslat(t, d)

	ayarlardaKurulumAc(t, a)
	tus(a, "enter", "tab", "enter")
	bekle(t, a, "hata penceresi", func() bool {
		m, ok := a.ActiveModal().(*InfoModal)
		return ok && m.Title() == "Kurulum başarısız"
	})
	m := a.ActiveModal().(*InfoModal)
	// InfoModal satır kırmaz: uzun satır pencereden taşar (QEMU'da görüldü).
	for _, l := range m.lines {
		if n := len([]rune(l)); n > 72 {
			t.Errorf("satır %d sütun, pencereden taşar: %q", n, l)
		}
	}
	metin := strings.Join(m.lines, " ")
	for _, want := range []string{"Hata: bölüm tablosu", "Neden: Error: Partition(s) on /dev/sdz", "Ne yapmalı: Diski", "ISO'yu bir USB'ye yazın."} {
		if !strings.Contains(metin, want) {
			t.Errorf("hata penceresinde %q yok:\n%s", want, metin)
		}
	}
	if strings.Contains(metin, "exit status") {
		t.Errorf("pencere yalnızca çıkış kodunu gösteriyor:\n%s", metin)
	}
}

// TestAyarlardanKurulumIptalKlavyeyiKilitlemez: disk listesi ve onay Esc
// ile kapatılınca kurulum başlamaz ve panelin tuşları çalışır.
func TestAyarlardanKurulumIptalKlavyeyiKilitlemez(t *testing.T) {
	kapi := sahteKurucu(t, false)
	defer kapiyiAc(t, kapi)
	d := &sahteDaemon{disks: []ipc.DiskTarget{sahteDisk}}
	a, _ := newTestApp(t)
	a.cl = sahteDaemonBaslat(t, d)

	for _, iptal := range [][]string{{"esc"}, {"enter", "esc"}, {"enter", "enter"}} {
		ayarlardaKurulumAc(t, a)
		tus(a, iptal...)
		if m := a.ActiveModal(); m != nil {
			t.Fatalf("%v sonrası pencere açık kaldı: %s", iptal, m.Title())
		}
		if a.runningJobs()[installJobID] {
			t.Fatalf("%v sonrası iptal edilen kurulum başladı", iptal)
		}
		onceki := a.Cursor()
		tus(a, "up")
		if a.Cursor() == onceki {
			t.Fatalf("%v sonrası 'yukarı' imleci oynatmadı — klavye ölü", iptal)
		}
	}
	if a.runningJobs()[installJobID] {
		t.Fatal("iptal edilen kurulum başladı")
	}
}

// TestDataKaliciMi: /proc/mounts'tan /data'nın kalıcılığı.
func TestDataKaliciMi(t *testing.T) {
	cases := []struct {
		mounts string
		want   bool
	}{
		{"rootfs / rootfs rw 0 0\n", false},
		{"tmpfs /data tmpfs rw 0 0\n", false},
		{"/dev/sdb3 /data ext4 rw 0 0\n", true},
		{"/dev/loop0 /data ext4 rw 0 0\n", true},
		// Son bağlama kazanır.
		{"/dev/sdb3 /data ext4 rw 0 0\ntmpfs /data tmpfs rw 0 0\n", false},
		{"/dev/sdb3 /data/x ext4 rw 0 0\n", false},
	}
	for _, c := range cases {
		if got, _ := dataPersistentFrom(c.mounts); got != c.want {
			t.Errorf("%q: %v, beklenen %v", c.mounts, got, c.want)
		}
	}
}

// ayarSatiriCizildi, Ayarlar bölümünü imleç idx'teyken çizer ve o satırın
// tıklanabilir bölgesinin (yani satırın kendisinin) ekranda olup olmadığını
// söyler.
func ayarSatiriCizildi(a *App, idx int) bool {
	a.gotoSection(SecSettings)
	a.setFocus(FocusContent)
	a.SetCursor(idx)
	a.Draw()
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, z := range a.zones {
		if z.kind == zoneRow && z.idx == idx {
			return true
		}
	}
	return false
}

// TestAyarlarinHerSatiriCizilir: imlecin gidebildiği HER ayar satırı ekranda
// görünmeli. Ölçülen hata: 800x600'de 12 satırdan 11'i çiziliyordu; imleç
// "Kurulum sihirbazı"na iniyor ama satır ekranda yoktu. "Diske / USB'ye kur"
// eklenince liste daha da uzadı; sığmayan liste imleci izleyerek kaymalı.
func TestAyarlarinHerSatiriCizilir(t *testing.T) {
	for _, boy := range [][2]int{{800, 600}, {1024, 768}, {1280, 720}, {1280, 800}, {1920, 1080}} {
		a, _ := newSizedApp(t, boy[0], boy[1])
		for i := 0; i < int(settingCount); i++ {
			if !ayarSatiriCizildi(a, i) {
				t.Errorf("%dx%d: imleç %q satırındayken satır çizilmedi",
					boy[0], boy[1], settingsRows[i].label)
			}
		}
	}
}

// TestKalicilikSayfasiHerDurumdaCizilir: Kalıcılık sayfasının her durumu
// çizilir ve seçilebilir bir satır sunar (satırsız sayfa = kilitlenen
// sihirbaz). MCOS_SIHIRBAZ_SHOTS=<klasör> verilirse ekran görüntüleri yazılır.
func TestKalicilikSayfasiHerDurumdaCizilir(t *testing.T) {
	dir := os.Getenv("MCOS_SIHIRBAZ_SHOTS")
	durumlar := []struct {
		ad  string
		p   persistDurum
		msg string
	}{
		{"sorulmadi", persistSorulmadi, ""},
		{"suruyor", persistSuruyor, ""},
		{"tamam", persistTamam, "USB kalıcı yapıldı — /data artık reboot'ta korunur"},
		{"gecersiz", persistGecersiz, "mcos-persist: Bu sistem salt okunur bir ortamdan (DVD/ISO) açıldı; kalıcılık eklenemez."},
		{"hata", persistHata, "kalıcılık başarısız: mcos-persist: HATA: USB'de boş alan yok"},
		{"zatenvar", persistZatenVar, "/data kalıcı aygıttan bağlı: /dev/sdb3"},
	}
	for _, boy := range [][2]int{{800, 600}, {1280, 800}} {
		for _, dd := range durumlar {
			a, img := newSizedApp(t, boy[0], boy[1])
			a.StartSetup()
			s := a.setupState()
			a.setupUpdate(s, func(s *Setup) {
				s.step, s.cursor, s.persist, s.persistMsg = stepPersist, 0, dd.p, dd.msg
			})
			a.Draw()
			secilebilir := 0
			for _, r := range s.rows(a) {
				if r.kind != rowInfo {
					secilebilir++
				}
			}
			if secilebilir == 0 {
				t.Errorf("%s: seçilebilir satır yok — sihirbaz kilitlenir", dd.ad)
			}
			if countInk(img) < 1000 {
				t.Errorf("%s: sayfa boş çizildi", dd.ad)
			}
			if dir != "" {
				writePNG(t, filepath.Join(dir, fmt.Sprintf("%dx%d-kalicilik-%s.png", boy[0], boy[1], dd.ad)), img)
			}
		}
	}
	if dir == "" {
		return
	}
	// Ayarlar: yeni satır ve disk seçici.
	a, img := newSizedApp(t, 1280, 800)
	a.gotoSection(SecSettings)
	a.setFocus(FocusContent)
	a.SetCursor(int(setInstall))
	a.Draw()
	writePNG(t, filepath.Join(dir, "1280x800-ayarlar.png"), img)
	a.OpenModal(newInstallPicker([]ipc.DiskTarget{sahteDisk,
		{Device: "/dev/nvme0n1", Model: "Samsung 970", SizeBytes: 512 << 30, HasPersist: true}}))
	for i := 0; i < 30; i++ {
		a.Tick()
	}
	time.Sleep(400 * time.Millisecond) // açılış soluklaşması bitsin
	a.Draw()
	writePNG(t, filepath.Join(dir, "1280x800-ayarlar-disk-secici.png"), img)
	a2, img2 := newSizedApp(t, 800, 600)
	a2.gotoSection(SecSettings)
	a2.setFocus(FocusContent)
	a2.SetCursor(int(setWizard))
	a2.Draw()
	writePNG(t, filepath.Join(dir, "800x600-ayarlar-son-satir.png"), img2)
}

// TestKalicilikHatasiSayfadaKalir: gerçek hata sayfada betiğin sebebiyle
// kalır (çift "kalıcılık başarısız:" öneki OLMADAN) ve "Yeniden dene" ile
// "Şimdilik geç" sunulur; klavye çalışır.
func TestKalicilikHatasiSayfadaKalir(t *testing.T) {
	d := &sahteDaemon{persistHata: "mcos-persist: HATA: acilan USB otomatik bulunamadi"}
	a, s := bagliSihirbaz(t, d)
	eski := procMounts
	procMounts = "/yok/mounts"
	defer func() { procMounts = eski }()
	a.setupUpdate(s, func(s *Setup) { s.step = stepPersist; s.cursor = 0 })
	tus(a, "enter", "tab", "enter")
	bekle(t, a, "kalıcılık hatası", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return s.persist == persistHata && len(a.jobs) == 0
	})
	a.mu.Lock()
	msg := s.persistMsg
	a.mu.Unlock()
	if msg != "mcos-persist: HATA: acilan USB otomatik bulunamadi" {
		t.Fatalf("sayfadaki sebep %q", msg)
	}
	if e := a.LastEvent(); e == nil || strings.Count(e.Text, "kalıcılık başarısız") != 1 {
		t.Fatalf("alt çubuk iletisi önek tekrarlıyor ya da yok: %+v", e)
	}
	if got := satirEtiketi(a, s); got != "Yeniden dene" {
		t.Fatalf("ilk satır %q", got)
	}
	klavyeCanli(t, a, s, "kalıcılık hatasından sonra")
}

// TestZatenKaliciSistemdeSorulmaz: /data kalıcı bir aygıttan bağlıysa
// (kalıcı USB, diske kurulu sistem) sayfa "Bu USB'ye kalıcı kaydet" SORMAZ.
func TestZatenKaliciSistemdeSorulmaz(t *testing.T) {
	d := &sahteDaemon{}
	a, s := bagliSihirbaz(t, d)
	f := filepath.Join(t.TempDir(), "mounts")
	if err := os.WriteFile(f, []byte("rootfs / rootfs rw 0 0\n/dev/sdb3 /data ext4 rw 0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	eski := procMounts
	procMounts = f
	defer func() { procMounts = eski }()

	a.setupUpdate(s, func(s *Setup) { s.step = stepPersist; s.cursor = 0 })
	a.setupEnter(s)
	for _, r := range s.rows(a) {
		if r.key == "persist" {
			t.Fatalf("zaten kalıcı sistemde kalıcılık soruluyor: %q", r.label)
		}
	}
	a.mu.Lock()
	durum, msg := s.persist, s.persistMsg
	a.mu.Unlock()
	if durum != persistZatenVar || !strings.Contains(msg, "/dev/sdb3") {
		t.Fatalf("durum=%d msg=%q", durum, msg)
	}
}
