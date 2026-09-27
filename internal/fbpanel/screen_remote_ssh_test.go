package fbpanel

import (
	"strings"
	"testing"
	"unicode/utf8"

	"mcos/internal/ipcclient"
)

// Ölçülen sorun: sunucu çöktüğünde pencere yalnızca "açık ama çalışmıyor"
// diyordu; daemon'un gönderdiği NEDEN (Note) hiç çizilmiyordu.
func TestSSHModalShowsDaemonReason(t *testing.T) {
	st := ipcclient.SSHStatus{
		Available: true, Enabled: true, Running: false, PasswordSet: true, Port: 22,
		Note: "sunucu durdu: Bind to port 22 on 0.0.0.0 failed: Address already in use. / " +
			"Cannot bind any address. (exit status 255); kalıcı depolama yok: parola ve " +
			"anahtarlar yeniden başlatınca silinir",
	}
	lines := sshModalLines(st)
	joined := strings.Join(lines, " ")
	for _, want := range []string{"Address already in use", "Cannot bind any address", "kalıcı depolama yok"} {
		if !strings.Contains(strings.Join(strings.Fields(joined), " "), want) {
			t.Errorf("neden pencerede yok: %q\n%s", want, strings.Join(lines, "\n"))
		}
	}
	for _, l := range lines {
		if utf8.RuneCountInString(l) > 48 {
			t.Errorf("satır pencereden taşar (%d): %q", utf8.RuneCountInString(l), l)
		}
	}
}

// Parola yok + anahtar var: daemon parola girişini kapatıyor. Kullanıcı
// parolayla denediğinde neden reddedildiğini pencerede görmeli.
func TestSSHModalExplainsKeyOnlyLogin(t *testing.T) {
	lines := strings.Join(sshModalLines(ipcclient.SSHStatus{
		Available: true, Enabled: true, Running: true, Keys: 1, User: "root",
		Addresses: []string{"10.0.2.15"}, Port: 22,
	}), "\n")
	if !strings.Contains(lines, "yalnızca açık anahtarla") {
		t.Fatalf("anahtar-yalnız giriş açıklanmıyor:\n%s", lines)
	}
	if !strings.Contains(lines, "ssh root@10.0.2.15") {
		t.Fatalf("bağlantı komutu yok:\n%s", lines)
	}
}

// Parolasız ve anahtarsız "SSH'ı aç" daemon'da reddedilir; pencere bunun
// yerine parolayı soran birleşik eylemi sunmalı.
func TestSSHActionsOfferPasswordFirst(t *testing.T) {
	values := func(st ipcclient.SSHStatus) string {
		var v []string
		for _, it := range sshActions(st) {
			v = append(v, it.Value.(string))
		}
		return strings.Join(v, ",")
	}
	if got := values(ipcclient.SSHStatus{Available: true}); got != "password,password+enable" {
		t.Fatalf("parolasız/anahtarsız: %s", got)
	}
	if got := values(ipcclient.SSHStatus{Available: true, PasswordSet: true}); got != "password,enable" {
		t.Fatalf("parolalı: %s", got)
	}
	if got := values(ipcclient.SSHStatus{Available: true, Keys: 1}); got != "password,enable" {
		t.Fatalf("anahtarlı: %s", got)
	}
	if got := values(ipcclient.SSHStatus{Available: true, Enabled: true}); got != "password,disable" {
		t.Fatalf("açık: %s", got)
	}
}

func TestSSHWrapKeepsEveryWord(t *testing.T) {
	s := "! açık ama çalışmıyor: Bind to port 22 on 0.0.0.0 failed: Address already in use. " +
		strings.Repeat("x", 70)
	out := sshWrap(s, 20)
	if strings.Join(strings.Fields(strings.Join(out, " ")), "") != strings.Join(strings.Fields(s), "") {
		t.Fatalf("sarmada kelime kayboldu:\n%q", out)
	}
	for _, l := range out {
		if utf8.RuneCountInString(l) > 20 {
			t.Fatalf("satır %d rune: %q", utf8.RuneCountInString(l), l)
		}
	}
}
