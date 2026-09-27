//go:build !unix

package framebus

import (
	"errors"
	"os"
)

func mmap(*os.File, int) ([]byte, error) { return nil, errors.New("framebus: yalnızca Unix") }
func munmap([]byte) error                { return nil }
