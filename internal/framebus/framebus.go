// Package framebus shares the panel's current frame with other processes.
//
// ════════════════════════════════════════════════════════════════════════════
// ── Neden var ───────────────────────────────────────────────────────────────
// ════════════════════════════════════════════════════════════════════════════
//
// Uzaktan ekran paylaşımı (VNC, mcosd içinde) ekranı /dev/fb0'dan okuyordu.
// Panel artık ekran kartına (DRM) doğrudan çiziyor; /dev/fb0'da yalnızca
// fbcon'un kendi tamponu kalıyor. Bu dosya olmadan VNC paneli DEĞİL, boş bir
// konsolu gösterirdi — bu kutu normalde uzaktan kullanılıyor.
//
// Panel her karesini buraya koyar, VNC buradan okur. İki süreç arasında tek
// bir tmpfs dosyası (mmap): kopyalama yok, soket yok, protokol yok.
//
// ── Maliyet: izleyen yoksa SIFIR ────────────────────────────────────────────
//
// 4K bir kare 33 MB. Her karede kopyalamak 144 Hz'de saniyede 4,7 GB demek.
// Bu yüzden okuyucu başlıktaki "son okuma" damgasını yazar; yazıcı ancak son
// birkaç saniyede bir okuyucu görmüşse kopyalar ve o zaman da yalnızca DEĞİŞEN
// satırları. VNC bağlı değilken panel kare başına tek bir zaman okuması yapar.
package framebus

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
	"unsafe"
)

// Path, panelin karesini paylaştığı yer (tmpfs).
const Path = "/run/mcos/panel.frame"

// Başlık düzeni (64 bayt, küçük-sonlu):
//
//	0   [4]byte  "MCFR"
//	4   uint32   sürüm (1)
//	8   uint32   genişlik
//	12  uint32   yükseklik
//	16  uint64   kare sayacı (her yayında artar)
//	24  int64    yazıcının son yaşam işareti (unix ns)
//	32  int64    okuyucunun son okuması (unix ns)
const (
	headerSize = 64
	offW       = 8
	offH       = 12
	offSeq     = 16
	offWriter  = 24
	offReader  = 32
	version    = 1
)

var magic = [4]byte{'M', 'C', 'F', 'R'}

// readerWindow: bu kadar süredir okuyucu görülmediyse kopyalama yapılmaz.
const readerWindow = 3 * time.Second

// aliveWindow: yazıcı bu kadar süredir yaşam işareti vermediyse okuyucu
// dosyayı bayat sayar (panel kapanmış ya da çökmüş).
const aliveWindow = 5 * time.Second

// ErrResized, yazıcının boyutu değiştiğinde okuyucuya döner; VNC oturumu
// yeni boyutla yeniden kurulmalı.
var ErrResized = errors.New("framebus: kare boyutu değişti")

// ── Yazıcı (panel) ──────────────────────────────────────────────────────────

// Writer publishes frames.
type Writer struct {
	path string
	f    *os.File
	mem  []byte
	w, h int
	// full: okuyucu yeni belirdi, bir sonraki yayın TAM kare olmalı.
	full bool
	seen bool
	last *image.RGBA
}

// Create makes (or replaces) the shared frame file.
func Create(path string, w, h int) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	wr := &Writer{path: path}
	if err := wr.Resize(w, h); err != nil {
		return nil, err
	}
	return wr, nil
}

// Resize re-creates the mapping for a new frame size.
func (wr *Writer) Resize(w, h int) error {
	wr.unmap()
	f, err := os.OpenFile(wr.path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	size := int64(headerSize + w*h*4)
	if err := f.Truncate(0); err != nil {
		f.Close()
		return err
	}
	// Seyrek dosya: tmpfs sayfaları ancak YAZILINCA ayırır. Okuyucu yoksa
	// hiç yazılmadığı için RAM harcanmaz.
	if err := f.Truncate(size); err != nil {
		f.Close()
		return err
	}
	mem, err := mmap(f, int(size))
	if err != nil {
		f.Close()
		return err
	}
	wr.f, wr.mem, wr.w, wr.h = f, mem, w, h
	copy(mem[0:4], magic[:])
	binary.LittleEndian.PutUint32(mem[4:], version)
	binary.LittleEndian.PutUint32(mem[offW:], uint32(w))
	binary.LittleEndian.PutUint32(mem[offH:], uint32(h))
	wr.full = true
	wr.Heartbeat()
	return nil
}

func (wr *Writer) unmap() {
	if wr.mem != nil {
		_ = munmap(wr.mem)
		wr.mem = nil
	}
	if wr.f != nil {
		wr.f.Close()
		wr.f = nil
	}
}

func (wr *Writer) i64(off int) *int64 {
	return (*int64)(unsafe.Pointer(&wr.mem[off]))
}

// readerActive reports whether someone read recently.
func (wr *Writer) readerActive() bool {
	if wr.mem == nil {
		return false
	}
	t := atomic.LoadInt64(wr.i64(offReader))
	aktif := t != 0 && time.Since(time.Unix(0, t)) < readerWindow
	if aktif && !wr.seen {
		wr.full = true // okuyucu yeni geldi: ilk kare tam olmalı
	}
	wr.seen = aktif
	return aktif
}

// Publish shares img if a reader is watching.
func (wr *Writer) Publish(img *image.RGBA) {
	wr.last = img
	if !wr.readerActive() {
		return
	}
	b := img.Bounds()
	if b.Dx() != wr.w || b.Dy() != wr.h {
		if wr.Resize(b.Dx(), b.Dy()) != nil {
			return
		}
	}
	stride := wr.w * 4
	pix := wr.mem[headerSize:]
	for y := 0; y < wr.h; y++ {
		s := img.Pix[y*img.Stride : y*img.Stride+stride]
		d := pix[y*stride : (y+1)*stride]
		if !wr.full && bytes.Equal(s, d) {
			continue
		}
		copy(d, s)
	}
	wr.full = false
	seq := (*uint64)(unsafe.Pointer(&wr.mem[offSeq]))
	atomic.AddUint64(seq, 1)
	wr.Heartbeat()
}

// Heartbeat marks the writer alive; panel bunu saniyede bir çağırır.
//
// Panel boştayken kare üretmez. Yaşam işareti olmasa VNC, açık ve sağlıklı
// bir paneli "kapanmış" sanıp /dev/fb0'a düşerdi. Ayrıca okuyucu YENİ
// bağlandıysa son kare hemen yayımlanır — yoksa ilk görüntü boş gelirdi.
func (wr *Writer) Heartbeat() {
	if wr.mem == nil {
		return
	}
	atomic.StoreInt64(wr.i64(offWriter), time.Now().UnixNano())
	if wr.last != nil && !wr.seen && wr.readerActive() {
		wr.Publish(wr.last)
	}
}

// Close removes the file: okuyucu paneli artık kapalı bilsin.
func (wr *Writer) Close() {
	wr.unmap()
	_ = os.Remove(wr.path)
}

// ── Okuyucu (VNC) ───────────────────────────────────────────────────────────

// Reader reads frames; vnc.Framebuffer'ı karşılar.
type Reader struct {
	path string
	f    *os.File
	mem  []byte
	w, h int
}

// Open maps the shared frame. Yazıcı canlı değilse hata döner.
func Open(path string) (*Reader, error) {
	r := &Reader{path: path}
	if err := r.remap(); err != nil {
		return nil, err
	}
	if !r.Alive() {
		r.Close()
		return nil, errors.New("framebus: panel canlı değil")
	}
	return r, nil
}

func (r *Reader) remap() error {
	r.Close()
	f, err := os.OpenFile(r.path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil || st.Size() < headerSize {
		f.Close()
		return errors.New("framebus: dosya geçersiz")
	}
	mem, err := mmap(f, int(st.Size()))
	if err != nil {
		f.Close()
		return err
	}
	if !bytes.Equal(mem[0:4], magic[:]) || binary.LittleEndian.Uint32(mem[4:]) != version {
		_ = munmap(mem)
		f.Close()
		return errors.New("framebus: tanınmayan biçim")
	}
	w := int(binary.LittleEndian.Uint32(mem[offW:]))
	h := int(binary.LittleEndian.Uint32(mem[offH:]))
	if int64(headerSize+w*h*4) > st.Size() {
		_ = munmap(mem)
		f.Close()
		return errors.New("framebus: dosya boyu başlıkla uyuşmuyor")
	}
	r.f, r.mem, r.w, r.h = f, mem, w, h
	r.touch() // yazıcı ilk kareyi hemen yayımlasın
	return nil
}

func (r *Reader) touch() {
	atomic.StoreInt64((*int64)(unsafe.Pointer(&r.mem[offReader])), time.Now().UnixNano())
}

// Alive reports whether the panel is still publishing.
func (r *Reader) Alive() bool {
	if r.mem == nil {
		return false
	}
	t := atomic.LoadInt64((*int64)(unsafe.Pointer(&r.mem[offWriter])))
	return t != 0 && time.Since(time.Unix(0, t)) < aliveWindow
}

// Size implements vnc.Framebuffer.
func (r *Reader) Size() (int, int) { return r.w, r.h }

// CurrentSize reads the writer's size now (yeniden boyutlanmayı fark etmek için).
func (r *Reader) CurrentSize() (int, int) {
	st, err := os.Stat(r.path)
	if err != nil || r.mem == nil {
		return 0, 0
	}
	if st.Size() != int64(len(r.mem)) {
		// Yazıcı dosyayı yeniden boyutlandırdı: başlığı yeni eşlemeden oku.
		f, err := os.Open(r.path)
		if err != nil {
			return 0, 0
		}
		defer f.Close()
		var hdr [headerSize]byte
		if _, err := f.ReadAt(hdr[:], 0); err != nil {
			return 0, 0
		}
		return int(binary.LittleEndian.Uint32(hdr[offW:])), int(binary.LittleEndian.Uint32(hdr[offH:]))
	}
	return int(binary.LittleEndian.Uint32(r.mem[offW:])), int(binary.LittleEndian.Uint32(r.mem[offH:]))
}

// Snapshot implements vnc.Framebuffer.
func (r *Reader) Snapshot(dst []byte) error {
	if r.mem == nil {
		return errors.New("framebus: kapalı")
	}
	if w, h := r.CurrentSize(); w != r.w || h != r.h {
		return ErrResized
	}
	r.touch()
	n := min(len(dst), r.w*r.h*4)
	copy(dst[:n], r.mem[headerSize:headerSize+n])
	return nil
}

// Close unmaps the file.
func (r *Reader) Close() error {
	if r.mem != nil {
		_ = munmap(r.mem)
		r.mem = nil
	}
	if r.f != nil {
		r.f.Close()
		r.f = nil
	}
	return nil
}
