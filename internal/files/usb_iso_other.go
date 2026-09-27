//go:build !linux

package files

import "errors"

// ScanUSBISOs is Linux-only.
func ScanUSBISOs(func(USBISOFile)) error {
	return errors.New("sistem güncellemesi yalnızca MCOS'ta")
}

// WithUSBFile is Linux-only.
func WithUSBFile(string, string, func(string) error) error {
	return errors.New("sistem güncellemesi yalnızca MCOS'ta")
}
