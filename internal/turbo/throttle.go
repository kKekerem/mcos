package turbo

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"mcos/internal/model"
)

// ── İşlemciyi yavaşlatan soğutma aygıtları ─────────────────────────────────
//
// fans.go bu aygıtlara BİLEREK dokunmaz ("Processor"u max_state'e çekmek
// işlemciyi yavaşlatır). Burada ters yön: turbo açılırken bayat bir
// kısıtlama varsa (cur_state > 0) 0'a indirilir.
//
// "Processor" (drivers/acpi/processor_thermal.c): ilk kademeler cpufreq
// azami frekansına bir freq_qos tavanı koyar (kademe başına %20), sonrakiler
// T-durumu (saat kesme) uygular. Bu tavan scaling_max_freq'i kırpar ve
// "yazıldı ama geri okuyunca farklı" olarak görünür; turbo isteği ne olursa
// olsun frekans o tavanı aşamaz.
//
// KORUMA SINIRI: aygıtın bağlı olduğu ısıl bölge şu an o aygıtın tetik
// noktasının ÜSTÜNDEYSE kısıtlama bayat değil, GERÇEK bir ısıl karardır;
// dokunulmaz ve tanıda "bölge sıcak" diye gösterilir. Kritik/sıcak tetik
// noktaları ve donanımın TjMax/PROCHOT koruması zaten hiç değiştirilmez.

// isThrottleCdev işlemciyi yavaşlatan soğutma aygıtı türlerini tanır.
func isThrottleCdev(t string) bool {
	switch {
	case t == "Processor", t == "intel_powerclamp":
		return true
	case strings.HasPrefix(t, "thermal-cpufreq"), strings.HasPrefix(t, "cpufreq-"):
		return true
	}
	return false
}

func (c *Controller) throttleCdevs() []string {
	m, _ := filepath.Glob(c.p("sys/class/thermal/cooling_device*"))
	sort.Slice(m, func(i, j int) bool { return cdevNum(m[i]) < cdevNum(m[j]) })
	var out []string
	for _, d := range m {
		if t, _ := readStr(filepath.Join(d, "type")); isThrottleCdev(t) {
			out = append(out, d)
		}
	}
	return out
}

func cdevNum(d string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(filepath.Base(d), "cooling_device"))
	return n
}

// cdevHot, aygıtın bağlı olduğu bir ısıl bölge o bağın tetik noktasının
// üstündeyse bölgeyi, sıcaklığı ve eşiği döner.
//
// Bağ, thermal_zoneN/cdevM -> ../cooling_deviceK sembolik bağıdır; eşik
// cdevM_trip_point dosyasındaki tetik numarasıdır (thermal_sysfs.c).
func (c *Controller) cdevHot(dev string) (zone string, temp, trip int64, hot bool) {
	base := filepath.Base(dev)
	zones, _ := filepath.Glob(c.p("sys/class/thermal/thermal_zone*"))
	for _, z := range zones {
		links, _ := filepath.Glob(filepath.Join(z, "cdev[0-9]*"))
		for _, l := range links {
			if strings.Contains(filepath.Base(l), "_") {
				continue // cdevM_trip_point, cdevM_weight
			}
			tgt, err := os.Readlink(l)
			if err != nil || filepath.Base(tgt) != base {
				continue
			}
			idx, err := readStr(l + "_trip_point")
			if err != nil {
				continue
			}
			tt := readInt(filepath.Join(z, "trip_point_"+idx+"_temp"))
			t := readInt(filepath.Join(z, "temp"))
			if tt > 0 && t >= tt {
				zt, _ := readStr(filepath.Join(z, "type"))
				if zt == "" {
					zt = filepath.Base(z)
				}
				return zt, t, tt, true
			}
		}
	}
	return "", 0, 0, false
}

// C, mili-santigradı "62 °C" biçimine çevirir.
func C(milli int64) string { return fmt.Sprintf("%d °C", (milli+500)/1000) }

// applyThrottle bayat işlemci kısıtlamalarını kaldırır (bkz. dosya başı).
func (c *Controller) applyThrottle() model.TurboItem {
	const name = "Isıl kısıtlama"
	devs := c.throttleCdevs()
	if len(devs) == 0 {
		return model.TurboItem{Name: name, State: model.TurboUnsupported,
			Detail: "işlemci kısıtlama aygıtı yok (ACPI Processor/powerclamp) — kısıtlama yalnızca donanımda"}
	}
	var zeroed, held, bad []string
	clean := 0
	for _, d := range devs {
		t, _ := readStr(filepath.Join(d, "type"))
		label := t + "/" + filepath.Base(d)
		cs := filepath.Join(d, "cur_state")
		cur := readInt(cs)
		if cur <= 0 { // powerclamp boştayken -1 okunur
			clean++
			continue
		}
		if z, temp, trip, hot := c.cdevHot(d); hot {
			held = append(held, fmt.Sprintf("%s=%d (%s %s ≥ eşik %s: gerçek ısıl karar, dokunulmadı)", label, cur, z, C(temp), C(trip)))
			continue
		}
		if err := c.record(cs, kindThrottle, ""); err != nil {
			bad = append(bad, label+": "+err.Error())
			continue
		}
		if err := c.write(cs, "0"); err != nil {
			bad = append(bad, label+": "+err.Error())
			continue
		}
		if c.verify(cs, "0", 0) {
			zeroed = append(zeroed, fmt.Sprintf("%s %d→0", label, cur))
		} else {
			bad = append(bad, label+": 0 yazıldı, geri okunan "+firstOf(readStr(cs)))
		}
	}
	parts := []string{fmt.Sprintf("%d aygıt, %d kısıtlamasız", len(devs), clean)}
	if len(zeroed) > 0 {
		parts = append(parts, "bayat kısıtlama kaldırıldı: "+strings.Join(zeroed, ", "))
	}
	parts = append(parts, held...)
	parts = append(parts, bad...)
	st := model.TurboOK
	if len(held) > 0 || len(bad) > 0 {
		st = model.TurboPartial
	}
	return model.TurboItem{Name: name, State: st, Detail: strings.Join(parts, "; ")}
}

// ── Kısıtlama sayaçları ─────────────────────────────────────────────────────

// throttleCounts, cpuN/thermal_throttle sayaçlarını toplar
// (drivers/thermal/intel/therm_throt.c). Çekirdek sayaçları çekirdek
// başınadır ve toplanır; paket sayaçları paketteki her CPU'da AYNI değeri
// gösterir, en büyüğü alınır (tek soket).
//
// Güç sınırı sayaçları (core/package_power_limit_count) yalnızca
// int_pln_enable komut satırı parametresiyle görünür; o parametre bilerek
// açılmadı (PLN kesmeleri sürekli güç sınırında kesme fırtınasına yol
// açtığı için çekirdekte öntanımlı kapalı). Güç sınırı bunun yerine RAPL
// tüketimi ve MSR'lerle ölçülür (bkz. diag.go).
func (c *Controller) throttleCounts() map[string]int64 {
	out := map[string]int64{}
	m, _ := filepath.Glob(c.p(cpuDir + "/cpu[0-9]*/thermal_throttle"))
	if len(m) == 0 {
		return out
	}
	out["cpus"] = int64(len(m))
	for _, d := range m {
		out["core"] += readInt(filepath.Join(d, "core_throttle_count"))
		out["core_ms"] += readInt(filepath.Join(d, "core_throttle_total_time_ms"))
		if v := readInt(filepath.Join(d, "package_throttle_count")); v > out["pkg"] {
			out["pkg"] = v
		}
		if v := readInt(filepath.Join(d, "package_throttle_total_time_ms")); v > out["pkg_ms"] {
			out["pkg_ms"] = v
		}
	}
	return out
}

// ── Sıcaklıklar ─────────────────────────────────────────────────────────────

// tempLines işlemci sıcaklığını (coretemp/k10temp/zenpower) ve ısıl
// bölgeleri tetik noktalarıyla birlikte verir. pkgMax: en yüksek işlemci
// sıcaklığı (mili °C), sınırlayan tahmini için.
func (c *Controller) tempLines() (lines []string, pkgMax int64) {
	hws, _ := filepath.Glob(c.p("sys/class/hwmon/hwmon*"))
	sort.Strings(hws)
	for _, hw := range hws {
		name := hwmonName(hw)
		switch name {
		case "coretemp", "k10temp", "zenpower":
		default:
			continue
		}
		ins, _ := filepath.Glob(filepath.Join(hw, "temp*_input"))
		sort.Strings(ins)
		var parts []string
		for _, in := range ins {
			v := readInt(in)
			if v <= 0 {
				continue
			}
			lb, _ := readStr(strings.TrimSuffix(in, "_input") + "_label")
			// coretemp çekirdek başına satır da verir; paket satırı yeter,
			// çekirdekler yalnızca en yüksekleriyle özetlenir.
			if strings.HasPrefix(lb, "Core ") {
				if v > pkgMax {
					pkgMax = v
				}
				continue
			}
			if lb == "" {
				lb = filepath.Base(strings.TrimSuffix(in, "_input"))
			}
			if v > pkgMax {
				pkgMax = v
			}
			s := lb + " " + C(v)
			if cr := readInt(strings.TrimSuffix(in, "_input") + "_crit"); cr > 0 {
				s += " (kritik " + C(cr) + ")"
			}
			parts = append(parts, s)
		}
		if len(parts) > 0 {
			lines = append(lines, name+": "+strings.Join(parts, ", "))
		}
	}
	zones, _ := filepath.Glob(c.p("sys/class/thermal/thermal_zone*"))
	sort.Strings(zones)
	for _, z := range zones {
		t := readInt(filepath.Join(z, "temp"))
		if t <= 0 {
			continue
		}
		zt, _ := readStr(filepath.Join(z, "type"))
		if zt == "x86_pkg_temp" && t > pkgMax {
			pkgMax = t
		}
		var trips []string
		types, _ := filepath.Glob(filepath.Join(z, "trip_point_*_type"))
		sort.Strings(types)
		for _, tf := range types {
			ty, _ := readStr(tf)
			tt := readInt(strings.TrimSuffix(tf, "_type") + "_temp")
			if tt <= 0 {
				continue
			}
			tr := map[string]string{"passive": "pasif", "active": "aktif", "hot": "sıcak", "critical": "kritik"}[ty]
			if tr == "" {
				tr = ty
			}
			trips = append(trips, tr+" "+C(tt))
		}
		s := zt + " " + C(t)
		if len(trips) > 0 {
			s += " (" + strings.Join(trips, ", ") + ")"
		}
		lines = append(lines, s)
	}
	return lines, pkgMax
}
