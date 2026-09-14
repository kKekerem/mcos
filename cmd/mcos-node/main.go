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
//  3. MCOS panelindeki "PC Eşleştirme" ekranı onu bulur — bulamazsa
//     kullanıcı adresini elle yazar,
//  4. eşleştikten sonra MCOS ona ortak dünyanın yarısını kurar ve burada
//     gerçek bir Minecraft sunucusu çalışır.
//
// Kullanıcının yapması gereken tek şey: programı açmak ve MCOS panelinde
// görünen eşleştirme anahtarını bir kez girmek.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
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
	// Key, MCOS panelindeki eşleştirme anahtarıdır. İki makinede AYNI
	// olmak zorundadır; kimlik kanıtı budur.
	Key  string `json:"key"`
	Name string `json:"name"`
	// RAMMB / CPUPercent: bu makinede bir sunucuya verilecek üst sınır.
	RAMMB      int `json:"ramMB,omitempty"`
	CPUPercent int `json:"cpuPercent,omitempty"`
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

func main() {
	var (
		dataRoot = flag.String("data", defaultDataRoot(), "veri klasörü")
		key      = flag.String("key", "", "MCOS panelindeki eşleştirme anahtarı")
		name     = flag.String("name", "", "bu makinenin görünen adı")
		port     = flag.Int("port", model.PairingPort, "eşleştirme portu")
		ramMB    = flag.Int("ram", 0, "sunucuya verilecek en fazla bellek (MB, 0 = otomatik)")
		cpuPct   = flag.Int("cpu", 0, "sunucuya verilecek en fazla CPU yüzdesi (0 = otomatik)")
		quiet    = flag.Bool("quiet", false, "durum ekranını gösterme")
	)
	flag.Parse()

	if err := run(*dataRoot, *key, *name, *port, *ramMB, *cpuPct, *quiet); err != nil {
		fmt.Fprintf(os.Stderr, "\n  HATA: %v\n\n", err)
		waitForExit()
		os.Exit(1)
	}
}

func run(dataRoot, key, name string, port, ramMB, cpuPct int, quiet bool) error {
	if name != "" {
		nameOnce.Do(func() { nameVal = name })
	}

	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		return fmt.Errorf("veri klasörü oluşturulamadı (%s): %w", dataRoot, err)
	}

	settings, err := loadSettings(dataRoot)
	if err != nil {
		return err
	}
	if key != "" {
		settings.Key = strings.TrimSpace(key)
	}
	if name != "" {
		settings.Name = name
	}
	if ramMB > 0 {
		settings.RAMMB = ramMB
	}
	if cpuPct > 0 {
		settings.CPUPercent = cpuPct
	}

	// Anahtar olmadan hiçbir şey yapamayız: MCOS'un gönderdiği istekler
	// imzasız kabul edilmez ve edilmemeli.
	if settings.Key == "" {
		settings.Key, err = promptForKey()
		if err != nil {
			return err
		}
	}
	if len(settings.Key) < 8 {
		return fmt.Errorf("eşleştirme anahtarı çok kısa — MCOS panelindeki " +
			"PC Eşleştirme ekranından tam anahtarı kopyalayın")
	}
	if settings.Name == "" {
		settings.Name = nodeName()
	}
	nameOnce.Do(func() { nameVal = settings.Name })
	if err := saveSettings(dataRoot, settings); err != nil {
		return err
	}

	// ── Günlük ──────────────────────────────────────────────────────────
	// Dosyaya yazıyoruz, ekrana DEĞİL: ekran durum tablosuna ait ve günlük
	// satırları onu sürekli bozardı. Sorun çıkarsa dosya orada.
	logFile, err := os.OpenFile(filepath.Join(dataRoot, "mcos-node.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("günlük dosyası açılamadı: %w", err)
	}
	defer logFile.Close()

	var logOut io.Writer = logFile
	if quiet {
		logOut = io.MultiWriter(logFile, os.Stdout)
	}
	lg := mlog.New(logOut, mlog.LevelInfo, 512)

	st, err := store.New(dataRoot)
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
	host.note("eşleştirme bekleniyor")

	mgr := cluster.NewManager(model.ClusterConfig{
		Enabled:   true,
		NodeName:  settings.Name,
		Port:      port,
		Secret:    settings.Key,
		Discovery: "mdns+udp",
		Role:      "helper",
	}, version.Version, st, lg, nil)

	// ── Anahtarla eşleşme ───────────────────────────────────────────────
	// Burada kullanıcının "şu makineyi eşleştir" diyeceği bir panel yok.
	// Elindeki tek kanıt, MCOS panelinde gördüğü ve buraya girdiği anahtar.
	// Doğru anahtarı sunan çağırıcı eşleştirilmiş sayılır.
	mgr.SetOpenPairing(true)

	coord := cluster.NewLinkCoordinator(mgr, host)
	mgr.AttachLink(coord)

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

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	if quiet {
		<-stop
		return nil
	}
	return uiLoop(host, mgr, settings, port, stop)
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

// promptForKey asks the user for the pairing key on first run.
func promptForKey() (string, error) {
	fmt.Print(`
  ┌────────────────────────────────────────────────────────────┐
  │  MCOS Düğüm — ilk kurulum                                  │
  └────────────────────────────────────────────────────────────┘

  Bu bilgisayarı MCOS'a ikinci PC olarak eklemek için MCOS
  panelindeki eşleştirme anahtarı gerekiyor.

  MCOS ekranında:  Sol menü → PC Eşleştirme → "Eşleştirme anahtarı"

  Anahtarı buraya yapıştırın: `)

	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return "", fmt.Errorf("anahtar girilmedi")
	}
	key := strings.TrimSpace(sc.Text())
	if key == "" {
		return "", fmt.Errorf("anahtar girilmedi")
	}
	fmt.Println()
	return key, nil
}

// waitForExit keeps the window open so the user can read an error.
//
// NEDEN: Windows'ta çift tıklamayla açılan bir konsol programı, hata verip
// çıktığında pencere ANINDA kapanır ve kullanıcı hiçbir şey göremez.
func waitForExit() {
	if runtime.GOOS != "windows" || os.Getenv("MCOS_NODE_NO_PAUSE") != "" {
		return
	}
	fmt.Print("  Kapatmak için Enter'a basın… ")
	bufio.NewScanner(os.Stdin).Scan()
}

// uiLoop draws the status screen until the user stops the program.
func uiLoop(h *nodeHost, mgr *cluster.Manager, s nodeSettings, port int,
	stop <-chan os.Signal) error {

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	draw(h, mgr, s, port)
	for {
		select {
		case <-stop:
			fmt.Print("\n\n  Kapatılıyor…\n\n")
			return nil
		case <-ticker.C:
			draw(h, mgr, s, port)
		}
	}
}
