//go:build linux

package fbinput

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Bu dosya /dev/input/event* aygıtlarını okur ve İKİ iş yapar:
//
//  1. UYKU KİPİ: "kullanıcı orada mı?" — herhangi bir girdi uyandırır.
//  2. İMLEÇ: fare ve touchpad hareketini panele iletir.
//
// ── Neden tek okuyucu? ──────────────────────────────────────────────────────
// Önce iki ayrı izleyici vardı (biri uyku, biri imleç için) ve ikisi de aynı
// /dev/input/event* dosyalarını açıyordu. Bir evdev aygıtını iki kez açmak
// hata vermez ama olaylar İKİYE BÖLÜNMEZ — her okuyucu kendi kuyruğunu alır,
// yani çekirdek olayı iki kez kopyalar. Bu doğru çalışır ama gereksizdir ve
// (daha kötüsü) bazı aygıtlarda ikinci açış EACCES döner. Tek okuyucu hem
// kesin hem ucuz.
//
// Klavye için ayrı bir okuyucuya gerek yok: konsol zaten açık ve tuşa
// basıldığında tty'den bayt gelir. Ama FARE hareketi tty'ye hiçbir şey
// göndermez — bu yüzden evdev şart.

// inputEventSize is sizeof(struct input_event) on linux/amd64.
//
//	struct input_event {
//	    struct timeval time;  // 2 × __kernel_long_t = 16 bayt
//	    __u16 type;           // 2
//	    __u16 code;           // 2
//	    __s32 value;          // 4
//	};
//
// 64-bit x86'da toplam 24 bayt. Bu sayı MİMARİYE BAĞLIDIR (32-bit'te 16'dır);
// bu dosya yalnızca amd64 hedefinde derlenen imajda kullanılıyor.
const inputEventSize = 24

// Linux girdi olay türleri (linux/input-event-codes.h).
// evRel/evAbs evdev_linux.go içinde tanımlı.
const (
	evSyn = 0x00
	evKey = 0x01
)

// Button identifies a pointer button.
type Button int

const (
	// ButtonNone: bu olay bir düğme olayı değil.
	ButtonNone Button = iota
	// ButtonLeft: birincil tıklama (seç / çalıştır).
	ButtonLeft
	// ButtonRight: ikincil tıklama (geri / iptal).
	ButtonRight
	// ButtonMiddle: orta düğme.
	ButtonMiddle
)

// PointerEvent is one decoded pointer change.
//
// DX/DY ve AbsX/AbsY PİKSEL cinsindendir. Dönüşüm burada yapılır çünkü
// touchpad'in ham birimleri aygıta özgüdür (kimi 0..1400, kimi 0..5000) ve
// paneli bu ayrıntıyla kirletmenin anlamı yok. Panel ekran boyutunu
// SetScreen ile bildirir.
type PointerEvent struct {
	// Kind is the device class that produced the event.
	Kind Kind
	// DX, DY is relative motion in pixels.
	DX, DY int
	// AbsX, AbsY is an absolute position in pixels (touchscreens).
	AbsX, AbsY int
	// HasAbs reports whether AbsX/AbsY are meaningful.
	HasAbs bool
	// Wheel is +1 per notch up, -1 per notch down.
	Wheel int
	// Button is the button this event concerns (ButtonNone if motion only).
	Button Button
	// Press/Release report a button state change.
	Press, Release bool
}

// pointerQueue is how many pointer events are buffered.
//
// Fare hızlı hareket ettiğinde saniyede ~1000 olay gelir; panel bunları
// birleştirerek tüketir. Kuyruk dolduğunda EN ESKİ olay düşürülür (aşağıda),
// çünkü imleçte önemli olan SON konumdur — eski bir hareketi beklemek
// imleci geciktirirdi.
const pointerQueue = 256

// Activity reports user input activity and pointer motion from evdev devices.
type Activity struct {
	// mu, aygıt listesini korur: yeniden tarama ve kopan aygıtın okuyucusu
	// onu ayrı goroutine'lerden değiştiriyor.
	mu     sync.Mutex
	files  []*os.File
	kinds  []Kind
	names  []string
	paths  map[string]bool
	last   atomic.Int64 // son etkinliğin UnixNano değeri
	closed atomic.Bool
	wake   chan struct{}
	ptr    chan PointerEvent
	media  chan string
	stop   chan struct{}
	once   sync.Once
	// onNew, yeniden taramada bulunan her yeni aygıt için çağrılır (günlük).
	onNew func(name string, k Kind)

	// Ekran boyutu: touchpad ve dokunmatik ekran ham değerlerini piksele
	// çevirmek için gerekir. Atomik, çünkü okuyucu goroutine'lerden okunur.
	scrW, scrH atomic.Int64
}

// WatchActivity opens every /dev/input/event* and watches for activity.
//
// Hiç aygıt bulunamazsa hata DÖNMEZ: klavyesiz/faresiz bir makinede panel yine
// çalışmalı, yalnızca fareyle uyandırma özelliği olmaz. Çağıran Devices() ile
// kaç aygıt izlendiğini öğrenebilir.
func WatchActivity() *Activity {
	a := &Activity{
		wake:  make(chan struct{}, 1),
		ptr:   make(chan PointerEvent, pointerQueue),
		media: make(chan string, 16),
		stop:  make(chan struct{}),
		paths: map[string]bool{},
	}
	a.last.Store(time.Now().UnixNano())
	// Panel SetScreen çağırana kadar makul bir varsayılan: 1080p. Yanlış
	// olsa bile imleç çalışır, yalnızca touchpad hassasiyeti bir miktar
	// kayar.
	a.scrW.Store(1920)
	a.scrH.Store(1080)

	a.scan()
	go a.rescanLoop()
	return a
}

// rescanInterval, yeni girdi aygıtlarının ne sıklıkla arandığı.
//
// ── Yakalanan gerçek eksik ──────────────────────────────────────────────────
//
// Aygıtlar yalnızca panel açılırken BİR KEZ taranıyordu. Oysa:
//   - I2C-HID touchpad'ler çekirdek açılışının GEÇ bir aşamasında belirir
//     (pinctrl/GPIO sürücüsü hazır olunca, ertelenmiş probe ile) — panel o
//     sırada çoktan açılmış olabilir. Kullanıcının "gerçek PC'lerde touchpad
//     yok" şikâyetinin kullanıcı alanındaki payı budur.
//   - Sonradan takılan USB fare ya da klavye hiç görülmüyordu.
//
// 2 saniye: takılan farenin "çalışmıyor" sanılmayacağı kadar kısa, boşta
// neredeyse maliyetsiz (tek bir dizin listesi).
const rescanInterval = 2 * time.Second

func (a *Activity) rescanLoop() {
	t := time.NewTicker(rescanInterval)
	defer t.Stop()
	for {
		select {
		case <-a.stop:
			return
		case <-t.C:
			a.scan()
		}
	}
}

// scan opens every /dev/input/event* that is not open yet.
func (a *Activity) scan() {
	paths, _ := filepath.Glob("/dev/input/event*")
	for _, p := range paths {
		a.mu.Lock()
		acik := a.paths[p]
		a.mu.Unlock()
		if acik || a.closed.Load() {
			continue
		}
		f, err := os.OpenFile(p, os.O_RDONLY, 0)
		if err != nil {
			// İzin yoksa veya aygıt kaybolduysa sessizce atla: tek bir
			// aygıtın açılmaması tüm izlemeyi düşürmemeli.
			continue
		}
		c := probe(f)
		a.mu.Lock()
		a.paths[p] = true
		a.files = append(a.files, f)
		a.kinds = append(a.kinds, c.Kind)
		a.names = append(a.names, c.Name)
		cb := a.onNew
		a.mu.Unlock()
		if cb != nil {
			cb(c.Name, c.Kind)
		}
		go a.read(p, f, c)
	}
}

// OnNewDevice registers a callback for devices found after startup.
func (a *Activity) OnNewDevice(fn func(name string, k Kind)) {
	a.mu.Lock()
	a.onNew = fn
	a.mu.Unlock()
}

// forget removes a device whose reader stopped (USB çıkarıldı).
func (a *Activity) forget(path string, f *os.File) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.paths, path)
	for i := range a.files {
		if a.files[i] == f {
			a.files = append(a.files[:i], a.files[i+1:]...)
			a.kinds = append(a.kinds[:i], a.kinds[i+1:]...)
			a.names = append(a.names[:i], a.names[i+1:]...)
			break
		}
	}
	_ = f.Close()
}

// MediaKeys yields laptop media key presses: "volumeup", "volumedown", "mute".
//
// Bu tuşlar (Fn+F3 vb., Fn kilidi kapalıyken çoğu dizüstüde doğrudan F3)
// TTY'ye HİÇBİR karakter göndermez; yalnızca evdev'de görünürler. Panel
// bunları diğer tuşlarla aynı yoldan işler (fbpanel.volumeKey).
func (a *Activity) MediaKeys() <-chan string { return a.media }

// Devices returns how many input devices are being watched.
func (a *Activity) Devices() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.files)
}

// Pointers returns a description of every pointer device found.
//
// Ayarlar ekranı bunu gösterir: kullanıcı "fare desteği açık ama çalışmıyor"
// dediğinde ilk soru "sistem fareyi görüyor mu?" olur. Liste boşsa yanıt
// hemen bellidir.
func (a *Activity) Pointers() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for i, k := range a.kinds {
		if k == KindOther {
			continue
		}
		n := a.names[i]
		if n == "" {
			n = filepath.Base(a.files[i].Name())
		}
		out = append(out, n+" ("+k.String()+")")
	}
	return out
}

// HasPointer reports whether any mouse/touchpad/touchscreen was found.
func (a *Activity) HasPointer() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, k := range a.kinds {
		if k != KindOther {
			return true
		}
	}
	return false
}

// SetScreen tells the watcher the framebuffer size so absolute devices can be
// mapped to pixels.
func (a *Activity) SetScreen(w, h int) {
	if w > 0 && h > 0 {
		a.scrW.Store(int64(w))
		a.scrH.Store(int64(h))
	}
}

// Pointer returns the pointer event channel.
func (a *Activity) Pointer() <-chan PointerEvent { return a.ptr }

// read consumes events from one device.
func (a *Activity) read(path string, f *os.File, c caps) {
	buf := make([]byte, inputEventSize*32)
	dec := newPointerDecoder(a, c)
	for {
		n, err := f.Read(buf)
		if err != nil {
			// Aygıt kayboldu (USB çıkarıldı) veya kapatıldı. Listeden düşür
			// ki aynı düğüm yeniden belirirse (tekrar takılırsa) açılsın.
			if !a.closed.Load() {
				a.forget(path, f)
			}
			return
		}
		if a.closed.Load() {
			return
		}
		for off := 0; off+inputEventSize <= n; off += inputEventSize {
			typ := binary.LittleEndian.Uint16(buf[off+16:])
			code := binary.LittleEndian.Uint16(buf[off+18:])
			val := int32(binary.LittleEndian.Uint32(buf[off+20:]))

			// evSyn yalnızca paket sınırıdır, etkinlik değildir; onu
			// saymak her aygıtın sürekli "etkin" görünmesine yol açardı.
			// Ama imleç için ÖNEMLİDİR: bir paketin bittiğini söyler ve
			// biriken hareket o anda gönderilir.
			if typ != evSyn {
				a.mark()
			}
			if typ == evKey {
				if ad, ok := mediaKey(code, val); ok {
					select {
					case a.media <- ad:
					default: // panel yetişemiyorsa fazla basış düşer
					}
				}
			}
			if dec != nil {
				dec.feed(typ, code, val)
			}
		}
	}
}

func (a *Activity) mark() {
	a.last.Store(time.Now().UnixNano())
	// Bloklamayan bildirim: kanal doluysa zaten uyandırma bekliyordur.
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// emit queues a pointer event, dropping the OLDEST if the queue is full.
//
// İmleçte önemli olan SON konumdur. Kuyruk dolduğunda yeni olayı atmak
// imleci dondurur; eskisini atmak yalnızca ara kareyi kaybettirir.
func (a *Activity) emit(e PointerEvent) {
	for {
		select {
		case a.ptr <- e:
			return
		default:
		}
		select {
		case <-a.ptr: // en eskiyi at, sonra tekrar dene
		default:
			return // başka biri boşalttı; kaybetmemek için vazgeç
		}
	}
}

// Wake returns a channel that receives on user activity.
func (a *Activity) Wake() <-chan struct{} { return a.wake }

// Idle returns how long there has been no input activity.
func (a *Activity) Idle() time.Duration {
	return time.Since(time.Unix(0, a.last.Load()))
}

// Touch records activity from another source (e.g. a key read from the tty).
//
// Klavye tty üzerinden okunduğu için evdev okuyucusu onu görmeyebilir; panel
// her tuşta bunu çağırarak boşta kalma sayacını sıfırlar.
func (a *Activity) Touch() { a.mark() }

// Close stops watching.
func (a *Activity) Close() error {
	a.once.Do(func() {
		a.closed.Store(true)
		close(a.stop)
		a.mu.Lock()
		for _, f := range a.files {
			_ = f.Close()
		}
		a.mu.Unlock()
	})
	return nil
}

// Medya tuşu kodları (linux/input-event-codes.h).
const (
	keyMute       = 113
	keyVolumeDown = 114
	keyVolumeUp   = 115
)

// mediaKey maps an EV_KEY event to a panel key name.
//
// val: 1 = basış, 2 = basılı tutma tekrarı, 0 = bırakma. Ses aç/kıs basılı
// tutulunca TEKRARLAMALI (seviye akarak değişsin); sessiz ise yalnızca
// basışta — tekrarında her 30 ms'de bir aç/kapa yapardı.
func mediaKey(code uint16, val int32) (string, bool) {
	switch code {
	case keyVolumeUp:
		return "volumeup", val == 1 || val == 2
	case keyVolumeDown:
		return "volumedown", val == 1 || val == 2
	case keyMute:
		return "mute", val == 1
	}
	return "", false
}

// ── İmleç çözücü ────────────────────────────────────────────────────────────

// tapMaxDuration is how long a touchpad tap may last to count as a click.
//
// 250 ms: insan "dokunuşu" tipik olarak 60-150 ms sürer. Daha uzun tutmak,
// kısa bir sürükleme başlangıcını yanlışlıkla tıklama yapardı.
const tapMaxDuration = 250 * time.Millisecond

// tapMaxTravel is how far the finger may move (in pad fraction) and still tap.
//
// Yüzde 4: parmak dokunurken hep biraz kayar. Daha dar bir eşik, gerçek
// dokunuşların çoğunu reddeder.
const tapMaxTravel = 0.04

// scrollPixelsPerNotch is how much two-finger travel makes one wheel notch.
const scrollPixelsPerNotch = 40.0

// pointerDecoder turns one device's raw events into PointerEvents.
type pointerDecoder struct {
	a    *Activity
	c    caps
	kind Kind

	// Biriken hareket: evSyn gelene kadar bekletilir, böylece tek bir
	// paketten tek bir olay çıkar.
	dx, dy float64
	wheel  int

	// Mutlak aygıtlar için son konum (ham birim).
	//
	// ── Düzeltilen gerçek hata: EKSENLER AYRI İZLENMELİ ─────────────────
	//
	// Burada tek bir haveLast bayrağı vardı ve hareket YALNIZCA hem X hem
	// Y aynı pakette geldiğinde hesaplanıyordu. Ama çekirdek DEĞİŞMEYEN
	// bir ekseni GÖNDERMEZ (drivers/input/input.c: aynı değer bastırılır).
	//
	// Sonuç ölçüldü (ELAN1200 aralıklarıyla, 1920x1080):
	//
	//	saf YATAY sürükleme, 30 kare  -> 0 olay, 0 piksel
	//	saf DİKEY sürükleme,  30 kare -> 0 olay, 0 piksel
	//	çapraz (iki eksen de değişir) -> 29 olay, çalışıyor
	//	karışık (X her kare, Y her 4.) -> paketlerin %77,5'i DÜŞÜYOR
	//
	// Yani touchpad "bazen çalışmıyor" değil, DÜZ hareket hiç çalışmıyordu
	// ve eğik hareketin dörtte üçü yutuluyordu. Kullanıcının "takılıyor"
	// dediği hissin girdi tarafındaki payı budur.
	//
	// Artık her eksen kendi referansını tutuyor: gelmeyen eksenin farkı
	// sıfır sayılıyor, gelen eksen normal işleniyor.
	// İKİ AYRI KAVRAM, bilerek ayrıldı:
	//
	//	curX/curY + haveX/haveY : o eksenin BİLİNEN son konumu. Bir kez
	//	                          geldikten sonra hep geçerlidir.
	//	lastX/lastY + refX/refY : touchpad farkının ölçüldüğü REFERANS.
	//	                          Yeni dokunuşta sıfırlanır (sıçrama olmasın).
	//
	// Bunları tek bayrakta birleştirmek, dokunmatik ekranda ilk dokunuşun
	// yutulmasına yol açıyordu.
	curX, curY   int32
	haveX, haveY bool
	lastX, lastY int32
	refX, refY   bool
	gotX, gotY   bool

	// Çoklu dokunmada YALNIZCA ilk parmak (slot 0) imleci sürer. İkinci
	// parmağın olaylarını hareket sanmak, iki parmakla kaydırırken imlecin
	// zıplamasına yol açardı.
	slot int32

	touching bool
	// btnDuringTouch: bu temas sirasinda fiziksel bir dugmeye basildi.
	btnDuringTouch bool
	twoFing        bool
	touchAt        time.Time
	travel         float64
	scrollAc       float64

	pending []PointerEvent
}

// newPointerDecoder returns a decoder, or nil for non-pointer devices.
func newPointerDecoder(a *Activity, c caps) *pointerDecoder {
	if c.Kind == KindOther {
		return nil
	}
	d := &pointerDecoder{a: a, c: c, kind: c.Kind}
	if c.Hover {
		// Mutlak işaretçinin BAŞLANGIÇ konumu çekirdekte zaten var
		// (EVIOCGABS "value"). Onu bilinen konum saymazsak ilk hareket
		// kaybolabilir: çekirdek DEĞİŞMEYEN ekseni göndermez, VNC
		// istemcisi imleci önce yalnızca dikey oynatırsa X hiç gelmez ve
		// haveX yanlış kalır -> imleç yerinde sayar.
		d.curX, d.haveX = c.RangeX.Value, true
		d.curY, d.haveY = c.RangeY.Value, true
	}
	return d
}

// feed consumes one raw event.
func (d *pointerDecoder) feed(typ, code uint16, val int32) {
	switch typ {
	case evRel:
		switch code {
		case relX:
			d.dx += float64(val)
		case relY:
			d.dy += float64(val)
		case relWheel:
			d.wheel += int(val)
		case relHWhl:
			// Yatay tekerlek panelde kullanılmıyor; yoksayılır.
		}

	case evAbs:
		switch code {
		case absMTSlot:
			d.slot = val
		case absX, absMTPosX:
			if d.slot == 0 {
				d.curX, d.gotX, d.haveX = val, true, true
			}
		case absY, absMTPosY:
			if d.slot == 0 {
				d.curY, d.gotY, d.haveY = val, true, true
			}
		}

	case evKey:
		switch code {
		case btnTouch:
			d.onTouch(val != 0)
		case btnToolDoubl:
			d.twoFing = val != 0
			// İkinci parmak indiğinde/kalktığında konum referansı geçersiz:
			// sürücü ilk parmağın konumunu yeniden bildirmeden hareket
			// hesaplamak sıçramaya yol açar.
			d.refX, d.refY = false, false
		case btnToolTripl:
			d.refX, d.refY = false, false
		case btnLeft:
			d.button(ButtonLeft, val != 0)
		case btnRight:
			d.button(ButtonRight, val != 0)
		case btnMiddle:
			d.button(ButtonMiddle, val != 0)
		}

	case evSyn:
		d.flush()
	}
}

// onTouch handles finger down/up on an absolute device.
func (d *pointerDecoder) onTouch(down bool) {
	if down {
		d.touching = true
		d.touchAt = time.Now()
		d.travel = 0
		d.scrollAc = 0
		// Bu temas sirasinda FIZIKSEL bir dugmeye basildi mi? Bkz. asagisi.
		d.btnDuringTouch = false
		// YENİ dokunuşta referansı sıfırla: parmak pad'in başka bir
		// köşesine indiğinde eski konumdan fark almak imleci fırlatırdı.
		// BİLİNEN konum (haveX/haveY) korunur; bozulan yalnızca referanstır.
		d.refX, d.refY = false, false
		return
	}
	wasTouching := d.touching
	d.touching = false
	d.refX, d.refY = false, false
	if !wasTouching {
		return
	}

	// -- Yakalanan gercek hata (1): clickpad cift tikliyordu -------------
	// Dizustu clickpad'lerinde pedin KENDISI dugmedir. Tek bir fiziksel
	// tik su olaylari uretir: BTN_TOUCH=1 ... BTN_LEFT=1 ... BTN_LEFT=0 ...
	// BTN_TOUCH=0. Gercek dugme zaten bir Press+Release kuyruga aliyordu;
	// sonra buradaki dokunarak-tiklama mantigi temas kisa surdugu icin
	// IKINCI bir Press+Release daha ekliyordu. Panel ayni noktada dort olay
	// gorup bunu CIFT TIK sayiyordu: tek tiklama iki kez calisiyordu --
	// listelerde bir satir secilip aninda calistiriliyordu.
	//
	// libinput da ayni seyi yapar: fiziksel dugme kullanildiysa
	// dokunarak-tiklama bastirilir.
	if d.btnDuringTouch {
		return
	}

	// -- Yakalanan gercek hata (2): dokunmatik ekran hic tiklayamiyordu --
	// Denetim yalnizca KindTouchpad'e izin veriyordu. Oysa dokunmatik bir
	// EKRAN (KindTouchscreen) BTN_LEFT HIC uretmez -- tek gonderdigi
	// EV_KEY kodu BTN_TOUCH'tir. Yani ekrana dokunmak imleci oraya
	// tasiyor ama HICBIR SEYI acamiyordu: dokunmatik ekranli bir makinede
	// panel tamamen kullanilamazdi.
	if d.kind != KindTouchpad && d.kind != KindTouchscreen {
		return
	}
	// Dokunarak tıklama: kısa süre + az hareket.
	if time.Since(d.touchAt) > tapMaxDuration || d.travel > tapMaxTravel {
		return
	}
	btn := ButtonLeft
	if d.twoFing {
		btn = ButtonRight // iki parmakla dokunmak = sağ tık
	}
	d.pending = append(d.pending,
		PointerEvent{Kind: d.kind, Button: btn, Press: true},
		PointerEvent{Kind: d.kind, Button: btn, Release: true})
}

// button queues a physical button change.
func (d *pointerDecoder) button(b Button, pressed bool) {
	e := PointerEvent{Kind: d.kind, Button: b}
	if pressed {
		// Temas suresince fiziksel dugme kullanildigini isaretle ki
		// parmak kalkinca UZERINE bir de sahte tik eklenmesin.
		d.btnDuringTouch = true
		e.Press = true
	} else {
		e.Release = true
	}
	d.pending = append(d.pending, e)
}

// flush converts the accumulated packet into at most one motion event plus
// any queued button events.
func (d *pointerDecoder) flush() {
	defer func() { d.pending = d.pending[:0] }()

	scrW := float64(d.a.scrW.Load())
	scrH := float64(d.a.scrH.Load())

	switch d.kind {
	case KindTouchscreen:
		// Dokunmatik ekranda parmağın DEĞDİĞİ nokta imlecin gittiği yerdir.
		//
		// İKİ eksen de en az bir kez görülmüş olmalı, ama AYNI pakette
		// gelmeleri gerekmez: değişmeyen eksenin son değeri hâlâ geçerlidir.
		//
		// Hover aygıtında (VNC, sanal tablet) temas YOKTUR: bildirilen her
		// konum imlecin yeridir. Temas şartı burada VNC faresini tamamen
		// öldürüyordu (bkz. absPointerKind).
		if (d.gotX || d.gotY) && d.haveX && d.haveY && (d.touching || d.c.Hover) {
			x, okX := mapAxis(d.curX, d.c.RangeX, scrW)
			y, okY := mapAxis(d.curY, d.c.RangeY, scrH)
			if okX && okY {
				d.a.emit(PointerEvent{Kind: d.kind, HasAbs: true,
					AbsX: int(math.Round(x)), AbsY: int(math.Round(y))})
			}
		}
		// Tekerlek: mutlak işaretçiler (VNC) tekerleği REL_WHEEL olarak
		// bildirir. Bu dal eskiden emitMotion'ı hiç çağırmıyordu, yani
		// biriken tekerlek adımı sessizce kayboluyordu. dx/dy burada hep
		// sıfır; gönderilen yalnızca tekerlektir.
		d.emitMotion()

	case KindTouchpad:
		if (d.gotX || d.gotY) && d.touching {
			// Gelmeyen eksenin farkı SIFIR: çekirdek onu göndermediyse o
			// eksende hareket YOKTUR (bkz. haveX/haveY yorumu).
			if d.refX || d.refY {
				var rdx, rdy float64
				if d.gotX && d.refX {
					rdx = float64(d.curX - d.lastX)
				}
				if d.gotY && d.refY {
					rdy = float64(d.curY - d.lastY)
				}
				spanX := float64(d.c.RangeX.Max - d.c.RangeX.Min)
				spanY := float64(d.c.RangeY.Max - d.c.RangeY.Min)
				if spanX > 0 && spanY > 0 {
					// Pad'in tamamını katetmek ekranın tamamını katetsin:
					// dizüstü touchpad'lerinde beklenen his budur.
					fx := rdx / spanX
					fy := rdy / spanY
					d.travel += math.Abs(fx) + math.Abs(fy)
					if d.twoFing {
						// İki parmak = kaydırma.
						d.scrollAc += fy * scrH
						for d.scrollAc >= scrollPixelsPerNotch {
							d.scrollAc -= scrollPixelsPerNotch
							d.wheel--
						}
						for d.scrollAc <= -scrollPixelsPerNotch {
							d.scrollAc += scrollPixelsPerNotch
							d.wheel++
						}
					} else {
						d.dx += fx * scrW
						d.dy += fy * scrH
					}
				}
			}
			// Referans YALNIZCA gelen eksende güncellenir: gelmeyen
			// eksenin son değeri hâlâ geçerli.
			if d.gotX {
				d.lastX, d.refX = d.curX, true
			}
			if d.gotY {
				d.lastY, d.refY = d.curY, true
			}
		}
		d.emitMotion()

	default: // KindMouse
		d.emitMotion()
	}

	d.gotX, d.gotY = false, false
	for _, e := range d.pending {
		d.a.emit(e)
	}
}

// emitMotion sends the accumulated relative motion, if any.
func (d *pointerDecoder) emitMotion() {
	idx, idy := int(math.Round(d.dx)), int(math.Round(d.dy))
	// Yuvarlamada kaybolan kesri SAKLA: yavaş touchpad hareketinde her
	// paket 0.4 piksel taşır; atarsak imleç hiç kıpırdamaz.
	d.dx -= float64(idx)
	d.dy -= float64(idy)

	if idx == 0 && idy == 0 && d.wheel == 0 {
		return
	}
	d.a.emit(PointerEvent{Kind: d.kind, DX: idx, DY: idy, Wheel: d.wheel})
	d.wheel = 0
}

// mapAxis converts a raw absolute value to a pixel coordinate.
func mapAxis(v int32, r absInfo, size float64) (float64, bool) {
	span := float64(r.Max - r.Min)
	if span <= 0 {
		return 0, false
	}
	f := (float64(v) - float64(r.Min)) / span
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return f * (size - 1), true
}
