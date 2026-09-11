//go:build !linux

package fbinput

import "time"

// Activity is a no-op outside Linux (no evdev).
type Activity struct{ wake chan struct{} }

// WatchActivity returns an inert watcher so panel code compiles on a dev host.
func WatchActivity() *Activity { return &Activity{wake: make(chan struct{})} }

func (*Activity) Devices() int            { return 0 }
func (a *Activity) Wake() <-chan struct{} { return a.wake }
func (*Activity) Idle() time.Duration     { return 0 }
func (*Activity) Touch()                  {}
func (*Activity) Close() error            { return nil }
