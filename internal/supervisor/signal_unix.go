//go:build !windows

package supervisor

import (
	"os"
	"syscall"
)

// terminate asks the process to exit gracefully (SIGTERM).
func terminate(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}
