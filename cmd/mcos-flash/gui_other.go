//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
)

// isElevated: Linux'ta ham aygıta kök yazar.
func isElevated() bool { return os.Geteuid() == 0 }

// relaunchElevated: Linux'ta pencere kendini yükseltmez; kök hakkıyla bir
// tarayıcı açmak kullanıcının profilini bozardı (bkz. deskgui.openBrowser).
func relaunchElevated([]string) error {
	return errors.New("Linux'ta kök hakkıyla başlatın: sudo ./mcos-flash --gui")
}

// attachConsole: Linux'ta konsol zaten vardır.
func attachConsole() {}

func guiFatal(err error) { fmt.Fprintln(os.Stderr, "HATA:", err) }
