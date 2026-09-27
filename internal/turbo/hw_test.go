package turbo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"mcos/internal/model"
)

// ── Gerçek PC geri bildirimi: "4,4 GHz olması lazımken 2,4 GHz, fan dönmüyor"
//
// Bu dosyadaki sınamalar o geri bildirimin kök nedenlerini (RAPL PL1,
// geri okunmayan cpufreq tavanı, bayat ısıl kısıtlama, EC fan yolları) ve
// bir sonraki denemede nedeni kesin gösterecek tanıyı sahte sysfs üzerinde
// sınar. Ana makinenin /sys'ine hiç dokunulmaz.

// raplRig, BIOS'u PL1'i 15 W'a sabitlemiş bir dizüstüdür: PL2 25 W (8 sn),
// TDP (PL1 max_power) 15 W, PKG_POWER_INFO azami gücü 0 (doldurulmamış).
// MMIO bölgesi (INT340X) BIOS kilitli; psys bölgesine dokunulmamalı.
func raplRig(f *fake) {
	f.put(cpuD+"/online", "0-3")
	z := "sys/class/powercap/intel-rapl:0/"
	f.put(z+"name", "package-0")
	f.put(z+"enabled", "1")
	f.put(z+"energy_uj", "1000000")
	f.put(z+"max_energy_range_uj", "262143328850")
	f.put(z+"constraint_0_name", "long_term")
	f.put(z+"constraint_0_power_limit_uw", "15000000")
	f.put(z+"constraint_0_max_power_uw", "15000000")
	f.put(z+"constraint_0_time_window_us", "7995392")
	f.put(z+"constraint_1_name", "short_term")
	f.put(z+"constraint_1_power_limit_uw", "25000000")
	f.put(z+"constraint_1_max_power_uw", "0")
	f.put(z+"constraint_1_time_window_us", "2440")
	f.put(z+"constraint_2_name", "peak_power")
	f.put(z+"constraint_2_power_limit_uw", "90000000")
	f.put(z+"constraint_2_max_power_uw", "50000000")
	// Alt bölge: ayrı bir sınır değil, atlanmalı.
	f.put("sys/class/powercap/intel-rapl:0:0/name", "core")
	f.put("sys/class/powercap/intel-rapl:0:0/constraint_0_name", "long_term")
	f.put("sys/class/powercap/intel-rapl:0:0/constraint_0_power_limit_uw", "0")
	// psys: adaptör sınırı.
	f.put("sys/class/powercap/intel-rapl:1/name", "psys")
	f.put("sys/class/powercap/intel-rapl:1/constraint_0_name", "long_term")
	f.put("sys/class/powercap/intel-rapl:1/constraint_0_power_limit_uw", "28000000")
	f.put("sys/class/powercap/intel-rapl:1/constraint_0_max_power_uw", "0")
	// MMIO: BIOS kilitli.
	m := "sys/class/powercap/intel-rapl-mmio:0/"
	f.put(m+"name", "package-0")
	f.put(m+"enabled", "0")
	f.put(m+"constraint_0_name", "long_term")
	f.put(m+"constraint_0_power_limit_uw", "15000000")
	f.put(m+"constraint_0_max_power_uw", "15000000")
	f.put(m+"constraint_0_time_window_us", "28000000")
	f.put(m+"constraint_1_name", "short_term")
	f.put(m+"constraint_1_power_limit_uw", "25000000")
	f.put(m+"constraint_1_max_power_uw", "0")
	// Denetim türü dizinleri (bölge değil).
	f.put("sys/class/powercap/intel-rapl/enabled", "1")
	f.put("sys/class/powercap/intel-rapl-mmio/enabled", "1")
}

// raplHook çekirdeğin davranışını taklit eder: MMIO kilitli (EACCES,
// rapl_write_pl_data), zaman penceresi 2^Y*(1+F/4) biçimine aşağı yuvarlanır.
func raplHook(f *fake) func(rel, val string) (bool, error) {
	return func(rel, val string) (bool, error) {
		switch {
		case strings.HasPrefix(rel, "sys/class/powercap/intel-rapl-mmio:0/constraint_"):
			return true, syscall.EACCES
		case strings.HasSuffix(rel, "time_window_us") && val == "28000000":
			f.put(rel, "27983872")
			return true, nil
		}
		return false, nil
	}
}

func TestRAPLRaisesPL1ToPL2AndRestores(t *testing.T) {
	f := newFake(t)
	raplRig(f)
	c, k := newCtl(f)
	k.hook = raplHook(f)

	st := c.Enable()

	z := "sys/class/powercap/intel-rapl:0/"
	// ASIL SINAMA: PL1 15 W'ta kalırsa tüm çekirdekler ~2,4 GHz'e iner.
	f.want(z+"constraint_0_power_limit_uw", "25000000")
	f.want(z+"constraint_1_power_limit_uw", "25000000") // max_power 0: uydurma değer yok
	f.want(z+"constraint_0_time_window_us", "27983872")
	f.want(z+"constraint_2_power_limit_uw", "90000000") // PL4 dokunulmaz
	f.want("sys/class/powercap/intel-rapl:1/constraint_0_power_limit_uw", "28000000")
	f.want("sys/class/powercap/intel-rapl:0:0/constraint_0_power_limit_uw", "0")
	if k.wrote(z+"enabled", "1") || k.wrote("sys/class/powercap/intel-rapl-mmio:0/enabled", "1") {
		t.Error("enabled yazıldı: 0 okunan bölgede 1 yazmak sınır EKLER ya da kilitli bölgede hata verir")
	}
	it := item(st, "Güç sınırı (RAPL)")
	if it.State != model.TurboPartial || !strings.Contains(it.Detail, "PL1 15,0 W → 25,0 W") ||
		!strings.Contains(it.Detail, "BIOS kilitli") {
		t.Errorf("RAPL maddesi yükseltmeyi ve MMIO kilidini göstermiyor: %+v", it)
	}
	// Zaman penceresi yuvarlaması (%0,06) uyuşmazlık sayılmamalı.
	if g := item(st, "Geri okuma"); g.State != "" {
		t.Errorf("yuvarlama uyuşmazlık sayıldı: %+v", g)
	}

	// Firmware PL1'i geri çekerse Refresh yeniden yazar ve sayar.
	f.put(z+"constraint_0_power_limit_uw", "15000000")
	c.Refresh()
	f.want(z+"constraint_0_power_limit_uw", "25000000")
	if c.raplReasserts != 1 {
		t.Errorf("geri çekme sayısı %d, beklenen 1", c.raplReasserts)
	}

	c.Disable()
	f.want(z+"constraint_0_power_limit_uw", "15000000")
	f.want(z+"constraint_1_power_limit_uw", "25000000")
	f.want(z+"constraint_0_time_window_us", "7995392")
}

// PL2'nin max_power_uw'si (PKG_POWER_INFO "Maximum Power") dolu ve makulse
// PL2 ona, PL1 de yeni PL2'ye çıkar; bozuk alan (4095 W) yok sayılır.
func TestRAPLUsesMaxPowerWhenSane(t *testing.T) {
	for _, tc := range []struct {
		max, want string
	}{
		{"45000000", "45000000"},
		{"4095000000", "25000000"},
	} {
		f := newFake(t)
		raplRig(f)
		f.put("sys/class/powercap/intel-rapl:0/constraint_1_max_power_uw", tc.max)
		c, k := newCtl(f)
		k.hook = raplHook(f)
		c.Enable()
		f.want("sys/class/powercap/intel-rapl:0/constraint_1_power_limit_uw", tc.want)
		f.want("sys/class/powercap/intel-rapl:0/constraint_0_power_limit_uw", tc.want)
	}
}

// AMD'de RAPL bölgesi yalnızca enerji sayacıdır: güç sınırı "desteklenmiyor"
// ve nedeni (SMU) açıkça yazılmalı; hiçbir şey yazılmamalı.
func TestRAPLAMDEnergyOnly(t *testing.T) {
	f := newFake(t)
	f.put(cpuD+"/online", "0")
	f.put("proc/cpuinfo", "processor\t: 0\nvendor_id\t: AuthenticAMD\nmodel name\t: AMD Ryzen 7 5800U\n")
	f.put("sys/class/powercap/intel-rapl:0/name", "package-0")
	f.put("sys/class/powercap/intel-rapl:0/energy_uj", "5")
	c, k := newCtl(f)
	st := c.Enable()
	if it := item(st, "Güç sınırı (RAPL)"); it.State != model.TurboUnsupported || !strings.Contains(it.Detail, "SMU") {
		t.Errorf("AMD RAPL maddesi: %+v", it)
	}
	if len(k.log) != 0 {
		t.Errorf("AMD'de yazım yapıldı: %v", k.log)
	}
}

// Yazım BAŞARILI dönse de değer tutmayabilir: ACPI _PPC ya da "Processor"
// soğutma aygıtı cpufreq'e freq_qos tavanı koyar ve scaling_max_freq
// kırpılır. Önceki sürüm bunu "tamam" gösteriyordu.
func TestReadbackMismatchReportsFrequencyCap(t *testing.T) {
	f := newFake(t)
	intelPstate(f, 2)
	c, k := newCtl(f)
	base := intelHook(f, 2)
	k.hook = func(rel, val string) (bool, error) {
		if h, err := base(rel, val); h || err != nil {
			return h, err
		}
		if strings.HasSuffix(rel, "/scaling_max_freq") || strings.HasSuffix(rel, "/scaling_min_freq") {
			f.put(rel, "2400000") // QoS tavanı: çekirdek min/max'ı kırpar
			return true, nil
		}
		return false, nil
	}
	c.sleep = func(time.Duration) {}
	st := c.Enable()
	g := item(st, "Geri okuma")
	if g.State != model.TurboPartial || !strings.Contains(g.Detail, "policy0/scaling_max_freq: yazıldı 4700000, geri okunan 2400000") {
		t.Errorf("geri okuma uyuşmazlığı gösterilmedi: %+v", g)
	}
	st = c.Diagnose(0)
	if !strings.Contains(st.Limiter, "frekans tavanı 2400 MHz") {
		t.Errorf("sınırlayan %q, beklenen frekans tavanı", st.Limiter)
	}
	if !strings.Contains(st.Summary, "sınırlayan: frekans tavanı") {
		t.Errorf("özet sınırlayanı söylemiyor: %q", st.Summary)
	}
	if d := diagItem(st, "cpu 0-1"); d.State != model.TurboPartial || !strings.Contains(d.Detail, "TAVAN") {
		t.Errorf("politika tanısı tavanı göstermiyor: %+v", d)
	}
}

// ── Isıl kısıtlama ──────────────────────────────────────────────────────────

// throttleRig: iki "Processor" aygıtı kısıtlı (cur_state 3). cooling_device0
// soğuk bir bölgeye bağlı (bayat kısıtlama), cooling_device1 pasif eşiğin
// üstündeki bir bölgeye bağlı (gerçek ısıl karar).
func throttleRig(t *testing.T, f *fake) {
	f.put(cpuD+"/online", "0-1")
	for i, temp := range []string{"50000", "97000"} {
		cd := fmt.Sprintf("sys/class/thermal/cooling_device%d/", i)
		f.put(cd+"type", "Processor")
		f.put(cd+"cur_state", "3")
		f.put(cd+"max_state", "10")
		z := fmt.Sprintf("sys/class/thermal/thermal_zone%d/", i)
		f.put(z+"type", "acpitz")
		f.put(z+"temp", temp)
		f.put(z+"trip_point_0_type", "passive")
		f.put(z+"trip_point_0_temp", "95000")
		f.put(z+"cdev0_trip_point", "0")
		if err := os.Symlink(fmt.Sprintf("../cooling_device%d", i), filepath.Join(f.root, z+"cdev0")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestThrottleStaleZeroedHotKept(t *testing.T) {
	f := newFake(t)
	throttleRig(t, f)
	c, _ := newCtl(f)
	st := c.Enable()
	f.want("sys/class/thermal/cooling_device0/cur_state", "0")
	// GÜVENLİK: bölge pasif eşiğin üstünde; kısıtlama gerçek bir karar.
	f.want("sys/class/thermal/cooling_device1/cur_state", "3")
	it := item(st, "Isıl kısıtlama")
	if it.State != model.TurboPartial || !strings.Contains(it.Detail, "3→0") || !strings.Contains(it.Detail, "97 °C ≥ eşik 95 °C") {
		t.Errorf("ısıl kısıtlama maddesi: %+v", it)
	}
	c.Disable()
	// Eski kısıtlama geri DAYATILMAZ: aygıtın sahibi ısıl yönetici.
	f.want("sys/class/thermal/cooling_device0/cur_state", "0")
}

// ── Fanlar: ThinkPad fan_control, hp-wmi, IdeaPad, geri okuma ───────────────

func TestFanDiagReadbackAndThinkpadNote(t *testing.T) {
	f := newFake(t)
	f.put(cpuD+"/online", "0")
	f.put("sys/class/hwmon/hwmon0/name", "hp")
	f.put("sys/class/hwmon/hwmon0/pwm1_enable", "2")
	f.put("sys/class/hwmon/hwmon0/fan1_input", "0")
	f.put("sys/class/hwmon/hwmon1/name", "coretemp")
	f.put("sys/class/hwmon/hwmon1/temp1_label", "Package id 0")
	f.put("sys/class/hwmon/hwmon1/temp1_input", "62000")
	f.put("sys/bus/platform/devices/VPC2004:00/fan_mode", "1")
	// fan_control=1 YOK: "commands:" satırları görünmez.
	f.put("proc/acpi/ibm/fan", "status:\t\tenabled\nspeed:\t\t2100\nlevel:\t\tauto")
	c, k := newCtl(f)
	k.hook = func(rel, val string) (bool, error) {
		if rel == "sys/class/hwmon/hwmon0/pwm1_enable" && val == "0" {
			f.put("sys/class/hwmon/hwmon0/fan1_input", "5200")
		}
		return false, nil
	}
	c.sleep = func(time.Duration) {}
	st := c.Enable()
	f.want("sys/class/hwmon/hwmon0/pwm1_enable", "0")
	f.want("sys/bus/platform/devices/VPC2004:00/fan_mode", "4")
	if it := item(st, "Fanlar"); !strings.Contains(it.Detail, "thinkpad_acpi.fan_control=1") {
		t.Errorf("ThinkPad fan_control notu yok: %+v", it)
	}
	st = c.Diagnose(0)
	var fan []string
	for _, it := range st.Diag {
		if it.Name == "Fan" {
			fan = append(fan, it.State+" "+it.Detail)
		}
	}
	all := strings.Join(fan, "\n")
	for _, w := range []string{
		"hp/pwm1: enable=0 5200 RPM — turbo sürüyor (tam)",
		"thinkpad (/proc/acpi/ibm/fan): status=enabled speed=2100 level=auto — fan denetimi KAPALI",
		"VPC2004:00/fan_mode = 4",
	} {
		if !strings.Contains(all, w) {
			t.Errorf("fan tanısında %q yok:\n%s", w, all)
		}
	}
	if d := diagItem(st, "Sıcaklık"); !strings.Contains(d.Detail, "Package id 0 62 °C") {
		t.Errorf("sıcaklık tanısı: %+v", d)
	}
	c.Disable()
	f.want("sys/class/hwmon/hwmon0/pwm1_enable", "2")
	f.want("sys/bus/platform/devices/VPC2004:00/fan_mode", "1")
}

// ── MSR tanısı: sınırlayanı KESİN göster ────────────────────────────────────

// fakeMSR, BIOS'u PL1'i 15 W'ta kilitlemiş bir Intel dizüstüsüdür: 0x64F'te
// PL1 biti, paket ısıl durumunda "güç sınırında" biti. cpu0 %99 meşgul ve
// 2400 MHz'de (TSC 2,5 GHz), cpu1 boşta.
type fakeMSR struct {
	phase int
	regs  map[uint32]uint64
}

func (m *fakeMSR) read(cpu int, reg uint32) (uint64, error) {
	switch reg {
	case msrTSC:
		return uint64(1e9 + m.phase*250_000_000), nil // 100 ms'de 250 M = 2,5 GHz
	case msrMPERF:
		busy := []uint64{247_500_000, 2_500_000}[cpu] // %99 / %1
		return uint64(5e8) + uint64(m.phase)*busy, nil
	case msrAPERF:
		// meşgulken 2400/2500 oranında sayar
		busy := []uint64{237_600_000, 2_400_000}[cpu]
		return uint64(4e8) + uint64(m.phase)*busy, nil
	}
	if v, ok := m.regs[reg]; ok {
		return v, nil
	}
	return 0, syscall.EIO
}

func TestMSRDiagFindsPL1Limiter(t *testing.T) {
	f := newFake(t)
	intelPstate(f, 2)
	f.put("proc/cpuinfo", "processor\t: 0\nvendor_id\t: GenuineIntel\nmodel name\t: Intel(R) Core(TM) i5-1135G7\nflags\t\t: fpu hwp\n")
	f.put("dev/kmsg", "6,1,100,-;intel_rapl_common: package-0:package:long_term locked by BIOS\n SUBSYSTEM=x\n6,2,101,-;usb 1-1: new device\n6,3,101,-;Kernel command line: quiet thinkpad_acpi.fan_control=1\n4,3,102,-;intel_pstate: Intel P-state driver initializing\n")
	m := &fakeMSR{regs: map[uint32]uint64{
		msrMiscEnable:     0,
		msrPerfLimit:      1<<10 | 1<<26,          // PL1 şu an + günlük
		msrPkgThermStatus: 1<<31 | 1<<10 | 38<<16, // güç sınırında, 100-38 = 62 °C
		msrTempTarget:     100 << 16,
		msrRAPLUnit:       3,                                     // 1/8 W
		msrPkgPowerLimit:  1<<63 | 1<<47 | 200<<32 | 1<<15 | 120, // PL1 15 W, PL2 25 W, kilitli
		msrPMEnable:       1,
		msrHWPCaps:        44,
		msrHWPRequest:     44 | 44<<8,
	}}
	c, k := newCtl(f)
	c.readMSR = m.read
	clock := time.Unix(1000, 0)
	c.now = func() time.Time { return clock }
	c.sleep = func(d time.Duration) { m.phase = 1; clock = clock.Add(100 * time.Millisecond) }
	st := c.Diagnose(100 * time.Millisecond)

	if !strings.Contains(st.Limiter, "PL1") {
		t.Errorf("sınırlayan %q, beklenen PL1", st.Limiter)
	}
	var all []string
	for _, it := range st.Diag {
		all = append(all, it.Name+": "+it.Detail)
	}
	s := strings.Join(all, "\n")
	for _, w := range []string{
		"0: 2400 MHz %99",
		"1: 2400 MHz %1",
		"0x64F=0x04000400 — şu an: PL1 güç sınırı; açılıştan beri: PL1 güç sınırı",
		"paket ısıl durumu: GÜÇ SINIRINDA, paket 62 °C",
		"PL1 15,0 W (etkin), PL2 25,0 W (etkin) — KİLİTLİ",
		"HWP isteği (cpu0): en düşük 44, en yüksek 44",
		"locked by BIOS",
		"intel_pstate: Intel P-state driver initializing",
		"İşlemci: Intel(R) Core(TM) i5-1135G7",
	} {
		if !strings.Contains(s, w) {
			t.Errorf("tanıda %q yok:\n%s", w, s)
		}
	}
	if strings.Contains(s, "usb 1-1") || strings.Contains(s, "SUBSYSTEM") || strings.Contains(s, "command line") {
		t.Errorf("ilgisiz çekirdek iletisi tanıya girdi:\n%s", s)
	}
	if len(k.log) != 0 {
		t.Errorf("tanı yazım yaptı: %v", k.log)
	}
	rep := Report(st, "sınama", clock)
	if !strings.Contains(rep, "Sınırlayan: PL1") || !strings.Contains(rep, "--- tanı ---") {
		t.Errorf("günlük raporu eksik:\n%s", rep)
	}
}

// Tüketim PL1'e dayandıysa (MSR olmasa bile) sınırlayan PL1'dir: RAPL
// enerji sayacı 100 ms'de 1,48 J = 14,8 W, PL1 15 W.
func TestRAPLConsumptionAtPL1IsLimiter(t *testing.T) {
	f := newFake(t)
	raplRig(f)
	intelPstate(f, 4)
	c, _ := newCtl(f)
	clock := time.Unix(1000, 0)
	c.now = func() time.Time { return clock }
	c.sleep = func(time.Duration) {
		f.put("sys/class/powercap/intel-rapl:0/energy_uj", "2480000")
		clock = clock.Add(100 * time.Millisecond)
	}
	st := c.Diagnose(100 * time.Millisecond)
	d := diagItem(st, "RAPL package-0 (MSR)")
	if !strings.Contains(d.Detail, "tüketim 14,8 W") || !strings.Contains(d.Detail, "PL1'E DAYANMIŞ") {
		t.Errorf("tüketim tanısı: %+v", d)
	}
	if !strings.Contains(st.Limiter, "PL1 15,0 W") {
		t.Errorf("sınırlayan %q, beklenen PL1 15 W", st.Limiter)
	}
}

// hp-wmi yazımı "başarılı" dönüp BIOS kipi değiştirmezse (okuma BIOS'un
// gerçek kipini döner) fan "tam güçte" SAYILMAMALI ve neden görünmeli.
func TestHPWMIRefusedIsReported(t *testing.T) {
	f := newFake(t)
	f.put(cpuD+"/online", "0")
	f.put("sys/class/hwmon/hwmon0/name", "hp")
	f.put("sys/class/hwmon/hwmon0/pwm1_enable", "2")
	c, k := newCtl(f)
	k.hook = func(rel, val string) (bool, error) {
		return rel == "sys/class/hwmon/hwmon0/pwm1_enable" && val == "0", nil // yok sayılır
	}
	st := c.Enable()
	if st.FansFull != 0 || st.FansTotal != 1 {
		t.Errorf("reddedilen hp fanı tam güçte sayıldı: %d/%d", st.FansFull, st.FansTotal)
	}
	if it := item(st, "Fanlar"); !strings.Contains(it.Detail, `geri okunan "2"`) {
		t.Errorf("fan maddesi reddi göstermiyor: %+v", it)
	}
}
