package turbo

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"mcos/internal/model"
)

// ── TANI ────────────────────────────────────────────────────────────────────
//
// Gerçek PC'de turbo açıkken işlemci "4,4 GHz olması lazımken 2,4 GHz"da
// kaldı, fan dönmedi, makine ısınmadı — ve ekranda hepsi "tamam" görünüyordu.
// Donanım elimizde olmadığı için bir sonraki denemede sebebi KESİN görmek
// gerekiyor. Diagnose her kolun GERÇEK durumunu donanımdan geri okur ve
// ölçer; panel "Turbo tanısı" penceresinde ve /data/log/turbo.log'da
// gösterilir. Kullanıcı bunun ekran görüntüsünü atabilir.
//
// Ölçüm penceresi boyunca (öntanımlı 250 ms) iki örnek alınır: RAPL enerji
// sayacından paket gücü (W), APERF/MPERF'ten çekirdek başına meşgul frekans.
// Bekleme kilit DIŞINDA yapılır; Status bu arada önbellekteki tanıyı döner.

// cpuInfo /proc/cpuinfo'nun ilk işlemci bloğundan üretici, model adı ve
// bayrakları okur.
func (c *Controller) cpuInfo() (vendor, name, flags string) {
	f, err := os.Open(c.p("proc/cpuinfo"))
	if err != nil {
		return "", "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		l := sc.Text()
		if strings.TrimSpace(l) == "" && vendor != "" {
			break
		}
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "vendor_id":
			vendor = strings.TrimSpace(v)
		case "model name":
			name = strings.TrimSpace(v)
		case "flags":
			flags = strings.TrimSpace(v)
		}
	}
	return vendor, name, flags
}

func (c *Controller) cpuVendor() string {
	v, _, _ := c.cpuInfo()
	return v
}

// energySnap RAPL bölgelerinin enerji sayaçlarını okur (µJ).
func energySnap(zones []raplZone) map[string]int64 {
	out := map[string]int64{}
	for _, z := range zones {
		if b, err := readStr(filepath.Join(z.dir, "energy_uj")); err == nil {
			if v, err := strconv.ParseInt(b, 10, 64); err == nil {
				out[z.dir] = v
			}
		}
	}
	return out
}

// Diagnose ölçer, tanı maddelerini üretir ve önbelleğe alır. Dönen durum
// Status ile aynıdır (tanı dahil).
func (c *Controller) Diagnose(window time.Duration) model.TurboStatus {
	c.mu.Lock()
	topo := c.topologyLocked()
	sleep, now := c.sleep, c.now
	c.mu.Unlock()

	zones := c.raplZones()
	cpus := topo.Online
	t0 := now()
	e0 := energySnap(zones)
	m0 := c.msrSnap(cpus)
	if window > 0 {
		sleep(window)
	}
	e1 := energySnap(zones)
	m1 := c.msrSnap(cpus)
	dt := now().Sub(t0).Seconds()

	c.mu.Lock()
	defer c.mu.Unlock()
	d := &diagBuilder{c: c}
	d.build(topo, zones, e0, e1, m0, m1, dt)
	c.diag = d.items
	c.diagAt = now()
	c.limiter = d.limiter()
	return c.statusLocked()
}

type diagBuilder struct {
	c     *Controller
	items []model.TurboItem
	// Sınırlayan tahmini için toplanan kanıtlar.
	noDriver, noTurbo, thermal, pl1, pl2, power, multicore, acpiThrottle bool
	capMHz, maxMHz, busyTop, pkgTemp                                     int
	pl1W                                                                 int64
	haveBusy                                                             bool
}

func (d *diagBuilder) add(name, state, detail string) {
	d.items = append(d.items, model.TurboItem{Name: name, State: state, Detail: detail})
}

func (d *diagBuilder) build(topo Topology, zones []raplZone, e0, e1 map[string]int64, m0, m1 map[int]msrSample, dt float64) {
	c := d.c
	vendor, name, flags := c.cpuInfo()
	// Açılış iletileri değişmez; bir kez taranır (RefreshKmsg yeniler).
	if c.kmsg == nil {
		c.kmsg = c.scanKmsg()
	}

	// ── İşlemci ve sürücü ──────────────────────────────────────────────
	cpuLine := name
	if cpuLine == "" {
		cpuLine = "(model adı okunamadı)"
	}
	cpuLine += fmt.Sprintf(" — %d çekirdek çevrimiçi", len(topo.Online))
	if len(topo.ECores) > 0 {
		cpuLine += fmt.Sprintf(", P %s / E %s", FormatList(topo.PCores), FormatList(topo.ECores))
	}
	d.add("İşlemci", model.TurboInfo, cpuLine)

	pols := c.policies()
	if len(pols) == 0 {
		d.noDriver = true
		det := "YOK — cpufreq sürücüsü yüklenmedi; frekans BIOS'un bıraktığı durumda kalır"
		if vendor == "AuthenticAMD" {
			det += " (AMD: çekirdekte X86_AMD_PSTATE / X86_ACPI_CPUFREQ gerekli)"
		} else if vendor == "" || strings.Contains(flags, "hypervisor") {
			det += " (sanal makinede beklenen durum)"
		}
		d.add("Frekans sürücüsü", model.TurboFailed, det)
	} else {
		drv, _ := readStr(filepath.Join(pols[0], "scaling_driver"))
		det := drv
		if s, err := readStr(c.p(cpuDir + "/intel_pstate/status")); err == nil {
			det += ", intel_pstate kipi " + s
		}
		if s, err := readStr(c.p(cpuDir + "/amd_pstate/status")); err == nil {
			det += ", amd_pstate kipi " + s
		}
		if hasWord(flags, "hwp") {
			det += ", HWP var"
		}
		d.add("Frekans sürücüsü", model.TurboInfo, det)
	}

	// ── Turbo anahtarları ──────────────────────────────────────────────
	var tk []string
	if v, err := readStr(c.p(cpuDir + "/intel_pstate/no_turbo")); err == nil {
		tk = append(tk, "no_turbo="+v)
		if v == "1" && c.active {
			d.noTurbo = true
		}
	}
	if v, err := readStr(c.p(cpuDir + "/cpufreq/boost")); err == nil {
		tk = append(tk, "boost="+v)
		if v == "0" && c.active {
			d.noTurbo = true
		}
	}
	if len(tk) > 0 {
		st := model.TurboInfo
		if d.noTurbo {
			st = model.TurboFailed
		}
		d.add("Turbo anahtarı", st, strings.Join(tk, ", "))
	}

	// ── Politikalar: istenen ve geri okunan ────────────────────────────
	d.policies(pols)

	// ── Ölçülen frekans ────────────────────────────────────────────────
	d.measured(topo, m0, m1, dt)

	// ── Güç sınırları ve tüketim ───────────────────────────────────────
	d.rapl(zones, e0, e1, dt, vendor)

	// ── MSR ─────────────────────────────────────────────────────────────
	lines, lim, err := c.msrDiag(vendor)
	switch {
	case err != nil && len(lines) == 0:
		d.add("MSR", model.TurboUnsupported, "okunamadı ("+err.Error()+") — çekirdekte CONFIG_X86_MSR yoksa /dev/cpu/N/msr olmaz")
	default:
		for _, l := range lines {
			st := model.TurboInfo
			if strings.Contains(l, "KAPALI") || strings.Contains(l, "GÜÇ SINIRINDA") || strings.Contains(l, "AZAMİ DEĞİL") ||
				strings.Contains(l, "KİLİTLİ") || (strings.Contains(l, "şu an: ") && !strings.Contains(l, "şu an: yok")) {
				st = model.TurboPartial
			}
			d.add("MSR", st, l)
		}
	}
	// ||: rapl() tüketimden güç sınırını zaten bulmuş olabilir.
	d.noTurbo = d.noTurbo || lim["noturbo"]
	d.pl1, d.pl2, d.power = d.pl1 || lim["pl1"], d.pl2 || lim["pl2"], d.power || lim["power"]
	d.thermal, d.multicore = d.thermal || lim["thermal"], d.multicore || lim["multicore"]

	// ── Isıl kısıtlama ─────────────────────────────────────────────────
	d.throttle()

	// ── Sıcaklık ───────────────────────────────────────────────────────
	tl, pkg := c.tempLines()
	d.pkgTemp = int(pkg / 1000)
	if len(tl) == 0 {
		d.add("Sıcaklık", model.TurboUnsupported, "işlemci sıcaklığı okunamadı (çekirdekte SENSORS_CORETEMP / SENSORS_K10TEMP?)")
	}
	for _, l := range tl {
		d.add("Sıcaklık", model.TurboInfo, l)
	}

	// ── Fanlar ─────────────────────────────────────────────────────────
	d.items = append(d.items, c.fanDiag()...)

	// ── Platform profili ───────────────────────────────────────────────
	if v, err := readStr(c.p(platformProfile)); err == nil {
		ch, _ := readStr(c.p(platformProfile + "_choices"))
		d.add("Platform profili", model.TurboInfo, v+" (seçenekler: "+ch+")")
	} else {
		d.add("Platform profili", model.TurboUnsupported, "yok (üretici sürücüsü yüklenmedi ya da desteklemiyor)")
	}

	// ── Geri okuma uyuşmazlıkları ──────────────────────────────────────
	for _, m := range c.mismatches {
		d.add("Geri okuma", model.TurboPartial, m)
	}

	// ── Çekirdek günlüğü (en başta taranır: fanDiag çakışma iletisine bakar)
	for _, l := range c.kmsg {
		d.add("Çekirdek günlüğü", model.TurboInfo, l)
	}

	lm := d.limiter()
	if lm == "" {
		lm = "belirlenemedi — sunucu yük altındayken yeniden bakın"
		if d.haveBusy && d.maxMHz > 0 && d.busyTop*100 >= d.maxMHz*95 {
			lm = "yok — yükteki çekirdek azami frekansta"
		}
	}
	d.add("Sınırlayan", model.TurboInfo, lm)
}

// policies, politikaları istenen/geri okunan değerleriyle gruplar: 16
// çekirdekte 16 aynı satır yerine "cpu0-15: …".
func (d *diagBuilder) policies(pols []string) {
	c := d.c
	type grp struct {
		cpus []int
		ok   bool
	}
	order := []string{}
	groups := map[string]*grp{}
	for _, pol := range pols {
		f := func(n string) string { return firstOf(readStr(filepath.Join(pol, n))) }
		mhz := func(n string) int { return int(readInt(filepath.Join(pol, n)) / 1000) }
		hw, mn, mx := mhz("cpuinfo_max_freq"), mhz("scaling_min_freq"), mhz("scaling_max_freq")
		if hw > d.maxMHz {
			d.maxMHz = hw
		}
		if mx > 0 && hw > 0 && mx < hw && (d.capMHz == 0 || mx < d.capMHz) {
			d.capMHz = mx
		}
		key := fmt.Sprintf("yönetici %s, EPP %s, en düşük %d / en yüksek %d / donanım azami %d / temel %d MHz",
			dash(f("scaling_governor")), dash(f("energy_performance_preference")), mn, mx, hw, int(c.baseKHz(pol)/1000))
		ok := hw > 0 && mn == hw && mx == hw && f("scaling_governor") == "performance"
		if mx > 0 && hw > 0 && mx < hw {
			key += fmt.Sprintf(" — TAVAN: en yüksek %d < donanım %d (platform/ısıl QoS sınırı)", mx, hw)
		}
		g := groups[key]
		if g == nil {
			g = &grp{ok: ok}
			groups[key] = g
			order = append(order, key)
		}
		cpus := ParseList(firstOf(readStr(filepath.Join(pol, "related_cpus"))))
		if len(cpus) == 0 {
			cpus = []int{polNum(pol)}
		}
		g.cpus = append(g.cpus, cpus...)
	}
	for _, k := range order {
		g := groups[k]
		sort.Ints(g.cpus)
		st := model.TurboInfo
		if c.active {
			st = model.TurboOK
			if !g.ok {
				st = model.TurboPartial
			}
		}
		d.add("cpu "+FormatList(g.cpus), st, k)
	}
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// measured çekirdek başına ÖLÇÜLEN frekansı yazar. MSR varsa gerçek meşgul
// frekans (APERF/MPERF) ve meşguliyet; yoksa sürücünün bildirdiği anlık
// frekans (scaling_cur_freq — intel_pstate'te bu da APERF/MPERF'ten gelir,
// ama boştaki çekirdekte eski kalabilir).
func (d *diagBuilder) measured(topo Topology, m0, m1 map[int]msrSample, dt float64) {
	var tscHz float64
	if a, b := m0[firstCPU(topo)], m1[firstCPU(topo)]; a.ok && b.ok && dt > 0 && b.tsc > a.tsc {
		tscHz = float64(b.tsc-a.tsc) / dt
	}
	var cells []string
	if tscHz > 0 {
		for _, cpu := range topo.Online {
			mhz, busy, ok := busyMHz(m0[cpu], m1[cpu], tscHz)
			if !ok {
				continue
			}
			d.haveBusy = true
			if busy >= 50 && mhz > d.busyTop {
				d.busyTop = mhz
			}
			cells = append(cells, fmt.Sprintf("%d: %d MHz %%%d", cpu, mhz, busy))
		}
	}
	name := "Ölçülen (MSR)"
	if len(cells) == 0 {
		name = "Ölçülen (sürücü)"
		mhz := CoreMHz(d.c.root)
		for cpu, v := range mhz {
			if v > 0 {
				cells = append(cells, fmt.Sprintf("%d: %d MHz", cpu, v))
			}
		}
	}
	if len(cells) == 0 {
		d.add("Ölçülen frekans", model.TurboUnsupported, "okunamadı (ne MSR ne cpufreq)")
		return
	}
	// Satır başına 6 çekirdek: 1366 piksel genişlikte de tek satıra sığsın.
	for i := 0; i < len(cells); i += 6 {
		j := i + 6
		if j > len(cells) {
			j = len(cells)
		}
		d.add(name, model.TurboInfo, strings.Join(cells[i:j], " · "))
	}
	if name == "Ölçülen (MSR)" {
		top := "azami frekans"
		if d.maxMHz > 0 {
			top = "azami " + GHz(d.maxMHz)
		}
		d.add(name, model.TurboInfo, "MHz = çekirdek UYANIKKEN çalıştığı frekans; % = meşguliyet. "+top+" yükteki (%50+) çekirdekte görünür")
	}
}

func firstCPU(t Topology) int {
	if len(t.Online) > 0 {
		return t.Online[0]
	}
	return 0
}

// rapl güç sınırlarını (şu an / özgün) ve ölçülen tüketimi yazar.
func (d *diagBuilder) rapl(zones []raplZone, e0, e1 map[string]int64, dt float64, vendor string) {
	c := d.c
	if len(zones) == 0 {
		det := "powercap yok — PL1/PL2 okunamıyor, tüketim ölçülemiyor (çekirdekte CONFIG_INTEL_RAPL?)"
		if vendor == "AuthenticAMD" {
			det += "; " + AMDRAPLNote
		}
		d.add("RAPL", model.TurboUnsupported, det)
		return
	}
	// Etkin PL1: tüm paket bölgelerinin (MSR ve MMIO) en küçüğü — donanım
	// ikisini birden uygular. Tüketim karşılaştırması bununla yapılır.
	for _, z := range zones {
		if k := z.con("long_term"); k != nil && strings.HasPrefix(z.name, "package") {
			if cur := readInt(k.limit); cur > 0 && (d.pl1W == 0 || cur < d.pl1W) {
				d.pl1W = cur
			}
		}
	}
	for _, z := range zones {
		var parts []string
		for _, k := range z.cons {
			cur := readInt(k.limit)
			s := fmt.Sprintf("%s %s", conName(k.name), W(cur))
			if ch := c.journalFind(k.limit); ch != nil && ch.Orig != strconv.FormatInt(cur, 10) {
				if o, err := strconv.ParseInt(ch.Orig, 10, 64); err == nil {
					s += " (özgün " + W(o) + ")"
				}
			}
			if t := readInt(k.tau); t > 0 {
				s += " τ " + sec(t)
			}
			if m := readInt(k.max); m > 0 && m <= raplMaxSane {
				s += " azami " + W(m)
			}
			parts = append(parts, s)
		}
		if en, err := readStr(filepath.Join(z.dir, "enabled")); err == nil {
			// get_domain_enable kilitli bölgede de 0 döner: iki anlamı var.
			if en == "0" && len(z.cons) > 0 {
				en += " (PL1 uygulanmıyor ya da BIOS kilitli)"
			}
			parts = append(parts, "enabled="+en)
		}
		st := model.TurboInfo
		if a, ok0 := e0[z.dir]; ok0 && dt > 0 {
			if b, ok1 := e1[z.dir]; ok1 {
				de := b - a
				if de < 0 { // sayaç taştı
					de += readInt(filepath.Join(z.dir, "max_energy_range_uj"))
				}
				if de >= 0 {
					pw := int64(float64(de) / dt) // µJ/s = µW
					parts = append(parts, "tüketim "+W(pw))
					if strings.HasPrefix(z.name, "package") && d.pl1W > 0 && pw*100 >= d.pl1W*92 {
						d.power = true
						st = model.TurboPartial
						parts = append(parts, "TÜKETİM PL1'E DAYANMIŞ")
					}
				}
			}
		}
		if len(z.cons) == 0 && strings.HasPrefix(z.name, "package") && vendor == "AuthenticAMD" {
			parts = append(parts, "güç sınırı arayüzü yok (SMU)")
		}
		d.add("RAPL "+z.label(), st, strings.Join(parts, ", "))
	}
	if c.raplReasserts > 0 {
		d.add("RAPL", model.TurboPartial, fmt.Sprintf("firmware güç sınırını %d kez geri çekti; turbo her 15 sn'de yeniden yazıyor", c.raplReasserts))
	}
}

func conName(n string) string {
	switch n {
	case "long_term":
		return "PL1"
	case "short_term":
		return "PL2"
	case "peak_power":
		return "PL4"
	}
	return n
}

func (c *Controller) journalFind(path string) *change {
	if c.j == nil {
		return nil
	}
	return c.j.find(path)
}

// throttle kısıtlama sayaçlarını ve işlemci soğutma aygıtlarını yazar.
func (d *diagBuilder) throttle() {
	c := d.c
	cnt := c.throttleCounts()
	if cnt["cpus"] == 0 {
		d.add("Kısıtlama sayaçları", model.TurboUnsupported, "thermal_throttle yok (Intel dışı işlemci ya da X86_THERMAL_VECTOR kapalı)")
	} else {
		s := fmt.Sprintf("çekirdek %d olay (%d ms), paket %d olay (%d ms)", cnt["core"], cnt["core_ms"], cnt["pkg"], cnt["pkg_ms"])
		st := model.TurboInfo
		if c.active && c.thrBase != nil {
			dc, dp := cnt["core"]-c.thrBase["core"], cnt["pkg"]-c.thrBase["pkg"]
			s += fmt.Sprintf("; turbo açıldığından beri +%d / +%d", dc, dp)
			if dc > 0 || dp > 0 {
				st = model.TurboPartial
				d.thermal = true
			}
		}
		d.add("Kısıtlama sayaçları", st, s)
	}
	devs := c.throttleCdevs()
	if len(devs) == 0 {
		return
	}
	byType := map[string][]string{}
	var types []string
	for _, dv := range devs {
		t, _ := readStr(filepath.Join(dv, "type"))
		cur, mx := readInt(filepath.Join(dv, "cur_state")), readInt(filepath.Join(dv, "max_state"))
		if cur > 0 {
			d.acpiThrottle = true
		}
		if _, ok := byType[t]; !ok {
			types = append(types, t)
		}
		byType[t] = append(byType[t], fmt.Sprintf("%d/%d", cur, mx))
	}
	for _, t := range types {
		st := model.TurboInfo
		for _, v := range byType[t] {
			if !strings.HasPrefix(v, "0/") && !strings.HasPrefix(v, "-1/") {
				st = model.TurboPartial
			}
		}
		d.add("Kısıtlama aygıtı", st, fmt.Sprintf("%s ×%d: %s", t, len(byType[t]), strings.Join(byType[t], " ")))
	}
}

// limiter kanıtlardan tek satırlık sınırlayan tahmini üretir. Sıra, en
// bağlayıcı etkenden başlar: sürücü yoksa ya da BIOS turbo'yu kapattıysa
// güç sınırının hiçbir önemi kalmaz.
func (d *diagBuilder) limiter() string {
	switch {
	case d.noDriver:
		return "frekans sürücüsü yok"
	case d.noTurbo:
		return "turbo BIOS'ta kapalı"
	case d.capMHz > 0:
		return fmt.Sprintf("frekans tavanı %d MHz (platform/ısıl QoS)", d.capMHz)
	case d.thermal:
		if d.pkgTemp > 0 {
			return fmt.Sprintf("ısıl sınır (paket %d °C)", d.pkgTemp)
		}
		return "ısıl sınır"
	case d.pl1:
		if d.pl1W > 0 {
			return "PL1 güç sınırı (" + W(d.pl1W) + ")"
		}
		return "PL1 güç sınırı"
	case d.pl2:
		return "PL2 güç sınırı"
	case d.power:
		if d.pl1W > 0 {
			return "güç sınırı (PL1 " + W(d.pl1W) + ")"
		}
		return "güç sınırı"
	case d.acpiThrottle:
		return "ACPI ısıl kısıtlama (Processor soğutma aygıtı)"
	case d.multicore:
		return "çok çekirdek turbo tavanı (etkin çekirdek sayısı)"
	}
	return ""
}

// ── Çekirdek günlüğü ────────────────────────────────────────────────────────

// kmsgPatterns: turbo ile ilgili çekirdek iletileri (küçük harfle aranır).
// Örnekler: "intel_rapl_common: package-0:package:long_term locked by BIOS",
// "intel_pstate: Turbo disabled by BIOS or unavailable on processor",
// "ACPI Warning: SystemIO range … conflicts with OpRegion" (Super I/O fan
// sürücüsünün neden yüklenmediği), "thinkpad_acpi: … fan control".
var kmsgPatterns = []string{
	"intel_pstate", "amd_pstate", "amd-pstate", "acpi-cpufreq", "acpi_cpufreq",
	"intel_rapl", "locked by bios", "turbo", "powerclamp", "int3400", "proc_thermal",
	"thinkpad_acpi", "dell_smm", "i8k", "hp_wmi", "asus_wmi", "asus-nb-wmi", "ideapad",
	"nct6775", "nct6683", "it87", "conflicts with opregion", "resource conflict",
	"clock throttled", "above threshold", "prochot", "coretemp", "k10temp",
}

const kmsgMax = 30

// scanKmsg /dev/kmsg'yi baştan okur ve ilgili satırları döner.
//
// Ham syscall ile, O_NONBLOCK: os.File, karakter aygıtını Go'nun
// bekleyicisine bağlar ve EAGAIN'de YENİ kayıt gelene kadar bekler; tanı
// asılı kalırdı. /dev/kmsg her read'de TEK kayıt verir ("pri,seq,ts,-;ileti"),
// devam satırları boşlukla başlar ve atlanır. Sahte kökte düz dosyadır; aynı
// ayrıştırma satır satır çalışır.
func (c *Controller) scanKmsg() []string {
	fd, err := syscall.Open(c.p("dev/kmsg"), syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return []string{}
	}
	defer syscall.Close(fd)
	out := []string{}
	seen := map[string]bool{}
	buf := make([]byte, 16*1024)
	var carry string
	for i := 0; i < 20000 && len(out) < kmsgMax; i++ {
		n, err := syscall.Read(fd, buf)
		if err == syscall.EPIPE { // kayıt, okunmadan halka tampondan düştü
			continue
		}
		if err != nil || n <= 0 {
			break
		}
		data := carry + string(buf[:n])
		lines := strings.Split(data, "\n")
		carry = lines[len(lines)-1]
		for _, l := range lines[:len(lines)-1] {
			if l == "" || l[0] == ' ' {
				continue
			}
			msg := l
			if h, m, ok := strings.Cut(l, ";"); ok && strings.Contains(h, ",") {
				msg = m
			}
			low := strings.ToLower(msg)
			// Komut satırı iletisi her parametreyi içerir ("thinkpad_acpi.
			// fan_control=1" kalıpla eşleşir); bilgi taşımayan gürültü.
			if strings.Contains(low, "command line:") {
				continue
			}
			for _, p := range kmsgPatterns {
				if strings.Contains(low, p) {
					if !seen[msg] && len(out) < kmsgMax {
						seen[msg] = true
						out = append(out, msg)
					}
					break
				}
			}
		}
	}
	return out
}

// Report tanıyı düz metin olarak verir (/data/log/turbo.log ve mcosctl).
func Report(st model.TurboStatus, reason string, when time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== %s — %s ===\n", when.Format("2006-01-02 15:04:05"), reason)
	fmt.Fprintf(&b, "Özet: %s\n", st.Summary)
	if st.Limiter != "" {
		fmt.Fprintf(&b, "Sınırlayan: %s\n", st.Limiter)
	}
	for _, it := range st.Items {
		fmt.Fprintf(&b, "  [%s] %s: %s\n", it.State, it.Name, it.Detail)
	}
	if len(st.Diag) > 0 {
		b.WriteString("  --- tanı ---\n")
	}
	for _, it := range st.Diag {
		fmt.Fprintf(&b, "  [%s] %s: %s\n", it.State, it.Name, it.Detail)
	}
	return b.String()
}
