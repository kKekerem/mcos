// Package netcfg applies network configuration (Wi-Fi association, DHCP, and
// timezone) on the MCOS appliance. The real work is Linux-only and shells out
// to the tools shipped in the Buildroot rootfs (wpa_supplicant / wpa_cli /
// udhcpc). On the Windows/macOS dev host every entry point is a safe no-op so
// the daemon still builds and runs for local testing.
package netcfg

// Network is one scanned wireless access point.
type Network struct {
	SSID    string `json:"ssid"`
	Signal  int    `json:"signal"` // 0-100 percent
	Secured bool   `json:"secured"`
}

// Scan returns nearby wireless networks (best signal first). On dev hosts or
// when no wireless interface/tooling exists it returns an empty list and nil.
func Scan() ([]Network, error) { return scan() }

// Apply associates with ssid using pass (empty = open network), brings up the
// interface with DHCP, and persists the credentials so the link is restored on
// boot. No-op on non-Linux hosts.
func Apply(ssid, pass string) error { return apply(ssid, pass) }

// ApplyTimezone points /etc/localtime at the given IANA zone (e.g.
// "Europe/Istanbul"). No-op on non-Linux hosts or when tz is empty.
func ApplyTimezone(tz string) error { return applyTimezone(tz) }

// BringUpWired brings up wired (non-loopback, non-wireless) interfaces and
// requests a DHCP lease in the background. Best-effort and non-blocking: missing
// interfaces/tools are ignored, so a Wi-Fi-only or offline box is unaffected.
// No-op on non-Linux hosts. Used by the OOBE / "Donanım" tab "connect wired"
// action and at startup — never at a point that could block boot.
func BringUpWired() error { return bringUpWired() }
