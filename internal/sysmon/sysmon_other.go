//go:build !linux

package sysmon

import "mcos/internal/model"

// Non-Linux stubs. The OS target is Linux, where the rich implementation lives;
// on the Windows/macOS dev host these return best-effort/empty values so the
// daemon still builds and runs for development and testing.

func cpuDetails(info *model.CPUInfo) {
	if info.Model == "" {
		info.Model = "Unknown CPU"
	}
}

func memory() model.MemInfo { return model.MemInfo{} }

func disks() []model.DiskInfo { return nil }

func gpus() []model.GPUInfo { return nil }

func enrichNIC(name string, nic *model.NICInfo) {}
