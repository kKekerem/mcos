package turbo

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"mcos/internal/model"
)

// Fan kanalı kipleri.
const (
	fanManual   = "elle"     // pwmN_enable=1, pwmN=255
	fanFull0    = "tam"      // pwmN_enable=0 (ABI: "denetim yok = tam hız")
	fanNoEnable = "pwm"      // enable dosyası yok; yalnızca pwmN=255
	fanCdev     = "soğutucu" // ısıl soğutma aygıtı cur_state=max_state
	fanThinkpad = "thinkpad" // /proc/acpi/ibm/fan "level 7"
	fanIdeapad  = "ideapad"  // VPC2004:*/fan_mode = 4
)

// ideapadFanGlob: ideapad-laptop'un fan kipi dosyası
// (Documentation/ABI/testing/sysfs-platform-ideapad-laptop). 4 = "Efficient
// Thermal Dissipation" (en güçlü soğutma); 2 = "Dust Cleaning" bir süre tam
// hız çevirip durur, 7/24 sunucu için uygun değil.
const ideapadFanGlob = "sys/bus/platform/devices/VPC2004:*/fan_mode"

// pwmFullMin: elle kipte okunan PWM bunun altındaysa "tam güç" sayılmaz.
// 255 değil, çünkü bazı yongalar değeri kırpar: it87'nin 7 bitlik eski
// yongaları 254, dell-smm üç kademede 254 döndürür.
const pwmFullMin = 230

// fullZeroDrivers: pwmN_enable=0 değerinin kaynak kodda "tam hız" olduğu
// DOĞRULANMIŞ sürücüler. Başka sürücülerde 0 yazılmaz, çünkü ABI'ye
// uymayanlar var: pwm-fan'da 0 "PWM kapalı, düzenleyici kapalı" = FAN DURUR
// (drivers/hwmon/pwm-fan.c: pwm_off_reg_off).
//   - hp:   hp-wmi.c hp_wmi_hwmon_write: 0 -> fan_speed_max_set(1), elle yok
//   - asus: asus-wmi.c ASUS_FAN_CTRL_FULLSPEED = 0 (SPEC83'te elle kip yok)
var fullZeroDrivers = map[string]bool{"hp": true, "asus": true}

type fanChan struct {
	label string
	mode  string
	en    string // pwmN_enable ya da cur_state/procfs yolu
	pwm   string
	max   string // soğutma aygıtında max_state değeri
}

var (
	rePWM   = regexp.MustCompile(`^pwm([0-9]+)$`)
	rePWMEn = regexp.MustCompile(`^pwm([0-9]+)_enable$`)
)

type hwmonChan struct {
	n       int
	pwm, en string
}

// hwmonChans bir hwmon aygıtının PWM kanallarını bulur. Eski tip sürücüler
// öznitelikleri hwmonN/device altına koyar; ikisine de bakılır.
func hwmonChans(dir string) []hwmonChan {
	byN := map[int]*hwmonChan{}
	for _, d := range []string{dir, filepath.Join(dir, "device")} {
		ents, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if m := rePWM.FindStringSubmatch(e.Name()); m != nil {
				n, _ := strconv.Atoi(m[1])
				ch := getCh(byN, n)
				if ch.pwm == "" {
					ch.pwm = filepath.Join(d, e.Name())
				}
			} else if m := rePWMEn.FindStringSubmatch(e.Name()); m != nil {
				n, _ := strconv.Atoi(m[1])
				ch := getCh(byN, n)
				if ch.en == "" {
					ch.en = filepath.Join(d, e.Name())
				}
			}
		}
	}
	var out []hwmonChan
	for _, ch := range byN {
		out = append(out, *ch)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].n < out[j].n })
	return out
}

func getCh(m map[int]*hwmonChan, n int) *hwmonChan {
	if m[n] == nil {
		m[n] = &hwmonChan{n: n}
	}
	return m[n]
}

func hwmonName(dir string) string {
	if n, err := readStr(filepath.Join(dir, "name")); err == nil {
		return n
	}
	n, _ := readStr(filepath.Join(dir, "device", "name"))
	return n
}

const tpFan = "proc/acpi/ibm/fan"

// countFanCandidates turbo kapalıyken "destekleniyor mu" sorusu için
// hiçbir şey yazmadan denetlenebilir fan sayısını verir.
func (c *Controller) countFanCandidates() int {
	n := 0
	hws, _ := filepath.Glob(c.p("sys/class/hwmon/hwmon*"))
	for _, hw := range hws {
		n += len(hwmonChans(hw))
	}
	n += len(c.fanCdevs())
	if exists(c.p(tpFan)) {
		n++
	}
	m, _ := filepath.Glob(c.p(ideapadFanGlob))
	return n + len(m)
}

// hasHwmon, adı verilen bir hwmon aygıtı var mı söyler.
func (c *Controller) hasHwmon(name string) bool {
	hws, _ := filepath.Glob(c.p("sys/class/hwmon/hwmon*"))
	for _, hw := range hws {
		if hwmonName(hw) == name {
			return true
		}
	}
	return false
}

// fanCdevs "fan" türündeki ısıl soğutma aygıtlarını döner.
//
// YALNIZCA türünde "fan" geçenler: "Processor" (cpufreq soğutması) ya da
// "intel_powerclamp" aygıtını max_state'e çekmek işlemciyi YAVAŞLATIR —
// turbonun tam tersi. ACPI fanı "Fan" (PNP0C0B) ya da ACPI kimliğiyle
// (ör. "FAN0"), dell-smm "dell-smm-fanN", acerhdf "acerhdf-fan" adını alır.
func (c *Controller) fanCdevs() []string {
	m, _ := filepath.Glob(c.p("sys/class/thermal/cooling_device*"))
	var out []string
	for _, d := range m {
		t, _ := readStr(filepath.Join(d, "type"))
		if strings.Contains(strings.ToLower(t), "fan") {
			out = append(out, d)
		}
	}
	return out
}

// applyFans tüm fanları son güce alır.
func (c *Controller) applyFans() (model.TurboItem, bool) {
	c.fans = nil
	c.fansTotal = 0
	var fails, notes []string
	thinkpadProc := exists(c.p(tpFan))

	hws, _ := filepath.Glob(c.p("sys/class/hwmon/hwmon*"))
	sort.Strings(hws)
	for _, hw := range hws {
		name := hwmonName(hw)
		// ThinkPad fanı procfs üzerinden sürülür; aynı fanı iki yoldan
		// sürmek, birinin geri yüklemesinin diğerini bozması demek.
		if name == "thinkpad" && thinkpadProc {
			continue
		}
		for _, ch := range hwmonChans(hw) {
			c.fansTotal++
			label := fmt.Sprintf("%s/pwm%d", name, ch.n)
			f, err := c.fanUp(name, label, ch)
			if err != nil {
				fails = append(fails, label+": "+err.Error())
				continue
			}
			c.fans = append(c.fans, f)
		}
	}

	dellHwmon := c.hasHwmon("dell_smm")
	for _, d := range c.fanCdevs() {
		t, _ := readStr(filepath.Join(d, "type"))
		// dell-smm AYNI fanı hem hwmon pwmN hem "dell-smm-fanN" soğutma
		// aygıtı olarak sunar (dell-smm-hwmon.c, 5.19+). İki yoldan sürmek
		// fanı iki kez saymak ve geri yüklemede birinin diğerini ezmesi
		// demek; hwmon yolu yeter.
		if dellHwmon && strings.HasPrefix(t, "dell-smm-fan") {
			continue
		}
		c.fansTotal++
		label := t + "/" + filepath.Base(d)
		mx, err := readStr(filepath.Join(d, "max_state"))
		if err != nil || mx == "0" {
			fails = append(fails, label+": max_state okunamadı")
			continue
		}
		cs := filepath.Join(d, "cur_state")
		if err := c.record(cs, kindCdev, filepath.Join(d, "max_state")); err != nil {
			fails = append(fails, label+": "+err.Error())
			continue
		}
		if err := c.write(cs, mx); err != nil {
			fails = append(fails, label+": "+err.Error())
			continue
		}
		c.fans = append(c.fans, fanChan{label: label, mode: fanCdev, en: cs, max: mx})
	}

	if thinkpadProc {
		c.fansTotal++
		if f, err := c.thinkpadUp(); err != nil {
			fails = append(fails, "thinkpad: "+err.Error())
		} else {
			c.fans = append(c.fans, f)
		}
		if b, _ := os.ReadFile(c.p(tpFan)); !strings.Contains(string(b), "commands:") {
			notes = append(notes, "ThinkPad fan denetimi çekirdekte kapalı: komut satırına thinkpad_acpi.fan_control=1 eklenmeli")
		}
	}

	ips, _ := filepath.Glob(c.p(ideapadFanGlob))
	for _, f := range ips {
		c.fansTotal++
		if err := c.set(f, "4"); err != nil {
			fails = append(fails, "ideapad/fan_mode: "+err.Error())
			continue
		}
		c.fans = append(c.fans, fanChan{label: "ideapad/fan_mode=4", mode: fanIdeapad, en: f})
	}

	if c.fansTotal == 0 {
		return model.TurboItem{Name: "Fanlar", State: model.TurboUnsupported,
			Detail: "denetlenebilir fan bulunamadı (hwmon PWM, ACPI fan, ThinkPad/Dell/ASUS/HP sürücüsü yok)"}, false
	}
	var labels []string
	for _, f := range c.fans {
		labels = append(labels, f.label)
	}
	d := fmt.Sprintf("%d/%d kanal son güçte", len(c.fans), c.fansTotal)
	if len(labels) > 0 {
		d += " (" + strings.Join(labels, ", ") + ")"
	}
	if len(fails) > 0 {
		d += "; alınamayan: " + strings.Join(fails, "; ")
	}
	if len(notes) > 0 {
		d += "; " + strings.Join(notes, "; ")
	}
	it := stateOf("Fanlar", len(c.fans), c.fansTotal, d)
	return it, len(c.fans) > 0
}

// fanUp tek bir hwmon kanalını son güce alır.
//
// Güvenlik: elle kipe geçtikten sonra okunan değer tam güce yakın değilse
// (sürücü PWM'i reddetti ya da yok saydı) kanal HEMEN özgün kipine döner;
// fan bir an bile "elle ama düşük" durumda bırakılmaz.
func (c *Controller) fanUp(name, label string, ch hwmonChan) (fanChan, error) {
	switch {
	case ch.pwm != "" && ch.en != "":
		if err := c.record(ch.pwm, kindPWM, ch.en); err != nil {
			return fanChan{}, err
		}
		if err := c.record(ch.en, kindPlain, ""); err != nil {
			return fanChan{}, err
		}
		origEn := c.j.find(ch.en).Orig
		// Önce PWM: bazı yongalar elle kipe geçerken önbellekteki PWM'i
		// yazar; önbellek soğuk anda düşük kalmışsa fan bir an yavaşlardı.
		_ = c.write(ch.pwm, "255")
		errEn := c.write(ch.en, "1")
		errPWM := c.write(ch.pwm, "255")
		if errEn == nil && errPWM == nil && readInt(ch.pwm) >= pwmFullMin {
			return fanChan{label: label, mode: fanManual, en: ch.en, pwm: ch.pwm}, nil
		}
		if fullZeroDrivers[name] && c.write(ch.en, "0") == nil {
			if v, _ := readStr(ch.en); v == "0" {
				return fanChan{label: label, mode: fanFull0, en: ch.en}, nil
			}
		}
		_ = c.write(ch.en, origEn) // özgün (otomatik) kipe hemen dön
		switch {
		case errEn != nil:
			return fanChan{}, fmt.Errorf("elle kipe geçilemedi (%v)", errEn)
		case errPWM != nil:
			return fanChan{}, fmt.Errorf("PWM yazılamadı (%v)", errPWM)
		default:
			return fanChan{}, fmt.Errorf("PWM okuması %d — sürücü tam gücü kabul etmedi", readInt(ch.pwm))
		}

	case ch.en != "": // PWM dosyası yok (hp-wmi): yalnızca kip seçilebilir
		if !fullZeroDrivers[name] {
			return fanChan{}, fmt.Errorf("yalnızca kip anahtarı var ve %q sürücüsünde 0'ın anlamı doğrulanmadı", name)
		}
		if err := c.record(ch.en, kindPlain, ""); err != nil {
			return fanChan{}, err
		}
		if err := c.write(ch.en, "0"); err != nil {
			return fanChan{}, err
		}
		// hp-wmi okumada BIOS'un gerçek kipini döner (hp_wmi_fan_speed_max_get):
		// yazım "başarılı" olup BIOS reddederse burada görünür.
		if !c.verify(ch.en, "0", 0) {
			return fanChan{}, fmt.Errorf("tam hız kipi yazıldı ama geri okunan %q", firstOf(readStr(ch.en)))
		}
		return fanChan{label: label, mode: fanFull0, en: ch.en}, nil

	default: // enable dosyası yok: BIOS denetimi sürer, biz yalnızca yükseltiriz
		if err := c.record(ch.pwm, kindPWMNoEnable, ""); err != nil {
			return fanChan{}, err
		}
		if err := c.write(ch.pwm, "255"); err != nil {
			return fanChan{}, err
		}
		return fanChan{label: label, mode: fanNoEnable, pwm: ch.pwm}, nil
	}
}

// reassertFan donanımın geri aldığı ayarı yeniden uygular (bkz. Refresh).
func (c *Controller) reassertFan(f fanChan) {
	switch f.mode {
	case fanManual:
		if v, _ := readStr(f.en); v != "1" || readInt(f.pwm) < pwmFullMin {
			_ = c.write(f.pwm, "255")
			_ = c.write(f.en, "1")
			_ = c.write(f.pwm, "255")
		}
	case fanFull0:
		if v, _ := readStr(f.en); v != "0" {
			_ = c.write(f.en, "0")
		}
	case fanNoEnable:
		if readInt(f.pwm) < pwmFullMin {
			_ = c.write(f.pwm, "255")
		}
	case fanThinkpad:
		if lv, _ := thinkpadLevel(f.en); lv != "7" {
			_ = c.write(f.en, "level 7")
		}
	case fanIdeapad:
		if v, _ := readStr(f.en); v != "4" {
			_ = c.write(f.en, "4")
		}
	case fanCdev:
		// Soğutma aygıtı YENİDEN zorlanmaz: ısıl yönetici hedefini
		// değiştirip aygıta yazdıysa o yazım bir güvenlik kararıdır.
	}
}

// ── ThinkPad ────────────────────────────────────────────────────────────────

// thinkpadLevel /proc/acpi/ibm/fan içindeki "level:" değerini okur.
func thinkpadLevel(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == "level" {
			return strings.TrimSpace(v), nil
		}
	}
	return "", fmt.Errorf("level satırı yok")
}

// thinkpadUp ThinkPad fanını 7. kademeye alır.
//
// Neden "full-speed" DEĞİL: thinkpad-acpi belgesi (admin-guide/laptops/
// thinkpad-acpi.rst) "full-speed/disengaged" kipinde EC'nin hız denetimini
// bırakıp fanı "donanım sınırlarını aşabilecek" hızda sürdüğünü söylüyor.
// 7, "önerilen en yüksek hız". 7/24 çalışan sunucuda fanı yıpratmamak için
// 7 seçildi.
func (c *Controller) thinkpadUp() (fanChan, error) {
	f := c.p(tpFan)
	b, _ := os.ReadFile(f)
	if !strings.Contains(string(b), "commands:") {
		return fanChan{}, fmt.Errorf("fan denetimi kapalı (thinkpad_acpi.fan_control=1 gerekli)")
	}
	if err := c.record(f, kindThinkpad, ""); err != nil {
		return fanChan{}, err
	}
	if err := c.write(f, "level 7"); err != nil {
		return fanChan{}, err
	}
	if lv, _ := thinkpadLevel(f); lv != "7" {
		c.mismatches = append(c.mismatches, fmt.Sprintf("%s: yazıldı level 7, geri okunan level %s", c.short(f), lv))
		return fanChan{}, fmt.Errorf("level 7 yazıldı ama EC %q bildiriyor", lv)
	}
	return fanChan{label: "thinkpad/fan", mode: fanThinkpad, en: f}, nil
}

// ── Soğutma aygıtı geri yüklemesi ───────────────────────────────────────────

// restoreCdev bir "Fan" soğutma aygıtını özgün değerine döndürür — ama
// YALNIZCA güvenliyse. skipped=true: bilerek dokunulmadı.
//
// Isıl yönetici (step_wise) aygıta yalnızca HEDEFİ DEĞİŞİNCE yazar
// (thermal_core __thermal_cdev_update, cdev->updated). İki tehlikeli durum:
//  1. Turbo sırasında yönetici hedefi değiştirip aygıta kendi değerini
//     yazdıysa (cur_state artık max değil), aygıt zaten yöneticinindir;
//     eski değeri yazmak onun kararını ezmek olur -> dokunma.
//  2. Bir ısıl bölge şu an bir "active" tetik noktasının ÜSTÜNDEYSE,
//     yöneticinin hedefi yüksektir ama hedef değişmediği için yeniden
//     yazmaz; soğuk anda kaydedilmiş düşük değeri yazarsak fan sıcak
//     makinede DÜŞÜK kalır -> dokunma. Fan tam güçte kalır; sıcaklık tetik
//     noktasının altına inince hedef değişir ve yönetici aygıtı kendisi
//     indirir: otomatiğe dönüş kendiliğinden olur.
func (c *Controller) restoreCdev(ch change) (skipped bool, err error) {
	mx, _ := readStr(ch.Pair)
	cur, _ := readStr(ch.Path)
	if cur != mx {
		return true, nil
	}
	if c.thermalZoneHot() {
		return true, nil
	}
	return false, c.write(ch.Path, ch.Orig)
}

// thermalZoneHot bir ısıl bölgenin herhangi bir "active" tetik noktasının
// üstünde olup olmadığını söyler.
func (c *Controller) thermalZoneHot() bool {
	zones, _ := filepath.Glob(c.p("sys/class/thermal/thermal_zone*"))
	for _, z := range zones {
		temp := readInt(filepath.Join(z, "temp"))
		types, _ := filepath.Glob(filepath.Join(z, "trip_point_*_type"))
		for _, tf := range types {
			if t, _ := readStr(tf); t != "active" {
				continue
			}
			trip := readInt(strings.TrimSuffix(tf, "_type") + "_temp")
			if trip > 0 && temp >= trip {
				return true
			}
		}
	}
	return false
}

// ── Tanı ────────────────────────────────────────────────────────────────────

// fanDiag her fan yolunun GERİ OKUNAN durumunu verir: kip, PWM, RPM.
//
// Gerçek PC'de "fan da çalışmıyor" denildi ama hangi yolun var olduğu,
// yazımın tutup tutmadığı ve fanın gerçekte kaç devirde döndüğü hiçbir
// yerde görünmüyordu. Dizüstülerde fan EC'dedir: üretici sürücüsü
// (thinkpad_acpi, dell-smm, hp-wmi, asus-wmi, ideapad) yoksa Linux fanı
// hiç göremez; bu da açıkça yazılır.
func (c *Controller) fanDiag() []model.TurboItem {
	var out []model.TurboItem
	add := func(st, s string) { out = append(out, model.TurboItem{Name: "Fan", State: st, Detail: s}) }
	driven := map[string]string{}
	for _, f := range c.fans {
		for _, p := range []string{f.en, f.pwm} {
			if p != "" {
				driven[p] = f.mode
			}
		}
	}
	hws, _ := filepath.Glob(c.p("sys/class/hwmon/hwmon*"))
	sort.Strings(hws)
	for _, hw := range hws {
		name := hwmonName(hw)
		chans := hwmonChans(hw)
		rpm := func(n int) string {
			for _, d := range []string{hw, filepath.Join(hw, "device")} {
				if v, err := readStr(filepath.Join(d, fmt.Sprintf("fan%d_input", n))); err == nil {
					return v + " RPM"
				}
			}
			return ""
		}
		seen := map[int]bool{}
		for _, ch := range chans {
			seen[ch.n] = true
			s := fmt.Sprintf("%s/pwm%d:", name, ch.n)
			if ch.en != "" {
				s += " enable=" + dash(firstOf(readStr(ch.en)))
			}
			if ch.pwm != "" {
				s += " pwm=" + dash(firstOf(readStr(ch.pwm)))
			}
			if r := rpm(ch.n); r != "" {
				s += " " + r
			}
			st := model.TurboInfo
			if m, ok := driven[ch.pwm]; ok || driven[ch.en] != "" {
				if !ok {
					m = driven[ch.en]
				}
				s += " — turbo sürüyor (" + m + ")"
				st = model.TurboOK
			} else if c.active {
				s += " — turbo SÜRMÜYOR"
				st = model.TurboPartial
			}
			add(st, s)
		}
		// PWM'i olmayan ama devri okunan fanlar (ör. yalnızca okuma
		// sürücüleri): en azından döndüğü görülsün.
		ins, _ := filepath.Glob(filepath.Join(hw, "fan*_input"))
		sort.Strings(ins)
		for _, in := range ins {
			n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(in), "fan"), "_input"))
			if seen[n] {
				continue
			}
			add(model.TurboInfo, fmt.Sprintf("%s/fan%d: %s RPM (yalnızca okuma, denetim yok)", name, n, firstOf(readStr(in))))
		}
	}
	if b, err := os.ReadFile(c.p(tpFan)); err == nil {
		var kv []string
		for _, l := range strings.Split(string(b), "\n") {
			k, v, ok := strings.Cut(l, ":")
			k = strings.TrimSpace(k)
			if ok && (k == "status" || k == "speed" || k == "level") {
				kv = append(kv, k+"="+strings.TrimSpace(v))
			}
		}
		s := "thinkpad (/proc/acpi/ibm/fan): " + strings.Join(kv, " ")
		st := model.TurboInfo
		if !strings.Contains(string(b), "commands:") {
			s += " — fan denetimi KAPALI (komut satırında thinkpad_acpi.fan_control=1 yok)"
			st = model.TurboPartial
		}
		add(st, s)
	}
	for _, d := range c.fanCdevs() {
		t, _ := readStr(filepath.Join(d, "type"))
		add(model.TurboInfo, fmt.Sprintf("%s/%s: %s/%s", t, filepath.Base(d),
			dash(firstOf(readStr(filepath.Join(d, "cur_state")))), dash(firstOf(readStr(filepath.Join(d, "max_state"))))))
	}
	for _, g := range []string{"sys/devices/platform/asus-*/fan_boost_mode", "sys/devices/platform/asus-*/throttle_thermal_policy", ideapadFanGlob} {
		m, _ := filepath.Glob(c.p(g))
		for _, f := range m {
			add(model.TurboInfo, c.short(f)+" = "+dash(firstOf(readStr(f))))
		}
	}
	if len(out) == 0 {
		add(model.TurboUnsupported, "hiçbir fan yolu yok: hwmon PWM, ACPI fanı, thinkpad/dell-smm/hp-wmi/asus-wmi/ideapad sürücüsü görünmüyor — dizüstüde fan EC'de ve üretici sürücüsü yoksa Linux'tan sürülemez; masaüstünde Super I/O (nct6775/it87) yüklenmemiş olabilir")
	}
	for _, l := range c.kmsg {
		if strings.Contains(strings.ToLower(l), "conflicts with opregion") {
			add(model.TurboPartial, "Super I/O ACPI kaynak çakışması görüldü: fan yongası sürücüsü engellenmiş olabilir (acpi_enforce_resources=lax ile açılır; risk: BIOS ile eşzamanlı erişim — bilerek öntanımlı DEĞİL)")
			break
		}
	}
	return out
}
