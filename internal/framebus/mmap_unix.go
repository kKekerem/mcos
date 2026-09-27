//go:build unix

package framebus

import (
	"os"

	"golang.org/x/sys/unix"
)

func mmap(f *os.File, n int) ([]byte, error) {
	return unix.Mmap(int(f.Fd()), 0, n, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
}

func munmap(b []byte) error { return unix.Munmap(b) }
