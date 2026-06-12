//go:build windows

package supervisor

import "os"

// terminate on Windows has no SIGTERM equivalent for console children, so we
// fall back to a hard kill. Graceful shutdown for Minecraft servers is handled
// one layer up by writing "stop" to stdin before this is ever reached.
func terminate(p *os.Process) error {
	return p.Kill()
}
