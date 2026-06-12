//go:build !linux

package timesync

import "time"

// setSystemClock is a no-op on dev hosts: MCOS only owns the clock on the Linux
// appliance, and we must not change a developer's machine time during testing.
func setSystemClock(_ time.Time) error { return nil }
