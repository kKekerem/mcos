package ipc

import "os"

// removeIfSocket deletes path only if it exists and is a Unix socket, so we
// clean up a stale socket from a crashed run without ever clobbering a real
// file by accident.
func removeIfSocket(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return nil // nothing there
	}
	if fi.Mode()&os.ModeSocket != 0 {
		return os.Remove(path)
	}
	return nil
}
