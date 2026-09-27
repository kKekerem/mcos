package drm

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Bu dosya PLATFORMDAN BAĞIMSIZDIR: mod listesi, seçim politikası ve panelin
// gösterdiği ekran bilgisi. Donanım gerektirmediği için geliştirme makinesinde
// de (Windows/macOS) derlenir ve sınanır; ioctl'ler drm_linux.go'da.

// ModeInfo mirrors struct drm_mode_modeinfo (include/uapi/drm/drm_mode.h).
//
// 68 bayt: __u32 + 10 x __u16 + 3 x __u32 + char[32]. drm_linux_test.go boyutu
// sabitler; bir alan kayması ioctl numarasını değiştirir.
type ModeInfo struct {
	Clock                                         uint32
	Hdisplay, HsyncStart, HsyncEnd, Htotal, Hskew uint16
	Vdisplay, VsyncStart, VsyncEnd, Vtotal, Vscan uint16
	Vrefresh                                      uint32
	Flags, Type                                   uint32
	Name                                          [32]byte
}

// modeTypePreferred, sürücünün "bu ekranın doğal modu" dediği bayrak
// (DRM_MODE_TYPE_PREFERRED).
const modeTypePreferred = 1 << 3

// Mod bayrakları (drm_mode.h DRM_MODE_FLAG_*).
const (
	flagInterlace = 1 << 4
	flagDblscan   = 1 << 5
)

// Mode is a display mode in the terms the panel cares about.
type Mode struct {
	Width, Height int
	// Refresh, saniyedeki tazeleme (Hz, aşağı yuvarlanmış tam sayı).
	Refresh int
	// MilliHz, tazelemenin tam değeri (59,94 ile 60,00 ayırt edilebilsin).
	MilliHz int
	// Preferred, sürücünün bu ekran için doğal saydığı mod.
	Preferred bool
	// Name, sürücünün verdiği ad ("1920x1080").
	Name string
	// Raw, çekirdeğe SETCRTC ile aynen geri verilen mod. Panel onu
	// yorumlamaz; aynı çözünürlük ve tazelemede iki farklı zamanlama
	// olabileceği için (CVT-RB ile DMT gibi) seçimin kendisi saklanır.
	Raw ModeInfo
}

// String makes a mode printable for the panel.
func (m Mode) String() string {
	return fmt.Sprintf("%dx%d @ %s Hz", m.Width, m.Height, m.HzText())
}

// HzText, tazelemeyi okunur biçimde verir: 60, 59,94, 143,86 ...
//
// Tam sayıya yuvarlamak 59,94'ü "59" yapıyor ve kullanıcı monitörünün
// "60 Hz" dediği modu listede bulamıyordu. Kesir ancak gerçekten varsa
// yazılıyor.
func (m Mode) HzText() string {
	if m.MilliHz <= 0 {
		return strconv.Itoa(m.Refresh)
	}
	tam, kesir := m.MilliHz/1000, (m.MilliHz%1000+5)/10
	if kesir >= 100 {
		tam, kesir = tam+1, 0
	}
	if kesir == 0 {
		return strconv.Itoa(tam)
	}
	return fmt.Sprintf("%d,%02d", tam, kesir)
}

// Key, modu display.conf'ta ve arayüzde tek satırla tanımlar: "1920x1080@60000".
//
// Tazeleme MİLİHERTZ olarak saklanıyor: 59,94 ile 60,00 aynı çözünürlükte iki
// ayrı moddur ve kullanıcının seçtiği tam olarak geri gelmeli.
func (m Mode) Key() string {
	return fmt.Sprintf("%dx%d@%d", m.Width, m.Height, m.MilliHz)
}

// cevir turns a kernel mode into the panel's view of it.
func cevir(m ModeInfo) Mode {
	mhz := refreshMilliHz(m)
	return Mode{
		Width:     int(m.Hdisplay),
		Height:    int(m.Vdisplay),
		Refresh:   mhz / 1000,
		MilliHz:   mhz,
		Preferred: m.Type&modeTypePreferred != 0,
		Name:      modAdi(m),
		Raw:       m,
	}
}

// refreshMilliHz computes the exact refresh rate in millihertz.
//
// ── Neden vrefresh alanına güvenilmiyor ─────────────────────────────────────
//
// Çekirdek o alanı DOLDURUYOR (drm_mode_convert_to_umode -> drm_mode_vrefresh)
// ama TAM SAYI Hz'e YUVARLIYOR: 59,94 ile 60,00 ayırt edilemez hâle geliyor.
// Aynı çözünürlükte iki modu sıralarken bu fark önemli.
//
// Formül çekirdeğin kendi matematiğiyle aynı:
//
//	mHz = clock_kHz * 1.000.000 / (htotal * vtotal)
//	geçmeli (interlace) ise pay  x2
//	çift tarama (dblscan) ise payda x2
//	vscan > 1 ise payda x vscan
func refreshMilliHz(m ModeInfo) int {
	if m.Htotal == 0 || m.Vtotal == 0 {
		return 0
	}
	pay := uint64(m.Clock) * 1_000_000
	payda := uint64(m.Htotal) * uint64(m.Vtotal)
	if m.Flags&flagInterlace != 0 {
		pay *= 2
	}
	if m.Flags&flagDblscan != 0 {
		payda *= 2
	}
	if m.Vscan > 1 {
		payda *= uint64(m.Vscan)
	}
	if payda == 0 {
		return 0
	}
	return int((pay + payda/2) / payda)
}

func modAdi(m ModeInfo) string {
	for i, b := range m.Name {
		if b == 0 {
			return string(m.Name[:i])
		}
	}
	return string(m.Name[:])
}

// modeList converts and filters the kernel's list.
//
// Geçmeli (interlace) modlar ELENİYOR: görsel olarak kötüler ve kullanıcının
// "en iyi mod" beklentisini karşılamıyorlar. Aynı çözünürlük ve tazelemede
// birden çok zamanlama (DMT + CVT) gelebilir; kullanıcıya ikisini birden
// göstermek "aynı satır iki kez" gibi görünür, bu yüzden TEKİLLEŞTİRİLİYOR
// (tercih edilen bayrağı taşıyan önde kalır).
func modeList(ham []ModeInfo) []Mode {
	out := make([]Mode, 0, len(ham))
	for _, m := range ham {
		if m.Flags&flagInterlace != 0 || m.Hdisplay == 0 || m.Vdisplay == 0 {
			continue
		}
		out = append(out, cevir(m))
	}
	sirala(out)
	tekil := out[:0]
	gorulen := map[string]bool{}
	for _, m := range out {
		k := m.Key()
		if gorulen[k] {
			continue
		}
		gorulen[k] = true
		tekil = append(tekil, m)
	}
	return tekil
}

// sirala orders modes so the panel can show the best first.
//
// Sıra: ÖNCE ALAN, sonra tazeleme, en son tercih. Kullanıcı "maks
// çözünürlükte maks yenileme hızında" dediği için listenin başı en büyük ve
// en hızlı olan. Tazelemeyi öne almak 640x480@75'i 1920x1080@60'ın üstüne
// çıkarırdı — istenenin tam tersi.
func sirala(ms []Mode) {
	sort.SliceStable(ms, func(i, j int) bool {
		a, b := ms[i], ms[j]
		if a.Width*a.Height != b.Width*b.Height {
			return a.Width*a.Height > b.Width*b.Height
		}
		if a.Width != b.Width {
			return a.Width > b.Width
		}
		if a.MilliHz != b.MilliHz {
			return a.MilliHz > b.MilliHz
		}
		return a.Preferred && !b.Preferred
	})
}

// Best returns the largest mode with the highest refresh.
//
// Liste sirala ile sıralanmışsa ilk eleman doğrudan cevaptır. Bağlayıcı
// türünü bilen ve daha akıllı seçim yapan Choose'dur; Best ona girdi verir.
func Best(ms []Mode) (Mode, bool) {
	if len(ms) == 0 {
		return Mode{}, false
	}
	cp := append([]Mode(nil), ms...)
	sirala(cp)
	return cp[0], true
}

// HighestRefresh returns the fastest mode at the given resolution.
//
// "Tazeleme hızı maks kaç destekliyorsa o olmalı" isteğinin karşılığı:
// çözünürlük sabit kalırken en hızlı seçenek bulunuyor.
func HighestRefresh(ms []Mode, w, h int) (Mode, bool) {
	var en Mode
	bulundu := false
	for _, m := range ms {
		if m.Width != w || m.Height != h {
			continue
		}
		if !bulundu || m.MilliHz > en.MilliHz ||
			(m.MilliHz == en.MilliHz && m.Preferred && !en.Preferred) {
			en, bulundu = m, true
		}
	}
	return en, bulundu
}

// Find returns the mode matching key ("1920x1080@60000").
//
// Tam eşleşme yoksa AYNI ÇÖZÜNÜRLÜKTE en yakın tazeleme döner: monitör
// değiştiğinde 60,00 Hz'lik kayıt 59,94 Hz'lik bir listede de karşılığını
// bulmalı. Çözünürlük hiç yoksa ok=false — çağıran politikaya döner.
func Find(ms []Mode, key string) (Mode, bool) {
	w, h, mhz, ok := parseKey(key)
	if !ok {
		return Mode{}, false
	}
	var en Mode
	fark := -1
	for _, m := range ms {
		if m.Width != w || m.Height != h {
			continue
		}
		d := m.MilliHz - mhz
		if d < 0 {
			d = -d
		}
		if fark < 0 || d < fark {
			en, fark = m, d
		}
	}
	return en, fark >= 0
}

// parseKey reads "WxH@mHz" (tazeleme kısmı isteğe bağlı: "1920x1080").
func parseKey(s string) (w, h, mhz int, ok bool) {
	s = strings.TrimSpace(s)
	coz, hz, _ := strings.Cut(s, "@")
	ws, hs, var1 := strings.Cut(coz, "x")
	if !var1 {
		return 0, 0, 0, false
	}
	var err error
	if w, err = strconv.Atoi(ws); err != nil || w <= 0 {
		return 0, 0, 0, false
	}
	if h, err = strconv.Atoi(hs); err != nil || h <= 0 {
		return 0, 0, 0, false
	}
	if hz != "" {
		if mhz, err = strconv.Atoi(hz); err != nil || mhz < 0 {
			return 0, 0, 0, false
		}
	}
	return w, h, mhz, true
}

// ── Bağlayıcı türleri (drm_mode.h DRM_MODE_CONNECTOR_*) ─────────────────────

const (
	connUnknown = 0
	connVirtual = 15
)

// connNames, çekirdeğin kendi adlandırmasıyla aynı (drm_connector.c
// drm_connector_enum_list): kullanıcı "HDMI-A-1" gördüğünde sysfs'te de aynı
// adı bulur.
var connNames = map[uint32]string{
	0: "Unknown", 1: "VGA", 2: "DVI-I", 3: "DVI-D", 4: "DVI-A",
	5: "Composite", 6: "SVIDEO", 7: "LVDS", 8: "Component", 9: "DIN",
	10: "DP", 11: "HDMI-A", 12: "HDMI-B", 13: "TV", 14: "eDP",
	15: "Virtual", 16: "DSI", 17: "DPI", 18: "Writeback", 19: "SPI", 20: "USB",
}

// ConnectorName returns the kernel-style name, e.g. "HDMI-A-1".
func ConnectorName(typ, typeID uint32) string {
	n, ok := connNames[typ]
	if !ok {
		n = fmt.Sprintf("Tip%d", typ)
	}
	return fmt.Sprintf("%s-%d", n, typeID)
}

// physical reports whether the connector drives a real monitor.
//
// Sanal (QEMU/VirtualBox) ve bilinmeyen (simpledrm) bağlayıcılarda EDID'in
// "doğal çözünürlük" diye bir anlamı yok: QEMU'nun sahte EDID'i 1280x800'ü
// tercih edilen işaretleyip 5120x2160'a kadar mod listeliyor (ölçüldü,
// virtio-gpu). Gerçek monitörde ise tercih edilen mod EDID standardı gereği
// panelin DOĞAL çözünürlüğüdür.
func physical(typ uint32) bool {
	return typ != connUnknown && typ != connVirtual
}

// virtualCap, sanal ekranda seçilecek en büyük boyutun alt sınırı.
//
// Sanal ekranda "doğal" çözünürlük yok; tercih edilen mod ana makinedeki
// pencerenin (ya da QEMU'nun varsayılan xres/yres'inin: 1280x800) boyutudur.
// Kullanıcı QEMU'da "çözünürlük hâlâ düşük" dedi, yani tercih edilene
// takılmak yanlış. Ama listenin en büyüğünü almak da yanlış: virtio-gpu'da
// o 5120x2160 (ölçüldü) — 44 MB'lık tampon ve ana makine ekranına sığmayan bir
// pencere. Orta yol: tercih edilen ile 1920x1080'in büyüğüne kadar olan en
// büyük mod.
const virtualCapW, virtualCapH = 1920, 1080

// Choose picks the startup mode for a connector.
//
// ── Politika (sırasıyla) ────────────────────────────────────────────────────
//
//  1. Kullanıcının kaydettiği mod (display.conf) listede varsa o.
//  2. Gerçek monitör (HDMI/DP/DVI/VGA/eDP/LVDS...) ve tercih edilen mod
//     varsa: O ÇÖZÜNÜRLÜKTE EN YÜKSEK TAZELEME. Tercih edilen mod EDID gereği
//     panelin doğal çözünürlüğüdür; ama tazelemesi çoğu zaman 60'tır —
//     144 Hz'lik bir monitör bile uyumluluk için 60'ı tercih eder. Kullanıcı
//     "maks kaç destekliyorsa o" dediği için tazeleme ayrıca en yükseğe
//     çıkarılıyor. Doğaldan BÜYÜK modlar (4K TV'lerdeki 4096x2160 gibi)
//     monitörde ölçeklenir ve bulanıklaşır; seçilmez.
//  3. Sanal/bilinmeyen bağlayıcı: max(tercih edilen, 1920x1080) içine sığan
//     en büyük mod, en yüksek tazelemeyle (bkz. virtualCap).
//  4. Hiçbiri uymazsa: listenin en büyüğü ve en hızlısı.
func Choose(ms []Mode, connType uint32, saved string) (Mode, string) {
	if len(ms) == 0 {
		return Mode{}, ""
	}
	if saved != "" {
		if m, ok := Find(ms, saved); ok {
			return m, "kaydedilen tercih"
		}
	}
	var pref *Mode
	for i := range ms {
		if ms[i].Preferred {
			pref = &ms[i]
			break
		}
	}
	if physical(connType) && pref != nil {
		if m, ok := HighestRefresh(ms, pref.Width, pref.Height); ok {
			return m, "monitörün doğal çözünürlüğü, en yüksek tazeleme"
		}
	}
	if !physical(connType) {
		capW, capH := virtualCapW, virtualCapH
		if pref != nil && pref.Width*pref.Height > capW*capH {
			capW, capH = pref.Width, pref.Height
		}
		var sigan []Mode
		for _, m := range ms {
			if m.Width <= capW && m.Height <= capH {
				sigan = append(sigan, m)
			}
		}
		if b, ok := Best(sigan); ok {
			if m, ok := HighestRefresh(ms, b.Width, b.Height); ok {
				return m, fmt.Sprintf("sanal ekran: %dx%d sınırına sığan en büyük mod", capW, capH)
			}
		}
	}
	b, _ := Best(ms)
	return b, "listenin en büyüğü"
}

// ── Panelin gösterdiği bilgi ────────────────────────────────────────────────

// Info describes the active display path for the panel's Ekran screen and
// the boot log.
//
// Kullanıcı "çözünürlük hâlâ düşük" dediğinde ilk bakılacak yer burası:
// hangi yol seçildi (ekran kartı mı, firmware framebuffer'ı mı), hangi sürücü,
// hangi modlar var ve değiştirilemiyorsa NEDEN.
type Info struct {
	// Backend, "drm" (ekran kartı, KMS) ya da "fbdev" (firmware
	// framebuffer'ı, /dev/fb0).
	Backend string
	// Driver, çekirdek sürücüsünün adı: i915, amdgpu, simpledrm, bochs-drm...
	Driver string
	// Device, açılan aygıt: /dev/dri/card0 ya da /dev/fb0.
	Device string
	// Connector, sürülen çıkış: "HDMI-A-1".
	Connector string
	// ConnType, çıkışın türü (drm_mode.h DRM_MODE_CONNECTOR_*). "Otomatik"
	// seçimi açılıştakiyle AYNI politikayı kullanabilsin diye (sanal ekranda
	// farklı kural var, bkz. Choose).
	ConnType uint32
	// Method, karenin ekrana nasıl gittiği: "sayfa çevirme", "DIRTYFB",
	// "doğrudan yazım", "fbdev (çekirdek ~20 Hz tarar)".
	Method string
	// Current, şu an ekranda olan mod.
	Current Mode
	// Modes, bu çıkışın desteklediği modlar (en iyiden kötüye).
	Modes []Mode
	// Changeable, modun çalışırken değiştirilebilip değiştirilemeyeceği.
	Changeable bool
	// Reason, Changeable=false ise NEDEN (kullanıcıya gösterilir).
	Reason string
	// Why, başlangıç modunun neden seçildiği.
	Why string
	// Outputs, bağlı tüm çıkışların kısa listesi ("HDMI-A-1 1920x1080").
	Outputs []string
	// Frames, bu yolun ekrana gönderdiği kare sayısı (ölçüm için).
	Frames uint64
	// FPS, son bir saniyede GERÇEKTEN ekrana giden kare sayısı. Panel boştayken
	// kare üretmediği için düşük görünmesi normaldir; hareket varken monitörün
	// tazeleme hızına yaklaşmalı.
	FPS float64
	// Lost, aygıt kaybolduysa ve yenisi aranıyorsa true.
	Lost bool
}

// Summary is a one-line description for logs.
func (i Info) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s", i.Backend)
	if i.Driver != "" {
		fmt.Fprintf(&b, " sürücü=%s", i.Driver)
	}
	if i.Device != "" {
		fmt.Fprintf(&b, " aygıt=%s", i.Device)
	}
	if i.Connector != "" {
		fmt.Fprintf(&b, " çıkış=%s", i.Connector)
	}
	if i.Current.Width > 0 {
		fmt.Fprintf(&b, " mod=%s", i.Current)
	}
	if i.Method != "" {
		fmt.Fprintf(&b, " yol=%s", i.Method)
	}
	fmt.Fprintf(&b, " modlar=%d", len(i.Modes))
	if !i.Changeable && i.Reason != "" {
		fmt.Fprintf(&b, " (değiştirilemez: %s)", i.Reason)
	}
	return b.String()
}
