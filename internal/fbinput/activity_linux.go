//go:build linux

package fbinput

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Bu dosya UYKU KİPİ için "kullanıcı orada mı?" sorusunu yanıtlar.
//
// Klavye için ayrı bir okuyucuya gerek yok: konsol zaten açık ve tuşa
// basıldığında tty'den bayt gelir. Ama FARE hareketi tty'ye hiçbir şey
// göndermez — ekranı fareyle uyandırabilmek için /dev/input/event* doğrudan
// okunmalıdır.

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
const (
	evSyn = 0x00
	evKey = 0x01
	evRel = 0x02
	evAbs = 0x03
)

// Activity reports user input activity from evdev devices.
type Activity struct {
	files  []*os.File
	last   atomic.Int64 // son etkinliğin UnixNano değeri
	closed atomic.Bool
	wake   chan struct{}
	once   sync.Once
}

// WatchActivity opens every /dev/input/event* and watches for activity.
//
// Hiç aygıt bulunamazsa hata DÖNMEZ: klavyesiz/faresiz bir makinede panel yine
// çalışmalı, yalnızca fareyle uyandırma özelliği olmaz. Çağıran Devices() ile
// kaç aygıt izlendiğini öğrenebilir.
func WatchActivity() *Activity {
	a := &Activity{wake: make(chan struct{}, 1)}
	a.last.Store(time.Now().UnixNano())

	paths, _ := filepath.Glob("/dev/input/event*")
	for _, p := range paths {
		f, err := os.OpenFile(p, os.O_RDONLY, 0)
		if err != nil {
			// İzin yoksa veya aygıt kaybolduysa sessizce atla: tek bir
			// aygıtın açılmaması tüm izlemeyi düşürmemeli.
			continue
		}
		a.files = append(a.files, f)
		go a.read(f)
	}
	return a
}

// Devices returns how many input devices are being watched.
func (a *Activity) Devices() int { return len(a.files) }

// read consumes events from one device.
func (a *Activity) read(f *os.File) {
	buf := make([]byte, inputEventSize*16)
	for {
		n, err := f.Read(buf)
		if err != nil {
			return // aygıt kayboldu (USB çıkarıldı) veya kapatıldı
		}
		if a.closed.Load() {
			return
		}
		for off := 0; off+inputEventSize <= n; off += inputEventSize {
			typ := binary.LittleEndian.Uint16(buf[off+16:])
			// evSyn yalnızca paket sınırıdır, etkinlik değildir; onu
			// saymak her aygıtın sürekli "etkin" görünmesine yol açardı.
			switch typ {
			case evKey, evRel, evAbs:
				a.mark()
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
		for _, f := range a.files {
			_ = f.Close()
		}
	})
	return nil
}
