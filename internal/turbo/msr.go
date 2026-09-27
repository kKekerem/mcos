package turbo

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
)

// ── MSR okumaları (yalnızca OKUMA; hiçbir MSR yazılmaz) ─────────────────────
//
// sysfs "işlemci ne İSTENDİ"yi söyler; MSR'ler "işlemci ne YAPIYOR ve
// NEDEN"i. Gerçek PC'de sysfs her şeyi "performance / azami" gösterirken
// işlemci 2,4 GHz'de kaldı; sebep ancak şu yazmaçlarla kesinleşir:
//
//	APERF/MPERF/TSC  gerçek meşgul frekansı ve meşguliyet (turbostat
//	                 Bzy_MHz/Busy% ile aynı hesap)
//	0x64F            frekansı kısan etkenler (PL1, PL2, ısı, PROCHOT, çok
//	                 çekirdek turbo tavanı) — 6. nesil Core ve sonrası
//	0x1B1            paket ısıl durumu: bit 10 = "şu an güç sınırında"
//	0x1A2            TjMax ve TCC ofseti (BIOS erken kısma ayarı)
//	0x606/0x610      RAPL birimleri ve MSR'deki PL1/PL2 + KİLİT biti
//	0x1A0            bit 38: BIOS turbo'yu kapatmış mı
//	0x771/0x774      HWP yeteneği ve GERÇEKTE programlanan HWP isteği
//	0xC0010015       AMD HWCR bit 25: çekirdek artırması (CPB) kapalı mı
//
// /dev/cpu/N/msr çekirdekte CONFIG_X86_MSR ister (kernel.config, "Turbo").

const (
	msrTSC            = 0x10
	msrMPERF          = 0xE7
	msrAPERF          = 0xE8
	msrMiscEnable     = 0x1A0
	msrTempTarget     = 0x1A2
	msrPkgThermStatus = 0x1B1
	msrRAPLUnit       = 0x606
	msrPkgPowerLimit  = 0x610
	msrPerfLimit      = 0x64F
	msrPMEnable       = 0x770
	msrHWPCaps        = 0x771
	msrHWPRequest     = 0x774
	msrAMDHWCR        = 0xC0010015
)

// readMSRFile /dev/cpu/N/msr'den 8 bayt okur; dosya konumu yazmaç
// numarasıdır (arch/x86/kernel/msr.c).
func (c *Controller) readMSRFile(cpu int, reg uint32) (uint64, error) {
	f, err := os.Open(c.p(fmt.Sprintf("dev/cpu/%d/msr", cpu)))
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var b [8]byte
	if _, err := f.ReadAt(b[:], int64(reg)); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b[:]), nil
}

// perfLimitBits, 0x64F (MSR_CORE_PERF_LIMIT_REASONS, 6. nesil Core+)
// durum bitleridir. Bit n+16 aynı etkenin "o günden beri oldu" günlüğüdür.
var perfLimitBits = []struct {
	bit  uint
	name string
}{
	{0, "PROCHOT"},
	{1, "ısıl (TCC)"},
	{4, "residency"},
	{5, "ortalama ısıl sınır (RATL)"},
	{6, "VR ısı"},
	{7, "VR akım (TDC)"},
	{8, "diğer (EDP/akım)"},
	{10, "PL1 güç sınırı"},
	{11, "PL2 güç sınırı"},
	{12, "çok çekirdek turbo tavanı"},
	{13, "turbo geçiş sönümü"},
}

// decodePerfLimit, 0x64F değerini "şu an" ve "günlük" listelerine açar.
func decodePerfLimit(v uint64) (now, logged []string) {
	for _, b := range perfLimitBits {
		if v&(1<<b.bit) != 0 {
			now = append(now, b.name)
		}
		if v&(1<<(b.bit+16)) != 0 {
			logged = append(logged, b.name)
		}
	}
	return now, logged
}

// msrDiag, Intel/AMD paket düzeyi MSR tanı satırlarını üretir. lim:
// sınırlayan tahmini için bulunan etkenler (ör. "pl1", "thermal").
func (c *Controller) msrDiag(vendor string) (lines []string, lim map[string]bool, err error) {
	lim = map[string]bool{}
	rd := func(reg uint32) (uint64, bool) {
		v, e := c.readMSR(0, reg)
		if e != nil && err == nil {
			err = e
		}
		return v, e == nil
	}
	// Erişim var mı? TSC her x86'da okunur; okunamıyorsa aygıt yok demek.
	if _, e := c.readMSR(0, msrTSC); e != nil {
		return nil, lim, e
	}
	err = nil
	if vendor == "AuthenticAMD" {
		if v, ok := rd(msrAMDHWCR); ok {
			if v&(1<<25) != 0 {
				lines = append(lines, "HWCR.CpbDis=1: çekirdek artırması (boost) BIOS/yazılımca KAPALI")
				lim["noturbo"] = true
			} else {
				lines = append(lines, "HWCR.CpbDis=0: çekirdek artırması (boost) açık")
			}
		}
		return lines, lim, nil
	}
	if vendor != "GenuineIntel" {
		return nil, lim, nil
	}
	if v, ok := rd(msrMiscEnable); ok {
		if v&(1<<38) != 0 {
			lines = append(lines, "IA32_MISC_ENABLE bit 38=1: turbo BIOS'ta KAPALI (Linux açamaz)")
			lim["noturbo"] = true
		} else {
			lines = append(lines, "IA32_MISC_ENABLE bit 38=0: turbo BIOS'ta açık")
		}
	}
	if v, ok := rd(msrPerfLimit); ok {
		now, logged := decodePerfLimit(v)
		s := fmt.Sprintf("0x64F=0x%08x — şu an: %s", v&0xFFFFFFFF, orNone(now))
		if len(logged) > 0 {
			s += "; açılıştan beri: " + strings.Join(logged, ", ")
		}
		lines = append(lines, s)
		if v&(1<<10) != 0 {
			lim["pl1"] = true
		}
		if v&(1<<11) != 0 {
			lim["pl2"] = true
		}
		if v&(1<<1) != 0 || v&1 != 0 {
			lim["thermal"] = true
		}
		if v&(1<<12) != 0 {
			lim["multicore"] = true
		}
	}
	var tjmax int64
	if v, ok := rd(msrTempTarget); ok {
		tjmax = int64((v >> 16) & 0xFF)
		off := int64((v >> 24) & 0x3F)
		s := fmt.Sprintf("TjMax %d °C", tjmax)
		if off > 0 {
			s += fmt.Sprintf(", TCC ofseti %d °C (işlemci %d °C'de kısmaya başlar)", off, tjmax-off)
		} else {
			s += ", TCC ofseti 0"
		}
		lines = append(lines, s)
	}
	if v, ok := rd(msrPkgThermStatus); ok {
		var st []string
		if v&1 != 0 {
			st = append(st, "ısıl sınırda")
			lim["thermal"] = true
		}
		if v&(1<<2) != 0 {
			st = append(st, "PROCHOT")
			lim["thermal"] = true
		}
		if v&(1<<10) != 0 {
			st = append(st, "GÜÇ SINIRINDA")
			lim["power"] = true
		}
		s := "paket ısıl durumu: " + orNone(st)
		if v&(1<<31) != 0 && tjmax > 0 {
			s += fmt.Sprintf(", paket %d °C", tjmax-int64((v>>16)&0x7F))
		}
		lines = append(lines, s)
	}
	if u, ok := rd(msrRAPLUnit); ok {
		if v, ok := rd(msrPkgPowerLimit); ok {
			pu := float64(uint64(1) << (u & 0xF))
			pl1 := float64(v&0x7FFF) / pu
			pl2 := float64((v>>32)&0x7FFF) / pu
			s := fmt.Sprintf("MSR_PKG_POWER_LIMIT: PL1 %.1f W (%s), PL2 %.1f W (%s)",
				pl1, onOff(v&(1<<15) != 0), pl2, onOff(v&(1<<47) != 0))
			if v&(1<<63) != 0 {
				s += " — KİLİTLİ (BIOS, bit 63)"
			} else {
				s += " — kilitsiz"
			}
			lines = append(lines, strings.ReplaceAll(s, ".", ","))
		}
	}
	if en, ok := rd(msrPMEnable); ok && en&1 != 0 {
		caps, ok1 := rd(msrHWPCaps)
		req, ok2 := rd(msrHWPRequest)
		if ok1 && ok2 {
			hi := caps & 0xFF
			mn, mx, epp := req&0xFF, (req>>8)&0xFF, (req>>24)&0xFF
			s := fmt.Sprintf("HWP isteği (cpu0): en düşük %d, en yüksek %d, EPP %d; yetenek en yüksek %d", mn, mx, epp, hi)
			if mn < hi || mx < hi {
				s += " — İSTEK AZAMİ DEĞİL"
			}
			lines = append(lines, s)
		}
	} else if ok {
		lines = append(lines, "HWP kapalı (IA32_PM_ENABLE=0): frekansı işletim sistemi P-durumlarıyla seçer")
	}
	return lines, lim, nil
}

func orNone(l []string) string {
	if len(l) == 0 {
		return "yok"
	}
	return strings.Join(l, ", ")
}

func onOff(b bool) string {
	if b {
		return "etkin"
	}
	return "devre dışı"
}

// msrSample bir çekirdeğin TSC/APERF/MPERF sayaçlarıdır.
type msrSample struct {
	tsc, aperf, mperf uint64
	ok                bool
}

func (c *Controller) msrSnap(cpus []int) map[int]msrSample {
	out := make(map[int]msrSample, len(cpus))
	for _, cpu := range cpus {
		var s msrSample
		var e1, e2, e3 error
		s.tsc, e1 = c.readMSR(cpu, msrTSC)
		s.aperf, e2 = c.readMSR(cpu, msrAPERF)
		s.mperf, e3 = c.readMSR(cpu, msrMPERF)
		s.ok = e1 == nil && e2 == nil && e3 == nil
		out[cpu] = s
	}
	return out
}

// busyMHz, iki örnek arasında çekirdeğin MEŞGUL frekansını ve meşguliyet
// yüzdesini verir: MHz = TSC hızı × ΔAPERF/ΔMPERF, meşgul = ΔMPERF/ΔTSC.
// APERF/MPERF yalnızca C0'da sayar; boştaki çekirdek düşük meşguliyetle
// görünür ama frekansı "uyandığı anlarda" doğru ölçülür. tscHz: TSC'nin
// saniyedeki artışı (duvar saatine göre hesaplanır).
func busyMHz(a, b msrSample, tscHz float64) (mhz, busy int, ok bool) {
	if !a.ok || !b.ok || b.tsc <= a.tsc || b.mperf <= a.mperf || b.aperf < a.aperf {
		return 0, 0, false
	}
	dA, dM, dT := float64(b.aperf-a.aperf), float64(b.mperf-a.mperf), float64(b.tsc-a.tsc)
	return int(tscHz*dA/dM/1e6 + 0.5), int(100*dM/dT + 0.5), true
}
