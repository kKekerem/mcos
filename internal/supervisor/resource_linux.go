//go:build linux

package supervisor

import (
	"syscall"
	"unsafe"
)

// applyLimits applies the process's scheduling priority (nice) and CPU affinity
// after it has started. Negative nice and affinity changes require privilege
// (the appliance runs as root); failures are ignored so a non-privileged dev
// host degrades gracefully.
func applyLimits(pid int, spec Spec) {
	if spec.Nice != 0 {
		_ = syscall.Setpriority(syscall.PRIO_PROCESS, pid, spec.Nice)
	}
	if len(spec.CPUAffinity) > 0 {
		setAffinity(pid, spec.CPUAffinity)
	}
}

// setAffinity pins the process to the given CPU cores via sched_setaffinity.
// We build the cpu_set_t bitmask by hand to avoid pulling in golang.org/x/sys.
func setAffinity(pid int, cpus []int) {
	const setBytes = 128 // room for 1024 CPUs
	var mask [setBytes]byte
	any := false
	for _, c := range cpus {
		if c >= 0 && c < setBytes*8 {
			mask[c/8] |= 1 << (uint(c) % 8)
			any = true
		}
	}
	if !any {
		return
	}
	_, _, _ = syscall.Syscall(syscall.SYS_SCHED_SETAFFINITY,
		uintptr(pid), uintptr(setBytes), uintptr(unsafe.Pointer(&mask[0])))
}
