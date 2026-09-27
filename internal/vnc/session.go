package vnc

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"time"
)

// ════════════════════════════════════════════════════════════════════════════
// OTURUM: kare güncellemeleri ve girdi
// ════════════════════════════════════════════════════════════════════════════
//
// ── Neden Raw kodlama ───────────────────────────────────────────────────────
//
// RFB'de yalnızca Raw kodlaması ZORUNLUDUR; her istemci onu anlar. Tight/ZRLE
// daha az bant genişliği kullanır ama sunucu tarafı karmaşıktır (zlib akış
// durumu, paletli alt-kodlamalar) ve yanlış uygulanması istemcide bozuk
// görüntü olarak çıkar — teşhisi zor bir arıza.
//
// Bunun yerine GÖNDERİLEN ALANI küçültüyoruz: ekran 32x32 karolara bölünüyor
// ve yalnızca DEĞİŞEN karolar gönderiliyor. Panelde tipik değişiklik bir
// satır vurgusu ya da dönen bir göstergedir; yani kare başına birkaç karo.
// Ölçü: 1080p tam kare 8,3 MB, tipik güncelleme 30-300 KB.
//
// ── Neden yoklama (polling) ─────────────────────────────────────────────────
//
// Çerçeve arabelleğinde "değişti" bildirimi yoktur; panel doğrudan belleğe
// yazar. Karşılaştırma tek yol. Yoklama aralığı istemci istek yaptığında
// değerlendiriliyor: RFB'de istemci her kareyi kendisi ister
// (FramebufferUpdateRequest), yani boşta duran bir istemci CPU yakmaz.

// tileSize is the damage-detection granularity.
//
// 32x32: karo başına 4 KB. Daha küçük karolar daha az veri gönderir ama
// dikdörtgen başlığı (12 bayt) ve karşılaştırma maliyeti artar. Daha büyük
// karolar tek piksel değişiminde 16 KB gönderir.
const tileSize = 32

// maxUpdateInterval bounds how often we re-scan the screen.
//
// 40 ms ≈ 25 kare/sn. Panelin kendi çizimi 60 kare/sn'ye çıkabiliyor ama
// VNC üzerinden 25 kare akıcı görünür ve ağı yarı yarıya rahatlatır.
const maxUpdateInterval = 40 * time.Millisecond

// session serves one connected client until it disconnects.
func (s *Server) session(conn net.Conn, br *bufio.Reader, bw *bufio.Writer) error {
	w, h := s.w, s.h
	cur := make([]byte, w*h*4)
	prev := make([]byte, w*h*4)
	havePrev := false

	// İstemcinin istediği piksel biçimi. Varsayılan, ServerInit'te
	// bildirdiğimiz doğal biçimdir; istemci SetPixelFormat ile değiştirebilir
	// ve o zaman UYMAK ZORUNDAYIZ (bkz. pixfmt.go).
	format := nativeFormat
	// row, dönüşüm için satır tamponu. Doğal biçimde hiç kullanılmaz.
	row := make([]byte, w*4)

	// İlk kare: istemci henüz istek yapmadan okumaya başlamıyoruz.
	lastScan := time.Time{}

	for {
		// Girdi mesajları bloklayan okumadır; bir istemci sessizse burada
		// bekleriz ve CPU harcamayız.
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Minute))
		msgType := make([]byte, 1)
		if _, err := io.ReadFull(br, msgType); err != nil {
			return err
		}

		switch msgType[0] {
		case msgSetPixelFormat:
			// 3 bayt dolgu + 16 bayt biçim.
			buf := make([]byte, 19)
			if _, err := io.ReadFull(br, buf); err != nil {
				return err
			}
			want := parsePixelFormat(buf[3:])
			if want.usable() {
				format = want
				if !want.isNative() && s.opt.Log != nil {
					s.opt.Log.Infof("vnc: istemci %d bitlik biçim istedi",
						want.bpp)
				}
			} else if s.opt.Log != nil {
				// Paletli kipler desteklenmiyor; doğal biçimde kalıyoruz.
				s.opt.Log.Warnf("vnc: desteklenmeyen piksel biçimi "+
					"(bpp=%d, trueColour=%v) — doğal biçim sürüyor",
					want.bpp, want.trueColour)
			}
			// Biçim değişti: istemcinin elindeki kare artık geçersiz.
			havePrev = false

		case msgSetEncodings:
			if _, err := io.CopyN(io.Discard, br, 1); err != nil {
				return err
			}
			var n uint16
			if err := binary.Read(br, binary.BigEndian, &n); err != nil {
				return err
			}
			if _, err := io.CopyN(io.Discard, br, int64(n)*4); err != nil {
				return err
			}

		case msgFramebufferUpdateRequest:
			buf := make([]byte, 9)
			if _, err := io.ReadFull(br, buf); err != nil {
				return err
			}
			incremental := buf[0] != 0

			// Ekranı yeniden taramadan önce en az bir kare aralığı bekle:
			// bazı istemciler istekleri arka arkaya yığar.
			if d := time.Since(lastScan); d < maxUpdateInterval {
				time.Sleep(maxUpdateInterval - d)
			}
			if err := s.opt.FB.Snapshot(cur); err != nil {
				return err
			}
			lastScan = time.Now()

			_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
			var err error
			if incremental && havePrev {
				err = writeDamage(bw, cur, prev, w, h, format, row)
			} else {
				err = writeFull(bw, cur, w, h, format, row)
			}
			if err != nil {
				return err
			}
			if err := bw.Flush(); err != nil {
				return err
			}
			copy(prev, cur)
			havePrev = true

		case msgKeyEvent:
			buf := make([]byte, 7)
			if _, err := io.ReadFull(br, buf); err != nil {
				return err
			}
			down := buf[0] != 0
			keysym := binary.BigEndian.Uint32(buf[3:7])
			if s.opt.Input != nil {
				if err := s.opt.Input.Key(keysym, down); err != nil && s.opt.Log != nil {
					s.opt.Log.Warnf("vnc: tuş olayı iletilemedi: %v", err)
				}
			}

		case msgPointerEvent:
			buf := make([]byte, 5)
			if _, err := io.ReadFull(br, buf); err != nil {
				return err
			}
			mask := buf[0]
			x := int(binary.BigEndian.Uint16(buf[1:3]))
			y := int(binary.BigEndian.Uint16(buf[3:5]))
			if s.opt.Input != nil {
				if err := s.opt.Input.Pointer(x, y, mask); err != nil && s.opt.Log != nil {
					s.opt.Log.Warnf("vnc: imleç olayı iletilemedi: %v", err)
				}
			}

		case msgClientCutText:
			// Pano aktarımı BİLEREK yok sayılıyor: MCOS'ta panoya yazacak
			// bir şey yok ve gelen metni bir yere koymak, uzaktan gelen
			// veriyi arayüze enjekte etmek olurdu.
			if _, err := io.CopyN(io.Discard, br, 3); err != nil {
				return err
			}
			var n uint32
			if err := binary.Read(br, binary.BigEndian, &n); err != nil {
				return err
			}
			// Boyut sınırı: kötü niyetli bir istemci 4 GB bildirip belleği
			// tüketemesin diye okumadan ATIYORUZ.
			if n > 1<<20 {
				n = 1 << 20
			}
			if _, err := io.CopyN(io.Discard, br, int64(n)); err != nil {
				return err
			}

		default:
			// Bilinmeyen mesaj: akışın neresinde olduğumuzu artık bilemeyiz.
			// Devam etmek, rastgele baytları koordinat sanmak demektir.
			return errUnknownMessage(msgType[0])
		}
	}
}

// errUnknownMessage keeps the error text in one place.
func errUnknownMessage(t byte) error {
	return &protocolError{t: t}
}

type protocolError struct{ t byte }

func (e *protocolError) Error() string {
	return "vnc: bilinmeyen istemci mesajı: " + itoa(int(e.t))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// writeFull sends the whole screen as one Raw rectangle.
func writeFull(bw *bufio.Writer, pix []byte, w, h int,
	format pixelFormat, row []byte) error {

	if err := writeUpdateHeader(bw, 1); err != nil {
		return err
	}
	if err := writeRectHeader(bw, 0, 0, w, h); err != nil {
		return err
	}
	if format.isNative() {
		_, err := bw.Write(pix)
		return err
	}
	bpp := format.bytesPerPixel()
	for y := 0; y < h; y++ {
		off := y * w * 4
		format.encodeRow(row, pix[off:off+w*4], w)
		if _, err := bw.Write(row[:w*bpp]); err != nil {
			return err
		}
	}
	return nil
}

// writeDamage sends only the tiles that changed since prev.
//
// Karolar SATIR SATIR birleştiriliyor: yan yana değişen karolar tek bir
// dikdörtgen olarak gönderiliyor. Bu, dikdörtgen başlığı sayısını (12 bayt +
// istemci tarafı işlem) belirgin biçimde düşürüyor — tam ekran değişiminde
// 2040 karo yerine 34 dikdörtgen.
func writeDamage(bw *bufio.Writer, cur, prev []byte, w, h int,
	format pixelFormat, row []byte) error {
	type rect struct{ x, y, w, h int }
	var rects []rect

	for ty := 0; ty < h; ty += tileSize {
		th := tileSize
		if ty+th > h {
			th = h - ty
		}
		runStart := -1
		for tx := 0; tx < w; tx += tileSize {
			tw := tileSize
			if tx+tw > w {
				tw = w - tx
			}
			if tileChanged(cur, prev, w, tx, ty, tw, th) {
				if runStart < 0 {
					runStart = tx
				}
				continue
			}
			if runStart >= 0 {
				rects = append(rects, rect{runStart, ty, tx - runStart, th})
				runStart = -1
			}
		}
		if runStart >= 0 {
			rects = append(rects, rect{runStart, ty, w - runStart, th})
		}
	}

	if len(rects) == 0 {
		// Değişiklik yok: BOŞ bir güncelleme gönderiyoruz. Hiç yanıt
		// vermemek, istemcinin sonsuza kadar beklemesi demektir.
		return writeUpdateHeader(bw, 0)
	}

	if err := writeUpdateHeader(bw, len(rects)); err != nil {
		return err
	}
	for _, r := range rects {
		if err := writeRectHeader(bw, r.x, r.y, r.w, r.h); err != nil {
			return err
		}
		bpp := format.bytesPerPixel()
		for y := r.y; y < r.y+r.h; y++ {
			off := (y*w + r.x) * 4
			if format.isNative() {
				if _, err := bw.Write(cur[off : off+r.w*4]); err != nil {
					return err
				}
				continue
			}
			format.encodeRow(row, cur[off:off+r.w*4], r.w)
			if _, err := bw.Write(row[:r.w*bpp]); err != nil {
				return err
			}
		}
	}
	return nil
}

// tileChanged reports whether any pixel in the tile differs.
func tileChanged(cur, prev []byte, w, tx, ty, tw, th int) bool {
	for y := ty; y < ty+th; y++ {
		off := (y*w + tx) * 4
		end := off + tw*4
		// Dilim karşılaştırması derleyici tarafından memcmp'e indirgenir;
		// bayt bayt dönmek 4 KB'lık bir karoda binlerce dal demekti.
		if string(cur[off:end]) != string(prev[off:end]) {
			return true
		}
	}
	return false
}

func writeUpdateHeader(bw *bufio.Writer, rects int) error {
	var b [4]byte
	b[0] = 0 // FramebufferUpdate
	b[1] = 0 // padding
	binary.BigEndian.PutUint16(b[2:], uint16(rects))
	_, err := bw.Write(b[:])
	return err
}

func writeRectHeader(bw *bufio.Writer, x, y, w, h int) error {
	var b [12]byte
	binary.BigEndian.PutUint16(b[0:], uint16(x))
	binary.BigEndian.PutUint16(b[2:], uint16(y))
	binary.BigEndian.PutUint16(b[4:], uint16(w))
	binary.BigEndian.PutUint16(b[6:], uint16(h))
	binary.BigEndian.PutUint32(b[8:], uint32(encRaw))
	_, err := bw.Write(b[:])
	return err
}
