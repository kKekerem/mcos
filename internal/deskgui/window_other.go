//go:build !windows

package deskgui

import (
	"errors"
	"os"
	"os/exec"
)

// openWindow: Windows dışında gömülü pencere yok. Linux'ta WebKitGTK gibi
// bir motor CGO ister; dağıtılan ikililer CGO'suz derleniyor.
func openWindow(*Server, RunOptions, <-chan struct{}) (bool, error) {
	return false, nil
}

// openBrowser starts the desktop's browser, if there is a desktop.
//
// Kök olarak ÇALIŞIYORSAK açmayız: mcos-flash.sh sudo ile çalışır ve kök
// hakkıyla açılan bir tarayıcı, kullanıcının profiline kök sahipli dosyalar
// bırakır. O durumda adres yazdırılır, kullanıcı kendi tarayıcısına yapıştırır.
func openBrowser(url string) error {
	if os.Geteuid() == 0 {
		return errors.New("kök hakkıyla tarayıcı açılmaz")
	}
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return errors.New("masaüstü oturumu yok")
	}
	p, err := exec.LookPath("xdg-open")
	if err != nil {
		return err
	}
	c := exec.Command(p, url)
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }()
	return nil
}
