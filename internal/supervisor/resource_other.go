//go:build !linux

package supervisor

// applyLimits is a no-op on non-Linux hosts (dev machines). Scheduling
// priority and CPU affinity are only enforced on the Linux appliance target.
func applyLimits(pid int, spec Spec) {}
