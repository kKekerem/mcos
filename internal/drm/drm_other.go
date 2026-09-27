//go:build !linux

package drm

import (
	"errors"
	"image"
	"time"
)

// MCOS yalnızca Linux'ta çalışıyor; bu dosya geliştirme makinesinde
// derlemenin sürmesi için var. Mod mantığı (mode.go) burada da derlenir ve
// sınanır.

// ErrLost, aygıtın kaybolduğunu bildirir.
var ErrLost = errors.New("drm: ekran aygıtı kayboldu")

// ErrNoMonitor: ekran kartı var ama bağlı monitör yok.
var ErrNoMonitor = errors.New("drm: bağlı monitör yok")

// Screen is unavailable off Linux.
type Screen struct{}

// OpenScreen always fails off Linux.
func OpenScreen(string) (*Screen, error) { return nil, errors.New("drm: yalnızca Linux") }

func (s *Screen) Present(*image.RGBA) error { return nil }
func (s *Screen) Check() bool               { return false }
func (s *Screen) SetMode(string) error      { return errors.New("drm: yalnızca Linux") }
func (s *Screen) SetSaved(string)           {}
func (s *Screen) Size() (int, int)          { return 0, 0 }
func (s *Screen) FramePeriod() time.Duration {
	return 16 * time.Millisecond
}
func (s *Screen) Info() Info      { return Info{} }
func (s *Screen) Blank(bool) bool { return false }
func (s *Screen) Close()          {}

// HotplugWatch is unavailable off Linux.
type HotplugWatch struct{}

func WatchHotplug() *HotplugWatch  { return nil }
func (h *HotplugWatch) Take() bool { return false }
func (h *HotplugWatch) Close()     {}
