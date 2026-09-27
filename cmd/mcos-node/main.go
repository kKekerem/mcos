// Command mcos-node turns an ordinary Windows or Linux PC into an extra MCOS
// machine: open it, and the MCOS box sees it, pairs with it, and can run half
// of a shared world here.
//
// ════════════════════════════════════════════════════════════════════════════
// NEDEN AYRI BİR PROGRAM
// ════════════════════════════════════════════════════════════════════════════
//
// MCOS bir işletim sistemidir: makineyi ona ayırmak gerekir. Ama kullanıcının
// evindeki ikinci bilgisayar çoğu zaman günlük kullandığı Windows makinesidir
// ve onu silip MCOS kurmak istemez.
//
// Bu program o boşluğu kapatır. Çalıştırıldığında:
//
//  1. kendine KARARLI bir kimlik üretir (bir kez, diske yazılır),
//  2. ağa kendini duyurur ve 2222 portunu dinler,
//  3. MCOS panelindeki "MCOS Paylaşım" ekranı onu bulur — bulamazsa
//     kullanıcı adresini elle yazar,
//  4. eşleştikten sonra MCOS ona ortak dünyanın yarısını kurar ve burada
//     gerçek bir Minecraft sunucusu çalışır.
//
// Kullanıcının yapması gereken tek şey: programı açmak ve MCOS "bu PC ile
// eşleşmek istiyor" dediğinde iki ekrandaki 6 haneli kod aynıysa "Kabul
// et"e basmak (pairing.go). Anahtar elle GİRİLMEZ; --key yalnızca yedek yol.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"mcos/internal/cluster"
	"mcos/internal/java"
	mlog "mcos/internal/log"
	"mcos/internal/model"
	"mcos/internal/server"
	"mcos/internal/store"
	"mcos/internal/supervisor"
	"mcos/internal/sysmon"
	"mcos/internal/version"
)

// nodeSettings is what this program remembers between runs.
type nodeSettings struct {
	// Key, MCOS'un küme anahtarıdır. İki makinede AYNI olmak zorundadır;
	// kimlik kanıtı budur. Normalde kodla eşleştirmede MCOS'tan şifreli
	// gelir ve buraya yazılır (pairing.go keyReceived); --key ile elle
	// girmek yedek yoldur. Boşsa düğüm yine başlar ve teklif bekler.
	Key  string `json:"key"`
	Name string `json:"name"`
	// RAMMB / CPUPercent: bu makinede bir sunucuya verilecek üst sınır.
	RAMMB      int `json:"ramMB,omitempty"`
	CPUPercent int `json:"cpuPercent,omitempty"`
	// SetupDone: Windows'ta ilk kurulum (program kopyası, güvenlik duvarı,
	// oturum açılışında başlatma) yapıldı mı. Yapıldıysa her açılışta UAC
	// sorulmaz.
	SetupDone bool `json:"setupDone,omitempty"`
}

var (
	nameOnce sync.Once
	nameVal  string
)

// nodeName returns the display name this node announces.
func nodeName() string {
	nameOnce.Do(func() {
		if h, err := os.Hostname(); err == nil && strings.TrimSpace(h) != "" {
			nameVal = h
			return
		}
		nameVal = "mcos-node"
	})
	return nameVal
}

// options are the command-line switches.
//
// Türkçe bayraklar: kullanıcı bunları README'den kopyalayıp yazacak.
type options struct {
	dataRoot   string
	key        string
	name       string
	port       int
	bind       string // --dinle: yalnızca bu adreste dinle (boş: hepsi)
	ramMB      int
	cpuPct     int
	quiet      bool
	background bool          // --arkaplan: pencere yok, yalnızca günlük
	setup      bool          // --kur: kurulumu (yeniden) yap
	noSetup    bool          // --kurma: taşınabilir kip, kurulum yapma
	stop       bool          // --durdur: arka plandaki düğümü durdur
	remove     bool          // --kaldir: durdur + kurulumu geri al
	duration   time.Duration // --sure: bu süre sonra kendiliğinden kapan
	showVer    bool
	saveOnly   bool   // --yalniz-ayar: anahtarı/adı kaydet ve çık (install.sh)
	firewall   string // (iç) yükseltilmiş kopya: güvenlik duvarı kuralları
	accept     string // --kabul KOD: çalışan düğümdeki eşleştirme isteğini kabul et
	reject     string // --reddet KOD

	// Pencereli arayüz (gui.go).
	console     bool          // --konsol: pencere yerine eski konsol ekranı
	gui         bool          // --gui: pencereyi zorla (Linux'ta adres yazdırılır)
	guiDuration time.Duration // --gui-sure: pencereyi bu süre sonra kapat (sınama)
	browser     bool          // --tarayici: WebView2 yerine varsayılan tarayıcı
	noOpen      bool          // --gui-acma: hiçbir şey açma, yalnızca adresi yaz
}

func main() {
	var o options
	flag.StringVar(&o.dataRoot, "data", defaultDataRoot(), "veri klasörü")
	flag.StringVar(&o.key, "key", "", "(gelişmiş) eşleştirme anahtarını elle gir; normalde gerekmez, MCOS kodla eşleştirir")
	flag.StringVar(&o.name, "name", "", "bu makinenin görünen adı")
	flag.IntVar(&o.port, "port", model.PairingPort, "eşleştirme portu")
	flag.StringVar(&o.bind, "dinle", "", "yalnızca bu adreste dinle (ör. 127.0.0.1; boş = bütün ağlar)")
	flag.IntVar(&o.ramMB, "ram", 0, "sunucuya verilecek en fazla bellek (MB, 0 = otomatik)")
	flag.IntVar(&o.cpuPct, "cpu", 0, "sunucuya verilecek en fazla CPU yüzdesi (0 = otomatik)")
	flag.BoolVar(&o.quiet, "quiet", false, "durum ekranı yerine günlüğü ekrana yaz")
	flag.BoolVar(&o.background, "arkaplan", false, "penceresiz çalış (oturum açılışında bu kullanılır)")
	flag.BoolVar(&o.setup, "kur", false, "kurulumu yap: program kopyası, güvenlik duvarı, oturum açılışında başlatma")
	flag.BoolVar(&o.noSetup, "kurma", false, "hiçbir kurulum yapma (taşınabilir kullanım, sınama)")
	flag.BoolVar(&o.stop, "durdur", false, "arka planda çalışan düğümü durdur")
	flag.BoolVar(&o.remove, "kaldir", false, "düğümü durdur, oturum açılışı kaydını ve güvenlik duvarı kurallarını sil")
	flag.DurationVar(&o.duration, "sure", 0, "bu süre sonunda kendiliğinden kapan (ör. 20s; sınama için)")
	flag.BoolVar(&o.showVer, "surum", false, "sürümü yaz ve çık")
	flag.BoolVar(&o.saveOnly, "yalniz-ayar", false, "anahtarı ve adı kaydet, düğümü başlatmadan çık")
	flag.StringVar(&o.firewall, "guvenlik-duvari-kur", "", "(iç kullanım) yönetici olarak kuralları ekle")
	flag.StringVar(&o.accept, "kabul", "", "MCOS'tan gelen eşleştirme isteğini kabul et (ör. --kabul 482913)")
	flag.StringVar(&o.reject, "reddet", "", "MCOS'tan gelen eşleştirme isteğini reddet (ör. --reddet 482913)")
	flag.BoolVar(&o.console, "konsol", false, "pencere yerine konsol durum ekranını kullan")
	flag.BoolVar(&o.gui, "gui", false, "pencereli arayüzü aç (Windows'ta varsayılan; Linux'ta adres yazdırılır)")
	flag.DurationVar(&o.guiDuration, "gui-sure", 0, "arayüzü bu süre sonunda kapat (ör. 15s; sınama için)")
	flag.BoolVar(&o.browser, "tarayici", false, "arayüzü pencere yerine varsayılan tarayıcıda aç")
	flag.BoolVar(&o.noOpen, "gui-acma", false, "arayüz için hiçbir şey açma, yalnızca adresini yaz")
	flag.Parse()

	// Pencereli arayüz: Windows'ta çift tıklamanın varsayılanı. Konsol
	// AÇILMAZ — kullanıcının şikâyeti tam olarak siyah pencereydi.
	if wantGUI(o, runtime.GOOS) {
		if err := runGUI(o); err != nil {
			appendLog(o.dataRoot, "HATA (arayüz): "+err.Error())
			guiFatal(err)
			os.Exit(1)
		}
		return
	}

	// Konsol her şeyden ÖNCE: Windows'ta GUI alt sistemiyle derlendiğimiz
	// için ekrana yazılacak her şey bundan sonra görünür.
	platformConsole(o.background || o.firewall != "")

	switch {
	case o.showVer:
		fmt.Println("mcos-node", version.Display())
		return
	case o.firewall != "":
		os.Exit(firewallElevated(o.firewall))
	case o.stop:
		os.Exit(requestStop(o.dataRoot))
	case o.accept != "":
		os.Exit(cliDecide(o.dataRoot, o.accept, true, os.Stdout, 150*time.Second))
	case o.reject != "":
		os.Exit(cliDecide(o.dataRoot, o.reject, false, os.Stdout, 0))
	case o.remove:
		code := requestStop(o.dataRoot)
		if err := removeSetup(o.port); err != nil {
			fmt.Fprintf(os.Stderr, "  %v\n", err)
			code = 1
		} else {
			fmt.Println("  Kurulum kaldırıldı. Veriler (dünyalar) yerinde duruyor:", o.dataRoot)
		}
		waitForExit(o)
		os.Exit(code)
	}

	if err := run(o); err != nil {
		fmt.Fprintf(os.Stderr, "\n  HATA: %v\n\n", err)
		appendLog(o.dataRoot, "HATA: "+err.Error())
		waitForExit(o)
		os.Exit(1)
	}
}

// appendLog writes one line to the log file even before the logger exists.
//
// Arka planda (penceresiz) çalışırken ekrana yazılan bir hata HİÇBİR YERDE
// görünmez; kullanıcıya bırakılan tek iz günlük dosyasıdır.
func appendLog(dataRoot, line string) {
	f, err := os.OpenFile(filepath.Join(dataRoot, "mcos-node.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), line)
}

func run(o options) error {
	if o.name != "" {
		nameOnce.Do(func() { nameVal = o.name })
	}
	if err := os.MkdirAll(o.dataRoot, 0o755); err != nil {
		return fmt.Errorf("veri klasörü oluşturulamadı (%s): %w", o.dataRoot, err)
	}

	// ── Tek örnek ───────────────────────────────────────────────────────
	// Arka planda zaten çalışan bir düğüm varsa ikincisini AÇMA: portlar
	// dolu olduğundan ikinci kopya hiçbir şey yapamaz ama "çalışıyor"
	// görünürdü. Çift tıklama bunun yerine çalışanın durumunu gösterir.
	// --yalniz-ayar çalışan kopyaya dokunmaz: install.sh ardından servisi
	// zaten yeniden başlatıyor.
	running := !o.saveOnly && runningInstance(o.dataRoot, o.bind, o.port)
	restart := false
	if running && !o.background && (o.key != "" || o.name != "" || o.ramMB > 0 || o.cpuPct > 0) {
		// Ayar DEĞİŞİYOR (ör. MCOS'taki anahtar yenilendi): çalışan kopya
		// eski ayarla sürerdi. Eskiden bu durumda "zaten çalışıyor" deyip
		// yeni anahtarı sessizce YOK SAYIYORDUK — panel "anahtar yanlış"
		// demeye devam ediyordu. Artık durdurulur, kaydedilir, yeniden
		// başlatılır.
		fmt.Println("  Ayar değişti; arka plandaki düğüm yeniden başlatılıyor…")
		if requestStop(o.dataRoot) == 0 {
			running, restart = false, true
		}
	}
	if running {
		if o.background {
			appendLog(o.dataRoot, "node: zaten çalışıyor, ikinci kopya kapanıyor")
			return nil
		}
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		watchLoop(o.dataRoot, stop, durationC(o.duration))
		return nil
	}

	settings, err := loadSettings(o.dataRoot)
	if err != nil {
		return err
	}
	if o.key != "" {
		settings.Key = strings.TrimSpace(o.key)
	}
	if o.name != "" {
		settings.Name = o.name
	}
	if o.ramMB > 0 {
		settings.RAMMB = o.ramMB
	}
	if o.cpuPct > 0 {
		settings.CPUPercent = o.cpuPct
	}

	// Anahtar YOKSA da başlanır. Eskiden burada "anahtarı yapıştırın" istemi
	// (konsol) ya da hata (arka plan) vardı: düğüm anahtar girilene kadar
	// ağda GÖRÜNMÜYORDU, MCOS onu bulamıyordu. Artık anahtarsız düğüm
	// taramada görünür ve kodla eşleştirme teklifini bekler (pairing.go).
	// Anahtar gelene kadar imzalı istekler (hello, linkSpec) yine REDDEDİLİR
	// (cluster authorizeTask: reasonNoKey) — güvenlik gevşemez.
	if settings.Key != "" && len(settings.Key) < 8 {
		return fmt.Errorf("eşleştirme anahtarı çok kısa — MCOS panelindeki " +
			"MCOS Paylaşım ekranından tam anahtarı kopyalayın")
	}
	if settings.Name == "" {
		settings.Name = nodeName()
	}
	nameOnce.Do(func() { nameVal = settings.Name })
	if err := saveSettings(o.dataRoot, settings); err != nil {
		return err
	}
	if o.saveOnly {
		// install.sh servis birimini etkinleştirmeden ÖNCE anahtarı yazar;
		// anahtarsız başlayan bir servis hata verip yeniden başlama
		// döngüsüne girerdi.
		fmt.Printf("  Ayarlar kaydedildi: %s (ad: %s)\n", settingsPath(o.dataRoot), settings.Name)
		return nil
	}

	// ── İlk kurulum (Windows) ───────────────────────────────────────────
	// Kullanıcının isteği: "server gibi olacak". Program bir kez açılınca
	// kendini kurar, güvenlik duvarına izin alır, oturum açılışında
	// penceresiz başlar — ve şimdi arka planda başlatılıp bu pencere
	// yalnızca DURUMU gösterir.
	if !o.background && !o.noSetup && (o.setup || setupNeeded(settings)) {
		fmt.Println("\n  Kurulum yapılıyor (güvenlik duvarı için bir kez yönetici izni istenebilir)…")
		exe, serr := runSetup(o.dataRoot, o.port)
		if serr != nil {
			fmt.Printf("  UYARI: %v\n  Yeniden denemek için: mcos-node --kur\n", serr)
			appendLog(o.dataRoot, "kurulum: "+serr.Error())
		}
		if exe != "" {
			settings.SetupDone = true
			_ = saveSettings(o.dataRoot, settings)
			if err := startBackground(exe, backgroundArgs(o)); err == nil &&
				waitRunning(o.dataRoot, o.bind, o.port, 15*time.Second) {
				fmt.Println("  Düğüm arka planda başlatıldı; oturum açıldığında kendiliğinden başlayacak.")
				stop := make(chan os.Signal, 1)
				signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
				watchLoop(o.dataRoot, stop, durationC(o.duration))
				return nil
			}
			fmt.Println("  Arka planda başlatılamadı; bu pencerede çalışılıyor.")
		}
	}

	if restart && !o.background {
		if err := startBackground(installedExe(), backgroundArgs(o)); err == nil &&
			waitRunning(o.dataRoot, o.bind, o.port, 15*time.Second) {
			stop := make(chan os.Signal, 1)
			signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
			watchLoop(o.dataRoot, stop, durationC(o.duration))
			return nil
		}
		fmt.Println("  Arka planda yeniden başlatılamadı; bu pencerede çalışılıyor.")
	}

	return serve(o, settings)
}

// backgroundArgs are the switches the background copy needs to see.
func backgroundArgs(o options) []string {
	var a []string
	if o.dataRoot != defaultDataRoot() {
		a = append(a, "--data", o.dataRoot)
	}
	if o.port != model.PairingPort {
		a = append(a, "--port", strconv.Itoa(o.port))
	}
	if o.bind != "" {
		a = append(a, "--dinle", o.bind)
	}
	return a
}

func waitRunning(dataRoot, bind string, port int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if runningInstance(dataRoot, bind, port) {
			return true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}

func durationC(d time.Duration) <-chan time.Time {
	if d <= 0 {
		return nil
	}
	return time.After(d)
}

// stopRequestFile asks a running node to shut down gracefully.
//
// Windows'ta başka bir sürece SIGTERM gönderilemez; TerminateProcess ise
// Minecraft'a dünyayı kaydetme fırsatı vermez. Küçük bir istek dosyası
// ağ açmadan, yalnızca aynı kullanıcının yapabileceği bir yoldur.
const stopRequestFile = "durdur.istek"

func requestStop(dataRoot string) int {
	st, ok := readStatus(dataRoot)
	if !ok || time.Since(st.Updated) > 10*time.Second {
		fmt.Println("  Arka planda çalışan bir düğüm yok.")
		return 0
	}
	if err := os.WriteFile(filepath.Join(dataRoot, stopRequestFile), []byte("dur\n"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "  Durdurma isteği yazılamadı:", err)
		return 1
	}
	fmt.Print("  Düğüm durduruluyor (sunucu dünyayı kaydediyor)…")
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := readStatus(dataRoot); !ok {
			fmt.Println(" durdu.")
			return 0
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Println(" zaman aşımı.")
	return 1
}

// serve runs the node until a signal, a stop request or --sure.
func serve(o options, settings nodeSettings) error {
	// ── Günlük ──────────────────────────────────────────────────────────
	// Dosyaya yazıyoruz, ekrana DEĞİL: ekran durum tablosuna ait ve günlük
	// satırları onu sürekli bozardı. Sorun çıkarsa dosya orada.
	logPath := filepath.Join(o.dataRoot, "mcos-node.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("günlük dosyası açılamadı: %w", err)
	}
	defer logFile.Close()

	var logOut io.Writer = logFile
	if o.quiet {
		logOut = io.MultiWriter(logFile, os.Stdout)
	}
	lg := mlog.New(logOut, mlog.LevelInfo, 512)
	lg.Infof("node: %s başlıyor (veri %s, port %d, arkaplan=%v)",
		version.Display(), o.dataRoot, o.port, o.background)

	// Bu süreç ve başlattığı her java.exe bir iş nesnesinde (Windows): düğüm
	// nasıl kapanırsa kapansın Java da kapanır.
	platformJob()
	_ = os.Remove(filepath.Join(o.dataRoot, stopRequestFile))

	st, err := store.New(o.dataRoot)
	if err != nil {
		return fmt.Errorf("veri deposu açılamadı: %w", err)
	}

	sup := supervisor.New(lg)
	jm := java.NewManager(st, lg)
	sm := server.NewManager(st, jm, sup, lg)

	host := &nodeHost{
		st:          st,
		servers:     sm,
		log:         lg,
		budgetRAMMB: budgetRAM(settings.RAMMB),
		budgetCPU:   budgetCPU(settings.CPUPercent),
	}
	if settings.Key == "" {
		host.note("eşleştirme bekleniyor — MCOS'ta: MCOS Paylaşım → Ağı tara → bu PC")
	} else {
		host.note("eşleştirme bekleniyor")
	}

	mgr := cluster.NewManager(model.ClusterConfig{
		Enabled:   true,
		NodeName:  settings.Name,
		Port:      o.port,
		Secret:    settings.Key,
		Discovery: "mdns+udp",
		Role:      "helper",
		Bind:      o.bind,
	}, version.Version, st, lg, nil)

	// ── Anahtarla eşleşme ───────────────────────────────────────────────
	// Burada kullanıcının "şu makineyi eşleştir" diyeceği bir panel yok.
	// Elindeki tek kanıt, MCOS panelinde gördüğü ve buraya girdiği anahtar.
	// Doğru anahtarı sunan çağırıcı eşleştirilmiş sayılır.
	mgr.SetOpenPairing(true)

	// ── Kodla eşleşme (anahtarsız) ──────────────────────────────────────
	// MCOS "pairOffer" gönderir; kullanıcı iki ekrandaki kodu karşılaştırıp
	// burada kabul eder, anahtar şifreli gelir ve node.json'a yazılır.
	// Başlamadan ÖNCE açılır: ilk teklif "desteklenmiyor" yanıtı almasın.
	pair := newPairTracker(o.dataRoot, mgr, lg)
	mgr.EnablePairOffers(pair.keyReceived)
	host.pair = pair

	coord := cluster.NewLinkCoordinator(mgr, host)
	mgr.AttachLink(coord)
	host.coord = coord

	if err := mgr.Start(); err != nil {
		return fmt.Errorf("ağ dinleyicisi başlatılamadı: %w", err)
	}
	defer mgr.Stop()

	if err := coord.Start(); err != nil {
		// Koordinatör yalnızca yerel moda topoloji sunar; başlamazsa
		// eşleştirme yine çalışır, ortak dünya devri çalışmaz.
		lg.Warnf("node: link koordinatörü başlatılamadı: %v", err)
		host.note("UYARI: ortak dünya koordinatörü başlatılamadı")
	}
	defer coord.Stop()

	// Önceden kurulmuş bir ortak dünya varsa yeniden başlat: kullanıcı
	// programı kapatıp açtığında sunucunun kendiliğinden gelmesi gerekir.
	go restorePrevious(host)

	ctx, cancel := context.WithCancel(context.Background())
	statusDone := make(chan struct{})
	kick := make(chan struct{}, 1)
	go pair.loop(ctx, kick)
	go func() {
		defer close(statusDone)
		statusLoop(ctx, o.dataRoot, func() nodeStatusFile {
			return snapshot(host, mgr, settings, o.port, logPath)
		}, kick)
	}()
	// Durum dosyası sunucular DURDUKTAN SONRA silinir: "--durdur" onun
	// kaybolmasını "durdu" sayıyor. Uçtan uca sınamada ölçüldü: dosya önce
	// siliniyordu ve "durdu" denirken Java hâlâ dünyayı kaydediyordu.
	defer func() {
		cancel()
		<-statusDone
	}()

	// ── Kapanışta sunucuları DURDUR ─────────────────────────────────────
	// Uçtan uca sınamada ölçüldü: düğüm SIGTERM ile kapandıktan sonra
	// java süreci AÇIK kaldı. Ekran "kapatınca sunucu durur" diyordu;
	// durmuyordu. Sahipsiz Java dünyayı kilitli tutar ve bir sonraki
	// açılışta aynı dünyada ikinci bir sunucu "port kullanımda" ile düşer.
	// Bu defer EN SON kaydedildiği için EN ÖNCE çalışır: sunucular, eş
	// bağlantıları, durum dosyası ve günlük hâlâ açıkken durdurulur.
	defer func() {
		host.note("kapanıyor — sunucu dünyayı kaydediyor…")
		lg.Infof("node: kapanıyor — sunucular durduruluyor (dünya kaydediliyor)")
		sup.StopAll()
		lg.Infof("node: kapandı")
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	// Durdurma isteği dosyası (bkz. requestStop) ve --sure aynı kanala
	// düşer: kapanış yolu TEK olsun.
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		until := durationC(o.duration)
		for {
			select {
			case <-ctx.Done():
				return
			case <-until:
				lg.Infof("node: --sure doldu")
				stop <- syscall.SIGTERM
				return
			case <-t.C:
				if _, err := os.Stat(filepath.Join(o.dataRoot, stopRequestFile)); err == nil {
					_ = os.Remove(filepath.Join(o.dataRoot, stopRequestFile))
					lg.Infof("node: durdurma isteği alındı")
					stop <- syscall.SIGTERM
					return
				}
			}
		}
	}()

	if o.quiet || o.background {
		<-stop
		return nil
	}
	return uiLoop(host, mgr, settings, o.port, logPath, stop)
}

// restorePrevious starts a shared world left over from a previous run.
func restorePrevious(h *nodeHost) {
	srv := h.sharedWorldServer()
	if srv == nil {
		return
	}
	h.install(srv.Clone())
}

// budgetRAM decides how much memory a server may use on this machine.
//
// Varsayılan, TOPLAM belleğin yarısı ve en fazla 8 GB. Bu, kullanıcının
// bilgisayarını kullanılamaz hâle getirmeden anlamlı bir sunucu çalıştırmaya
// yeter. Kullanıcı --ram ile ezebilir.
func budgetRAM(override int) int {
	if override > 0 {
		return override
	}
	total := int(sysmon.Memory().TotalBytes / (1 << 20))
	if total <= 0 {
		return 2048
	}
	half := total / 2
	switch {
	case half > 8192:
		return 8192
	case half < 1024:
		return 1024
	default:
		return half
	}
}

func budgetCPU(override int) int {
	if override > 0 {
		return override
	}
	// %75: bir çekirdek payı kullanıcıya kalsın, makine donmasın.
	return 75
}

// ── Ayar dosyası ────────────────────────────────────────────────────────────

func settingsPath(dataRoot string) string {
	return filepath.Join(dataRoot, "node.json")
}

func loadSettings(dataRoot string) (nodeSettings, error) {
	var s nodeSettings
	b, err := os.ReadFile(settingsPath(dataRoot))
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("ayarlar okunamadı: %w", err)
	}
	if err := json.Unmarshal(b, &s); err != nil {
		// Bozuk bir ayar dosyası programı engellememeli: kullanıcı anahtarı
		// yeniden girer.
		return nodeSettings{}, nil
	}
	return s, nil
}

func saveSettings(dataRoot string, s nodeSettings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := settingsPath(dataRoot)
	tmp := path + ".tmp"
	// 0600: anahtar bir sırdır, başka kullanıcılar okuyamamalı.
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("ayarlar kaydedilemedi: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("ayarlar kaydedilemedi: %w", err)
	}
	return nil
}

// waitForExit keeps the window open so the user can read an error.
//
// NEDEN: Windows'ta çift tıklamayla açılan bir konsol programı, hata verip
// çıktığında pencere ANINDA kapanır ve kullanıcı hiçbir şey göremez.
// Arka planda (penceresiz) bekleyecek bir pencere yok.
func waitForExit(o options) {
	if runtime.GOOS != "windows" || o.background || os.Getenv("MCOS_NODE_NO_PAUSE") != "" {
		return
	}
	fmt.Print("  Kapatmak için Enter'a basın… ")
	bufio.NewScanner(os.Stdin).Scan()
}

// uiLoop draws the status screen until the node is asked to stop.
func uiLoop(h *nodeHost, mgr *cluster.Manager, s nodeSettings, port int,
	logPath string, stop <-chan os.Signal) error {

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	footer := "Kapatmak için Ctrl+C. Kapatınca bu makinedeki sunucu durur."
	// Eşleştirme isteğini bu ekrandan "K" + Enter ile kabul edebilmek için.
	go consoleKeys(filepath.Dir(logPath), os.Stdin, setConsoleNote)
	draw(snapshot(h, mgr, s, port, logPath), footer)
	for {
		select {
		case <-stop:
			fmt.Print("\n\n  Kapatılıyor (sunucu dünyayı kaydediyor)…\n\n")
			return nil
		case <-ticker.C:
			draw(snapshot(h, mgr, s, port, logPath), footer)
		}
	}
}
