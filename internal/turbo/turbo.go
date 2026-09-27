// Package turbo, "Turbo" anahtarının donanımda GERÇEKTEN yaptığı işi yapar:
// işlemci frekansını tam güce çeker, paket güç sınırını (RAPL PL1/PL2)
// firmware'in izin verdiği tavana yükseltir, bayat ısıl kısıtlamayı kaldırır,
// fanları son güce alır, platform profilini "performance" yapar ve
// sunucuların sabitleneceği P-çekirdeklerini bulur. Her yazım GERİ OKUNUR;
// Diagnose donanımın gerçek durumunu ölçer (bkz. diag.go). Hepsi saf
// sysfs/procfs yazımı ve MSR OKUMASIDIR; hiçbir dış araç gerekmez.
//
// Gerçek PC geri bildirimi (ikinci tur): "turboda 4,4 GHz olması lazımken
// 2,4 GHz oluyor, fan çalışmıyor, PC ısınmıyor". Kaynak ve derlenmiş
// .config'ten çıkarılan nedenler: çekirdekte POWERCAP/RAPL yoktu (BIOS'un
// 15-28 W'lık PL1'i tüm çekirdekleri ~2,4 GHz'e çekiyordu), AMD için hiç
// cpufreq sürücüsü yoktu, dizüstü EC fan sürücülerinin hiçbiri derlenmemişti
// ve turbo boyunca tutulan cpu_dma_latency=0 tek çekirdek turbosunu
// engelliyordu (bkz. cstateItem, rapl.go, kernel.config "Turbo").
//
// ── Neden yazıldı ───────────────────────────────────────────────────────────
// Eskiden turbo yalnızca sunucuya nice -10 veriyor ve kaynak sınırlarını
// kaldırıyordu. Gerçek PC'de kullanıcı şunu gördü: turbo açık, işlemci hâlâ
// 400 MHz'de (intel_pstate "powersave" + EPP "balance_power"), fanlar sessiz
// kipte, sunucu verimlilik (E) çekirdeklerine düşebiliyor. "Turbo hiçbir
// işe yaramıyor" şikâyeti ölçülebilir biçimde doğruydu.
//
// ── Güvenlik ilkeleri ───────────────────────────────────────────────────────
//  1. Yazılan HER sysfs dosyasının özgün değeri, yazmadan ÖNCE günlüğe
//     (journal) alınır ve /run altına kaydedilir. Kapatınca, daemon
//     çıkarken ve çökme sonrası açılışta bu günlükten geri yüklenir.
//  2. Fan hiçbir zaman otomatikten DÜŞÜĞE çekilmez: elle kipe geçtikten sonra
//     okunan değer tam güce yakın değilse kanal hemen özgün (otomatik) kipine
//     döndürülür; geri yüklemede önce otomatik kip yazılır, eski düşük PWM
//     değeri otomatik kipe hiç yazılmaz.
//  3. İşlemcinin kendi ısıl korumasına (PROCHOT, TjMax, TCC, kritik/sıcak
//     tetik noktaları) DOKUNULMAZ. Güç sınırı yalnızca firmware'in ve
//     işlemcinin kendi bildirdiği değerlere (PL2, TDP, PKG_POWER_INFO) kadar
//     yükseltilir; psys ve PL4 dokunulmaz. "Processor" soğutma aygıtı
//     yalnızca bağlı bölge eşiğin ALTINDAYKEN (bayat kısıtlama) sıfırlanır;
//     bölge sıcaksa kısıtlama gerçek bir ısıl karardır ve korunur.
//
// ── Sınanabilirlik ──────────────────────────────────────────────────────────
// Tüm yollar bir kök dizine göre çözülür (üretimde "/"). Birim sınamaları
// t.TempDir() altında sahte bir sysfs ağacı kurar; ana makinenin gerçek
// /sys'ine asla yazılmaz.
package turbo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"mcos/internal/model"
)

// Controller turbo kollarını uygular ve geri alır.
type Controller struct {
	root      string
	statePath string
	// write, sysfs yazımının tek noktasıdır. Sınamalar bunu değiştirerek
	// çekirdeğin gerçek davranışlarını (EBUSY, EINVAL, PWM'i yok sayma)
	// taklit eder; düz dosyalar bunları kendiliğinden yapamaz.
	write func(path, val string) error

	mu        sync.Mutex
	j         *journal
	active    bool
	supported bool
	items     []model.TurboItem
	fans      []fanChan
	fansTotal int
	topo      *Topology
	// targetKHz: scaling_min_freq'e yazılan hedef (cpuinfo_max_freq).
	targetKHz int64

	// mismatches: "yazıldı ama geri okuyunca farklı" çıkan ayarlar (bkz.
	// verify). Gerçek PC'de turbo "tamam" dedi ama işlemci 2,4 GHz'de
	// kaldı; yazımın BAŞARILI dönmesi, değerin tuttuğu anlamına gelmiyor
	// (cpufreq tavanı başka bir QoS isteğiyle kırpar, RAPL birimlere yuvarlar,
	// firmware geri alır). Her yazım geri okunur, fark burada birikir.
	mismatches []string
	// raplTargets: turbo boyunca korunan güç sınırı hedefleri (bkz. Refresh).
	raplTargets []raplTarget
	// raplReasserts: firmware'in güç sınırını kaç kez geri çektiği.
	raplReasserts int
	// thrBase: Enable anındaki ısıl kısıtlama sayaçları; tanı farkı gösterir.
	thrBase map[string]int64

	// Tanı önbelleği (bkz. Diagnose). Status bunu döner; ölçüm (bekleme
	// içeren) Status'un içinde yapılmaz ki panel sorgusu gecikmesin.
	diag    []model.TurboItem
	diagAt  time.Time
	limiter string
	kmsg    []string
	// readMSR, /dev/cpu/N/msr okumasının tek noktası; sınamalar değiştirir.
	readMSR func(cpu int, reg uint32) (uint64, error)
	// sleep ve now, tanı ölçüm penceresi; sınamalar beklemesin ve ölçümü
	// belirlenimci yapsın diye değiştirilir.
	sleep func(time.Duration)
	now   func() time.Time
}

// New bir denetleyici kurar. root üretimde "/", statePath özgün değerlerin
// kaydedildiği dosyadır (tmpfs'te olmalı: yeniden başlatmada donanım zaten
// sıfırlanır, eski kayıt yanlış "özgün" değer olurdu).
func New(root, statePath string) *Controller {
	if root == "" {
		root = "/"
	}
	c := &Controller{root: root, statePath: statePath, write: writeSysfs, sleep: time.Sleep, now: time.Now}
	c.readMSR = c.readMSRFile
	return c
}

// SetWriter yalnızca sınamalar içindir: çekirdek davranışlarını taklit eder.
func (c *Controller) SetWriter(w func(path, val string) error) {
	c.mu.Lock()
	c.write = w
	c.mu.Unlock()
}

// p, kök dizine göre bir yol üretir.
func (c *Controller) p(rel string) string { return filepath.Join(c.root, rel) }

// ── sysfs yardımcıları ──────────────────────────────────────────────────────

func readStr(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func readInt(path string) int64 {
	s, err := readStr(path)
	if err != nil {
		return 0
	}
	v, err := strconv.ParseInt(strings.Fields(s + " 0")[0], 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// writeSysfs bir sysfs özniteliğine yazar.
//
// O_CREATE BİLEREK yok: sysfs'te dosya ya vardır ya yoktur. Sahte kökte
// olmayan bir dosyayı "oluşturup başarılı saymak", desteklenmeyen bir kolu
// uygulanmış gösterirdi — tam da düzelttiğimiz sessiz başarısızlık türü.
func writeSysfs(path, val string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(val)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// ── Özgün değer günlüğü ─────────────────────────────────────────────────────

// Değişiklik türleri: geri yükleme her türde farklı davranır.
const (
	kindPlain = ""
	// kindPWM: pwmN değeri. YALNIZCA eşi (pwmN_enable) özgün hâlinde "1"
	// (elle) ise geri yazılır. Otomatik kipteki bir kanala eski PWM değerini
	// yazmak, soğuk anda kaydedilmiş düşük bir değeri sıcak makineye
	// dayatmak olurdu.
	kindPWM = "pwm"
	// kindPWMNoEnable: enable dosyası olmayan kanal (ör. beyaz listede
	// olmayan Dell). BIOS'un otomatik denetimi hiç kapatılmadı; geri
	// yüklemede düşük eski değer YAZILMAZ, BIOS kendisi indirir.
	kindPWMNoEnable = "pwm-noenable"
	// kindCdev: "Fan" türündeki ısıl soğutma aygıtı (bkz. restoreCdev).
	kindCdev = "cdev"
	// kindThinkpad: /proc/acpi/ibm/fan; özgün değer "level" satırıdır.
	kindThinkpad = "tpfan"
	// kindThrottle: işlemciyi YAVAŞLATAN soğutma aygıtı ("Processor",
	// "intel_powerclamp"). Turbo bayat bir kısıtlamayı 0'a çeker; geri
	// yüklemede eski kısıtlama YAZILMAZ: aygıtın sahibi ısıl yöneticidir ve
	// bölge ısınırsa hedefi kendisi yeniden yükseltir. Soğuk makineye eski
	// bir kısıtlamayı geri dayatmak turbonun kapanınca işlemciyi gereksiz
	// yavaşlatması olurdu. Kayıt yalnızca tanı içindir (özgün değer).
	kindThrottle = "throttle"
)

type change struct {
	Path string `json:"path"`
	Orig string `json:"orig"`
	Kind string `json:"kind,omitempty"`
	Pair string `json:"pair,omitempty"`
}

type journal struct {
	Changes []change `json:"changes"`
}

func (j *journal) find(path string) *change {
	for i := range j.Changes {
		if j.Changes[i].Path == path {
			return &j.Changes[i]
		}
	}
	return nil
}

// loadJournalLocked belleği, yoksa dosyayı kullanır.
//
// Dosya ÖNCELİKLİ değil, bellek öncelikli: aynı oturumda ikinci kez
// Enable çağrıldığında zaten bellekte olan özgün değerler korunur. Dosya,
// daemon çöküp yeniden başladığında devreye girer; o zaman o anki sysfs
// değerleri turbonun kendi yazdıklarıdır ve "özgün" diye kaydedilmemelidir.
func (c *Controller) loadJournalLocked() {
	if c.j != nil {
		return
	}
	c.j = &journal{}
	if c.statePath == "" {
		return
	}
	b, err := os.ReadFile(c.statePath)
	if err != nil {
		return
	}
	var j journal
	if json.Unmarshal(b, &j) == nil {
		c.j = &j
	}
}

func (c *Controller) saveJournalLocked() {
	if c.statePath == "" || c.j == nil {
		return
	}
	b, err := json.MarshalIndent(c.j, "", " ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(c.statePath), 0o755)
	tmp := c.statePath + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, c.statePath)
	}
}

// record, bir dosyanın özgün değerini yazmadan ÖNCE günlüğe alır. Aynı yol
// ikinci kez kaydedilmez (ilk kayıt gerçek özgün değerdir).
func (c *Controller) record(path, kind, pair string) error {
	c.loadJournalLocked()
	if c.j.find(path) != nil {
		return nil
	}
	var orig string
	var err error
	if kind == kindThinkpad {
		orig, err = thinkpadLevel(path)
	} else {
		orig, err = readStr(path)
	}
	if err != nil {
		return err
	}
	c.j.Changes = append(c.j.Changes, change{Path: path, Orig: orig, Kind: kind, Pair: pair})
	// Her kayıttan sonra diske: yazım ortasında çökülürse bile özgün
	// değerler kaybolmasın. /run bir tmpfs; maliyeti yok denecek kadar az.
	c.saveJournalLocked()
	return nil
}

// set, düz bir özniteliği kaydedip yazar ve GERİ OKUR (bkz. verify).
// Değer zaten istenen değerse hiçbir şey yapmaz (gereksiz EBUSY ve günlük
// kalabalığı olmasın).
func (c *Controller) set(path, val string) error { return c.setTol(path, val, 0) }

// setTol, set'in sayısal geri okuma payı verilebilen hâli.
func (c *Controller) setTol(path, val string, tol float64) error {
	cur, err := readStr(path)
	if err != nil {
		return err
	}
	if cur == val {
		return nil
	}
	if err := c.record(path, kindPlain, ""); err != nil {
		return err
	}
	if err := c.write(path, val); err != nil {
		return err
	}
	c.verify(path, val, tol)
	return nil
}

// verify, az önce yazılan değeri GERİ OKUR ve farkı mismatches'e ekler.
//
// tol: sayısal değerlerde kabul edilen göreli sapma (0 = binde 5). RAPL güç
// değerleri donanım birimine (çoğunlukla 1/8 W) yuvarlanır; o yüzden
// çağıran daha geniş bir pay verir. Sayısal olmayan değer birebir eşleşmeli.
func (c *Controller) verify(path, want string, tol float64) bool {
	got, err := readStr(path)
	if err != nil {
		c.mismatches = append(c.mismatches, fmt.Sprintf("%s: yazıldı %s, geri okunamadı (%v)", c.short(path), want, err))
		return false
	}
	if sameValue(want, got, tol) {
		return true
	}
	c.mismatches = append(c.mismatches, fmt.Sprintf("%s: yazıldı %s, geri okunan %s", c.short(path), want, got))
	return false
}

// sameValue iki sysfs değerini karşılaştırır (bkz. verify).
func sameValue(want, got string, tol float64) bool {
	if want == got {
		return true
	}
	w, err1 := strconv.ParseFloat(want, 64)
	g, err2 := strconv.ParseFloat(got, 64)
	if err1 != nil || err2 != nil {
		return false
	}
	if tol <= 0 {
		tol = 0.005
	}
	d := w - g
	if d < 0 {
		d = -d
	}
	ref := w
	if ref < 0 {
		ref = -ref
	}
	return d <= ref*tol
}

// short, kullanıcıya gösterilecek kısa yol üretir: kök ve uzun ortak ön
// ekler atılır ("cpufreq/policy0/scaling_max_freq").
func (c *Controller) short(path string) string {
	s := strings.TrimPrefix(path, c.root)
	s = strings.TrimPrefix(s, "/")
	for _, p := range []string{cpuDir + "/", "sys/class/", "sys/devices/platform/", "sys/firmware/acpi/"} {
		s = strings.TrimPrefix(s, p)
	}
	return s
}

// restoreLocked günlükteki her değişikliği TERS sırayla geri alır.
//
// Ters sıra önemli: örneğin intel_pstate'te EPP, yönetici "performance"
// iken değiştirilemez (EBUSY). Uygularken önce EPP sonra yönetici yazıldı;
// geri alırken önce yönetici (powersave), sonra EPP geri gelir.
func (c *Controller) restoreLocked() (n int, errs []string) {
	c.loadJournalLocked()
	for i := len(c.j.Changes) - 1; i >= 0; i-- {
		ch := c.j.Changes[i]
		var err error
		switch ch.Kind {
		case kindPWMNoEnable, kindThrottle:
			continue
		case kindPWM:
			if pe := c.j.find(ch.Pair); pe == nil || pe.Orig != "1" {
				continue // otomatik kipteki kanala eski PWM yazılmaz
			}
			err = c.write(ch.Path, ch.Orig)
		case kindThinkpad:
			err = c.write(ch.Path, "level "+ch.Orig)
		case kindCdev:
			var skipped bool
			skipped, err = c.restoreCdev(ch)
			if skipped {
				continue
			}
		default:
			err = c.write(ch.Path, ch.Orig)
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", strings.TrimPrefix(ch.Path, c.root), err))
			continue
		}
		n++
	}
	c.j = &journal{}
	if c.statePath != "" {
		_ = os.Remove(c.statePath)
	}
	return n, errs
}

// ── Genel arayüz ────────────────────────────────────────────────────────────

// Enable turbonun tüm donanım kollarını uygular ve sonucu madde madde döner.
// Tekrar çağrılabilir: özgün değerler ilk çağrıdakiler olarak kalır.
func (c *Controller) Enable() model.TurboStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadJournalLocked()
	c.mismatches = nil
	c.thrBase = c.throttleCounts()

	var items []model.TurboItem
	hw := false

	cpuItems, ok := c.applyCPU()
	items = append(items, cpuItems...)
	hw = hw || ok

	// Platform profili RAPL'dan ÖNCE: ThinkPad/IdeaPad/HP'de "performance"
	// profili firmware'e PL1/PL2'yi kendisi yazdırır. Önce profil, sonra
	// güç sınırı okunup yükseltilir; tersi olsaydı profil bizim değerimizi
	// firmware'in performans değeriyle ezerdi.
	it, ok := c.applyPlatformProfile()
	items = append(items, it)
	hw = hw || ok

	if it, ok, present := c.applyVendor(); present {
		items = append(items, it)
		hw = hw || ok
	}

	it, ok = c.applyRAPL()
	items = append(items, it)
	hw = hw || ok

	items = append(items, c.applyThrottle())

	it, ok = c.applyFans()
	items = append(items, it)
	hw = hw || ok

	items = append(items, cstateItem())

	if len(c.mismatches) > 0 {
		items = append(items, model.TurboItem{Name: "Geri okuma", State: model.TurboPartial,
			Detail: fmt.Sprintf("%d ayar yazıldı ama geri okuyunca FARKLI: %s",
				len(c.mismatches), strings.Join(c.mismatches, "; "))})
	}

	c.items = items
	c.active = true
	c.supported = hw
	return c.statusLocked()
}

// cstateItem derin C-durumlarının neden AÇIK bırakıldığını söyler.
//
// ── Düzeltilen hata: PM QoS tek çekirdek turbosunu engelliyordu ─────────
// Önceki sürüm turbo boyunca /dev/cpu_dma_latency=0 tutuyordu. Gerçek PC'de
// sonuç "4,4 GHz olması lazımken 2,4 GHz" oldu. İki sebep:
//  1. 0 µs isteğiyle boştaki çekirdek poll_idle'da döner, yani C0'da kalır.
//     Intel/AMD turbo tavanı ETKİN çekirdek sayısına bağlıdır (çekirdek
//     başına turbo oranı tablosu, MSR_TURBO_RATIO_LIMIT): tüm çekirdekler
//     "etkin" görününce yükteki çekirdek bile tek çekirdek oranına (4,4 GHz)
//     değil, tüm çekirdek oranına çıkabiliyordu.
//  2. Azami frekans isteğiyle (scaling_min_freq = azami) dönen boş
//     çekirdekler paket gücünü PL1'e dayıyor; donanım TÜM çekirdekleri güç
//     sınırına sığacak frekansa (dizüstüde ~2,4 GHz) indiriyordu.
//
// Artık boştaki çekirdek uyur, bütçeyi yükteki çekirdeğe bırakır; uyanınca
// HWP isteği (en düşük = azami) yüzünden doğrudan azami frekansta başlar.
func cstateItem() model.TurboItem {
	return model.TurboItem{Name: "C-durumları", State: model.TurboOK,
		Detail: "derin uyku AÇIK bırakıldı: boştaki çekirdek uyur, güç/turbo bütçesi yükteki çekirdeğe kalır (tek çekirdek turbosu için gerekli)"}
}

// Disable her şeyi özgün hâline döndürür.
func (c *Controller) Disable() model.TurboStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, errs := c.restoreLocked()
	c.active = false
	c.targetKHz = 0
	c.supported = false
	c.fans = nil
	c.fansTotal = 0
	c.items = nil
	c.raplTargets = nil
	c.raplReasserts = 0
	c.mismatches = nil
	if n > 0 || len(errs) > 0 {
		st := model.TurboItem{Name: "Geri yükleme", State: model.TurboOK,
			Detail: fmt.Sprintf("%d ayar özgün değerine döndü", n)}
		if len(errs) > 0 {
			st.State = model.TurboPartial
			st.Detail += "; geri alınamayan: " + strings.Join(errs, "; ")
		}
		c.items = []model.TurboItem{st}
	}
	return c.statusLocked()
}

// RestoreLeftover, açılışta turbo KAPALIYKEN önceki oturumdan kalan
// değişiklikleri geri alır (daemon turbo açıkken çöktüyse fanlar hâlâ elle
// tam güçte, yönetici hâlâ "performance" olur). Geri alınan ayar sayısını
// döner.
func (c *Controller) RestoreLeftover() (int, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active {
		return 0, nil
	}
	c.j = nil // dosyadan okunsun
	return c.restoreLocked()
}

// Refresh turbo açıkken periyodik çağrılır: donanımın/BIOS'un geri aldığı
// fan ayarlarını yeniden uygular.
//
// Neden gerekli: Dell (enable dosyası olmayan kanallar) ve bazı ThinkPad'ler
// fanı birkaç saniye sonra kendi algoritmasına geri çekiyor; tek seferlik
// yazım "fanlar %100" yazıp gerçekte sessiz kalmak olurdu.
func (c *Controller) Refresh() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active {
		return
	}
	for _, f := range c.fans {
		c.reassertFan(f)
	}
	c.reassertRAPL()
}

// Active turbonun uygulanmış olup olmadığını söyler.
func (c *Controller) Active() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active
}

// Status canlı durumu döner (anlık frekanslar dahil).
func (c *Controller) Status() model.TurboStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statusLocked()
}

// Topology önbellekteki çekirdek yerleşimini döner.
func (c *Controller) Topology() Topology {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.topologyLocked()
}

func (c *Controller) topologyLocked() Topology {
	if c.topo == nil {
		t := DetectTopology(c.root)
		c.topo = &t
	}
	return *c.topo
}

func (c *Controller) statusLocked() model.TurboStatus {
	t := c.topologyLocked()
	st := model.TurboStatus{
		Active:     c.active,
		Items:      append([]model.TurboItem(nil), c.items...),
		Hybrid:     len(t.ECores) > 0,
		PCores:     FormatList(t.PCores),
		ECores:     FormatList(t.ECores),
		CoreMethod: t.Method,
		FansFull:   len(c.fans),
		FansTotal:  c.fansTotal,
		TargetMHz:  int(c.targetKHz / 1000),
		Limiter:    c.limiter,
		Diag:       append([]model.TurboItem(nil), c.diag...),
	}
	if !c.diagAt.IsZero() {
		st.DiagAt = c.diagAt.Unix()
	}
	pols := c.policies()
	st.Driver, st.Governor = c.driverAndGovernor(pols)
	for _, pol := range pols {
		if b := int(c.baseKHz(pol) / 1000); b > st.BaseMHz {
			st.BaseMHz = b
		}
		if m := int(readInt(filepath.Join(pol, "cpuinfo_max_freq")) / 1000); m > st.MaxMHz {
			st.MaxMHz = m
		}
	}
	mhz := CoreMHz(c.root)
	for _, cpu := range t.PCores {
		if cpu < len(mhz) && mhz[cpu] > st.CurMHz {
			st.CurMHz = mhz[cpu]
		}
	}
	if c.active {
		st.Supported = c.supported
		st.Items = append(st.Items, c.coreItem(t))
	} else {
		st.Supported = len(pols) > 0 || exists(c.p(platformProfile)) || c.countFanCandidates() > 0 ||
			len(c.raplZones()) > 0
	}
	st.Summary = Summary(st)
	return st
}

// coreItem, P-çekirdeği tespitinin sonucunu madde olarak verir. Sabitlemeyi
// daemon (supervisor) yapar; burada yalnızca hangi çekirdeklerin neden
// seçildiği anlatılır.
func (c *Controller) coreItem(t Topology) model.TurboItem {
	if len(t.ECores) > 0 {
		return model.TurboItem{Name: "P-çekirdekleri", State: model.TurboOK,
			Detail: fmt.Sprintf("sunucular %s çekirdeklerine sabitlenir; E-çekirdekleri %s (%s)",
				FormatList(t.PCores), FormatList(t.ECores), t.Method)}
	}
	return model.TurboItem{Name: "P-çekirdekleri", State: model.TurboOK,
		Detail: fmt.Sprintf("hibrit değil — sunucular tüm çekirdeklerde (%s)", FormatList(t.PCores))}
}

// Summary tek satırlık özet üretir, ör.
// "performans kipi, P-çekirdekleri 0-7, fanlar %100, hedef 4,7 GHz (azami),
// şu an 4,5 GHz".
func Summary(st model.TurboStatus) string {
	cores := "tüm çekirdekler " + st.PCores
	if st.Hybrid {
		cores = "P-çekirdekleri " + st.PCores
	}
	if st.PCores == "" {
		cores = "tüm çekirdekler"
	}
	if !st.Active {
		return "kapalı"
	}
	if !st.Supported {
		return "bu makinede frekans/fan denetimi yok — yalnızca öncelik ve " + cores
	}
	var parts []string
	switch {
	case st.Governor == "performance":
		parts = append(parts, "performans kipi")
	case st.Driver == "":
		parts = append(parts, "frekans denetimi yok")
	default:
		parts = append(parts, "yönetici "+st.Governor)
	}
	parts = append(parts, cores)
	switch {
	case st.FansTotal == 0:
		parts = append(parts, "fan denetimi yok")
	case st.FansFull == st.FansTotal:
		parts = append(parts, "fanlar %100")
	case st.FansFull == 0:
		parts = append(parts, "fanlar alınamadı")
	default:
		parts = append(parts, fmt.Sprintf("fanlar %%100 (%d/%d)", st.FansFull, st.FansTotal))
	}
	if st.TargetMHz > 0 {
		parts = append(parts, "hedef "+GHz(st.TargetMHz)+" (azami)")
	}
	if st.CurMHz > 0 {
		parts = append(parts, "şu an "+GHz(st.CurMHz))
	}
	// Hedefle anlık arasındaki farkın SEBEBİ özetin kendisinde: kullanıcı
	// "4,4 yerine 2,4" görünce nedenini aramak zorunda kalmasın.
	if st.Limiter != "" {
		parts = append(parts, "sınırlayan: "+st.Limiter)
	}
	return strings.Join(parts, ", ")
}

// GHz MHz değerini Türkçe ondalıkla yazar: 3400 -> "3,4 GHz".
func GHz(mhz int) string {
	return strings.Replace(fmt.Sprintf("%.1f GHz", float64(mhz)/1000), ".", ",", 1)
}
