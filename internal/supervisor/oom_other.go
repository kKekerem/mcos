//go:build !linux

package supervisor

func cgroupOOMKills(string) int { return 0 }
func globalOOMKills() int       { return 0 }
