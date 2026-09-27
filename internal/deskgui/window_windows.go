//go:build windows

package deskgui

import (
	"errors"
	"os"
	"runtime"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

var (
	user32                    = windows.NewLazySystemDLL("user32.dll")
	procGetDpiForSystem       = user32.NewProc("GetDpiForSystem")
	procFindWindowW           = user32.NewProc("FindWindowW")
	procDestroyWindow         = user32.NewProc("DestroyWindow")
	procSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	dwmapi                    = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

// openWindow shows the page in a WebView2 window and runs its message loop.
//
// Dönüş (false, nil): WebView2 çalışma zamanı YOK — çağıran tarayıcıya
// düşer. Windows 11'de ve güncel Windows 10'da çalışma zamanı hazır gelir;
// eski/kırpılmış kurulumlarda olmayabilir.
func openWindow(s *Server, o RunOptions, stop <-chan struct{}) (bool, error) {
	// Çalışma zamanı yoksa pencere HİÇ oluşturulmamalı: go-webview2 önce
	// pencereyi açıp sonra motoru gömmeye çalışıyor; motor yoksa ekranda
	// boş, yanıtsız bir pencere kalırdı.
	if v, err := webviewloader.GetInstalledVersion(); err != nil || v == "" {
		return false, nil
	}

	// WebView2 bir COM/pencere nesnesidir: oluşturulduğu iş parçacığında
	// yaşar ve ileti döngüsü AYNI iş parçacığında dönmelidir. Go
	// zamanlayıcısı gorutini başka bir OS iş parçacığına taşırsa pencere
	// donar.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	scale := dpiScale()
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:  webviewDataDir(o.DataDir),
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  o.Title,
			Width:  uint(float64(o.Width) * scale),
			Height: uint(float64(o.Height) * scale),
			IconId: uint(o.IconID),
			Center: true,
		},
	})
	if w == nil {
		// Motor gömülemedi ama pencere açılmış olabilir (bkz. yukarı):
		// kullanıcıya boş bir çerçeve bırakmayalım.
		destroyStray(o.Title)
		return false, errors.New("WebView2 başlatılamadı")
	}
	hwnd := uintptr(w.Window())
	darkTitleBar(hwnd)
	if o.MinWidth > 0 && o.MinHeight > 0 {
		w.SetSize(int(float64(o.MinWidth)*scale), int(float64(o.MinHeight)*scale), webview2.HintMin)
	}
	procSetForegroundWindow.Call(hwnd)
	w.Navigate(s.URL())

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-stop:
			// Destroy yalnızca WM_CLOSE gönderir (PostMessage): başka bir
			// iş parçacığından çağrılması güvenlidir.
			w.Destroy()
		case <-done:
		}
	}()
	w.Run()
	return true, nil
}

// webviewDataDir picks WebView2's profile folder.
func webviewDataDir(dir string) string {
	if dir == "" {
		dir = os.TempDir()
	}
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// dpiScale is the system DPI relative to 96.
//
// Manifest programı DPI-farkında ilan ediyor (go-winres "gui"): Windows
// pencereyi büyütmez, boyutları FİZİKSEL piksel sayar. %150 ölçekli bir
// dizüstünde 960 piksellik pencere küçücük görünürdü.
func dpiScale() float64 {
	if procGetDpiForSystem.Find() != nil {
		return 1
	}
	dpi, _, _ := procGetDpiForSystem.Call()
	if dpi < 96 {
		return 1
	}
	return float64(dpi) / 96
}

// darkTitleBar asks DWM for a dark caption (Windows 10 20H1+ / 11).
//
// Koyu arayüzün üstünde bembeyaz bir başlık çubuğu göze batıyor; özellik
// yoksa çağrı sessizce başarısız olur.
func darkTitleBar(hwnd uintptr) {
	if procDwmSetWindowAttribute.Find() != nil {
		return
	}
	const dwmwaUseImmersiveDarkMode = 20
	on := int32(1)
	procDwmSetWindowAttribute.Call(hwnd, dwmwaUseImmersiveDarkMode,
		uintptr(unsafe.Pointer(&on)), unsafe.Sizeof(on))
}

// destroyStray closes a half-created webview window by class and title.
func destroyStray(title string) {
	cls, _ := windows.UTF16PtrFromString("webview")
	t, _ := windows.UTF16PtrFromString(title)
	if h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(t))); h != 0 {
		procDestroyWindow.Call(h)
	}
}

// openBrowser opens the address in the default browser.
func openBrowser(url string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	u, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, u, nil, nil, windows.SW_SHOWNORMAL)
}
