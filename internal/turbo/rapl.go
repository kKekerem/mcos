package turbo

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"mcos/internal/model"
)

// ── RAPL güç sınırları (PL1/PL2) ────────────────────────────────────────────
//
// ── Neden gerekli ───────────────────────────────────────────────────────────
// Gerçek PC'de turbo açıkken işlemci "4,4 GHz olması lazımken 2,4 GHz"da
// kaldı ve makine ısınmadı. Frekans kolları (scaling_min_freq = azami, EPP =
// performance) yalnızca bir İSTEK; işlemci bu isteği paket güç sınırına
// sığdırır. Dizüstülerde BIOS'un uzun süreli sınırı (PL1) 15-28 W'tır: birkaç
// saniyelik PL2 penceresinden sonra tüm çekirdekler PL1'e sığan frekansa
// (tam da ~2,4 GHz) iner, güç düşük kaldığı için makine ısınmaz, EC fanı
// da hızlanmaz. Kullanıcının üç belirtisinin (düşük frekans, ısınmama, fan
// sessiz) ortak açıklaması budur.
//
// Önceki çekirdekte CONFIG_POWERCAP hiç yoktu: bu sınırlar Linux'tan ne
// okunabiliyor ne değiştirilebiliyordu (kernel.config, "Turbo" bölümü).
//
// ── Ne yapılır (drivers/powercap/intel_rapl_common.c) ──────────────────────
//   - constraint "short_term" (PL2) -> constraint_1_max_power_uw'ye kadar
//     (PKG_POWER_INFO "Maximum Power": işlemcinin izin verdiği en büyük ayar).
//   - constraint "long_term" (PL1) -> max(şu anki PL1, TDP (PL1'in
//     max_power_uw'si = PKG_POWER_INFO "Thermal Spec Power"), PL2). Yani
//     firmware'in zaten KISA süre için izin verdiği güç SÜREKLİ verilir.
//     Kendi uydurduğumuz bir sayıya çıkılmaz: tavan, işlemcinin ve
//     firmware'in kendi bildirdiği değerlerdir.
//   - PL1 zaman penceresi 28 saniyenin altındaysa 28 saniyeye uzatılır.
//   - psys (platform/şarj aleti sınırı) ve PL4 (anlık tepe) DOKUNULMAZ:
//     biri adaptörü, öbürü voltaj düzenleyiciyi korur.
//   - "enabled" YAZILMAZ: 0 okunuyorsa ya PL1 zaten uygulanmıyordur (daha
//     fazla güç demek; 1 yazmak sınır EKLER) ya da BIOS kilitlemiştir
//     (get_domain_enable kilitliyken 0 döner). İkisinde de yazmak yanlış.
//
// Isıl koruma DEVREDE KALIR: TjMax/PROCHOT donanımdadır; güç sınırı
// kalkınca işlemci ısınır ve gerekirse ısıl sınırda kendisi yavaşlar.
//
// ── AMD ─────────────────────────────────────────────────────────────────────
// AMD'de intel_rapl yalnızca enerji sayacını verir (rapl_defaults_amd: güç
// sınırı ilkeli yok). PPT/TDC/EDC/STAPM sınırları SMU'dadır; çekirdeğin bir
// arayüzü yoktur, ryzenadj gibi SMU araçları KAPSAM DIŞI. Tanı yine de
// tüketimi ölçer.

const powercapDir = "sys/class/powercap"

const (
	// raplTau: PL1 zaman penceresi hedefi (µs). Intel masaüstü öntanımlısı.
	raplTau = 28_000_000
	// raplMaxSane: bunun üstündeki max_power_uw bozuk/doldurulmamış alan
	// sayılır (PKG_POWER_INFO'da 0x7FFF birim = 4095 W görülüyor).
	raplMaxSane = 1_000_000_000
	// raplTol: güç değeri donanım birimine (1/8 W) yuvarlanır.
	raplTol = 0.02
	// raplTauTol: zaman penceresi 2^Y*(1+F/4) biçimine AŞAĞI yuvarlanır
	// (rapl_compute_time_window_core); 28 s -> 27,98 s.
	raplTauTol = 0.15
)

// AMDRAPLNote, AMD'de güç sınırının neden değiştirilemediğini söyler.
const AMDRAPLNote = "AMD: güç sınırları (PPT/TDC/EDC/STAPM) işlemcinin SMU'sunda; Linux çekirdeği bunlar için arayüz sunmuyor (ryzenadj gibi SMU araçları kapsam dışı) — yalnızca tüketim ölçülür"

type raplTarget struct {
	path string
	val  int64
	tol  float64
}

type raplConstraint struct {
	name            string // long_term, short_term, peak_power
	limit, max, tau string // dosya yolları
}

type raplZone struct {
	dir   string
	name  string // package-0, psys
	iface string // MSR | MMIO
	cons  []raplConstraint
}

func (z raplZone) label() string { return z.name + " (" + z.iface + ")" }

func (z raplZone) con(name string) *raplConstraint {
	for i := range z.cons {
		if z.cons[i].name == name {
			return &z.cons[i]
		}
	}
	return nil
}

// raplZones üst düzey RAPL bölgelerini döner (alt bölgeler core/uncore/dram
// ayrı sınır değildir, atlanır).
//
// /sys/class/powercap'te "intel-rapl" ve "intel-rapl-mmio" denetim türü
// dizinleri, "intel-rapl:0" paket bölgesi, "intel-rapl:0:0" alt bölgedir.
// MMIO bölgesi (INT340X işlemci ısıl aygıtı) dizüstülerde AYRI bir PL1
// taşır; etkin sınır ikisinin küçüğüdür, bu yüzden ikisi de yükseltilir.
func (c *Controller) raplZones() []raplZone {
	m, _ := filepath.Glob(c.p(powercapDir + "/intel-rapl*"))
	sort.Strings(m)
	var out []raplZone
	for _, d := range m {
		base := filepath.Base(d)
		iface := "MSR"
		switch {
		case strings.HasPrefix(base, "intel-rapl-mmio:"):
			iface = "MMIO"
		case strings.HasPrefix(base, "intel-rapl:"):
		default:
			continue
		}
		if strings.Count(base, ":") != 1 {
			continue
		}
		name, err := readStr(filepath.Join(d, "name"))
		if err != nil {
			continue
		}
		z := raplZone{dir: d, name: name, iface: iface}
		for i := 0; i < 8; i++ {
			n, err := readStr(filepath.Join(d, fmt.Sprintf("constraint_%d_name", i)))
			if err != nil {
				break
			}
			f := func(s string) string { return filepath.Join(d, fmt.Sprintf("constraint_%d_%s", i, s)) }
			z.cons = append(z.cons, raplConstraint{name: n,
				limit: f("power_limit_uw"), max: f("max_power_uw"), tau: f("time_window_us")})
		}
		out = append(out, z)
	}
	return out
}

// W, mikrovatı Türkçe ondalıklı vata çevirir: 15000000 -> "15,0 W".
func W(uw int64) string {
	return strings.Replace(fmt.Sprintf("%.1f W", float64(uw)/1e6), ".", ",", 1)
}

// sec, mikrosaniyeyi saniyeye çevirir: 28000000 -> "28,0 s". PL2 penceresi
// milisaniyeler mertebesindedir (2440 µs); "0,0 s" yazmamak için ms'ye geçer.
func sec(us int64) string {
	if us < 1_000_000 {
		return strings.Replace(fmt.Sprintf("%.1f ms", float64(us)/1e3), ".", ",", 1)
	}
	return strings.Replace(fmt.Sprintf("%.1f s", float64(us)/1e6), ".", ",", 1)
}

// raplErr, yazım hatasını kullanıcı diline çevirir. EACCES:
// rapl_write_pl_data, BIOS kilit bitini (MSR_PKG_POWER_LIMIT bit 63 ya da
// MMIO eşdeğeri) görünce döner.
func raplErr(err error) string {
	if errors.Is(err, syscall.EACCES) || errors.Is(err, fs.ErrPermission) {
		return "BIOS kilitli (kilit biti) — Linux'tan değiştirilemez"
	}
	return err.Error()
}

// setRAPL bir güç/zaman değerini günlüğe alıp yazar, geri okur ve turbo
// boyunca korunacak hedeflere ekler.
func (c *Controller) setRAPL(path string, v int64, tol float64) error {
	val := strconv.FormatInt(v, 10)
	if err := c.setTol(path, val, tol); err != nil {
		return err
	}
	c.raplTargets = append(c.raplTargets, raplTarget{path: path, val: v, tol: tol})
	return nil
}

// applyRAPL paket güç sınırlarını yükseltir (bkz. dosya başı).
func (c *Controller) applyRAPL() (model.TurboItem, bool) {
	const name = "Güç sınırı (RAPL)"
	c.raplTargets = nil
	zones := c.raplZones()
	if len(zones) == 0 {
		d := "powercap/intel-rapl yok — çekirdekte CONFIG_INTEL_RAPL kapalı ya da işlemci RAPL sunmuyor; PL1/PL2 BIOS değerinde kalır"
		if c.cpuVendor() == "AuthenticAMD" {
			d = AMDRAPLNote
		}
		return model.TurboItem{Name: name, State: model.TurboUnsupported, Detail: d}, false
	}
	var parts []string
	okN, badN, limN := 0, 0, 0
	for _, z := range zones {
		if !strings.HasPrefix(z.name, "package") {
			continue // psys: adaptör koruması, dokunulmaz
		}
		pl1, pl2 := z.con("long_term"), z.con("short_term")
		if pl1 == nil {
			parts = append(parts, z.label()+": güç sınırı yok, yalnızca enerji ölçümü")
			continue
		}
		limN++
		cur1 := readInt(pl1.limit)
		tdp := readInt(pl1.max)
		if tdp > raplMaxSane {
			tdp = 0
		}
		var cur2, new2 int64
		var zp, zbad []string
		if pl2 != nil {
			cur2 = readInt(pl2.limit)
			new2 = cur2
			if m := readInt(pl2.max); m > new2 && m <= raplMaxSane {
				new2 = m
			}
			if new2 != cur2 {
				// PL2 önce: PL1'in PL2'yi aştığı ara durum hiç oluşmasın.
				if err := c.setRAPL(pl2.limit, new2, raplTol); err != nil {
					zbad = append(zbad, "PL2: "+raplErr(err))
					new2 = cur2
				}
			}
		}
		new1 := cur1
		for _, v := range []int64{tdp, new2} {
			if v > new1 {
				new1 = v
			}
		}
		if new1 != cur1 {
			if err := c.setRAPL(pl1.limit, new1, raplTol); err != nil {
				zbad = append(zbad, "PL1: "+raplErr(err))
				new1 = cur1
			}
		}
		if t := readInt(pl1.tau); t > 0 && t < raplTau {
			if err := c.setRAPL(pl1.tau, raplTau, raplTauTol); err != nil {
				zbad = append(zbad, "τ: "+raplErr(err))
			}
		}
		// Gösterilen değerler GERİ OKUNAN değerlerdir, yazmak istediğimiz
		// değil: donanım yuvarlar ya da reddeder.
		got1 := readInt(pl1.limit)
		if got1 != cur1 {
			zp = append(zp, fmt.Sprintf("PL1 %s → %s", W(cur1), W(got1)))
		} else {
			zp = append(zp, "PL1 "+W(got1))
		}
		if pl2 != nil {
			got2 := readInt(pl2.limit)
			if got2 != cur2 {
				zp = append(zp, fmt.Sprintf("PL2 %s → %s", W(cur2), W(got2)))
			} else {
				zp = append(zp, "PL2 "+W(got2))
			}
		}
		if t := readInt(pl1.tau); t > 0 {
			zp = append(zp, "τ "+sec(t))
		}
		if len(zbad) > 0 {
			badN++
			zp = append(zp, zbad...)
		} else {
			okN++
		}
		parts = append(parts, z.label()+": "+strings.Join(zp, ", "))
	}
	if limN == 0 {
		d := strings.Join(parts, "; ")
		if c.cpuVendor() == "AuthenticAMD" {
			d = AMDRAPLNote
		}
		return model.TurboItem{Name: name, State: model.TurboUnsupported, Detail: d}, false
	}
	st := model.TurboOK
	switch {
	case okN == 0:
		st = model.TurboFailed
	case badN > 0:
		st = model.TurboPartial
	}
	return model.TurboItem{Name: name, State: st, Detail: strings.Join(parts, "; ")}, okN > 0
}

// reassertRAPL, firmware'in geri çektiği güç sınırını yeniden yazar.
//
// Neden: bazı dizüstülerde EC/BIOS, güç kaynağı ya da profil olayında
// PL1'i kendi değerine yeniden yazıyor (Lenovo, HP). Tek seferlik yazım
// "PL1 51 W" gösterip birkaç dakika sonra 15 W'a dönmek olurdu. Sayı tanıda
// görünür: "firmware N kez geri aldı".
func (c *Controller) reassertRAPL() {
	for _, t := range c.raplTargets {
		cur, err := readStr(t.path)
		if err != nil {
			continue
		}
		want := strconv.FormatInt(t.val, 10)
		if sameValue(want, cur, t.tol) {
			continue
		}
		if c.write(t.path, want) == nil {
			c.raplReasserts++
		}
	}
}
