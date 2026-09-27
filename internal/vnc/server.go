// Package vnc serves the MCOS screen over RFB (VNC) so the panel can be used
// from RealVNC Viewer, TigerVNC, Remmina or any other standard client.
//
// ════════════════════════════════════════════════════════════════════════════
// KULLANICININ İSTEĞİ
// ════════════════════════════════════════════════════════════════════════════
//
//	"realvnc ile bağlanma bu da olsun"
//
// ════════════════════════════════════════════════════════════════════════════
// TASARIM KARARLARI
// ════════════════════════════════════════════════════════════════════════════
//
//  1. EKRANIN KENDİSİ YAYINLANIR, AYRI BİR ARAYÜZ DEĞİL.
//     Sunucu /dev/fb0'ı okur. Yani VNC'den görülen şey, monitörde görülenin
//     TA KENDİSİDİR: açılış animasyonu, kurulum sihirbazı, kilit ekranı,
//     kapanış animasyonu — hepsi. Paralel bir "uzak arayüz" yazmak, iki
//     arayüzün zamanla ayrışması demekti.
//
//  2. GİRDİ uinput İLE VERİLİR.
//     Gelen tuş/fare olayları sanal bir aygıt üzerinden çekirdeğe yazılır;
//     panel onları kendi evdev okuyucusuyla, yerel klavyeyle AYNI yoldan
//     alır. Paneli özel bir soketle beslemek, "VNC'de çalışıyor ama
//     klavyede çalışmıyor" türü hatalar üretirdi.
//
//  3. KİMLİK DOĞRULAMA ZORUNLU.
//     VNC Authentication (RFB güvenlik türü 2) uygulanıyor. Parolasız
//     ("None") bağlantı HİÇ SUNULMUYOR: ekranı paylaşan bir port, panelin
//     tam denetimi demektir. Parola yoksa sunucu AÇILMAZ.
//
//  4. VARSAYILAN KAPALI.
//     Uzaktan kontrol köprüsüyle aynı kural: kullanıcı açıkça açana kadar
//     hiçbir port dinlenmez.
//
// ════════════════════════════════════════════════════════════════════════════
// NEDEN HAZIR BİR VNC SUNUCUSU DEĞİL
// ════════════════════════════════════════════════════════════════════════════
//
// x11vnc X sunucusu ister (MCOS'ta X yok). directvnc/fbvnc gibi çerçeve
// arabelleği sunucuları Buildroot'ta ya yok ya da bakımsız ve C ile yazılmış;
// imaja libvncserver çekmek hem yer hem de saldırı yüzeyi demekti.
//
// RFB'nin sunucu tarafı, ihtiyacımız olan kadarıyla küçüktür: el sıkışma,
// DES ile parola doğrulama ve dikdörtgen güncellemeleri. Tamamı saf Go,
// CGO_ENABLED=0 ile derlenen ikiliye giriyor.
package vnc

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// DefaultPort is where the VNC server listens.
//
// 5900 = VNC ekran :0. RealVNC Viewer'a yalnızca IP yazmak yeterli olsun diye
// standart porttan sapılmadı; kullanıcı "192.168.1.20" yazıp bağlanabilmeli.
const DefaultPort = 5900

// Framebuffer is the screen source. internal/fbdev implements it.
type Framebuffer interface {
	// Size returns width and height in pixels.
	Size() (int, int)
	// Snapshot copies the current screen into dst (RGBA, 4 bytes/pixel).
	// Returns an error if the screen could not be read.
	Snapshot(dst []byte) error
}

// Injector delivers keyboard and pointer events to the system.
type Injector interface {
	// Key presses (down=true) or releases a key, given an X11 keysym.
	Key(keysym uint32, down bool) error
	// Pointer moves the cursor to x,y with the given button mask.
	Pointer(x, y int, buttons uint8) error
	// Close releases the virtual device.
	Close() error
}

// Logger is the subset of the daemon logger this package needs.
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
}

// Options configure the server.
type Options struct {
	Addr     string // ":5900"
	Password string // VNC parolası (1-8 karakter kullanılır)
	Name     string // istemcide görünen masaüstü adı
	FB       Framebuffer
	Input    Injector // nil ise salt-izleme
	Log      Logger
	// MaxClients caps concurrent viewers.
	//
	// Her istemci kendi kare kopyasını tutuyor (1080p'de 8,3 MB) ve kendi
	// karşılaştırmasını yapıyor. Sınırsız bırakmak, bir sunucu makinesinde
	// belleği ve CPU'yu bir anda tüketebilirdi.
	MaxClients int
}

// Server is an RFB 3.8 server over TCP.
type Server struct {
	opt Options

	mu sync.Mutex
	// ln, dinleyici. KİLİT ALTINDA: Serve() onu bir goroutine'de kuruyor,
	// Addr()/Close() ise başka goroutine'lerden okuyor.
	//
	// Yarış dedektörü bunu yakaladı ve gerçek bir hataydı: daemon,
	// sunucuyu bir goroutine'de başlatıp hemen ardından durumunu
	// sorabiliyor (panel "açık mı?" diye yokluyor). Kilitsiz okuma, yarım
	// yazılmış bir arayüz değeri görmek demektir.
	ln      net.Listener
	clients int
	// w, h are the screen size, read once at New.
	//
	// Her istemci KENDİ karşılaştırma tamponunu tutuyor (bkz. session).
	// Ortak bir "son kare" tutmak daha ucuz görünür ama iki istemci farklı
	// hızlarda güncelleme isteyebiliyor; ortak tampon, yavaş istemcinin
	// hızlı istemcinin karesiyle karşılaştırılması demekti — sonuç eksik
	// güncelleme, yani ekranda kalıcı artıklar.
	w, h int
}

// New validates options and builds the server.
func New(o Options) (*Server, error) {
	if o.FB == nil {
		return nil, errors.New("vnc: çerçeve arabelleği yok")
	}
	if strings.TrimSpace(o.Password) == "" {
		// Uzaktan kontrol köprüsüyle AYNI kural: parolasız açılan bir
		// dinleyici, ağdaki herkese ekranı ve klavyeyi verirdi.
		return nil, errors.New("vnc: parola yok — ekran paylaşımı açılamaz")
	}
	if o.Addr == "" {
		o.Addr = fmt.Sprintf(":%d", DefaultPort)
	}
	if o.Name == "" {
		o.Name = "MCOS"
	}
	if o.MaxClients <= 0 {
		o.MaxClients = 4
	}
	w, h := o.FB.Size()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("vnc: geçersiz ekran boyutu %dx%d", w, h)
	}
	return &Server{opt: o, w: w, h: h}, nil
}

// Addr returns the bound address (valid after Serve starts).
func (s *Server) Addr() string {
	s.mu.Lock()
	ln := s.ln
	s.mu.Unlock()
	if ln == nil {
		return s.opt.Addr
	}
	return ln.Addr().String()
}

// Serve accepts clients until the listener is closed.
func (s *Server) Serve() error {
	ln, err := net.Listen("tcp", s.opt.Addr)
	if err != nil {
		return fmt.Errorf("vnc portu açılamadı (%s): %w", s.opt.Addr, err)
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	if s.opt.Log != nil {
		s.opt.Log.Infof("vnc: ekran paylaşımı açık (%s), %dx%d",
			ln.Addr(), s.w, s.h)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			return nil // Close() çağrıldı
		}
		s.mu.Lock()
		over := s.clients >= s.opt.MaxClients
		if !over {
			s.clients++
		}
		s.mu.Unlock()

		if over {
			// Sessizce kapatmak yerine sebebi söylüyoruz: RFB el sıkışması
			// başlamadan kapanan bir bağlantı, istemcide "sunucu yok" diye
			// görünür ve kullanıcı ağı suçlar.
			_ = reject(conn, "MCOS: en fazla istemci sayısına ulaşıldı")
			continue
		}

		go func() {
			defer func() {
				s.mu.Lock()
				s.clients--
				s.mu.Unlock()
				conn.Close()
				// Panik bir istemciyi düşürür, DAEMON'U DEĞİL: ekran
				// paylaşımı yüzünden sunucu yönetiminin durması kabul
				// edilemez.
				if r := recover(); r != nil && s.opt.Log != nil {
					s.opt.Log.Warnf("vnc: istemci hatası: %v", r)
				}
			}()
			if err := s.handle(conn); err != nil && s.opt.Log != nil {
				s.opt.Log.Warnf("vnc: %s bağlantısı kapandı: %v",
					conn.RemoteAddr(), err)
			}
		}()
	}
}

// Close stops the listener.
func (s *Server) Close() error {
	s.mu.Lock()
	ln := s.ln
	s.ln = nil
	s.mu.Unlock()
	if ln != nil {
		return ln.Close()
	}
	return nil
}

// Clients reports how many viewers are connected.
func (s *Server) Clients() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clients
}

// reject sends an RFB "connection failed" message before hanging up.
func reject(conn net.Conn, reason string) error {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte(rfbVersion)); err != nil {
		return err
	}
	buf := make([]byte, 12)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return err
	}
	// Güvenlik türü sayısı 0 = başarısız, ardından sebep.
	out := []byte{0}
	out = binary.BigEndian.AppendUint32(out, uint32(len(reason)))
	out = append(out, reason...)
	_, err := conn.Write(out)
	return err
}

// ── RFB protokolü ───────────────────────────────────────────────────────────

const rfbVersion = "RFB 003.008\n"

const (
	secTypeVNCAuth = 2

	msgSetPixelFormat           = 0
	msgSetEncodings             = 2
	msgFramebufferUpdateRequest = 3
	msgKeyEvent                 = 4
	msgPointerEvent             = 5
	msgClientCutText            = 6

	encRaw       = 0
	encCopyRect  = 1
	encDesktopSz = -223
)

// handle runs one client session.
func (s *Server) handle(conn net.Conn) error {
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	br := bufio.NewReaderSize(conn, 4096)
	bw := bufio.NewWriterSize(conn, 1<<16)

	// ── 1. Sürüm ────────────────────────────────────────────────────────
	if _, err := bw.WriteString(rfbVersion); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	ver := make([]byte, 12)
	if _, err := io.ReadFull(br, ver); err != nil {
		return err
	}
	// RFB 3.3 ve 3.7 istemcileri de 3.8 akışıyla konuşabilir; sürüm
	// pazarlığında ısrar etmek eski istemcileri dışarıda bırakırdı.

	// ── 2. Güvenlik ─────────────────────────────────────────────────────
	// YALNIZCA VNC Authentication sunuluyor. "None" listeye HİÇ konmuyor:
	// istemci onu seçebilseydi parola devre dışı kalırdı.
	if _, err := bw.Write([]byte{1, secTypeVNCAuth}); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	choice := make([]byte, 1)
	if _, err := io.ReadFull(br, choice); err != nil {
		return err
	}
	if choice[0] != secTypeVNCAuth {
		return fmt.Errorf("istemci desteklenmeyen güvenlik türü seçti: %d", choice[0])
	}
	if err := s.vncAuth(br, bw); err != nil {
		return err
	}

	// ── 3. Başlatma ─────────────────────────────────────────────────────
	shared := make([]byte, 1)
	if _, err := io.ReadFull(br, shared); err != nil {
		return err
	}
	if err := s.writeServerInit(bw); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return err
	}

	if s.opt.Log != nil {
		s.opt.Log.Infof("vnc: %s bağlandı", conn.RemoteAddr())
	}

	return s.session(conn, br, bw)
}

// vncAuth runs the DES challenge-response handshake.
func (s *Server) vncAuth(br *bufio.Reader, bw *bufio.Writer) error {
	challenge := make([]byte, 16)
	if _, err := rand.Read(challenge); err != nil {
		return err
	}
	if _, err := bw.Write(challenge); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return err
	}

	got := make([]byte, 16)
	if _, err := io.ReadFull(br, got); err != nil {
		return err
	}
	want := vncEncrypt(challenge, s.opt.Password)

	// Sabit süreli karşılaştırma: erken çıkan bir karşılaştırma, doğru
	// önekin uzunluğunu zamanlamadan sızdırır.
	if subtle.ConstantTimeCompare(got, want) != 1 {
		// Başarısızlık: durum 1 + sebep. Sebebi GENEL tutuyoruz.
		out := []byte{0, 0, 0, 1}
		reason := "parola yanlış"
		out = binary.BigEndian.AppendUint32(out, uint32(len(reason)))
		out = append(out, reason...)
		_, _ = bw.Write(out)
		_ = bw.Flush()
		if s.opt.Log != nil {
			s.opt.Log.Warnf("vnc: yanlış parolayla bağlantı denemesi")
		}
		// Yavaşlatma: kaba kuvvet denemesini pahalı yapar. RFB'de
		// bağlantı başına tek deneme hakkı var, yani bu gecikme meşru
		// kullanıcıyı yalnızca bir kez yanlış yazdığında etkiler.
		time.Sleep(2 * time.Second)
		return errors.New("kimlik doğrulama başarısız")
	}

	if _, err := bw.Write([]byte{0, 0, 0, 0}); err != nil { // OK
		return err
	}
	return bw.Flush()
}

// writeServerInit sends the framebuffer geometry and pixel format.
func (s *Server) writeServerInit(bw *bufio.Writer) error {
	var b []byte
	b = binary.BigEndian.AppendUint16(b, uint16(s.w))
	b = binary.BigEndian.AppendUint16(b, uint16(s.h))

	// Piksel biçimi: 32 bit, true colour, BGRA sırası.
	//
	// Çerçeve arabelleği Go'nun image.RGBA düzenindedir (R,G,B,A) ve
	// little-endian bir makinede 32 bitlik bir piksel olarak okunduğunda
	// 0xAABBGGRR olur. RFB'ye big-endian-flag=0 diyerek aynı baytları
	// olduğu gibi gönderiyoruz; kaydırmalar buna göre: mavi 16, yeşil 8,
	// kırmızı 0. Böylece piksel başına hiçbir dönüşüm yapılmıyor —
	// 1080p'de kare başına 2 milyon dönüşüm demek olurdu.
	b = append(b,
		32,     // bits-per-pixel
		24,     // depth
		0,      // big-endian-flag
		1,      // true-colour-flag
		0, 255, // red-max
		0, 255, // green-max
		0, 255, // blue-max
		0,       // red-shift
		8,       // green-shift
		16,      // blue-shift
		0, 0, 0, // padding
	)
	b = binary.BigEndian.AppendUint32(b, uint32(len(s.opt.Name)))
	b = append(b, s.opt.Name...)
	_, err := bw.Write(b)
	return err
}
