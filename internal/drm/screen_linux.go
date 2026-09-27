//go:build linux

package drm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// ════════════════════════════════════════════════════════════════════════════
// EKRAN: panelin karelerini ekran kartına, monitörün hızında basan katman
// ════════════════════════════════════════════════════════════════════════════
//
// ── Kare nasıl ekrana gidiyor ───────────────────────────────────────────────
//
// İki tampon var. Monitör birini tararken panel ötekine yazar, sonra PAGE_FLIP
// ile "bir sonraki dikey boşlukta ötekine geç" denir. Çekirdek geçiş olunca
// fd'ye bir olay yazar; bir sonraki kareye ancak o olay gelince başlanır.
// Böylece:
//   - yırtılma yok (taranan tampona asla yazılmıyor),
//   - kare hızı MONİTÖRÜN hızı: 60 Hz'de 60, 144 Hz'de 144 kare,
//   - fbdev'in 20 Hz tavanı yok (o yol hiç kullanılmıyor).
//
// Sayfa çevirmeyi desteklemeyen bir sürücüde tek tampona yazılıp DIRTYFB ile
// değişen satırlar bildirilir; o da desteklenmiyorsa sürücü tamponu zaten
// doğrudan tarıyordur.
//
// ── Aygıt kopması ───────────────────────────────────────────────────────────
//
// Gerçek PC'de açılışta simpledrm gelir; i915/amdgpu probe edince onu söker
// ve yeni bir kart açar. Eski karttaki her ioctl ErrLost döner. Ekran o anda
// kartları yeniden tarar, en iyisini açar, modunu kurar ve Size()'ı
// değiştirir; panel bir sonraki karede yeni boyuta göre kendini kurar.

// Screen drives one output (ve aynı çözünürlükteki yansıları).
type Screen struct {
	mu sync.Mutex

	dev     *Device
	format  fbFormat
	prim    output
	clones  []output
	bufs    [2]*buffer
	front   int
	pending int
	// flip: PAGE_FLIP çalışıyor. dirty: DIRTYFB anlamlı (sürücüde karşılığı var).
	flip, dirty  bool
	flipTimeouts int
	dmg          *damage
	blanked      bool

	lost     bool
	lastTry  time.Time
	why      string
	saved    string
	frames   uint64
	stamps   []time.Time
	evbuf    []byte
	switched bool

	// hp: monitör takma/çıkarma dinleyicisi (hotplug_linux.go). sig: son
	// kurulumdaki bağlı çıkışların imzası; olay geldiğinde yalnızca imza
	// değiştiyse ekran yeniden kurulur.
	hp  *hotplugWatch
	sig string
}

// output is one connector + the CRTC driving it.
type output struct {
	conn connector
	crtc uint32
	mode Mode
}

// OpenScreen finds the best display and sets it up.
//
// saved, kullanıcının kaydettiği mod ("2560x1440@143912"); boşsa politika
// (Choose) seçer: monitörün doğal çözünürlüğü, o çözünürlükte EN YÜKSEK
// tazeleme.
func OpenScreen(saved string) (*Screen, error) {
	s := &Screen{saved: saved}
	if err := s.open(); err != nil {
		return nil, err
	}
	s.hp = watchHotplug()
	return s, nil
}

// open scans every card and sets up the first one that drives a monitor.
func (s *Screen) open() error {
	var hatalar []string
	monitorsuz := 0
	var yedek *Device
	var yedekCons []connector
	var yedekCrtcs []uint32
	for _, yol := range Cards() {
		d, err := OpenCard(yol)
		if err != nil {
			hatalar = append(hatalar, fmt.Sprintf("%s: %v", yol, err))
			continue
		}
		if !d.hasDumb() {
			hatalar = append(hatalar, fmt.Sprintf("%s (%s): CPU tamponu desteği yok", yol, d.Driver))
			d.Close()
			continue
		}
		crtcs, conns, err := d.resources()
		if err != nil {
			hatalar = append(hatalar, fmt.Sprintf("%s (%s): %v", yol, d.Driver, err))
			d.Close()
			continue
		}
		var bagli []connector
		for _, id := range conns {
			c, err := d.connectorInfo(id)
			if err == nil && c.Connected && len(c.Modes) > 0 {
				bagli = append(bagli, c)
			}
		}
		if len(bagli) == 0 {
			hatalar = append(hatalar, fmt.Sprintf("%s (%s): bağlı ekran yok", yol, d.Driver))
			monitorsuz++
			d.Close()
			continue
		}
		// simpledrm SON ÇARE: aynı anda gerçek bir sürücünün kartı da
		// varsa (devralma henüz bitmemiş olabilir) gerçeği seç.
		if d.Driver == "simpledrm" && yedek == nil {
			yedek, yedekCons, yedekCrtcs = d, bagli, crtcs
			continue
		}
		if yedek != nil {
			yedek.Close()
		}
		return s.setup(d, bagli, crtcs)
	}
	if yedek != nil {
		return s.setup(yedek, yedekCons, yedekCrtcs)
	}
	if len(hatalar) == 0 {
		return fmt.Errorf("drm: /dev/dri altında ekran kartı yok")
	}
	if monitorsuz > 0 {
		// En az bir kart sağlam ama monitörsüz: monitör takılınca açılır.
		return fmt.Errorf("%w: %v", ErrNoMonitor, hatalar)
	}
	return fmt.Errorf("drm: kullanılabilir ekran yok: %v", hatalar)
}

// setup takes over one card.
func (s *Screen) setup(d *Device, bagli []connector, crtcs []uint32) error {
	if err := d.AcquireMaster(); err != nil {
		d.Close()
		return err
	}
	s.dev = d
	s.format = 0
	s.sig = outputSig(bagli)

	// ── Birincil çıkış ──────────────────────────────────────────────────
	adaylar := make([]outputCand, 0, len(bagli))
	for _, c := range bagli {
		oc := outputCand{conn: c}
		if c.EncoderID != 0 {
			if e, err := d.encoderInfo(c.EncoderID); err == nil && e.CrtcID != 0 {
				oc.activeCrtc = e.CrtcID
				if cr, err := d.getCrtc(e.CrtcID); err == nil && cr.ModeValid != 0 {
					oc.activeArea = int(cr.Mode.Hdisplay) * int(cr.Mode.Vdisplay)
				}
			}
		}
		adaylar = append(adaylar, oc)
	}
	pi := pickPrimary(adaylar)
	kullanilan := map[uint32]bool{}
	pc := adaylar[pi]
	crtc := pc.activeCrtc
	if crtc == 0 {
		crtc = s.freeCrtc(pc.conn, crtcs, kullanilan)
	}
	if crtc == 0 {
		s.close()
		return fmt.Errorf("drm: %s için boş crtc yok", pc.conn.Name())
	}
	kullanilan[crtc] = true
	s.prim = output{conn: pc.conn, crtc: crtc}

	s.flip = true
	s.flipTimeouts = 0
	m, why := Choose(pc.conn.Modes, pc.conn.Type, s.saved)
	if err := s.applyMode(m); err != nil {
		s.close()
		return err
	}
	s.why = why

	// ── Yansılar: aynı çözünürlüğü destekleyen diğer bağlı ekranlar ────
	//
	// fbcon açılışta bütün bağlı ekranları klon kipinde yakar. Panel
	// yalnızca birini alsaydı ötekinde donmuş konsol yazısı kalırdı —
	// dizüstü + harici monitörde "ikinci ekran bozuk" izlenimi. Aynı
	// çözünürlüğü sunan her ekran aynı tamponu tarar ve birlikte çevrilir.
	// Farklı çözünürlükteki ekranlara DOKUNULMUYOR (yarım görüntü
	// göstermektense olduğu gibi kalsın).
	for i, a := range adaylar {
		if i == pi {
			continue
		}
		cm, ok := HighestRefresh(a.conn.Modes, s.prim.mode.Width, s.prim.mode.Height)
		if !ok {
			continue
		}
		cc := a.activeCrtc
		if cc == 0 || kullanilan[cc] {
			cc = s.freeCrtc(a.conn, crtcs, kullanilan)
		}
		if cc == 0 {
			continue
		}
		o := output{conn: a.conn, crtc: cc, mode: cm}
		if err := s.dev.setCrtc(cc, s.bufs[s.front].fb, []uint32{a.conn.ID}, &cm.Raw); err != nil {
			continue
		}
		kullanilan[cc] = true
		s.clones = append(s.clones, o)
	}

	// DIRTYFB'nin karşılığı var mı? Sıfır dikdörtgenle bir kez dene:
	// ENOSYS = sürücü tamponu zaten doğrudan tarıyor, bildirim gereksiz.
	s.dirty = true
	if err := s.dev.dirtyFB(s.bufs[s.front].fb, nil); err != nil {
		s.dirty = false
	}
	return nil
}

// freeCrtc returns a CRTC that can drive c and is not taken.
func (s *Screen) freeCrtc(c connector, crtcs []uint32, taken map[uint32]bool) uint32 {
	for _, eid := range c.Encoders {
		e, err := s.dev.encoderInfo(eid)
		if err != nil {
			continue
		}
		for i, id := range crtcs {
			if e.PossibleCrtcs&(1<<uint(i)) != 0 && !taken[id] {
				return id
			}
		}
	}
	return 0
}

// outputCand is the input of pickPrimary (saf veri: testte sınanıyor).
type outputCand struct {
	conn       connector
	activeCrtc uint32
	activeArea int
}

// pickPrimary chooses which connected output the panel should drive.
//
//  1. Firmware/fbcon'un ŞU AN sürdüğü çıkış: kullanıcının açılışta baktığı
//     ekran odur. Birden çoksa en büyük görüntülü olanı.
//  2. Hiçbiri sürülmüyorsa gerçek bir monitör (HDMI/DP/eDP...), sanal değil.
//  3. Hiçbiri yoksa ilki.
func pickPrimary(c []outputCand) int {
	en, enAlan := -1, -1
	for i, a := range c {
		if a.activeCrtc != 0 && a.activeArea > enAlan {
			en, enAlan = i, a.activeArea
		}
	}
	if en >= 0 {
		return en
	}
	for i, a := range c {
		if physical(a.conn.Type) {
			return i
		}
	}
	return 0
}

// applyMode allocates buffers of the mode's size and programs the CRTC.
//
// Mod reddedilirse (bant genişliği: 4K@144 bir DP 1.2 bağlantısına sığmaz)
// aynı çözünürlükte daha düşük tazelemeler, sonra küçük çözünürlükler
// denenir. Kullanıcı KARA EKRANLA değil, çalışan en iyi modla karşılaşmalı.
func (s *Screen) applyMode(m Mode) error {
	var sonHata error
	for _, aday := range fallbackOrder(m, s.prim.conn.Modes) {
		if err := s.tryMode(aday); err != nil {
			if errors.Is(err, ErrLost) {
				return err
			}
			sonHata = err
			continue
		}
		return nil
	}
	if sonHata == nil {
		sonHata = fmt.Errorf("drm: uygulanacak mod yok")
	}
	return sonHata
}

func (s *Screen) tryMode(m Mode) error {
	s.waitFlips()
	conns := []uint32{s.prim.conn.ID}

	// ── Az video belleğine dayanıklı sıra ───────────────────────────────
	//
	// Ölçüldü (QEMU std VGA, 16 MB video belleği): iki 1920x1080 tampon
	// (2 x 8,3 MB) SIĞMIYOR; ikincisini taramak için sabitlemek ENOMEM
	// döndü ve ekran KARA kaldı. Aynı sınır gerçek sunucularda da var
	// (ASPEED/AST BMC grafikleri, mgag200, VMware). Bu yüzden:
	//   1. Görüntülenmeyen eski tampon ÖNCE bırakılır.
	//   2. Yeni tek tampon kurulur; sığmazsa CRTC bir an kapatılıp (eski
	//      tamponun sabitlemesi kalksın) yeniden denenir.
	//   3. İkinci tampon (sayfa çevirme için) ancak SONRA, yer varsa ayrılır.
	if b := s.bufs[1-s.front]; b != nil {
		s.dev.freeBuffer(b)
		s.bufs[1-s.front] = nil
	}
	eskiOn := s.bufs[s.front]
	nb, err := s.dev.newBuffer(m.Width, m.Height, &s.format)
	if err != nil {
		return err
	}
	err = s.dev.setCrtc(s.prim.crtc, nb.fb, conns, &m.Raw)
	if err != nil && !errors.Is(err, ErrLost) {
		// Eski tampon (ya da açılışta fbcon'unki) hâlâ sabitli olabilir.
		_ = s.dev.setCrtc(s.prim.crtc, 0, nil, nil)
		for _, o := range s.clones {
			_ = s.dev.setCrtc(o.crtc, 0, nil, nil)
		}
		err = s.dev.setCrtc(s.prim.crtc, nb.fb, conns, &m.Raw)
		if err != nil && eskiOn != nil {
			// Yeni mod olmadı: eskisini eski tamponla geri kur.
			_ = s.dev.setCrtc(s.prim.crtc, eskiOn.fb, conns, &s.prim.mode.Raw)
			for _, o := range s.clones {
				_ = s.dev.setCrtc(o.crtc, eskiOn.fb, []uint32{o.conn.ID}, &o.mode.Raw)
			}
		}
	}
	if err != nil {
		s.dev.freeBuffer(nb)
		return fmt.Errorf("drm: %s uygulanamadı: %w", m, err)
	}

	// Yansılar yeni çözünürlükte de aynısını sunmalı; sunmayanı kapat (eski
	// tampona bakmaya devam edemez — o tampon siliniyor).
	kalan := s.clones[:0]
	for _, o := range s.clones {
		cm, ok := HighestRefresh(o.conn.Modes, m.Width, m.Height)
		if ok && s.dev.setCrtc(o.crtc, nb.fb, []uint32{o.conn.ID}, &cm.Raw) == nil {
			o.mode = cm
			kalan = append(kalan, o)
			continue
		}
		_ = s.dev.setCrtc(o.crtc, 0, nil, nil)
	}
	s.clones = kalan
	if eskiOn != nil {
		s.dev.freeBuffer(eskiOn)
	}
	s.bufs = [2]*buffer{nb, nil}
	s.front = 0
	s.pending = 0
	s.prim.mode = m
	s.dmg = newDamage(m.Width, m.Height)
	if s.flip {
		if b2, err := s.dev.newBuffer(m.Width, m.Height, &s.format); err == nil {
			s.bufs[1] = b2
		} else {
			s.flip = false
		}
	}
	return nil
}

// fallbackOrder lists modes to try: the wanted one first.
func fallbackOrder(m Mode, all []Mode) []Mode {
	out := []Mode{m}
	for _, x := range all { // aynı çözünürlük, azalan tazeleme
		if x.Width == m.Width && x.Height == m.Height && x.Key() != m.Key() && x.MilliHz < m.MilliHz {
			out = append(out, x)
		}
	}
	for _, x := range all { // küçük çözünürlükler (liste zaten iyiden kötüye)
		if x.Width*x.Height < m.Width*m.Height {
			out = append(out, x)
		}
	}
	return out
}

// ── Kare gönderme ───────────────────────────────────────────────────────────

// Present puts img on the screen.
//
// Aygıt kaybolduysa HATA DÖNMEZ: panel çalışmaya devam eder, ekran
// arka planda yeni kartı arar. Hata dönseydi panel döngüsü kapanırdı ve
// kullanıcı sürücü devralmasının ortasında konsola düşerdi.
func (s *Screen) Present(img *image.RGBA) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lost && !s.reopenLocked() {
		return nil
	}
	if s.blanked || s.bufs[s.front] == nil {
		return nil
	}
	s.dmg.observe(img)
	var err error
	if s.flip {
		err = s.presentFlip(img)
	} else {
		err = s.presentSingle(img)
	}
	if errors.Is(err, ErrLost) {
		s.markLost()
		return nil
	}
	return err
}

func (s *Screen) presentFlip(img *image.RGBA) error {
	if err := s.waitFlips(); err != nil {
		return err
	}
	back := 1 - s.front
	b := s.bufs[back]
	spans := s.dmg.take(back)
	if len(spans) == 0 {
		return nil // arka tampon zaten güncel, çevrilecek yeni bir şey yok
	}
	for _, sp := range spans {
		copyRows(b.mem, int(b.pitch), b.w, b.h, img, sp[0], sp[1], b.direct)
	}
	err := s.dev.pageFlip(s.prim.crtc, b.fb, 0)
	if errors.Is(err, unix.EBUSY) {
		// Bir önceki çevirme hâlâ uçuşta (olay kaçtıysa): bekle, bir kez daha.
		s.pending = 1
		s.waitFlips()
		err = s.dev.pageFlip(s.prim.crtc, b.fb, 0)
	}
	if err != nil {
		if errors.Is(err, ErrLost) {
			return err
		}
		// ── Sayfa çevirme olmadı: ZATEN EKRANDA OLAN tampona geç ─────────
		//
		// Yakalanan gerçek hata (QEMU std VGA'da ölçüldü): burada arka
		// tamponu SETCRTC ile taratmaya çalışılıyordu. Çevirmenin başarısız
		// olma sebebi çoğu zaman video belleğinin dolu olmasıdır (ENOMEM) ve
		// aynı tamponu sabitlemek aynı sebeple başarısız oluyordu; her kare
		// görünmeyen tampona yazılıyor, ekran KARA kalıyordu.
		//
		// Ön tampon zaten sabitli ve taranıyor; onun "henüz yazılmadı"
		// kümesi de eksiksiz (arka tampona yazmak onu temizlemedi). Arka
		// tampon bırakılır — belleği geri verilir — ve tek tampona geçilir.
		s.flip = false
		s.dev.freeBuffer(b)
		s.bufs[back] = nil
		return s.presentSingle(img)
	}
	s.pending = 1
	kalan := s.clones[:0]
	for i, o := range s.clones {
		if e := s.dev.pageFlip(o.crtc, b.fb, uint64(i+1)); e != nil {
			// Yansı çevrilemiyorsa bırak; birincil ekranı yavaşlatmasın.
			_ = s.dev.setCrtc(o.crtc, 0, nil, nil)
			continue
		}
		s.pending++
		kalan = append(kalan, o)
	}
	s.clones = kalan
	s.front = back
	s.count()
	return nil
}

func (s *Screen) presentSingle(img *image.RGBA) error {
	b := s.bufs[s.front]
	spans := s.dmg.take(s.front)
	s.dmg.take(1 - s.front) // kullanılmayan tamponun kümesi birikmesin
	if len(spans) == 0 {
		return nil
	}
	for _, sp := range spans {
		copyRows(b.mem, int(b.pitch), b.w, b.h, img, sp[0], sp[1], b.direct)
	}
	if s.dirty {
		clips := make([]clipRect, 0, len(spans))
		for _, sp := range spans {
			clips = append(clips, clipRect{X1: 0, Y1: uint16(sp[0]), X2: uint16(b.w), Y2: uint16(sp[1])})
		}
		if err := s.dev.dirtyFB(b.fb, clips); err != nil {
			if errors.Is(err, ErrLost) {
				return err
			}
			s.dirty = false
		}
	}
	s.count()
	return nil
}

// waitFlips blocks until every queued flip has completed.
//
// Bekleme bir kareden uzun sürerse (olay hiç gelmiyorsa) sonsuza dek
// beklenmez: sayaç sıfırlanır. Üç kez üst üste olursa sayfa çevirme bu
// sürücüde güvenilmez sayılır ve tek tampona geçilir.
func (s *Screen) waitFlips() error {
	if s.pending <= 0 || s.dev == nil {
		s.pending = 0
		return nil
	}
	sonra := time.Now().Add(120 * time.Millisecond)
	if s.evbuf == nil {
		s.evbuf = make([]byte, 1024)
	}
	for s.pending > 0 {
		kalan := time.Until(sonra)
		if kalan <= 0 {
			s.pending = 0
			s.flipTimeouts++
			if s.flipTimeouts >= 3 {
				s.flip = false
			}
			return nil
		}
		fds := []unix.PollFd{{Fd: int32(s.dev.Fd()), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, int(kalan/time.Millisecond)+1)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		if fds[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return ErrLost
		}
		r, err := unix.Read(s.dev.Fd(), s.evbuf)
		if err == unix.EINTR || err == unix.EAGAIN {
			continue
		}
		if err == unix.ENODEV {
			return ErrLost
		}
		if err != nil {
			return err
		}
		s.pending -= countFlipEvents(s.evbuf[:r])
	}
	s.flipTimeouts = 0
	return nil
}

// countFlipEvents parses a read() of DRM events.
//
//	struct drm_event { __u32 type; __u32 length; }  + gövde
//	DRM_EVENT_FLIP_COMPLETE = 0x02
func countFlipEvents(b []byte) int {
	n := 0
	for len(b) >= 8 {
		typ := binary.LittleEndian.Uint32(b[0:4])
		ln := int(binary.LittleEndian.Uint32(b[4:8]))
		if ln < 8 || ln > len(b) {
			break
		}
		if typ == 0x02 {
			n++
		}
		b = b[ln:]
	}
	return n
}

func (s *Screen) count() {
	s.frames++
	now := time.Now()
	s.stamps = append(s.stamps, now)
	// Son bir saniyenin damgaları: ölçülen kare hızı Ekran bölümünde görünür.
	i := 0
	for i < len(s.stamps) && now.Sub(s.stamps[i]) > time.Second {
		i++
	}
	s.stamps = s.stamps[i:]
}

// ── Aygıt kopması ───────────────────────────────────────────────────────────

func (s *Screen) markLost() {
	s.lost = true
	s.pending = 0
}

// reopenLocked tries to switch to whatever card is there now.
func (s *Screen) reopenLocked() bool {
	if time.Since(s.lastTry) < 500*time.Millisecond {
		return false
	}
	s.lastTry = time.Now()
	eskiDev, eskiBufs := s.dev, s.bufs
	istenen := s.saved
	if s.prim.mode.Width > 0 && istenen == "" {
		istenen = s.prim.mode.Key()
	}
	yeni := &Screen{saved: istenen}
	if err := yeni.open(); err != nil {
		return false
	}
	// Eski aygıtın tamponları: yalnızca eşlemeleri kaldır (ioctl'ler zaten
	// ErrLost döner), sonra fd'yi kapat. Hotplug yolunda eski aygıt zaten
	// kapatılmış olabilir (dev nil).
	if eskiDev != nil {
		for _, b := range eskiBufs {
			if b != nil {
				eskiDev.freeBuffer(b)
			}
		}
		eskiDev.Close()
	}
	s.adopt(yeni)
	return true
}

// adopt takes over a freshly opened screen's state (dinleyici ve kayıtlı
// tercih bizde kalır).
func (s *Screen) adopt(yeni *Screen) {
	s.dev, s.format, s.prim, s.clones = yeni.dev, yeni.format, yeni.prim, yeni.clones
	s.bufs, s.front, s.pending = yeni.bufs, yeni.front, 0
	s.flip, s.dirty, s.flipTimeouts = yeni.flip, yeni.dirty, 0
	s.dmg, s.why, s.sig = yeni.dmg, yeni.why, yeni.sig
	s.blanked, s.lost, s.switched = false, false, true
}

// hotplugLocked re-reads the connected outputs after a hotplug event and
// rebuilds the display if the set of monitors really changed.
func (s *Screen) hotplugLocked() {
	bagli, err := s.dev.connectedList()
	if err != nil {
		if errors.Is(err, ErrLost) {
			s.markLost()
		}
		return
	}
	sig := outputSig(bagli)
	if sig == s.sig {
		return // HPD oynadı ama aynı monitörler: dokunma (titreme olmasın)
	}
	if len(bagli) == 0 {
		// Son monitör de çıkarıldı. Düzeni koru: kurulacak ekran yok.
		// İmza "boş" olarak kaydedilir; monitör geri takılınca imza yine
		// değişir ve o zaman yeniden kurulur (DP bağlantısı yeniden
		// eğitilmeli, eski CRTC ayarı kendiliğinden geri gelmez).
		s.sig = sig
		return
	}
	// Baştan kur. Önce ESKİYİ bırak: master bizde kaldıkça aynı kartı
	// yeniden açan open() SET_MASTER alamaz. Mod seçimi KAYITLI tercihle
	// yapılır, şu anki modla değil: yeni monitörün doğal çözünürlüğü ve en
	// yüksek tazelemesi eski monitörünkinden farklı olabilir.
	s.close()
	s.clones = nil
	yeni := &Screen{saved: s.saved}
	if err := yeni.open(); err != nil {
		// Açılamadıysa aygıt kaybı gibi davran: Check her yarım saniyede
		// bir yeniden dener (reopenLocked).
		s.prim.mode = Mode{}
		s.markLost()
		return
	}
	s.adopt(yeni)
	s.why = "monitör değişti; " + yeni.why
}

// Check is called periodically by the panel (saniyede bir).
//
// Panel boştayken Present çağrılmaz; sürücü devralması tam o sırada olursa
// kimse fark etmezdi ve ekran yeni sürücünün konsolunu gösterirdi. Burada
// ucuz bir sorguyla aygıtın hâlâ yerinde olduğuna bakılıyor. Boyut ya da
// aygıt değiştiyse true döner; panel yeniden kurulup tam kare çizmeli.
func (s *Screen) Check() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.lost && s.dev != nil {
		if _, err := s.dev.getCrtc(s.prim.crtc); errors.Is(err, ErrLost) {
			s.markLost()
		}
	}
	if s.lost {
		s.reopenLocked()
	}
	if !s.lost && s.dev != nil && s.hp.take() {
		if s.blanked {
			// Ekran uykudayken kurmak monitörü uyandırırdı; uyanınca bakılır.
			s.hp.again()
		} else {
			s.hotplugLocked()
		}
	}
	if s.switched {
		s.switched = false
		return true
	}
	return false
}

// ── Mod değişimi ────────────────────────────────────────────────────────────

// SetMode switches to the mode with the given key without restarting.
//
// Başarılıysa Size() yeni boyutu döner; çağıran tuvali yeniden kurmalı.
// Onay/geri alma (15 sn) arayüzün işi: yanlış mod seçilirse ekran kararabilir
// ve kullanıcı göremez — arayüz süre dolunca eski anahtarla yeniden çağırır.
func (s *Screen) SetMode(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lost || s.dev == nil {
		return fmt.Errorf("drm: ekran şu an hazır değil")
	}
	m, ok := Find(s.prim.conn.Modes, key)
	if !ok {
		return fmt.Errorf("drm: %s bu ekranda yok", key)
	}
	if err := s.tryMode(m); err != nil {
		if errors.Is(err, ErrLost) {
			s.markLost()
		}
		return err
	}
	s.why = "kullanıcının seçimi"
	return nil
}

// SetSaved records the preference used when the device is reopened.
func (s *Screen) SetSaved(key string) {
	s.mu.Lock()
	s.saved = key
	s.mu.Unlock()
}

// ── Sorgular ────────────────────────────────────────────────────────────────

// Size returns the current mode's size.
func (s *Screen) Size() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prim.mode.Width, s.prim.mode.Height
}

// FramePeriod is one refresh interval of the current mode.
func (s *Screen) FramePeriod() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prim.mode.MilliHz <= 0 {
		return 16 * time.Millisecond
	}
	return time.Duration(int64(time.Second) * 1000 / int64(s.prim.mode.MilliHz))
}

// Info describes the display path for the panel and the log.
func (s *Screen) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := Info{Backend: "drm", Current: s.prim.mode, Why: s.why, Frames: s.frames, Lost: s.lost}
	if s.dev != nil {
		i.Driver, i.Device = s.dev.Driver, s.dev.Path
	}
	i.Connector = s.prim.conn.Name()
	i.ConnType = s.prim.conn.Type
	i.Modes = append([]Mode(nil), s.prim.conn.Modes...)
	switch {
	case s.flip:
		i.Method = "sayfa çevirme (dikey boşluğa kilitli)"
	case s.dirty:
		i.Method = "tek tampon + DIRTYFB"
	default:
		i.Method = "tek tampon (doğrudan tarama)"
	}
	i.Outputs = append(i.Outputs, fmt.Sprintf("%s %dx%d", i.Connector, s.prim.mode.Width, s.prim.mode.Height))
	for _, o := range s.clones {
		i.Outputs = append(i.Outputs, fmt.Sprintf("%s %dx%d (yansı)", o.conn.Name(), o.mode.Width, o.mode.Height))
	}
	if len(s.stamps) > 1 {
		d := s.stamps[len(s.stamps)-1].Sub(s.stamps[0])
		if d > 0 {
			i.FPS = float64(len(s.stamps)-1) / d.Seconds()
		}
	}
	i.Changeable = len(i.Modes) > 1
	if !i.Changeable {
		if i.Driver == "simpledrm" {
			i.Reason = "ekran kartı sürücüsü yüklenmedi; firmware'in açtığı tek mod kullanılıyor (simpledrm)"
		} else {
			i.Reason = "sürücü bu ekran için tek mod bildiriyor"
		}
	}
	return i
}

// ── Uyku ────────────────────────────────────────────────────────────────────

// Blank turns the outputs off/on (CRTC kapatılınca monitör bekleme kipine geçer).
func (s *Screen) Blank(off bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dev == nil || s.lost {
		return false
	}
	s.waitFlips()
	if off {
		_ = s.dev.setCrtc(s.prim.crtc, 0, nil, nil)
		for _, o := range s.clones {
			_ = s.dev.setCrtc(o.crtc, 0, nil, nil)
		}
		s.blanked = true
		return true
	}
	fb := s.bufs[s.front].fb
	_ = s.dev.setCrtc(s.prim.crtc, fb, []uint32{s.prim.conn.ID}, &s.prim.mode.Raw)
	for _, o := range s.clones {
		_ = s.dev.setCrtc(o.crtc, fb, []uint32{o.conn.ID}, &o.mode.Raw)
	}
	s.blanked = false
	return true
}

// Close releases everything; fbcon kendi modunu geri yükler.
func (s *Screen) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hp.close()
	s.hp = nil
	s.close()
}

func (s *Screen) close() {
	if s.dev == nil {
		return
	}
	s.waitFlips()
	for i, b := range s.bufs {
		s.dev.freeBuffer(b)
		s.bufs[i] = nil
	}
	s.dev.ReleaseMaster()
	s.dev.Close()
	s.dev = nil
}
