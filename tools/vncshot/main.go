// Command vncshot connects to a VNC server, grabs one frame and writes a PNG.
//
// ════════════════════════════════════════════════════════════════════════════
// NEDEN VAR
// ════════════════════════════════════════════════════════════════════════════
//
// internal/vnc'nin testleri protokolü bayt düzeyinde doğruluyor ama hepsi
// AYNI SÜREÇTE koşuyor. "Gerçekten ağ üzerinden bağlanılıyor mu, gerçek ekran
// mı geliyor?" sorusunun cevabı değiller.
//
// Bu araç o boşluğu kapatıyor: MCOS'un çalıştığı makineye TCP ile bağlanır,
// RFB el sıkışmasını yapar, bir kare ister ve PNG'ye yazar. Sanal makinede
// açılan MCOS'un ekranını geliştirme makinesinden görmenin yolu budur.
//
// Kullanım:
//
//	go run ./tools/vncshot 127.0.0.1:5900 PAROLA cikti.png
//
// İmaja GİRMEZ: tools/ altındaki her şey yalnızca geliştirme içindir.
package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"os"
	"time"

	"mcos/internal/vnc"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr,
			"kullanım: vncshot <adres:port> <parola> <çıktı.png>")
		os.Exit(2)
	}
	addr, password, out := os.Args[1], os.Args[2], os.Args[3]
	// İsteğe bağlı 4. argüman: kare almadan ÖNCE gönderilecek tuş.
	// Girdi enjeksiyonunun (uinput) gerçekten çalıştığını kanıtlamak için.
	keyName := ""
	if len(os.Args) > 4 {
		keyName = os.Args[4]
	}

	img, name, err := grabWithKey(addr, password, keyName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hata:", err)
		os.Exit(1)
	}

	f, err := os.Create(out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hata:", err)
		os.Exit(1)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fmt.Fprintln(os.Stderr, "hata:", err)
		os.Exit(1)
	}
	b := img.Bounds()
	fmt.Printf("bağlandı: %s (%q) — %dx%d -> %s\n",
		addr, name, b.Dx(), b.Dy(), out)
}

// keysyms are the X11 values for the keys this tool can send.
var keysyms = map[string]uint32{
	"down": 0xFF54, "up": 0xFF52, "left": 0xFF51, "right": 0xFF53,
	"enter": 0xFF0D, "esc": 0xFF1B, "tab": 0xFF09, "g": 'g', "w": 'w',
}

// grabWithKey optionally presses a key, then captures a frame.
func grabWithKey(addr, password, keyName string) (*image.RGBA, string, error) {
	return grab(addr, password, keyName)
}

// grab performs the whole RFB exchange and returns one frame.
func grab(addr, password, keyName string) (*image.RGBA, string, error) {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
	br := bufio.NewReaderSize(conn, 1<<16)

	// ── Sürüm ───────────────────────────────────────────────────────────
	ver := make([]byte, 12)
	if _, err := io.ReadFull(br, ver); err != nil {
		return nil, "", fmt.Errorf("sürüm okunamadı: %w", err)
	}
	if _, err := conn.Write(ver); err != nil {
		return nil, "", err
	}

	// ── Güvenlik ────────────────────────────────────────────────────────
	nTypes := make([]byte, 1)
	if _, err := io.ReadFull(br, nTypes); err != nil {
		return nil, "", err
	}
	if nTypes[0] == 0 {
		// Sunucu reddetti; sebebi oku.
		var l uint32
		if err := binary.Read(br, binary.BigEndian, &l); err != nil {
			return nil, "", err
		}
		reason := make([]byte, l)
		_, _ = io.ReadFull(br, reason)
		return nil, "", fmt.Errorf("sunucu reddetti: %s", reason)
	}
	types := make([]byte, nTypes[0])
	if _, err := io.ReadFull(br, types); err != nil {
		return nil, "", err
	}
	hasVNCAuth := false
	for _, t := range types {
		if t == 2 {
			hasVNCAuth = true
		}
	}
	if !hasVNCAuth {
		return nil, "", fmt.Errorf("sunucu VNC parolası istemiyor (türler: %v)", types)
	}
	if _, err := conn.Write([]byte{2}); err != nil {
		return nil, "", err
	}

	challenge := make([]byte, 16)
	if _, err := io.ReadFull(br, challenge); err != nil {
		return nil, "", err
	}
	// Şifreleme SUNUCUYLA AYNI koddan geliyor: burada ikinci bir uygulama
	// yazmak, ikisinin ayrışması demekti (ve o ayrışma yalnızca gerçek bir
	// istemci bağlanmaya çalıştığında görünürdü).
	if _, err := conn.Write(vnc.Encrypt(challenge, password)); err != nil {
		return nil, "", err
	}
	res := make([]byte, 4)
	if _, err := io.ReadFull(br, res); err != nil {
		return nil, "", err
	}
	if binary.BigEndian.Uint32(res) != 0 {
		var l uint32
		if err := binary.Read(br, binary.BigEndian, &l); err == nil {
			reason := make([]byte, l)
			_, _ = io.ReadFull(br, reason)
			return nil, "", fmt.Errorf("parola reddedildi: %s", reason)
		}
		return nil, "", fmt.Errorf("parola reddedildi")
	}

	// ── Başlatma ────────────────────────────────────────────────────────
	if _, err := conn.Write([]byte{1}); err != nil { // shared
		return nil, "", err
	}
	head := make([]byte, 24)
	if _, err := io.ReadFull(br, head); err != nil {
		return nil, "", err
	}
	w := int(binary.BigEndian.Uint16(head[0:2]))
	h := int(binary.BigEndian.Uint16(head[2:4]))
	bpp := head[4]
	nameLen := binary.BigEndian.Uint32(head[20:24])
	nameBuf := make([]byte, nameLen)
	if _, err := io.ReadFull(br, nameBuf); err != nil {
		return nil, "", err
	}
	if bpp != 32 {
		return nil, "", fmt.Errorf("beklenmeyen piksel derinliği: %d", bpp)
	}

	// ── İsteğe bağlı tuş ────────────────────────────────────────────────
	if keyName != "" {
		ks, ok := keysyms[keyName]
		if !ok {
			return nil, "", fmt.Errorf("bilinmeyen tuş: %s", keyName)
		}
		for _, down := range []bool{true, false} {
			msg := []byte{4, 0, 0, 0} // KeyEvent
			if down {
				msg[1] = 1
			}
			msg = binary.BigEndian.AppendUint32(msg, ks)
			if _, err := conn.Write(msg); err != nil {
				return nil, "", err
			}
			time.Sleep(60 * time.Millisecond)
		}
		// Panelin tuşu işleyip yeniden çizmesi için bekle.
		time.Sleep(1500 * time.Millisecond)
	}

	// ── Kare iste ───────────────────────────────────────────────────────
	req := []byte{3, 0} // FramebufferUpdateRequest, incremental=0
	req = binary.BigEndian.AppendUint16(req, 0)
	req = binary.BigEndian.AppendUint16(req, 0)
	req = binary.BigEndian.AppendUint16(req, uint16(w))
	req = binary.BigEndian.AppendUint16(req, uint16(h))
	if _, err := conn.Write(req); err != nil {
		return nil, "", err
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	uh := make([]byte, 4)
	if _, err := io.ReadFull(br, uh); err != nil {
		return nil, "", err
	}
	if uh[0] != 0 {
		return nil, "", fmt.Errorf("beklenmeyen sunucu mesajı: %d", uh[0])
	}
	rects := int(binary.BigEndian.Uint16(uh[2:4]))
	for i := 0; i < rects; i++ {
		rh := make([]byte, 12)
		if _, err := io.ReadFull(br, rh); err != nil {
			return nil, "", err
		}
		rx := int(binary.BigEndian.Uint16(rh[0:2]))
		ry := int(binary.BigEndian.Uint16(rh[2:4]))
		rw := int(binary.BigEndian.Uint16(rh[4:6]))
		rhh := int(binary.BigEndian.Uint16(rh[6:8]))
		enc := int32(binary.BigEndian.Uint32(rh[8:12]))
		if enc != 0 {
			return nil, "", fmt.Errorf("Raw dışı kodlama geldi: %d", enc)
		}
		row := make([]byte, rw*4)
		for y := 0; y < rhh; y++ {
			if _, err := io.ReadFull(br, row); err != nil {
				return nil, "", fmt.Errorf("piksel verisi eksik: %w", err)
			}
			o := img.PixOffset(rx, ry+y)
			copy(img.Pix[o:o+rw*4], row)
			// Alfa kanalı RFB'de taşınmaz; PNG için opak yapıyoruz.
			for x := 0; x < rw; x++ {
				img.Pix[o+x*4+3] = 255
			}
		}
	}
	return img, string(nameBuf), nil
}
