//go:build linux

package drm

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ════════════════════════════════════════════════════════════════════════════
// DUMB BUFFER (CPU'nun yazdığı, ekran kartının taradığı tampon)
// ════════════════════════════════════════════════════════════════════════════
//
// "Dumb buffer" hızlandırma gerektirmeyen, düz piksel yazılan bir bellek
// bloğudur. Mesa ya da GPU'ya özel bir şey gerekmiyor; her KMS sürücüsü
// destekliyor (DRM_CAP_DUMB_BUFFER). Panel kendi çizimini yazılımda yapıyor;
// ekran kartının işi bu tamponu monitöre, monitörün hızında taramak.
//
// ── Neden iki piksel biçimi ─────────────────────────────────────────────────
//
// Panelin tuvali image.RGBA: bellekte R,G,B,A baytları. DRM'de bunun BİREBİR
// karşılığı XBGR8888'dir (küçük-sonlu 32 bitlik kelime x:B:G:R -> bellekte
// R,G,B,x). O biçim destekleniyorsa satırlar DÖNÜŞÜMSÜZ kopyalanır.
//
// Ama her sürücü XBGR8888'i birincil düzlemde kabul etmiyor (simpledrm
// yalnızca firmware'in biçimini + XRGB8888'i sunar). O zaman evrensel biçim
// XRGB8888'e (bellekte B,G,R,x) düşülür ve kopyalarken R ile B yer değiştirir.
// Hangisinin seçildiği ilk tamponda belirlenir ve aygıt boyunca sabit kalır.

const (
	fourccXRGB8888 = 'X' | 'R'<<8 | '2'<<16 | '4'<<24
	fourccXBGR8888 = 'X' | 'B'<<8 | '2'<<16 | '4'<<24
)

type createDumb struct {
	Height, Width, Bpp, Flags uint32
	Handle, Pitch             uint32
	Size                      uint64
}

type mapDumb struct {
	Handle, Pad uint32
	Offset      uint64
}

type destroyDumb struct{ Handle uint32 }

// fbCmd is struct drm_mode_fb_cmd (eski ADDFB).
type fbCmd struct {
	FbID, Width, Height, Pitch, Bpp, Depth, Handle uint32
}

// fbCmd2 is struct drm_mode_fb_cmd2 (ADDFB2). Go, Modifier'dan önce 4 baytlık
// hizalama boşluğunu kendisi koyar; toplam 104 bayt (testte sabit).
type fbCmd2 struct {
	FbID, Width, Height, PixelFormat, Flags uint32
	Handles, Pitches, Offsets               [4]uint32
	Modifier                                [4]uint64
}

// buffer is one scan-out buffer.
type buffer struct {
	handle, fb, pitch uint32
	w, h              int
	mem               []byte
	// direct: bayt düzeni RGBA ile aynı (XBGR8888) — dönüşümsüz kopya.
	direct bool
}

// fbFormat, aygıtın kabul ettiği biçim; 0 = henüz denenmedi.
type fbFormat uint32

// newBuffer allocates, registers and maps one buffer.
func (d *Device) newBuffer(w, h int, format *fbFormat) (*buffer, error) {
	cd := createDumb{Width: uint32(w), Height: uint32(h), Bpp: 32}
	if err := d.ioctl(nrCreateDumb, unsafe.Sizeof(cd), unsafe.Pointer(&cd)); err != nil {
		return nil, fmt.Errorf("drm: %dx%d tampon ayrılamadı: %w", w, h, err)
	}
	b := &buffer{handle: cd.Handle, pitch: cd.Pitch, w: w, h: h}

	fb, direct, err := d.addFB(b, format)
	if err != nil {
		d.destroyDumb(b.handle)
		return nil, err
	}
	b.fb, b.direct = fb, direct

	md := mapDumb{Handle: b.handle}
	if err := d.ioctl(nrMapDumb, unsafe.Sizeof(md), unsafe.Pointer(&md)); err != nil {
		d.freeBuffer(b)
		return nil, fmt.Errorf("drm: tampon eşlenemedi: %w", err)
	}
	mem, err := unix.Mmap(d.Fd(), int64(md.Offset), int(cd.Size),
		unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		d.freeBuffer(b)
		return nil, fmt.Errorf("drm: mmap: %w", err)
	}
	b.mem = mem
	// Temiz başla: yeni tamponda önceki kullanıcının kalıntısı olabilir ve
	// ilk kare yalnızca DEĞİŞEN satırları yazsaydı o kalıntı ekranda kalırdı.
	clear(b.mem)
	return b, nil
}

// addFB registers the buffer, trying the zero-copy format first.
func (d *Device) addFB(b *buffer, format *fbFormat) (fb uint32, direct bool, err error) {
	try2 := func(f uint32) (uint32, error) {
		c := fbCmd2{Width: uint32(b.w), Height: uint32(b.h), PixelFormat: f}
		c.Handles[0], c.Pitches[0] = b.handle, b.pitch
		err := d.ioctl(nrAddFB2, unsafe.Sizeof(c), unsafe.Pointer(&c))
		return c.FbID, err
	}
	switch *format {
	case fourccXBGR8888:
		fb, err = try2(fourccXBGR8888)
		return fb, true, err
	case fourccXRGB8888:
		fb, err = try2(fourccXRGB8888)
		if err == nil {
			return fb, false, nil
		}
		// ADDFB2'yi hiç tanımayan eski bir sürücü: eski ADDFB (bpp=32,
		// depth=24 çekirdekte XRGB8888'e eşlenir).
		return d.addFBLegacy(b)
	}
	if fb, err = try2(fourccXBGR8888); err == nil {
		*format = fourccXBGR8888
		return fb, true, nil
	}
	if err == ErrLost {
		return 0, false, err
	}
	*format = fourccXRGB8888
	if fb, err = try2(fourccXRGB8888); err == nil {
		return fb, false, nil
	}
	return d.addFBLegacy(b)
}

func (d *Device) addFBLegacy(b *buffer) (uint32, bool, error) {
	c := fbCmd{Width: uint32(b.w), Height: uint32(b.h), Pitch: b.pitch,
		Bpp: 32, Depth: 24, Handle: b.handle}
	if err := d.ioctl(nrAddFB, unsafe.Sizeof(c), unsafe.Pointer(&c)); err != nil {
		return 0, false, fmt.Errorf("drm: framebuffer kaydedilemedi: %w", err)
	}
	return c.FbID, false, nil
}

// freeBuffer releases a buffer. Aygıt kaybolmuşsa ioctl'ler başarısız olur;
// o durumda yalnızca eşleme kaldırılır (bellek çekirdekte fd ile gider).
func (d *Device) freeBuffer(b *buffer) {
	if b == nil {
		return
	}
	if b.mem != nil {
		_ = unix.Munmap(b.mem)
		b.mem = nil
	}
	if b.fb != 0 {
		id := b.fb
		_ = d.ioctl(nrRmFB, unsafe.Sizeof(id), unsafe.Pointer(&id))
		b.fb = 0
	}
	d.destroyDumb(b.handle)
	b.handle = 0
}

func (d *Device) destroyDumb(h uint32) {
	if h == 0 {
		return
	}
	dd := destroyDumb{Handle: h}
	_ = d.ioctl(nrDestroyDumb, unsafe.Sizeof(dd), unsafe.Pointer(&dd))
}

// ── Sayfa çevirme ve hasar bildirimi ────────────────────────────────────────

type pageFlipCmd struct {
	CrtcID, FbID, Flags, Reserved uint32
	UserData                      uint64
}

type dirtyCmd struct {
	FbID, Flags, Color, NumClips uint32
	ClipsPtr                     uint64
}

type clipRect struct{ X1, Y1, X2, Y2 uint16 }

const pageFlipEvent = 0x01

func (d *Device) pageFlip(crtc, fb uint32, user uint64) error {
	c := pageFlipCmd{CrtcID: crtc, FbID: fb, Flags: pageFlipEvent, UserData: user}
	return d.ioctl(nrPageFlip, unsafe.Sizeof(c), unsafe.Pointer(&c))
}

// dirtyFB tells a shadow-buffered driver which rows changed.
//
// Gölge tamponlu sürücüler (simpledrm, bochs, virtio-gpu) çizilen tamponu
// kendileri firmware belleğine/ana makineye kopyalar; DIRTYFB onlara "yalnızca
// şu satırlar değişti" der ve kopya o bölgeyle sınırlı kalır.
func (d *Device) dirtyFB(fb uint32, clips []clipRect) error {
	c := dirtyCmd{FbID: fb}
	if len(clips) > 0 {
		c.NumClips = uint32(len(clips))
		c.ClipsPtr = uint64(uintptr(unsafe.Pointer(&clips[0])))
	}
	err := d.ioctl(nrDirtyFB, unsafe.Sizeof(c), unsafe.Pointer(&c))
	// Dizi ioctl boyunca canlı kalmalı: adresi uintptr olarak yapıda.
	runtime.KeepAlive(clips)
	return err
}
