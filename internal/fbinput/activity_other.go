//go:build !linux

package fbinput

import "time"

// Bu dosya YALNIZCA geliştirme makinesinde (Windows/macOS) derlenir. evdev
// Linux'a özgüdür; burada hareketsiz bir izleyici döndürüyoruz ki panel kodu
// her yerde derlensin ve testler koşabilsin.

// Kind classifies an input device (see evdev_linux.go for the real values).
type Kind int

// Kind values mirror the Linux build so shared code compiles everywhere.
const (
	KindOther Kind = iota
	KindMouse
	KindTouchpad
	KindTouchscreen
)

// String returns a Turkish label.
func (k Kind) String() string {
	switch k {
	case KindMouse:
		return "fare"
	case KindTouchpad:
		return "touchpad"
	case KindTouchscreen:
		return "dokunmatik ekran"
	}
	return "diğer"
}

// Button identifies a pointer button.
type Button int

// Button values mirror the Linux build.
const (
	ButtonNone Button = iota
	ButtonLeft
	ButtonRight
	ButtonMiddle
)

// PointerEvent is one decoded pointer change (see activity_linux.go).
type PointerEvent struct {
	Kind           Kind
	DX, DY         int
	AbsX, AbsY     int
	HasAbs         bool
	Wheel          int
	Button         Button
	Press, Release bool
}

// Activity is a no-op outside Linux (no evdev).
type Activity struct {
	wake chan struct{}
	ptr  chan PointerEvent
}

// WatchActivity returns an inert watcher so panel code compiles on a dev host.
func WatchActivity() *Activity {
	return &Activity{wake: make(chan struct{}), ptr: make(chan PointerEvent)}
}

func (*Activity) Devices() int                   { return 0 }
func (*Activity) Pointers() []string             { return nil }
func (*Activity) HasPointer() bool               { return false }
func (*Activity) SetScreen(int, int)             {}
func (a *Activity) Pointer() <-chan PointerEvent { return a.ptr }
func (a *Activity) Wake() <-chan struct{}        { return a.wake }
func (*Activity) Idle() time.Duration            { return 0 }
func (*Activity) Touch()                         {}
func (*Activity) Close() error                   { return nil }
