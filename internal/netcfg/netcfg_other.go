//go:build !linux

package netcfg

// Dev-host stubs: MCOS only manages the network on the Linux appliance. On the
// developer's machine these are no-ops so the daemon runs without touching the
// host's real network configuration.

func scan() ([]Network, error) { return nil, nil }

func apply(ssid, pass string) error { return nil }

func applyTimezone(tz string) error { return nil }

func bringUpWired() error { return nil }
