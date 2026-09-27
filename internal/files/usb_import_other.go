//go:build !linux

package files

import (
	"errors"

	"mcos/internal/model"
)

// ScanUSBServerFolders is Linux-only.
func ScanUSBServerFolders() ([]model.USBServerFolder, error) {
	return nil, errors.New("USB'den aktarma yalnızca MCOS'ta")
}

// CopyUSBFolder is Linux-only.
func CopyUSBFolder(dev, rel, dest string) (ServerDirInfo, error) {
	return ServerDirInfo{}, errors.New("USB'den aktarma yalnızca MCOS'ta")
}
