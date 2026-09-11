package server

import (
	"errors"
	"strings"
	"testing"
)

// TestIsServerReadyLine, C2'nin regresyon testi: eskiden `Done (` VE `For help`
// birlikte aranıyordu, bu yüzden "For help" basmayan sürümlerde sunucu
// sonsuza kadar BAŞLIYOR kalıyordu.
func TestIsServerReadyLine(t *testing.T) {
	ready := []string{
		`[12:00:00] [Server thread/INFO]: Done (12.345s)! For help, type "help"`,
		`[Server thread/INFO]: Done (30.1s)!`,                 // Forge — "For help" yok
		`[modloading-worker-0/INFO]: Done (128.402s)!`,        // NeoForge
		`[Server thread/INFO]: Done (5.5s)! For help, type ?`, // farklı ipucu metni
	}
	for _, l := range ready {
		if !isServerReadyLine(l) {
			t.Errorf("isServerReadyLine(%q) = false, true olmalı", l)
		}
	}

	notReady := []string{
		`[Server thread/INFO]: Starting minecraft server version 1.21`,
		`[Server thread/INFO]: Preparing spawn area: 42%`,
		`[Server thread/INFO]: Done loading 12 plugins`, // "Done" var ama "(…)!" yok
		`[Server thread/INFO]: <Ahmet> Done (1s)!`,      // sohbet: parantez var ama Done ( yok
		``,
	}
	for _, l := range notReady {
		if isServerReadyLine(l) {
			t.Errorf("isServerReadyLine(%q) = true, false olmalı", l)
		}
	}
}

// TestIsPlayerEventLine, C4'ün regresyon testi: eskiden strings.Contains
// kullanıldığı için bir oyuncu sohbete "joined the game" yazınca sayaç
// bozuluyordu.
func TestIsPlayerEventLine(t *testing.T) {
	joins := []string{
		`[12:00:00] [Server thread/INFO]: Ahmet joined the game`,
		`[Server thread/INFO]: Notch joined the game`,
		`[Server thread/INFO]: Ahmet joined the game   `, // sondaki boşluk
	}
	for _, l := range joins {
		if !isPlayerEventLine(l, "joined the game") {
			t.Errorf("gerçek katılım olayı kaçırıldı: %q", l)
		}
	}

	chat := []string{
		`[Server thread/INFO]: <Ahmet> ben de joined the game`,
		`[Server thread/INFO]: <Mehmet> joined the game`,
		`[Server thread/INFO]: <troll> Ahmet joined the game`,
	}
	for _, l := range chat {
		if isPlayerEventLine(l, "joined the game") {
			t.Errorf("sohbet satırı katılım sayıldı: %q", l)
		}
	}

	// İfade satırın sonunda değilse olay değildir.
	if isPlayerEventLine(`[INFO]: Ahmet joined the game lobby`, "joined the game") {
		t.Error("ifade satır sonunda olmadığı hâlde olay sayıldı")
	}

	// Ayrılma olayı da aynı kurala uymalı.
	if !isPlayerEventLine(`[INFO]: Ahmet left the game`, "left the game") {
		t.Error("gerçek ayrılma olayı kaçırıldı")
	}
	if isPlayerEventLine(`[INFO]: <Ahmet> left the game`, "left the game") {
		t.Error("sohbet satırı ayrılma sayıldı")
	}
}

// TestStartErrorUserMessage, C1'in testi: kullanıcıya gösterilen mesaj Türkçe
// ve eyleme dönüştürülebilir olmalı; teknik ayrıntı ondan ayrı durmalı.
func TestStartErrorUserMessage(t *testing.T) {
	cause := errors.New("open /data/servers/srv_x/data/.mcos-launch.json: no such file or directory")
	err := startErr(StageLaunch, "Kurulum kaydı okunamadı. Sürümü yeniden kurun.", cause)

	user := UserMessage(err)
	if strings.Contains(user, "no such file") {
		t.Errorf("kullanıcı mesajı teknik ayrıntı içeriyor: %q", user)
	}
	if !strings.Contains(user, "Kurulum kaydı okunamadı") {
		t.Errorf("kullanıcı mesajı ipucu içermiyor: %q", user)
	}
	if !strings.Contains(user, string(StageLaunch)) {
		t.Errorf("kullanıcı mesajı aşamayı belirtmiyor: %q", user)
	}

	// Tam hata metni teknik ayrıntıyı KORUMALI (günlük için).
	if !strings.Contains(err.Error(), "no such file") {
		t.Errorf("hata metni teknik ayrıntıyı kaybetti: %q", err.Error())
	}
	// errors.Is/As zinciri çalışmalı.
	if !errors.Is(err, cause) {
		t.Error("Unwrap zinciri kırılmış")
	}

	// Tipli olmayan hatalar olduğu gibi geçmeli.
	plain := errors.New("server already running")
	if got := UserMessage(plain); got != "server already running" {
		t.Errorf("UserMessage(plain) = %q", got)
	}
	if got := UserMessage(nil); got != "" {
		t.Errorf("UserMessage(nil) = %q, boş olmalı", got)
	}
}
