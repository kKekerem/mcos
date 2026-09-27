//go:build !linux

package supervisor

// applyLimits is a no-op on non-Linux hosts (dev machines). Scheduling
// priority and CPU affinity are only enforced on the Linux appliance target.
func applyLimits(pid int, spec Spec) {}

// pinAllThreads is a no-op outside Linux.
func pinAllThreads(pid int, cpus []int) int { return 0 }

// niceAllThreads is a no-op outside Linux.
func niceAllThreads(pid, nice int) {}

func pinTurboThreads(pid int, main []int) (int, bool) { return 0, false }

// PinProcess is a no-op off Linux.
func PinProcess(pid int, cpus []int) int { return 0 }
