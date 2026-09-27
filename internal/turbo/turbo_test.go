package turbo

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"mcos/internal/model"
)

// ── Sahte sysfs ─────────────────────────────────────────────────────────────
//
// Her sınama t.TempDir() altında bir kök kurar; ana makinenin gerçek /sys'ine
// ASLA yazılmaz. Düz dosyalar çekirdeğin yan etkilerini (EBUSY, no_turbo'nun
// cpuinfo_max_freq'i değiştirmesi, sürücünün PWM'i yok sayması) kendiliğinden
// yapamaz; bunları fakeKernel yazıcısı taklit eder.

type fake struct {
	t    *testing.T
	root string
}

func newFake(t *testing.T) *fake { return &fake{t: t, root: t.TempDir()} }

func (f *fake) put(rel, val string) {
	f.t.Helper()
	p := filepath.Join(f.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(val+"\n"), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// linkCPU gerçek sysfs'teki gibi cpuN/cpufreq -> ../cpufreq/policyN bağı kurar.
func (f *fake) linkCPU(n int) {
	f.t.Helper()
	d := filepath.Join(f.root, cpuD, "cpu"+itoa(n))
	if err := os.MkdirAll(d, 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Symlink("../cpufreq/policy"+itoa(n), filepath.Join(d, "cpufreq")); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fake) get(rel string) string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.root, rel))
	if err != nil {
		f.t.Fatalf("%s okunamadı: %v", rel, err)
	}
	return strings.TrimSpace(string(b))
}

func (f *fake) want(rel, val string) {
	f.t.Helper()
	if got := f.get(rel); got != val {
		f.t.Errorf("%s = %q, beklenen %q", rel, got, val)
	}
}

// kernel, çekirdeğin sysfs yan etkilerini taklit eden yazıcıdır ve her
// yazımı sırasıyla kaydeder.
type kernel struct {
	f    *fake
	mu   sync.Mutex
	log  []string
	hook func(rel, val string) (handled bool, err error)
}

func (k *kernel) write(path, val string) error {
	rel, _ := filepath.Rel(k.f.root, path)
	k.mu.Lock()
	k.log = append(k.log, rel+"="+val)
	k.mu.Unlock()
	if k.hook != nil {
		if handled, err := k.hook(rel, val); handled || err != nil {
			return err
		}
	}
	return writeSysfs(path, val)
}

func (k *kernel) wrote(rel, val string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, l := range k.log {
		if l == rel+"="+val {
			return true
		}
	}
	return false
}

func newCtl(f *fake) (*Controller, *kernel) {
	c := New(f.root, filepath.Join(f.root, "run/mcos/turbo.json"))
	k := &kernel{f: f}
	c.SetWriter(k.write)
	return c, k
}

func item(st model.TurboStatus, name string) model.TurboItem {
	for _, it := range st.Items {
		if it.Name == name {
			return it
		}
	}
	return model.TurboItem{}
}

const cpuD = "sys/devices/system/cpu"

// ── intel_pstate ────────────────────────────────────────────────────────────

// intelPstate, HWP'li bir Intel dizüstünü kurar: temel 1,7 GHz, turbo 4,7 GHz,
// kullanıcının gördüğü gibi "powersave" + "balance_power" ve 400 MHz taban.
// no_turbo=1 iken çekirdek cpuinfo_max_freq'i turbo olmayan azamiye indirir;
// bu, hedefin no_turbo=0'dan ÖNCE okunması hatasını yakalamak için kuruldu.
func intelPstate(f *fake, n int) {
	f.put(cpuD+"/online", "0-"+itoa(n-1))
	f.put(cpuD+"/intel_pstate/status", "active")
	f.put(cpuD+"/intel_pstate/no_turbo", "1")
	f.put(cpuD+"/intel_pstate/min_perf_pct", "9")
	f.put(cpuD+"/intel_pstate/max_perf_pct", "100")
	for i := 0; i < n; i++ {
		p := cpuD + "/cpufreq/policy" + itoa(i) + "/"
		f.put(p+"scaling_driver", "intel_pstate")
		f.put(p+"related_cpus", itoa(i))
		f.put(p+"scaling_available_governors", "performance powersave")
		f.put(p+"scaling_governor", "powersave")
		f.put(p+"energy_performance_available_preferences", "default performance balance_performance balance_power power")
		f.put(p+"energy_performance_preference", "balance_power")
		f.put(p+"base_frequency", "1700000")
		f.put(p+"cpuinfo_min_freq", "400000")
		f.put(p+"cpuinfo_max_freq", "1700000") // no_turbo=1
		f.put(p+"scaling_min_freq", "400000")
		f.put(p+"scaling_max_freq", "1700000")
		f.put(p+"scaling_cur_freq", "400000")
		f.linkCPU(i)
	}
}

func intelHook(f *fake, n int) func(rel, val string) (bool, error) {
	return func(rel, val string) (bool, error) {
		switch {
		case rel == cpuD+"/intel_pstate/no_turbo":
			mx := "4700000"
			if val == "1" {
				mx = "1700000"
			}
			for i := 0; i < n; i++ {
				f.put(cpuD+"/cpufreq/policy"+itoa(i)+"/cpuinfo_max_freq", mx)
			}
		case strings.HasSuffix(rel, "/energy_performance_preference"):
			// intel_pstate.c:795 — performance politikasında EPP yazımı EBUSY.
			gov := f.get(filepath.Dir(rel) + "/scaling_governor")
			if gov == "performance" && val != "performance" {
				return true, syscall.EBUSY
			}
		case strings.HasSuffix(rel, "/scaling_governor") && val == "performance":
			// Çalışan çekirdek anında hedefe tırmansın (anlık MHz gösterimi).
			f.put(filepath.Dir(rel)+"/scaling_cur_freq", "4500000")
		}
		return false, nil
	}
}

// Kullanıcının şikâyeti: turbo açık, işlemci 400 MHz'de. Hedef artık temel
// frekans değil, işlemcinin EN YÜKSEK frekansıdır (scaling_min_freq =
// cpuinfo_max_freq = 4,7 GHz).
func TestIntelPstateTargetsMaxFrequency(t *testing.T) {
	f := newFake(t)
	intelPstate(f, 4)
	c, k := newCtl(f)
	k.hook = intelHook(f, 4)

	st := c.Enable()

	f.want(cpuD+"/intel_pstate/no_turbo", "0")
	f.want(cpuD+"/intel_pstate/min_perf_pct", "100")
	f.want(cpuD+"/intel_pstate/max_perf_pct", "100")
	for i := 0; i < 4; i++ {
		p := cpuD + "/cpufreq/policy" + itoa(i) + "/"
		f.want(p+"scaling_governor", "performance")
		f.want(p+"energy_performance_preference", "performance")
		f.want(p+"scaling_max_freq", "4700000")
		// ASIL SINAMA: taban temel frekansta (1700000) ya da 400 MHz'de
		// kalırsa turbo "işe yaramıyor" demektir.
		f.want(p+"scaling_min_freq", "4700000")
	}
	if st.TargetMHz != 4700 || st.BaseMHz != 1700 || st.MaxMHz != 4700 {
		t.Errorf("hedef/temel/azami = %d/%d/%d MHz, beklenen 4700/1700/4700", st.TargetMHz, st.BaseMHz, st.MaxMHz)
	}
	if st.CurMHz != 4500 {
		t.Errorf("anlık MHz = %d, beklenen 4500", st.CurMHz)
	}
	if !st.Supported || st.Driver != "intel_pstate" || st.Governor != "performance" {
		t.Errorf("durum yanlış: %+v", st)
	}
	if !strings.Contains(st.Summary, "hedef 4,7 GHz (azami)") || !strings.Contains(st.Summary, "şu an 4,5 GHz") {
		t.Errorf("özet hedefi ve anlık frekansı göstermiyor: %q", st.Summary)
	}
	if it := item(st, "Frekans hedefi"); it.State != model.TurboOK || !strings.Contains(it.Detail, "ısı/güç sınırı") {
		t.Errorf("frekans hedefi maddesi: %+v", it)
	}

	// Kapatınca HER ŞEY özgün hâline dönmeli. Sıra önemli: EPP, yönetici
	// performance iken geri yazılırsa EBUSY döner (hook) ve kalıcı kalırdı.
	c.Disable()
	f.want(cpuD+"/intel_pstate/no_turbo", "1")
	f.want(cpuD+"/intel_pstate/min_perf_pct", "9")
	for i := 0; i < 4; i++ {
		p := cpuD + "/cpufreq/policy" + itoa(i) + "/"
		f.want(p+"scaling_governor", "powersave")
		f.want(p+"energy_performance_preference", "balance_power")
		f.want(p+"scaling_min_freq", "400000")
		f.want(p+"scaling_max_freq", "1700000")
	}
	if _, err := os.Stat(filepath.Join(f.root, "run/mcos/turbo.json")); !os.IsNotExist(err) {
		t.Errorf("geri yüklemeden sonra günlük dosyası silinmedi (err=%v)", err)
	}
}

// ── acpi-cpufreq ────────────────────────────────────────────────────────────

// acpi-cpufreq'te Intel turbo durumu "temel + 1 MHz" sahte girdisidir
// (3401000). Hedef bu girdi olmalı; temel (3400000) değil.
func TestACPICpufreqBoostAndTurboEntry(t *testing.T) {
	f := newFake(t)
	f.put(cpuD+"/online", "0-1")
	f.put(cpuD+"/cpufreq/boost", "0")
	for i := 0; i < 2; i++ {
		p := cpuD + "/cpufreq/policy" + itoa(i) + "/"
		f.put(p+"scaling_driver", "acpi-cpufreq")
		f.put(p+"related_cpus", itoa(i))
		f.put(p+"scaling_available_governors", "performance schedutil")
		f.put(p+"scaling_governor", "schedutil")
		f.put(p+"scaling_available_frequencies", "3401000 3400000 2800000 2000000 800000")
		f.put(p+"cpuinfo_max_freq", "3401000")
		f.put(p+"scaling_min_freq", "800000")
		f.put(p+"scaling_max_freq", "3401000")
	}
	c, _ := newCtl(f)
	st := c.Enable()

	f.want(cpuD+"/cpufreq/boost", "1")
	for i := 0; i < 2; i++ {
		p := cpuD + "/cpufreq/policy" + itoa(i) + "/"
		f.want(p+"scaling_governor", "performance")
		f.want(p+"scaling_min_freq", "3401000")
	}
	if st.BaseMHz != 3400 || st.TargetMHz != 3401 {
		t.Errorf("temel/hedef = %d/%d, beklenen 3400/3401", st.BaseMHz, st.TargetMHz)
	}
	if it := item(st, "Enerji tercihi (EPP)"); it.State != model.TurboUnsupported {
		t.Errorf("acpi-cpufreq'te EPP yok, madde %q olmalı: %+v", model.TurboUnsupported, it)
	}
	if it := item(st, "Turbo Boost"); it.State != model.TurboOK {
		t.Errorf("boost maddesi: %+v", it)
	}
	c.Disable()
	f.want(cpuD+"/cpufreq/boost", "0")
	f.want(cpuD+"/cpufreq/policy0/scaling_governor", "schedutil")
	f.want(cpuD+"/cpufreq/policy0/scaling_min_freq", "800000")
}

// ── amd-pstate ──────────────────────────────────────────────────────────────

// amd-pstate'te boost kapalıyken cpuinfo_max_freq nominal frekanstır
// (amd-pstate.c amd_pstate_set_boost); boost=1 ÖNCE yazılmalı ki hedef
// 5,0 GHz olsun, 3,8 GHz değil.
func TestAMDPstateBoostThenMax(t *testing.T) {
	f := newFake(t)
	f.put(cpuD+"/online", "0-1")
	f.put(cpuD+"/cpufreq/boost", "0")
	for i := 0; i < 2; i++ {
		p := cpuD + "/cpufreq/policy" + itoa(i) + "/"
		f.put(p+"scaling_driver", "amd-pstate-epp")
		f.put(p+"related_cpus", itoa(i))
		f.put(p+"scaling_available_governors", "performance powersave")
		f.put(p+"scaling_governor", "powersave")
		f.put(p+"energy_performance_available_preferences", "default performance balance_performance balance_power power")
		f.put(p+"energy_performance_preference", "balance_performance")
		f.put(p+"cpuinfo_max_freq", "3800000")
		f.put(p+"scaling_min_freq", "400000")
		f.put(p+"scaling_max_freq", "3800000")
		f.put(cpuD+"/cpu"+itoa(i)+"/acpi_cppc/nominal_freq", "3800")
	}
	c, k := newCtl(f)
	k.hook = func(rel, val string) (bool, error) {
		if rel == cpuD+"/cpufreq/boost" {
			mx := "3800000"
			if val == "1" {
				mx = "5000000"
			}
			f.put(cpuD+"/cpufreq/policy0/cpuinfo_max_freq", mx)
			f.put(cpuD+"/cpufreq/policy1/cpuinfo_max_freq", mx)
		}
		if strings.HasSuffix(rel, "/energy_performance_preference") &&
			f.get(filepath.Dir(rel)+"/scaling_governor") == "performance" && val != "performance" {
			return true, syscall.EBUSY // amd-pstate.c:220
		}
		return false, nil
	}
	st := c.Enable()
	f.want(cpuD+"/cpufreq/boost", "1")
	f.want(cpuD+"/cpufreq/policy0/scaling_min_freq", "5000000")
	f.want(cpuD+"/cpufreq/policy1/scaling_min_freq", "5000000")
	f.want(cpuD+"/cpufreq/policy0/energy_performance_preference", "performance")
	if st.BaseMHz != 3800 || st.TargetMHz != 5000 {
		t.Errorf("temel/hedef = %d/%d, beklenen 3800/5000", st.BaseMHz, st.TargetMHz)
	}
	c.Disable()
	f.want(cpuD+"/cpufreq/boost", "0")
	f.want(cpuD+"/cpufreq/policy0/energy_performance_preference", "balance_performance")
	f.want(cpuD+"/cpufreq/policy0/scaling_governor", "powersave")
	f.want(cpuD+"/cpufreq/policy0/scaling_min_freq", "400000")
}

// ── Sanal makine: hiçbir kol yok ────────────────────────────────────────────

// QEMU'da cpufreq, hwmon ve platform profili yoktur. Turbo "açık" diye
// yalan söylememeli; her kol madde madde "desteklenmiyor" demeli.
func TestVirtualMachineReportsUnsupported(t *testing.T) {
	f := newFake(t)
	f.put(cpuD+"/online", "0-3")
	f.put("proc/cpuinfo", "processor\t: 0\ncpu MHz\t\t: 2995.200\n\nprocessor\t: 1\ncpu MHz\t\t: 2995.200\n")
	c, k := newCtl(f)
	st := c.Enable()
	if st.Supported {
		t.Errorf("sanal makinede Supported=true: %+v", st)
	}
	for _, n := range []string{"İşlemci frekansı", "Platform profili", "Fanlar", "Güç sınırı (RAPL)", "Isıl kısıtlama"} {
		if it := item(st, n); it.State != model.TurboUnsupported {
			t.Errorf("%s: durum %q, beklenen %q (%s)", n, it.State, model.TurboUnsupported, it.Detail)
		}
	}
	if !strings.Contains(st.Summary, "frekans/fan denetimi yok") {
		t.Errorf("özet dürüst değil: %q", st.Summary)
	}
	if len(k.log) != 0 {
		t.Errorf("desteklenmeyen makinede yazım yapıldı: %v", k.log)
	}
	if mhz := CoreMHz(f.root); len(mhz) < 2 || mhz[1] != 2995 {
		t.Errorf("cpufreq yokken /proc/cpuinfo'ya düşülmedi: %v", mhz)
	}
	// Tanı da çökmeden "desteklenmiyor" maddelerini göstermeli ve yine
	// hiçbir şey yazmamalı (QEMU'da gözlenen durumun birim karşılığı).
	c.sleep = func(time.Duration) {}
	st = c.Diagnose(time.Millisecond)
	for _, n := range []string{"Frekans sürücüsü", "RAPL", "MSR", "Sıcaklık", "Fan", "Platform profili"} {
		if it := diagItem(st, n); it.State != model.TurboUnsupported && it.State != model.TurboFailed {
			t.Errorf("tanı %s: durum %q, beklenen desteklenmiyor/hata (%s)", n, it.State, it.Detail)
		}
	}
	if len(k.log) != 0 {
		t.Errorf("tanı yazım yaptı: %v", k.log)
	}
	if st.Limiter != "frekans sürücüsü yok" {
		t.Errorf("sınırlayan %q, beklenen \"frekans sürücüsü yok\"", st.Limiter)
	}
}

func diagItem(st model.TurboStatus, name string) model.TurboItem {
	for _, it := range st.Diag {
		if it.Name == name {
			return it
		}
	}
	return model.TurboItem{}
}

// ── C-durumları ────────────────────────────────────────────────────────────

// Düzeltilen hata: turbo /dev/cpu_dma_latency=0 tutuyordu. Boştaki
// çekirdekler C0'da (poll) kalıp "etkin" sayılıyor, tek çekirdek turbosu
// (4,4 GHz) yerine güç sınırına sığan tüm çekirdek frekansı (2,4 GHz)
// çıkıyordu. Turbo bu isteği ARTIK YAPMAMALI.
func TestCStatesNotBlockedDuringTurbo(t *testing.T) {
	f := newFake(t)
	f.put(cpuD+"/online", "0")
	f.put("dev/cpu_dma_latency", "")
	c, k := newCtl(f)
	st := c.Enable()
	b, _ := os.ReadFile(filepath.Join(f.root, "dev/cpu_dma_latency"))
	if len(bytes.TrimSpace(b)) != 0 {
		t.Errorf("cpu_dma_latency'ye yazıldı (%v): derin uyku engellenir, tek çekirdek turbosu ölür", b)
	}
	for _, l := range k.log {
		if strings.HasPrefix(l, "dev/cpu_dma_latency") {
			t.Errorf("PM QoS yazımı: %s", l)
		}
	}
	if st.CStatesOff {
		t.Error("CStatesOff=true: turbo derin uykuyu kapatıyor")
	}
	if it := item(st, "C-durumları"); it.State != model.TurboOK || !strings.Contains(it.Detail, "AÇIK") {
		t.Errorf("C-durumları maddesi nedeni anlatmıyor: %+v", it)
	}
}

// ── Fanlar ──────────────────────────────────────────────────────────────────

func fanRig(f *fake) {
	f.put(cpuD+"/online", "0")
	// nct6775: kanal 1 otomatik (SmartFan IV = 5), kanal 2 kullanıcı elle 120.
	f.put("sys/class/hwmon/hwmon0/name", "nct6775")
	f.put("sys/class/hwmon/hwmon0/pwm1", "80")
	f.put("sys/class/hwmon/hwmon0/pwm1_enable", "5")
	f.put("sys/class/hwmon/hwmon0/pwm2", "120")
	f.put("sys/class/hwmon/hwmon0/pwm2_enable", "1")
	// it87: sürücü PWM yazımını yok sayıyor (okuma 80'de kalıyor).
	f.put("sys/class/hwmon/hwmon1/name", "it87")
	f.put("sys/class/hwmon/hwmon1/pwm1", "80")
	f.put("sys/class/hwmon/hwmon1/pwm1_enable", "2")
	// hp-wmi: PWM yok, yalnızca kip; 0 = tam hız, 2 = otomatik.
	f.put("sys/class/hwmon/hwmon2/name", "hp")
	f.put("sys/class/hwmon/hwmon2/pwm1_enable", "2")
	// ACPI fanı + dokunulmaması gereken işlemci soğutma aygıtı.
	f.put("sys/class/thermal/cooling_device0/type", "Fan")
	f.put("sys/class/thermal/cooling_device0/cur_state", "0")
	f.put("sys/class/thermal/cooling_device0/max_state", "1")
	f.put("sys/class/thermal/cooling_device1/type", "Processor")
	f.put("sys/class/thermal/cooling_device1/cur_state", "0")
	f.put("sys/class/thermal/cooling_device1/max_state", "10")
	f.put("sys/class/thermal/thermal_zone0/temp", "45000")
	f.put("sys/class/thermal/thermal_zone0/trip_point_0_type", "active")
	f.put("sys/class/thermal/thermal_zone0/trip_point_0_temp", "70000")
	// ThinkPad (fan_control=1 ile "commands:" satırları görünür).
	f.put("proc/acpi/ibm/fan", "status:\t\tenabled\nspeed:\t\t2100\nlevel:\t\tauto\ncommands:\tlevel <level> (<level> is 0-7, auto, disengaged, full-speed)")
}

func fanHook(f *fake) func(rel, val string) (bool, error) {
	return func(rel, val string) (bool, error) {
		switch rel {
		case "sys/class/hwmon/hwmon1/pwm1":
			return true, nil // it87: yazım "başarılı" ama değer değişmiyor
		case "proc/acpi/ibm/fan":
			lv := strings.TrimPrefix(val, "level ")
			f.put(rel, "status:\t\tenabled\nspeed:\t\t4800\nlevel:\t\t"+lv+"\ncommands:\tlevel <level>")
			return true, nil
		}
		return false, nil
	}
}

func TestFansFullAndRestore(t *testing.T) {
	f := newFake(t)
	fanRig(f)
	c, k := newCtl(f)
	k.hook = fanHook(f)

	st := c.Enable()

	f.want("sys/class/hwmon/hwmon0/pwm1_enable", "1")
	f.want("sys/class/hwmon/hwmon0/pwm1", "255")
	f.want("sys/class/hwmon/hwmon0/pwm2", "255")
	f.want("sys/class/hwmon/hwmon2/pwm1_enable", "0")
	f.want("sys/class/thermal/cooling_device0/cur_state", "1")
	f.want("sys/class/thermal/cooling_device1/cur_state", "0") // işlemci YAVAŞLATILMAMALI
	if lv, _ := thinkpadLevel(filepath.Join(f.root, "proc/acpi/ibm/fan")); lv != "7" {
		t.Errorf("ThinkPad fan kademesi %q, beklenen 7", lv)
	}
	// GÜVENLİK: PWM'i kabul etmeyen it87 kanalı "elle ama düşük" kalmamalı.
	f.want("sys/class/hwmon/hwmon1/pwm1_enable", "2")
	if k.wrote("sys/class/hwmon/hwmon1/pwm1_enable", "0") {
		t.Error("doğrulanmamış sürücüye pwm_enable=0 yazıldı (pwm-fan'da 0 = fan durur)")
	}
	if st.FansTotal != 6 || st.FansFull != 5 {
		t.Errorf("fanlar %d/%d, beklenen 5/6", st.FansFull, st.FansTotal)
	}
	if it := item(st, "Fanlar"); it.State != model.TurboPartial || !strings.Contains(it.Detail, "it87/pwm1") {
		t.Errorf("fan maddesi başarısız kanalı göstermiyor: %+v", it)
	}

	c.Disable()
	f.want("sys/class/hwmon/hwmon0/pwm1_enable", "5")
	f.want("sys/class/hwmon/hwmon0/pwm2_enable", "1")
	f.want("sys/class/hwmon/hwmon0/pwm2", "120")
	f.want("sys/class/hwmon/hwmon2/pwm1_enable", "2")
	f.want("sys/class/thermal/cooling_device0/cur_state", "0")
	if lv, _ := thinkpadLevel(filepath.Join(f.root, "proc/acpi/ibm/fan")); lv != "auto" {
		t.Errorf("ThinkPad otomatiğe dönmedi: %q", lv)
	}
	// GÜVENLİK: otomatik kipteki kanala soğuk anda kaydedilmiş düşük PWM
	// (80) geri yazılmamalı.
	if k.wrote("sys/class/hwmon/hwmon0/pwm1", "80") {
		t.Error("otomatik kanala eski düşük PWM (80) yazıldı")
	}
}

// Isıl bölge bir "active" tetik noktasının üstündeyken ACPI fanı soğuk anda
// kaydedilmiş düşük değere İNDİRİLMEMELİ: ısıl yönetici hedefi değişmediği
// için yeniden yazmaz ve fan sıcak makinede kapalı kalırdı.
func TestCdevNotLoweredWhenHot(t *testing.T) {
	f := newFake(t)
	fanRig(f)
	c, k := newCtl(f)
	k.hook = fanHook(f)
	c.Enable()
	f.put("sys/class/thermal/thermal_zone0/temp", "82000")
	c.Disable()
	f.want("sys/class/thermal/cooling_device0/cur_state", "1")
}

// Daemon turbo açıkken çökerse: yeni süreç günlüğü /run'dan okur ve
// özgün değerleri geri yükler. İkinci Enable, turbonun kendi yazdığı
// değerleri "özgün" diye kaydetmemeli.
func TestCrashRecoveryUsesJournal(t *testing.T) {
	f := newFake(t)
	fanRig(f)
	intelPstate(f, 2)
	a, ka := newCtl(f)
	ka.hook = func(rel, val string) (bool, error) {
		if h, err := fanHook(f)(rel, val); h || err != nil {
			return h, err
		}
		return intelHook(f, 2)(rel, val)
	}
	a.Enable()
	// a "çöktü". b aynı kök ve günlükle başlar ve önce Enable çağrılır.
	b, kb := newCtl(f)
	kb.hook = ka.hook
	b.Enable()
	b.Disable()
	f.want("sys/class/hwmon/hwmon0/pwm1_enable", "5")
	f.want(cpuD+"/cpufreq/policy0/scaling_governor", "powersave")
	f.want(cpuD+"/cpufreq/policy0/scaling_min_freq", "400000")

	// Açılışta turbo kapalıysa kalıntı doğrudan geri alınır.
	c1, k1 := newCtl(f)
	k1.hook = ka.hook
	c1.Enable()
	c2, k2 := newCtl(f)
	k2.hook = ka.hook
	n, errs := c2.RestoreLeftover()
	if n == 0 || len(errs) > 0 {
		t.Errorf("kalıntı geri alınamadı: n=%d hatalar=%v", n, errs)
	}
	f.want("sys/class/hwmon/hwmon0/pwm1_enable", "5")
	f.want(cpuD+"/intel_pstate/no_turbo", "1")
}

// Donanım fanı otomatiğe geri çekerse Refresh yeniden tam güce almalı.
func TestRefreshReassertsFans(t *testing.T) {
	f := newFake(t)
	fanRig(f)
	c, k := newCtl(f)
	k.hook = fanHook(f)
	c.Enable()
	f.put("sys/class/hwmon/hwmon0/pwm1_enable", "5") // BIOS geri aldı
	c.Refresh()
	f.want("sys/class/hwmon/hwmon0/pwm1_enable", "1")
	f.want("sys/class/hwmon/hwmon0/pwm1", "255")
}

// ── Hibrit P/E tespiti ──────────────────────────────────────────────────────

func TestTopologyIntelHybridPMU(t *testing.T) {
	f := newFake(t)
	f.put(cpuD+"/online", "0-15")
	f.put("sys/devices/cpu_core/cpus", "0-7")
	f.put("sys/devices/cpu_atom/cpus", "8-15")
	tp := DetectTopology(f.root)
	if FormatList(tp.PCores) != "0-7" || FormatList(tp.ECores) != "8-15" || !strings.Contains(tp.Method, "PMU") {
		t.Errorf("hibrit PMU: %+v", tp)
	}
}

func TestTopologyCapacity(t *testing.T) {
	f := newFake(t)
	f.put(cpuD+"/online", "0-5")
	for i := 0; i < 6; i++ {
		v := "1024"
		if i >= 4 {
			v = "420"
		}
		f.put(cpuD+"/cpu"+itoa(i)+"/cpu_capacity", v)
	}
	tp := DetectTopology(f.root)
	if FormatList(tp.PCores) != "0-3" || FormatList(tp.ECores) != "4-5" || tp.Method != "cpu_capacity" {
		t.Errorf("kapasite: %+v", tp)
	}
}

func TestTopologyFreqClusters(t *testing.T) {
	f := newFake(t)
	f.put(cpuD+"/online", "0-9")
	for i := 0; i < 10; i++ {
		v := "4700000"
		if i >= 4 {
			v = "3600000" // Alder Lake E: 3,6/4,7 = 0,77
		}
		f.put(cpuD+"/cpu"+itoa(i)+"/cpufreq/cpuinfo_max_freq", v)
	}
	tp := DetectTopology(f.root)
	if FormatList(tp.PCores) != "0-3" || FormatList(tp.ECores) != "4-9" {
		t.Errorf("frekans kümesi: %+v", tp)
	}
}

// Turbo Boost Max 3.0: iki "gözde" çekirdek %4 daha hızlı. Hibrit DEĞİL;
// sunucu iki çekirdeğe hapsolmamalı.
func TestTopologyTBM3NotHybrid(t *testing.T) {
	f := newFake(t)
	f.put(cpuD+"/online", "0-7")
	for i := 0; i < 8; i++ {
		v := "5100000"
		if i < 2 {
			v = "5300000"
		}
		f.put(cpuD+"/cpu"+itoa(i)+"/cpufreq/cpuinfo_max_freq", v)
	}
	tp := DetectTopology(f.root)
	if len(tp.ECores) != 0 || FormatList(tp.PCores) != "0-7" {
		t.Errorf("TBM3 hibrit sanıldı: %+v", tp)
	}
}

// Bir çekirdeğin değeri okunamıyorsa ayrım yapılmamalı.
func TestTopologyMissingValueNoSplit(t *testing.T) {
	f := newFake(t)
	f.put(cpuD+"/online", "0-3")
	f.put(cpuD+"/cpu0/cpufreq/cpuinfo_max_freq", "4700000")
	f.put(cpuD+"/cpu1/cpufreq/cpuinfo_max_freq", "3000000")
	f.put(cpuD+"/cpu2/cpufreq/cpuinfo_max_freq", "4700000")
	tp := DetectTopology(f.root)
	if len(tp.ECores) != 0 || len(tp.PCores) != 4 {
		t.Errorf("eksik veriyle ayrım yapıldı: %+v", tp)
	}
}

func TestParseFormatList(t *testing.T) {
	if got := FormatList(ParseList("0-3,8, 10-11,2")); got != "0-3,8,10-11" {
		t.Errorf("FormatList(ParseList) = %q", got)
	}
	if GHz(4700) != "4,7 GHz" {
		t.Errorf("GHz = %q", GHz(4700))
	}
}

func itoa(i int) string { return strconv.Itoa(i) }
