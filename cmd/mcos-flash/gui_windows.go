//go:build windows

package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole = kernel32.NewProc("AttachConsole")
	procAllocConsole  = kernel32.NewProc("AllocConsole")
	procSetConsoleCP  = kernel32.NewProc("SetConsoleCP")
	procMessageBoxW   = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")
)

// isElevated: ham diske yazmak yönetici hakkı ister.
func isElevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

// relaunchElevated starts this program again through UAC and does not wait.
//
// Pencere yönetici hakkı olmadan da açılır ve aygıtları listeler (sorgu
// sıfır erişim maskesiyle yapılır, bkz. flash.openForQuery). Yazmak için
// hak gerekince kullanıcı düğmeye basar; yükseltilmiş kopya kendi penceresini
// açar, bu kopya kapanır.
func relaunchElevated(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = syscall.EscapeArg(a)
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	params, _ := windows.UTF16PtrFromString(strings.Join(quoted, " "))
	if err := windows.ShellExecute(0, verb, file, params, nil, windows.SW_SHOWNORMAL); err != nil {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return errors.New("yönetici izni verilmedi (UAC reddedildi)")
		}
		return err
	}
	return nil
}

// attachConsole gives the text interface a console.
//
// make flash-windows programı GUI alt sistemiyle (-H windowsgui) derler ki
// çift tıklamada siyah pencere AÇILMASIN. Konsol kipinde ise metnin bir yere
// yazılması gerekir; mcos-node'daki (platform_windows.go platformConsole) ile
// aynı üç durum:
//   - std tanıtıcıları zaten geçerli (yönlendirme, konsol alt sistemi): dokunma;
//   - bir konsoldan (.bat, cmd) açıldı: o konsola bağlan;
//   - çift tıklandı (--konsol ile): yeni konsol aç.
func attachConsole() {
	if h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil &&
		h != 0 && h != windows.InvalidHandle {
		return
	}
	const attachParentProcess = 0xFFFFFFFF
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r == 0 {
		procAllocConsole.Call()
	}
	if out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		os.Stdout, os.Stderr = out, out
		_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, windows.Handle(out.Fd()))
		_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(out.Fd()))
	}
	if in, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = in
		_ = windows.SetStdHandle(windows.STD_INPUT_HANDLE, windows.Handle(in.Fd()))
		// main.go'daki okuyucu paket açılışında ESKİ (geçersiz) os.Stdin ile
		// kuruldu; yenilenmezse onay satırı hiç okunamaz ve konsol akışı
		// "girdi okunamadı" ile düşer.
		stdin = bufio.NewReader(os.Stdin)
	}
	_ = windows.SetConsoleOutputCP(65001)
	procSetConsoleCP.Call(65001)
}

// guiFatal: pencereli programın stderr'i görünmez; hata bir kutuda gösterilir.
func guiFatal(err error) {
	fmt.Fprintln(os.Stderr, "HATA:", err)
	t, _ := windows.UTF16PtrFromString("MCOS USB Kurucu")
	m, _ := windows.UTF16PtrFromString("Arayüz açılamadı:\n\n" + err.Error() +
		"\n\nMetin arayüzü için: mcos-flash.exe --konsol")
	const mbIconError = 0x10
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), mbIconError)
}
