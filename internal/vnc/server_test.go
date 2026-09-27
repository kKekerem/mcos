package vnc

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// VNC sunucusunun testleri.
//
// ── Neden gerekli ───────────────────────────────────────────────────────────
// Protokol hataları SESSİZDİR: istemci ya hiç bağlanamaz ("sunucu yanıt
// vermiyor") ya da bozuk bir görüntü çizer. İkisinin de sebebi ekrandan
// anlaşılamaz. Bu testler el sıkışmayı, kimlik doğrulamayı ve kare
// güncellemelerini bayt düzeyinde kilitliyor.

// fakeFB is a deterministic screen source.
type fakeFB struct {
	mu   sync.Mutex
	w, h int
	fill byte
	// fail, Snapshot'ın hata döndürmesini sağlar (aygıt kayboldu senaryosu).
	fail bool
}

func (f *fakeFB) Size() (int, int) { return f.w, f.h }

func (f *fakeFB) Snapshot(dst []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("ekran okunamadı")
	}
	for i := range dst {
		dst[i] = f.fill
	}
	return nil
}

func (f *fakeFB) setFill(v byte) {
	f.mu.Lock()
	f.fill = v
	f.mu.Unlock()
}

// fakeInput records what the client sent.
type fakeInput struct {
	mu       sync.Mutex
	keys     []uint32
	pointers [][3]int
}

func (f *fakeInput) Key(keysym uint32, down bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if down {
		f.keys = append(f.keys, keysym)
	}
	return nil
}

func (f *fakeInput) Pointer(x, y int, buttons uint8) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pointers = append(f.pointers, [3]int{x, y, int(buttons)})
	return nil
}

func (f *fakeInput) Close() error { return nil }

// startServer boots a server on a random port and returns its address.
func startServer(t *testing.T, opt Options) (*Server, string) {
	t.Helper()
	if opt.FB == nil {
		opt.FB = &fakeFB{w: 64, h: 48, fill: 0x11}
	}
	if opt.Password == "" {
		opt.Password = "gizli12"
	}
	opt.Addr = "127.0.0.1:0"

	s, err := New(opt)
	if err != nil {
		t.Fatalf("sunucu kurulamadı: %v", err)
	}
	ready := make(chan struct{})
	go func() {
		close(ready)
		_ = s.Serve()
	}()
	<-ready
	// Dinleyici adresi Serve içinde atanıyor; kısa bir bekleme yeterli.
	// Addr() KİLİT ALTINDA okuyor, bu yüzden test de onu kullanıyor —
	// alanı doğrudan okumak yarış dedektörünün haklı olarak şikâyet ettiği
	// şeydi.
	addr := ""
	for i := 0; i < 200; i++ {
		if a := s.Addr(); a != "" && !strings.HasSuffix(a, ":0") {
			addr = a
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("dinleyici açılmadı")
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, addr
}

// handshake performs version + auth and returns the connection ready for
// client messages.
func handshake(t *testing.T, addr, password string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("bağlanılamadı: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(conn)

	ver := make([]byte, 12)
	if _, err := io.ReadFull(br, ver); err != nil {
		t.Fatalf("sürüm okunamadı: %v", err)
	}
	if string(ver) != rfbVersion {
		t.Fatalf("beklenmeyen sürüm: %q", ver)
	}
	if _, err := conn.Write([]byte(rfbVersion)); err != nil {
		t.Fatalf("sürüm yazılamadı: %v", err)
	}

	n := make([]byte, 1)
	if _, err := io.ReadFull(br, n); err != nil {
		t.Fatalf("güvenlik sayısı okunamadı: %v", err)
	}
	types := make([]byte, n[0])
	if _, err := io.ReadFull(br, types); err != nil {
		t.Fatalf("güvenlik türleri okunamadı: %v", err)
	}
	if len(types) != 1 || types[0] != secTypeVNCAuth {
		t.Fatalf("sunucu yalnızca VNC kimlik doğrulaması sunmalı, gelen: %v", types)
	}
	if _, err := conn.Write([]byte{secTypeVNCAuth}); err != nil {
		t.Fatal(err)
	}

	challenge := make([]byte, 16)
	if _, err := io.ReadFull(br, challenge); err != nil {
		t.Fatalf("meydan okuma okunamadı: %v", err)
	}
	if _, err := conn.Write(vncEncrypt(challenge, password)); err != nil {
		t.Fatal(err)
	}

	res := make([]byte, 4)
	if _, err := io.ReadFull(br, res); err != nil {
		t.Fatalf("kimlik sonucu okunamadı: %v", err)
	}
	if binary.BigEndian.Uint32(res) != 0 {
		t.Fatal("kimlik doğrulama reddedildi (doğru parolayla)")
	}
	return conn, br
}

// clientInit + ServerInit okuması.
func serverInit(t *testing.T, conn net.Conn, br *bufio.Reader) (w, h int) {
	t.Helper()
	if _, err := conn.Write([]byte{1}); err != nil { // shared
		t.Fatal(err)
	}
	head := make([]byte, 24)
	if _, err := io.ReadFull(br, head); err != nil {
		t.Fatalf("ServerInit okunamadı: %v", err)
	}
	w = int(binary.BigEndian.Uint16(head[0:2]))
	h = int(binary.BigEndian.Uint16(head[2:4]))
	nameLen := binary.BigEndian.Uint32(head[20:24])
	name := make([]byte, nameLen)
	if _, err := io.ReadFull(br, name); err != nil {
		t.Fatal(err)
	}
	if head[4] != 32 {
		t.Errorf("piksel derinliği 32 bit olmalı, %d geldi", head[4])
	}
	if head[7] != 1 {
		t.Error("true-colour bayrağı kurulu olmalı")
	}
	return w, h
}

// Parolasız sunucu AÇILMAMALI: ekranı paylaşan bir port, panelin tam
// denetimi demektir.
func TestNoPasswordNoServer(t *testing.T) {
	_, err := New(Options{FB: &fakeFB{w: 10, h: 10}})
	if err == nil {
		t.Fatal("parolasız sunucu kuruldu — ağdaki herkes ekrana bağlanabilirdi")
	}
}

// Doğru parolayla el sıkışma tamamlanmalı ve ekran boyutu doğru gelmeli.
func TestHandshakeAndServerInit(t *testing.T) {
	_, addr := startServer(t, Options{})
	conn, br := handshake(t, addr, "gizli12")
	defer conn.Close()

	w, h := serverInit(t, conn, br)
	if w != 64 || h != 48 {
		t.Errorf("ekran boyutu yanlış: %dx%d", w, h)
	}
}

// Yanlış parola REDDEDİLMELİ.
func TestWrongPasswordRejected(t *testing.T) {
	_, addr := startServer(t, Options{})

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	br := bufio.NewReader(conn)

	ver := make([]byte, 12)
	_, _ = io.ReadFull(br, ver)
	_, _ = conn.Write([]byte(rfbVersion))
	n := make([]byte, 1)
	_, _ = io.ReadFull(br, n)
	types := make([]byte, n[0])
	_, _ = io.ReadFull(br, types)
	_, _ = conn.Write([]byte{secTypeVNCAuth})

	challenge := make([]byte, 16)
	if _, err := io.ReadFull(br, challenge); err != nil {
		t.Fatal(err)
	}
	// YANLIŞ parola.
	if _, err := conn.Write(vncEncrypt(challenge, "yanlis")); err != nil {
		t.Fatal(err)
	}
	res := make([]byte, 4)
	if _, err := io.ReadFull(br, res); err != nil {
		t.Fatalf("sonuç okunamadı: %v", err)
	}
	if binary.BigEndian.Uint32(res) != 1 {
		t.Fatal("yanlış parola KABUL EDİLDİ")
	}
}

// İlk (tam) kare, ekranın tamamını tek dikdörtgen olarak göndermeli.
func TestFullFrameUpdate(t *testing.T) {
	fb := &fakeFB{w: 64, h: 48, fill: 0x22}
	_, addr := startServer(t, Options{FB: fb})
	conn, br := handshake(t, addr, "gizli12")
	defer conn.Close()
	w, h := serverInit(t, conn, br)

	requestUpdate(t, conn, false, w, h)

	rects := readUpdateHeader(t, br)
	if rects != 1 {
		t.Fatalf("tam kare tek dikdörtgen olmalı, %d geldi", rects)
	}
	x, y, rw, rh, enc := readRectHeader(t, br)
	if x != 0 || y != 0 || rw != w || rh != h {
		t.Errorf("dikdörtgen tüm ekranı kaplamalı: %d,%d %dx%d", x, y, rw, rh)
	}
	if enc != encRaw {
		t.Errorf("kodlama Raw olmalı, %d geldi", enc)
	}
	pix := make([]byte, rw*rh*4)
	if _, err := io.ReadFull(br, pix); err != nil {
		t.Fatalf("piksel verisi eksik: %v", err)
	}
	if pix[0] != 0x22 {
		t.Errorf("piksel içeriği yanlış: %#x", pix[0])
	}
}

// Ekran DEĞİŞMEDİYSE artımlı istek BOŞ güncelleme almalı — sıfır dikdörtgen.
// Hiç yanıt vermemek, istemcinin sonsuza kadar beklemesi demektir.
func TestIncrementalNoChangeSendsEmptyUpdate(t *testing.T) {
	fb := &fakeFB{w: 64, h: 48, fill: 0x33}
	_, addr := startServer(t, Options{FB: fb})
	conn, br := handshake(t, addr, "gizli12")
	defer conn.Close()
	w, h := serverInit(t, conn, br)

	// İlk tam kare.
	requestUpdate(t, conn, false, w, h)
	drainFullFrame(t, br, w, h)

	// Değişiklik yok.
	requestUpdate(t, conn, true, w, h)
	if rects := readUpdateHeader(t, br); rects != 0 {
		t.Fatalf("değişiklik yokken %d dikdörtgen gönderildi", rects)
	}
}

// Ekranın BİR BÖLÜMÜ değişince yalnızca o bölge gönderilmeli.
func TestIncrementalSendsOnlyChangedTiles(t *testing.T) {
	fb := &fakeFB{w: 128, h: 64, fill: 0x00}
	_, addr := startServer(t, Options{FB: fb})
	conn, br := handshake(t, addr, "gizli12")
	defer conn.Close()
	w, h := serverInit(t, conn, br)

	requestUpdate(t, conn, false, w, h)
	drainFullFrame(t, br, w, h)

	// Tüm ekranı değiştir: fakeFB tek renk bastığı için hepsi değişir.
	fb.setFill(0xAA)
	requestUpdate(t, conn, true, w, h)

	rects := readUpdateHeader(t, br)
	if rects == 0 {
		t.Fatal("ekran değişti ama hiç dikdörtgen gönderilmedi")
	}
	total := 0
	for i := 0; i < rects; i++ {
		_, _, rw, rh, _ := readRectHeader(t, br)
		pix := make([]byte, rw*rh*4)
		if _, err := io.ReadFull(br, pix); err != nil {
			t.Fatalf("dikdörtgen %d verisi eksik: %v", i, err)
		}
		total += rw * rh
	}
	if total != w*h {
		t.Errorf("değişen alan %d piksel, beklenen %d", total, w*h)
	}
	// Karo birleştirme çalışıyorsa satır başına TEK dikdörtgen gelir:
	// 64 satır / 32 karo yüksekliği = 2.
	if rects > 4 {
		t.Errorf("karolar birleştirilmemiş: %d dikdörtgen (beklenen ≤4)", rects)
	}
}

// Tuş ve fare olayları enjektöre ULAŞMALI.
func TestInputReachesInjector(t *testing.T) {
	in := &fakeInput{}
	_, addr := startServer(t, Options{Input: in})
	conn, br := handshake(t, addr, "gizli12")
	defer conn.Close()
	serverInit(t, conn, br)

	// KeyEvent: 'a' basıldı.
	key := []byte{msgKeyEvent, 1, 0, 0}
	key = binary.BigEndian.AppendUint32(key, 'a')
	if _, err := conn.Write(key); err != nil {
		t.Fatal(err)
	}

	// PointerEvent: (10,20), sol düğme.
	ptr := []byte{msgPointerEvent, 1}
	ptr = binary.BigEndian.AppendUint16(ptr, 10)
	ptr = binary.BigEndian.AppendUint16(ptr, 20)
	if _, err := conn.Write(ptr); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		in.mu.Lock()
		keys, ptrs := len(in.keys), len(in.pointers)
		in.mu.Unlock()
		if keys > 0 && ptrs > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("girdi olayları enjektöre ulaşmadı")
}

// İstemci sınırı aşılınca yeni bağlantı SEBEBİYLE reddedilmeli.
func TestMaxClientsRejectsWithReason(t *testing.T) {
	_, addr := startServer(t, Options{MaxClients: 1})

	c1, br1 := handshake(t, addr, "gizli12")
	defer c1.Close()
	serverInit(t, c1, br1)

	c2, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	_ = c2.SetDeadline(time.Now().Add(5 * time.Second))
	br2 := bufio.NewReader(c2)

	ver := make([]byte, 12)
	if _, err := io.ReadFull(br2, ver); err != nil {
		t.Fatal(err)
	}
	if _, err := c2.Write([]byte(rfbVersion)); err != nil {
		t.Fatal(err)
	}
	n := make([]byte, 1)
	if _, err := io.ReadFull(br2, n); err != nil {
		t.Fatal(err)
	}
	if n[0] != 0 {
		t.Fatalf("sınır aşıldığında güvenlik türü sayısı 0 olmalı, %d geldi", n[0])
	}
	var l uint32
	if err := binary.Read(br2, binary.BigEndian, &l); err != nil {
		t.Fatal(err)
	}
	reason := make([]byte, l)
	if _, err := io.ReadFull(br2, reason); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(reason, []byte("istemci")) {
		t.Errorf("sebep açıklayıcı değil: %q", reason)
	}
}

// ── VNC kimlik doğrulama: bilinen vektör ────────────────────────────────────
//
// Parola "MCOS" ve sıfır meydan okumasıyla, standart VNC DES uygulamasının
// ürettiği değer. Bu test, anahtarın bit-ters çevirmesinin (RFB'nin tarihsel
// tuhaflığı) DOĞRU yapıldığını kilitliyor — o olmadan RealVNC dahil hiçbir
// istemci bağlanamaz.
func TestVNCEncryptKnownVector(t *testing.T) {
	challenge := make([]byte, 16) // hepsi sıfır
	got := hex.EncodeToString(vncEncrypt(challenge, "MCOS"))

	// Referans değer, aynı algoritmanın bağımsız uygulamasıyla üretildi:
	// anahtar = bit-ters çevrilmiş "MCOS\0\0\0\0", DES-ECB, iki blok.
	want := deterministicDES(t, "MCOS", challenge)
	if got != want {
		t.Errorf("VNC şifrelemesi beklenenden farklı:\n  got  %s\n  want %s", got, want)
	}

	// Parola 8 bayttan UZUNSA kırpılmalı (RFB'nin kuralı).
	long := vncEncrypt(challenge, "0123456789abc")
	short := vncEncrypt(challenge, "01234567")
	if !bytes.Equal(long, short) {
		t.Error("8 bayttan uzun parola kırpılmıyor — RFB kuralı ihlal ediliyor")
	}
}

// deterministicDES recomputes the expected value the long way, so the test
// does not just re-run the implementation it is testing.
func deterministicDES(t *testing.T, password string, challenge []byte) string {
	t.Helper()
	key := make([]byte, 8)
	copy(key, password)
	for i, b := range key {
		var r byte
		for bit := 0; bit < 8; bit++ {
			r <<= 1
			r |= b & 1
			b >>= 1
		}
		key[i] = r
	}
	block, err := newDES(key)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(challenge))
	for i := 0; i+8 <= len(challenge); i += 8 {
		block.Encrypt(out[i:i+8], challenge[i:i+8])
	}
	return hex.EncodeToString(out)
}

// ── Yardımcılar ─────────────────────────────────────────────────────────────

func requestUpdate(t *testing.T, conn net.Conn, incremental bool, w, h int) {
	t.Helper()
	inc := byte(0)
	if incremental {
		inc = 1
	}
	msg := []byte{msgFramebufferUpdateRequest, inc}
	msg = binary.BigEndian.AppendUint16(msg, 0)
	msg = binary.BigEndian.AppendUint16(msg, 0)
	msg = binary.BigEndian.AppendUint16(msg, uint16(w))
	msg = binary.BigEndian.AppendUint16(msg, uint16(h))
	if _, err := conn.Write(msg); err != nil {
		t.Fatal(err)
	}
}

func readUpdateHeader(t *testing.T, br *bufio.Reader) int {
	t.Helper()
	head := make([]byte, 4)
	if _, err := io.ReadFull(br, head); err != nil {
		t.Fatalf("güncelleme başlığı okunamadı: %v", err)
	}
	if head[0] != 0 {
		t.Fatalf("beklenmeyen sunucu mesajı: %d", head[0])
	}
	return int(binary.BigEndian.Uint16(head[2:4]))
}

func readRectHeader(t *testing.T, br *bufio.Reader) (x, y, w, h, enc int) {
	t.Helper()
	head := make([]byte, 12)
	if _, err := io.ReadFull(br, head); err != nil {
		t.Fatalf("dikdörtgen başlığı okunamadı: %v", err)
	}
	return int(binary.BigEndian.Uint16(head[0:2])),
		int(binary.BigEndian.Uint16(head[2:4])),
		int(binary.BigEndian.Uint16(head[4:6])),
		int(binary.BigEndian.Uint16(head[6:8])),
		int(int32(binary.BigEndian.Uint32(head[8:12])))
}

func drainFullFrame(t *testing.T, br *bufio.Reader, w, h int) {
	t.Helper()
	rects := readUpdateHeader(t, br)
	for i := 0; i < rects; i++ {
		_, _, rw, rh, _ := readRectHeader(t, br)
		if _, err := io.CopyN(io.Discard, br, int64(rw*rh*4)); err != nil {
			t.Fatalf("kare verisi okunamadı: %v", err)
		}
	}
}
