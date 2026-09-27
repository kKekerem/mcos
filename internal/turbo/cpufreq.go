package turbo

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"mcos/internal/model"
)

// policies cpufreq politika dizinlerini sayısal sırayla döner.
func (c *Controller) policies() []string {
	m, _ := filepath.Glob(c.p(cpuDir + "/cpufreq/policy[0-9]*"))
	sort.Slice(m, func(i, j int) bool { return polNum(m[i]) < polNum(m[j]) })
	return m
}

func polNum(dir string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(filepath.Base(dir), "policy"))
	return n
}

func (c *Controller) driverAndGovernor(pols []string) (driver, gov string) {
	for i, pol := range pols {
		g, _ := readStr(filepath.Join(pol, "scaling_governor"))
		if i == 0 {
			driver, _ = readStr(filepath.Join(pol, "scaling_driver"))
			gov = g
		} else if g != gov {
			gov = "karışık"
		}
	}
	return driver, gov
}

// baseKHz bir politikanın TEMEL (turbo olmayan) frekansını kHz olarak bulur.
//
// Kaynak sırası:
//  1. base_frequency — intel_pstate (HWP) doğrudan bildirir.
//  2. cpuN/acpi_cppc/nominal_freq (MHz) — AMD amd-pstate ve CPPC'li Intel.
//  3. acpi-cpufreq frekans tablosu: Intel, turbo durumunu "temel + 1 MHz"
//     sahte girdisiyle bildirir (ör. 3401000 3400000 ...); o zaman ikinci
//     değer temeldir. AMD CPB'de artırma durumları tabloda yoktur, ilk
//     değer temeldir.
//
// YALNIZCA gösterim içindir ("temel 1,7 GHz, hedef 4,7 GHz"). Turbo hedefi
// temel frekans DEĞİL, cpuinfo_max_freq'tir (bkz. applyCPU). Bulunamazsa 0.
func (c *Controller) baseKHz(pol string) int64 {
	if v := readInt(filepath.Join(pol, "base_frequency")); v > 0 {
		return v
	}
	cpus := ParseList(firstOf(readStr(filepath.Join(pol, "related_cpus"))))
	if len(cpus) == 0 {
		cpus = []int{polNum(pol)}
	}
	if v := readInt(c.p(fmt.Sprintf("%s/cpu%d/acpi_cppc/nominal_freq", cpuDir, cpus[0]))); v > 0 {
		return v * 1000
	}
	drv, _ := readStr(filepath.Join(pol, "scaling_driver"))
	if drv != "acpi-cpufreq" {
		return 0
	}
	var fs []int64
	for _, f := range strings.Fields(firstOf(readStr(filepath.Join(pol, "scaling_available_frequencies")))) {
		if v, err := strconv.ParseInt(f, 10, 64); err == nil && v > 0 {
			fs = append(fs, v)
		}
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i] > fs[j] })
	switch {
	case len(fs) >= 2 && fs[0]-fs[1] == 1000:
		return fs[1]
	case len(fs) >= 1:
		return fs[0]
	}
	return 0
}

func firstOf(s string, _ error) string { return s }

// applyCPU işlemci frekans kollarını uygular.
func (c *Controller) applyCPU() ([]model.TurboItem, bool) {
	pols := c.policies()
	if len(pols) == 0 {
		return []model.TurboItem{{Name: "İşlemci frekansı", State: model.TurboUnsupported,
			Detail: "cpufreq sürücüsü yok — sanal makine ya da BIOS frekans denetimini vermiyor"}}, false
	}
	var items []model.TurboItem
	anyOK := false
	add := func(it model.TurboItem) {
		items = append(items, it)
		if it.State == model.TurboOK || it.State == model.TurboPartial {
			anyOK = true
		}
	}

	driver, _ := readStr(filepath.Join(pols[0], "scaling_driver"))
	drvDetail := driver

	// ── intel_pstate genel ayarları ─────────────────────────────────────
	//
	// max_perf_pct ÖNCE: intel_pstate min'i max'a kırpar; max bir önceki
	// oturumdan düşük kalmışsa min_perf_pct=100 sessizce etkisiz olurdu.
	// min_perf_pct=100: HWP isteğinde "en düşük = en yüksek performans"
	// (HWP min = max). no_turbo=0 politika döngüsünden ÖNCE yazılır: 1 iken
	// intel_pstate cpuinfo_max_freq'i turbo olmayan azamiye indirir ve
	// aşağıda okunan hedef turbo frekanslarını içermezdi.
	ip := c.p(cpuDir + "/intel_pstate")
	turboDone, turboFail, turboSeen := []string{}, []string{}, false
	if status, err := readStr(filepath.Join(ip, "status")); err == nil {
		drvDetail += " (" + status + " kip)"
		if status != "off" {
			var ok, bad []string
			for _, kv := range [][2]string{{"max_perf_pct", "100"}, {"min_perf_pct", "100"}} {
				f := filepath.Join(ip, kv[0])
				if !exists(f) {
					continue
				}
				if err := c.set(f, kv[1]); err != nil {
					bad = append(bad, fmt.Sprintf("%s: %v", kv[0], err))
				} else {
					ok = append(ok, kv[0]+"="+kv[1])
				}
			}
			if len(ok)+len(bad) > 0 {
				add(stateOf("Performans sınırları", len(ok), len(ok)+len(bad),
					strings.Join(append(ok, bad...), ", ")))
			}
			if f := filepath.Join(ip, "no_turbo"); exists(f) {
				turboSeen = true
				if err := c.set(f, "0"); err != nil {
					turboFail = append(turboFail, fmt.Sprintf("no_turbo=0 yazılamadı (%v) — BIOS'ta Turbo Boost kapalı olabilir", err))
				} else {
					turboDone = append(turboDone, "no_turbo=0")
				}
			}
		}
	}
	add(model.TurboItem{Name: "Frekans sürücüsü", State: model.TurboOK, Detail: drvDetail})

	// ── Genel artırma (acpi-cpufreq, amd-pstate) ───────────────────────
	if f := c.p(cpuDir + "/cpufreq/boost"); exists(f) {
		turboSeen = true
		if err := c.set(f, "1"); err != nil {
			turboFail = append(turboFail, fmt.Sprintf("boost=1 yazılamadı (%v)", err))
		} else {
			turboDone = append(turboDone, "boost=1")
		}
	}
	switch {
	case !turboSeen:
		add(model.TurboItem{Name: "Turbo Boost", State: model.TurboUnsupported,
			Detail: "sürücü turbo anahtarı sunmuyor (varsa donanım kendisi yönetiyor)"})
	case len(turboFail) > 0 && len(turboDone) == 0:
		add(model.TurboItem{Name: "Turbo Boost", State: model.TurboFailed, Detail: strings.Join(turboFail, "; ")})
	case len(turboFail) > 0:
		add(model.TurboItem{Name: "Turbo Boost", State: model.TurboPartial,
			Detail: strings.Join(append(turboDone, turboFail...), "; ")})
	default:
		add(model.TurboItem{Name: "Turbo Boost", State: model.TurboOK, Detail: "açık (" + strings.Join(turboDone, ", ") + ")"})
	}

	// ── Politika başına ─────────────────────────────────────────────────
	//
	// Sıra: azami frekans, EPP, yönetici, en düşük frekans. EPP yönetici
	// "performance" olmadan ÖNCE yazılmalı: intel_pstate ve amd-pstate-epp
	// performance politikasında EPP yazımını EBUSY ile reddeder.
	//
	// ── Hedef: işlemcinin EN YÜKSEK frekansı ────────────────────────────
	// scaling_min_freq = cpuinfo_max_freq (turbo/boost dahil azami). Kullanıcı
	// "temel frekans 1,7 GHz ise 2-3 GHz'de çalışsın, 400 MHz'de değil"
	// dedi; ilk sürüm tabanı yalnızca temel frekansa çekiyordu, bu da turbo
	// frekanslarını işlemcinin keyfine bırakıyordu. Artık en düşük istek en
	// yüksek frekanstır: intel_pstate'te HWP min = max, amd-pstate'te
	// min_perf = highest_perf, acpi-cpufreq'te turbo girdisi (temel+1 MHz)
	// sabit istenir.
	//
	// DÜRÜST OLMAK GEREKEN YER: bu bir İSTEKTİR. PL1/PL2 güç sınırları,
	// TjMax/PROCHOT ısıl koruması ve çok çekirdekli turbo tablosu donanımca
	// uygulanmaya devam eder; ısı ya da güç sınırı gelirse işlemci kendisi
	// düşürür. Bu korumalara bilerek dokunulmuyor.
	var govOK, govN, eppOK, eppN, minOK, minN int
	var govErr, eppErr, minErr string
	var target int64
	for _, pol := range pols {
		f := func(n string) string { return filepath.Join(pol, n) }

		if maxHW := readInt(f("cpuinfo_max_freq")); maxHW > 0 && exists(f("scaling_max_freq")) {
			// Başka bir araç ya da önceki oturum tavanı düşürmüşse turbo
			// frekanslarına hiç çıkılamaz.
			_ = c.set(f("scaling_max_freq"), strconv.FormatInt(maxHW, 10))
		}

		if exists(f("energy_performance_preference")) {
			avail, _ := readStr(f("energy_performance_available_preferences"))
			if hasWord(avail, "performance") {
				eppN++
				if err := c.set(f("energy_performance_preference"), "performance"); err != nil {
					// EBUSY: yönetici zaten performance ise çekirdek EPP'yi
					// kendisi 0 (performance) yapmıştır; bu bir başarıdır.
					if cur, _ := readStr(f("energy_performance_preference")); cur == "performance" {
						eppOK++
					} else {
						eppErr = err.Error()
					}
				} else {
					eppOK++
				}
			}
		}

		avail, _ := readStr(f("scaling_available_governors"))
		if hasWord(avail, "performance") {
			govN++
			if err := c.set(f("scaling_governor"), "performance"); err != nil {
				govErr = err.Error()
			} else {
				govOK++
			}
		}

		// Kırpma YOK: scaling_max_freq bir platform sınırıyla düşükse
		// çekirdek min'i kendisi max'a kırpar (cpufreq_set_policy); biz
		// gerçek hedefi isteriz.
		if maxHW := readInt(f("cpuinfo_max_freq")); maxHW > 0 && exists(f("scaling_min_freq")) {
			minN++
			if err := c.set(f("scaling_min_freq"), strconv.FormatInt(maxHW, 10)); err != nil {
				minErr = err.Error()
			} else {
				minOK++
				if maxHW > target {
					target = maxHW
				}
			}
		}
	}
	c.targetKHz = target
	n := len(pols)
	if govN == 0 {
		add(model.TurboItem{Name: "İşlemci yöneticisi", State: model.TurboUnsupported,
			Detail: "\"performance\" yöneticisi sunulmuyor (çekirdekte CPU_FREQ_GOV_PERFORMANCE?)"})
	} else {
		add(stateOf("İşlemci yöneticisi", govOK, n, withErr(fmt.Sprintf("performance — %d/%d politika", govOK, n), govErr)))
	}
	if eppN == 0 {
		add(model.TurboItem{Name: "Enerji tercihi (EPP)", State: model.TurboUnsupported,
			Detail: "bu sürücü EPP sunmuyor"})
	} else {
		add(stateOf("Enerji tercihi (EPP)", eppOK, eppN, withErr(fmt.Sprintf("performance — %d/%d politika", eppOK, eppN), eppErr)))
	}
	if minN == 0 {
		add(model.TurboItem{Name: "Frekans hedefi", State: model.TurboUnsupported,
			Detail: "sürücü azami frekansı bildirmiyor — taban değiştirilmedi"})
	} else {
		add(stateOf("Frekans hedefi", minOK, minN, withErr(
			fmt.Sprintf("en düşük frekans = azami (%s) — azami frekans isteniyor; "+
				"ısı/güç sınırı gelirse işlemci kendisi düşürür", GHz(int(target/1000))), minErr)))
	}
	return items, anyOK
}

// stateOf başarı sayısına göre durum seçer.
func stateOf(name string, ok, total int, detail string) model.TurboItem {
	st := model.TurboOK
	switch {
	case ok == 0:
		st = model.TurboFailed
	case ok < total:
		st = model.TurboPartial
	}
	return model.TurboItem{Name: name, State: st, Detail: detail}
}

func withErr(s, err string) string {
	if err == "" {
		return s
	}
	return s + " (hata: " + err + ")"
}

func hasWord(list, w string) bool {
	for _, f := range strings.Fields(list) {
		if f == w {
			return true
		}
	}
	return false
}

// ── Platform profili ────────────────────────────────────────────────────────

const platformProfile = "sys/firmware/acpi/platform_profile"

// applyPlatformProfile ACPI platform profilini "performance" yapar.
//
// Dizüstülerde (ThinkPad, IdeaPad, HP, ASUS) bu, üreticinin güç sınırlarını
// (PL1/PL2) ve fan eğrisini birlikte değiştiren TEK anahtardır; frekans
// ayarları tek başına güç sınırına takılır.
func (c *Controller) applyPlatformProfile() (model.TurboItem, bool) {
	f := c.p(platformProfile)
	cur, err := readStr(f)
	if err != nil {
		return model.TurboItem{Name: "Platform profili", State: model.TurboUnsupported,
			Detail: "yok (üretici sürücüsü platform profili sunmuyor)"}, false
	}
	choices, _ := readStr(f + "_choices")
	if !hasWord(choices, "performance") {
		return model.TurboItem{Name: "Platform profili", State: model.TurboUnsupported,
			Detail: "\"performance\" seçeneği yok (seçenekler: " + choices + ")"}, false
	}
	if err := c.set(f, "performance"); err != nil {
		return model.TurboItem{Name: "Platform profili", State: model.TurboFailed, Detail: err.Error()}, false
	}
	d := "performance"
	if cur != "performance" {
		d += " (önceki: " + cur + ")"
	}
	return model.TurboItem{Name: "Platform profili", State: model.TurboOK, Detail: d}, true
}

// applyVendor üreticiye özgü performans kiplerini açar (şimdilik ASUS).
//
// asus-wmi'de throttle_thermal_policy=1 ve fan_boost_mode=1 "overboost"
// demektir (drivers/platform/x86/asus-wmi.c: ASUS_THROTTLE_THERMAL_POLICY_
// OVERBOOST, ASUS_FAN_BOOST_MODE_OVERBOOST). present=false: bu üretici yok,
// madde listesini kalabalıklaştırmamak için hiç gösterilmez.
func (c *Controller) applyVendor() (it model.TurboItem, ok, present bool) {
	var done, bad []string
	for _, n := range []string{"throttle_thermal_policy", "fan_boost_mode"} {
		m, _ := filepath.Glob(c.p("sys/devices/platform/asus-*/" + n))
		for _, f := range m {
			present = true
			if err := c.set(f, "1"); err != nil {
				bad = append(bad, n+": "+err.Error())
			} else {
				done = append(done, n+"=1")
			}
		}
	}
	if !present {
		return it, false, false
	}
	it = stateOf("ASUS performans kipi", len(done), len(done)+len(bad), strings.Join(append(done, bad...), ", "))
	return it, len(done) > 0, true
}
