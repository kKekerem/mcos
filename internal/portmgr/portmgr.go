// Package portmgr handles port assignment for servers: finding a free port,
// detecting conflicts with other configured servers, and probing whether a
// port is actually bindable on the host.
package portmgr

import (
	"fmt"
	"net"

	"mcos/internal/model"
)

// DefaultPort is the canonical Minecraft port.
const DefaultPort = 25565

// IsBindable reports whether tcp port can currently be bound on all interfaces.
func IsBindable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// UsedPorts returns the set of ports already assigned to servers (excluding the
// server with excludeID, so a server can keep its own port on update).
func UsedPorts(servers []*model.Server, excludeID string) map[int]bool {
	used := map[int]bool{}
	for _, s := range servers {
		if s.ID == excludeID {
			continue
		}
		used[s.Port] = true
	}
	return used
}

// FindFree returns a usable port: the preferred one if free+bindable, otherwise
// the next free port scanning upward from DefaultPort, finally an OS-assigned
// ephemeral port as a last resort.
func FindFree(preferred int, used map[int]bool) (int, error) {
	if preferred > 0 && !used[preferred] && IsBindable(preferred) {
		return preferred, nil
	}
	for p := DefaultPort; p < DefaultPort+500; p++ {
		if !used[p] && IsBindable(p) {
			return p, nil
		}
	}
	// Ask the OS for any free port.
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, fmt.Errorf("portmgr: no free port available: %w", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// Conflict reports the id of another server using the same port, or "".
func Conflict(servers []*model.Server, port int, excludeID string) string {
	for _, s := range servers {
		if s.ID != excludeID && s.Port == port {
			return s.ID
		}
	}
	return ""
}
