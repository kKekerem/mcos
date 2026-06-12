//go:build linux

package timesync

import (
	"os/exec"
	"syscall"
	"time"
)

// setSystemClock sets the kernel wall clock with settimeofday, then best-effort
// writes it to the hardware clock (harmless if there is no RTC). Requires root,
// which the appliance daemon always has.
func setSystemClock(t time.Time) error {
	tv := syscall.Timeval{
		Sec:  t.Unix(),
		Usec: int64(t.Nanosecond() / 1000),
	}
	if err := syscall.Settimeofday(&tv); err != nil {
		return err
	}
	// Persist to the RTC if one exists; ignore failures (battery-less boxes).
	_ = exec.Command("hwclock", "-w", "--utc").Run()
	return nil
}
