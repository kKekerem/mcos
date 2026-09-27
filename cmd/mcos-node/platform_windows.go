//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Bu dosya düğümü Windows'ta bir SERVİS gibi davranır kılar:
//
//   - PENCERESİZ çalışabilir (--arkaplan): program GUI alt sistemiyle
//     derlenir (-H windowsgui), yani kendiliğinden konsol açmaz. Kullanıcı
//     çift tıkladığında konsolu programın kendisi açar.
//   - Oturum açılınca kendiliğinden başlar: HKCU\...\Run anahtarı. Yönetici
//     hakkı İSTEMEZ (Görev Zamanlayıcı'nın "oturum açılışında" tetikleyicisi
//     yönetici ister; kullanıcıdan gereksiz yere UAC onayı almamak için Run
//     anahtarı seçildi).
//   - Güvenlik duvarı izni: Windows Defender, 2222/25565/27893 portlarına
//     gelen bağlantıları VARSAYILAN olarak engeller ve kullanıcı bunu
//     yalnızca MCOS'ta "yanıt yok" olarak görür. İlk kurulumda BİR KEZ UAC
//     ile kural eklenir; kurallar yalnızca YEREL AĞDAN (localsubnet) gelen
//     bağlantılara izin verir.
//   - Düğüm kapanınca Java da kapanır: süreç bir İŞ NESNESİNE (Job Object)
//     alınır. Windows, üst süreç ölünce alt süreçleri öldürmez; iş nesnesi
//     olmadan görev yöneticisinden kapatılan bir düğüm, dünyayı kilitli
//     tutan sahipsiz bir java.exe bırakırdı ve bir sonraki açılış "port
//     kullanımda" ile düşerdi.

var (
	kernel32            = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole   = kernel32.NewProc("AttachConsole")
	procAllocConsole    = kernel32.NewProc("AllocConsole")
	procSetConsoleTitle = kernel32.NewProc("SetConsoleTitleW")
	procSetConsoleCP    = kernel32.NewProc("SetConsoleCP")
	shell32             = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteEx  = shell32.NewProc("ShellExecuteExW")
)

// Güvenlik duvarı kural adları. ASCII: netsh bazı dil paketlerinde ASCII
// dışı adları bozuk gösterebiliyor.
const (
	fwRuleTCP = "MCOS Node TCP"
	fwRuleUDP = "MCOS Node UDP"
	runKey    = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue  = "MCOS Node"
)

// platformConsole gives an interactive run a console window.
//
// GUI alt sisteminde konsol YOKTUR. Üç durum:
//   - std tanıtıcıları zaten geçerli (WSL, yönlendirme, betik): onları kullan;
//   - bir konsoldan (cmd, .bat) açıldı: o konsola bağlan;
//   - Explorer'dan çift tıklandı: yeni bir konsol aç.
func platformConsole(background bool) {
	if background {
		return
	}
	if h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil &&
		h != 0 && h != windows.InvalidHandle {
		enableVT(h)
		return
	}
	const attachParentProcess = 0xFFFFFFFF // (DWORD)-1
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r == 0 {
		procAllocConsole.Call()
	}
	if out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		os.Stdout, os.Stderr = out, out
		_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, windows.Handle(out.Fd()))
		_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(out.Fd()))
		enableVT(windows.Handle(out.Fd()))
	}
	if in, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = in
		_ = windows.SetStdHandle(windows.STD_INPUT_HANDLE, windows.Handle(in.Fd()))
	}
	// UTF-8: Türkçe harfler ve çizgi karakterleri 65001 olmadan bozuk çıkar.
	_ = windows.SetConsoleOutputCP(65001)
	procSetConsoleCP.Call(65001)
	if t, err := windows.UTF16PtrFromString("MCOS Düğüm"); err == nil {
		procSetConsoleTitle.Call(uintptr(unsafe.Pointer(t)))
	}
}

// enableVT turns on ANSI escape handling (the status screen redraws in place).
//
// Eski konsol (conhost) bunu varsayılan olarak AÇMAZ; açılmazsa ekran
// "←[1A" gibi çöp karakterlerle dolar.
func enableVT(h windows.Handle) {
	var mode uint32
	if windows.GetConsoleMode(h, &mode) == nil {
		_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|
			windows.ENABLE_PROCESSED_OUTPUT)
	}
}

// jobHandle stays open for the life of the process; closing it (or dying)
// kills every child — that is the whole point.
var jobHandle windows.Handle

// platformJob puts this process (and so every java.exe it starts) in a job
// that is killed when we exit.
func platformJob() {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return
	}
	if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		windows.CloseHandle(job)
		return
	}
	jobHandle = job
}

func setupNeeded(s nodeSettings) bool { return !s.SetupDone }

// programDir is where the node installs itself.
//
// İndirilenler klasöründen çalıştırılan bir programı "oturum açılışında
// başlat"a bağlamak kırılgandır: kullanıcı indirilenleri temizler ve düğüm
// sessizce ölür. Program bu yüzden kendi veri klasörünün yanına kopyalanır.
func programDir() string {
	if d := os.Getenv("LOCALAPPDATA"); d != "" {
		return filepath.Join(d, "MCOS-Node", "program")
	}
	return filepath.Join(exeDir(), "program")
}

// installedExe is the copy that starts at logon (or this one if none).
func installedExe() string {
	p := filepath.Join(programDir(), "mcos-node.exe")
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	exe, _ := os.Executable()
	return exe
}

// runSetup installs the program, the firewall rules and the logon start.
//
// Dönüş: kurulu programın yolu. Adımlardan biri başarısız olsa bile
// diğerleri denenir; hatalar birleştirilip döner, çağıran kullanıcıya söyler.
func runSetup(dataRoot string, port int) (string, error) {
	var errs []string

	exe, err := installProgram()
	if err != nil {
		errs = append(errs, "program kopyalanamadı: "+err.Error())
		exe, _ = os.Executable()
	}

	if !firewallRulesPresent() {
		if err := addFirewallRules(port); err != nil {
			errs = append(errs, "güvenlik duvarı: "+err.Error())
		}
	}

	if err := setRunKey(runKeyCmdline(exe, dataRoot, port)); err != nil {
		errs = append(errs, "oturum açılışında başlatma: "+err.Error())
	}
	if len(errs) > 0 {
		return exe, errors.New(strings.Join(errs, "; "))
	}
	return exe, nil
}

// installProgram copies the exe and the mod jars into programDir.
func installProgram() (string, error) {
	src, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(src); err == nil {
		src = r
	}
	dir := programDir()
	dst := filepath.Join(dir, "mcos-node.exe")
	if strings.EqualFold(filepath.Clean(src), filepath.Clean(dst)) {
		return dst, nil // zaten kurulu yerden çalışıyoruz
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := copyPlain(src, dst); err != nil {
		return "", err
	}
	// Ortak dünya jar'ları programın YANINDAKİ mods/link'te aranır
	// (linkLocator): kopyalanmazsa kurulu program ortak dünyada "index-
	// fabric.tsv yok" der. Kopyalanamaması programı kurmayı engellemez;
	// neden, ortak dünya kurulurken durum satırında görünür.
	_, _ = copyLinkJars(dir)
	return dst, nil
}

func copyPlain(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// runKeyCmdline is the command the Run key starts at logon.
//
// Kurulum (runSetup) ve penceredeki "oturum açılışında başlat" anahtarı AYNI
// satırı yazmalı: ikisi farklı yazsaydı anahtarı kapatıp açmak düğümü başka
// bir veri klasörüyle başlatabilirdi.
func runKeyCmdline(exe, dataRoot string, port int) string {
	args := `"` + exe + `" --arkaplan`
	if dataRoot != defaultDataRoot() {
		args += ` --data "` + dataRoot + `"`
	}
	if port != 2222 {
		args += fmt.Sprintf(" --port %d", port)
	}
	return args
}

// autostartStatus reads the Run key (okumak hiçbir şeyi değiştirmez).
func autostartStatus() autostartState {
	st := autostartState{Supported: true}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return st
	}
	defer k.Close()
	if v, _, err := k.GetStringValue(runValue); err == nil && strings.TrimSpace(v) != "" {
		st.Enabled = true
	}
	return st
}

// setAutostart turns the logon start on or off from the window.
func setAutostart(dataRoot string, port int, on bool) error {
	if !on {
		return deleteRunKey()
	}
	return setRunKey(runKeyCmdline(installedExe(), dataRoot, port))
}

// openFolder shows a folder in Explorer.
//
// explorer.exe başarılı olsa bile 1 ile çıkar; çıkış kodu beklenmez.
func openFolder(path string) error {
	c := exec.Command("explorer.exe", path)
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }()
	return nil
}

var procMessageBox = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")

// guiFatal tells the user why the window could not open.
//
// Pencereli programın konsolu yoktur: stderr'e yazılan hata HİÇBİR YERDE
// görünmez ve kullanıcı "çift tıkladım, hiçbir şey olmadı" der.
func guiFatal(err error) {
	fmt.Fprintln(os.Stderr, "HATA:", err)
	t, _ := windows.UTF16PtrFromString("MCOS Düğüm")
	m, _ := windows.UTF16PtrFromString("Arayüz açılamadı:\n\n" + err.Error() +
		"\n\nKonsol ekranı için: mcos-node.exe --konsol")
	const mbIconError = 0x10
	procMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), mbIconError)
}

func setRunKey(cmdline string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(runValue, cmdline)
}

func deleteRunKey() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(runValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// hiddenCmd runs a console tool without flashing a window.
//
// GUI alt sistemindeki bir programdan netsh çağırmak, her çağrıda bir an
// açılıp kapanan siyah bir pencere gösterirdi.
func hiddenCmd(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true,
		CreationFlags: windows.CREATE_NO_WINDOW}
	return c
}

func firewallRulesPresent() bool {
	for _, r := range []string{fwRuleTCP, fwRuleUDP} {
		if hiddenCmd("netsh", "advfirewall", "firewall", "show", "rule", "name="+r).Run() != nil {
			return false
		}
	}
	return true
}

// firewallPorts lists the inbound ports a node needs.
//
// 2222 eşleştirme, 25565-25600 Minecraft (düğüm dolu portta bir sonrakini
// seçer), 27893-27899 eklentiler arası oyuncu aktarımı. UDP 27891 LAN keşfi.
func firewallPorts(port int) string {
	ports := "2222,25565-25600,27893-27899"
	if port != 2222 && port > 0 {
		ports = fmt.Sprintf("%d,%s", port, ports)
	}
	return ports
}

// addFirewallRules adds the rules, elevating ONCE through UAC if needed.
func addFirewallRules(port int) error {
	if windows.GetCurrentProcessToken().IsElevated() {
		if firewallElevated(firewallPorts(port)) != 0 {
			return errors.New("netsh kuralı ekleyemedi")
		}
		return nil
	}
	code, err := runElevated("--guvenlik-duvari-kur", firewallPorts(port))
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("kural eklenemedi (çıkış kodu %d)", code)
	}
	return nil
}

// firewallElevated runs in the elevated child: add (or with "-" remove) rules.
func firewallElevated(ports string) int {
	if ports == "-" {
		for _, r := range []string{fwRuleTCP, fwRuleUDP} {
			_ = hiddenCmd("netsh", "advfirewall", "firewall", "delete", "rule", "name="+r).Run()
		}
		return 0
	}
	// Önce sil: yeniden kurulumda aynı adlı ikinci bir kural birikmesin.
	for _, r := range []string{fwRuleTCP, fwRuleUDP} {
		_ = hiddenCmd("netsh", "advfirewall", "firewall", "delete", "rule", "name="+r).Run()
	}
	common := []string{"dir=in", "action=allow", "enable=yes", "profile=any",
		// Yalnızca YEREL AĞ: kural internetten gelen bağlantılara kapı açmasın.
		"remoteip=localsubnet"}
	tcp := append([]string{"advfirewall", "firewall", "add", "rule", "name=" + fwRuleTCP,
		"protocol=TCP", "localport=" + ports}, common...)
	udp := append([]string{"advfirewall", "firewall", "add", "rule", "name=" + fwRuleUDP,
		"protocol=UDP", "localport=27891"}, common...)
	if hiddenCmd("netsh", tcp...).Run() != nil || hiddenCmd("netsh", udp...).Run() != nil {
		return 1
	}
	return 0
}

// shellExecuteInfo mirrors SHELLEXECUTEINFOW (amd64 hizalaması Go ile aynı).
type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     windows.Handle
}

// runElevated re-runs this program as administrator and waits for it.
func runElevated(args ...string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return -1, err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	params, _ := windows.UTF16PtrFromString(strings.Join(quoteArgs(args), " "))
	const (
		seeMaskNoCloseProcess = 0x00000040
		seeMaskNoAsync        = 0x00000100
		swHide                = 0
	)
	info := shellExecuteInfo{fMask: seeMaskNoCloseProcess | seeMaskNoAsync,
		lpVerb: verb, lpFile: file, lpParameters: params, nShow: swHide}
	info.cbSize = uint32(unsafe.Sizeof(info))
	if r, _, e := procShellExecuteEx.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		if errors.Is(e, windows.ERROR_CANCELLED) {
			return -1, errors.New("yönetici izni verilmedi (UAC reddedildi)")
		}
		return -1, e
	}
	defer windows.CloseHandle(info.hProcess)
	if _, err := windows.WaitForSingleObject(info.hProcess, 120_000); err != nil {
		return -1, err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.hProcess, &code); err != nil {
		return -1, err
	}
	return int(code), nil
}

func quoteArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = syscall.EscapeArg(a)
	}
	return out
}

// removeSetup undoes runSetup (the data folder is kept: it holds worlds).
func removeSetup(port int) error {
	var errs []string
	if err := deleteRunKey(); err != nil {
		errs = append(errs, "oturum açılışı kaydı: "+err.Error())
	}
	{
		var code int
		var err error
		if windows.GetCurrentProcessToken().IsElevated() {
			code = firewallElevated("-")
		} else {
			code, err = runElevated("--guvenlik-duvari-kur", "-")
		}
		if err != nil || code != 0 {
			errs = append(errs, fmt.Sprintf("güvenlik duvarı kuralları silinemedi (%v)", err))
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// startBackground launches the installed copy without a window.
//
// CREATE_BREAKAWAY_FROM_JOB: bu (görüntüleyici) süreç bir iş nesnesinin
// içindeyse (Windows Terminal, WSL) arka plandaki düğüm onunla birlikte
// ÖLMEMELİ. İzin verilmezse bayraksız yeniden denenir.
func startBackground(exe string, args []string) error {
	try := func(flags uint32) error {
		c := exec.Command(exe, append([]string{"--arkaplan"}, args...)...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags}
		if err := c.Start(); err != nil {
			return err
		}
		return c.Process.Release()
	}
	base := uint32(windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP)
	if err := try(base | windows.CREATE_BREAKAWAY_FROM_JOB); err != nil {
		return try(base)
	}
	return nil
}
