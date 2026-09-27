//go:build linux

package sshd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcos/internal/model"
)

// newTestManager points the shadow file at a temp copy of the image's real
// /etc/shadow; the developer machine's own /etc/shadow is never touched.
func newTestManager(t *testing.T, dataDir string) (*Manager, string) {
	t.Helper()
	shadow := filepath.Join(t.TempDir(), "shadow")
	if err := os.WriteFile(shadow, []byte(buildrootShadow), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(dataDir, nil)
	m.shadowPath = shadow
	return m, shadow
}

// rootHash reads root's hash field from a shadow file.
func rootHash(t *testing.T, shadow string) string {
	t.Helper()
	b, err := os.ReadFile(shadow)
	if err != nil {
		t.Fatal(err)
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if f := strings.Split(ln, ":"); f[0] == "root" && len(f) > 1 {
			return f[1]
		}
	}
	t.Fatal("shadow'da root satırı yok")
	return ""
}

// checks reports whether password verifies against hash the way sshd does:
// crypt(parola, shadow_alanı) == shadow_alanı.
func checks(password, hash string) bool {
	got, err := sha512Crypt(password, hash)
	return err == nil && got == hash
}

// Kullanıcının asıl şikâyeti: panelden parola koyunca "chpasswd bulunamadı".
// Bu sınama imajın durumunu taklit eder: ne sabit dizinlerde ne PATH'te
// harici bir program bulunabilir. Parola yine de konabilmeli.
func TestSetPasswordNeedsNoExternalTool(t *testing.T) {
	t.Setenv("PATH", "")
	old := binDirs
	binDirs = nil
	t.Cleanup(func() { binDirs = old })
	if p := findBin("chpasswd"); p != "" {
		t.Fatalf("düzenek hatası: chpasswd hâlâ bulunuyor (%s)", p)
	}
	m, shadow := newTestManager(t, t.TempDir())
	if err := m.SetPassword("root", "yenisifre123"); err != nil {
		t.Fatalf("parola konamadı: %v", err)
	}
	h := rootHash(t, shadow)
	if !checks("yenisifre123", h) {
		t.Fatalf("shadow'daki özet doğru parolayı doğrulamıyor: %s", h)
	}
	if checks("root", h) || checks("yanlis-sifre", h) {
		t.Fatal("shadow'daki özet YANLIŞ parolayı doğruluyor")
	}
	// Diğer satırlar (özellikle sshd'nin "*" satırı) korunmalı.
	b, _ := os.ReadFile(shadow)
	if !strings.Contains(string(b), "\nsshd:*:::::::\n") {
		t.Fatalf("sshd satırı bozuldu:\n%s", b)
	}
}

// Asıl kalıcılık sınaması: kök RAM'de, yeniden başlatınca /etc/shadow imajın
// hâline ("root" parolası) döner. Kalıcı klasördeki özet SSH açılırken geri
// yazılmalı.
func TestPasswordSurvivesReboot(t *testing.T) {
	data := t.TempDir()
	m, _ := newTestManager(t, data)
	if err := m.SetPassword("root", "yenisifre123"); err != nil {
		t.Fatal(err)
	}
	pf := filepath.Join(data, "ssh", passwordFile)
	st, err := os.Stat(pf)
	if err != nil {
		t.Fatalf("parola kalıcı klasöre yazılmadı: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("kalıcı özet izni %v, 0600 olmalı", st.Mode().Perm())
	}
	if b, _ := os.ReadFile(pf); strings.Contains(string(b), "yenisifre123") {
		t.Fatal("kalıcı dosyada parolanın KENDİSİ var")
	}

	// "Yeniden başlatma": yeni süreç, yeni yönetici, shadow imajın hâlinde.
	m2, shadow2 := newTestManager(t, data)
	if !strings.HasPrefix(rootHash(t, shadow2), "$5$") {
		t.Fatal("düzenek hatası: yeniden başlatma sonrası shadow imajın hâlinde değil")
	}
	if !m2.HasPassword() {
		t.Fatal("yeniden başlatınca parola 'yok' görünüyor")
	}
	ok, err := m2.restorePassword()
	if err != nil || !ok {
		t.Fatalf("kalıcı parola geri yazılamadı: ok=%v err=%v", ok, err)
	}
	h := rootHash(t, shadow2)
	if !checks("yenisifre123", h) {
		t.Fatalf("yeniden başlatmadan sonra parola geçmiyor; shadow: %s", h)
	}
	if strings.HasPrefix(h, "$5$") {
		t.Fatal("imajın varsayılan 'root' parolası hâlâ yerinde")
	}
}

func TestSetPasswordRejects(t *testing.T) {
	m, shadow := newTestManager(t, t.TempDir())
	before, _ := os.ReadFile(shadow)
	for _, c := range []struct{ user, pw string }{
		{"root", ""}, {"root", "iki\nsatir12"}, {"root", "nul\x00karakter"}, {"admin", "yenisifre123"},
	} {
		if err := m.SetPassword(c.user, c.pw); err == nil {
			t.Errorf("%q/%q kabul edildi", c.user, c.pw)
		}
	}
	after, _ := os.ReadFile(shadow)
	if string(before) != string(after) {
		t.Fatal("reddedilen parola shadow'u değiştirdi")
	}
	if m.HasPassword() {
		t.Fatal("reddedilen parola kalıcı olarak kaydedildi")
	}
	// ":" artık serbest: chpasswd'ın "kullanıcı:parola" kısıtı kalktı.
	if err := m.SetPassword("root", "iki:nokta:olur"); err != nil {
		t.Fatalf("iki nokta içeren parola reddedildi: %v", err)
	}
	if !checks("iki:nokta:olur", rootHash(t, shadow)) {
		t.Fatal("iki nokta içeren parola doğrulanmadı")
	}
}

// Bozuk bir kalıcı dosya shadow'a ASLA yazılmamalı ve panel nedenini
// göstermeli.
func TestCorruptStoredHashIsRefused(t *testing.T) {
	data := t.TempDir()
	m, shadow := newTestManager(t, data)
	if err := os.MkdirAll(filepath.Join(data, "ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "ssh", passwordFile), []byte("$6$yarim"), 0o600); err != nil {
		t.Fatal(err)
	}
	if m.HasPassword() {
		t.Fatal("bozuk dosya geçerli parola sayıldı")
	}
	ok, err := m.restorePassword()
	if ok || err == nil {
		t.Fatalf("bozuk özet uygulandı: ok=%v err=%v", ok, err)
	}
	if b, _ := os.ReadFile(shadow); string(b) != buildrootShadow {
		t.Fatal("bozuk özet shadow'a yazıldı")
	}
	st := m.Status(model.SSHConfig{Enabled: true, PasswordSet: true})
	if st.PasswordSet || !strings.Contains(st.Note, "bozuk") {
		t.Fatalf("durum bozuk dosyayı göstermiyor: %+v", st)
	}
}

// Eski sürüm parolayı yalnızca RAM'e yazıyordu: config'te PasswordSet=true
// kalmış ama gerçekte parola yok. Durum config'e değil kalıcı dosyaya
// bakmalı.
func TestStatusPasswordFromStoreNotConfigFlag(t *testing.T) {
	m, _ := newTestManager(t, t.TempDir())
	st := m.Status(model.SSHConfig{PasswordSet: true})
	if st.PasswordSet {
		t.Fatal("kalıcı parola yokken 'parola kurulu' gösteriliyor")
	}
	if !strings.Contains(st.Note, "parolayı yeniden koyun") {
		t.Fatalf("not nedeni söylemiyor: %q", st.Note)
	}
	if err := m.SetPassword("root", "yenisifre123"); err != nil {
		t.Fatal(err)
	}
	if st := m.Status(model.SSHConfig{}); !st.PasswordSet {
		t.Fatal("parola konduktan sonra 'kurulu' görünmüyor")
	}
}

// Ölçülen güvenlik açığı: kalıcı parola yokken sunucu parola girişine açıktı
// ve imajın varsayılan root/root parolası kabul ediliyordu.
func TestSSHDConfigPasswordAuthFollowsStoredPassword(t *testing.T) {
	m, _ := newTestManager(t, t.TempDir())
	if err := os.MkdirAll(m.hostKeyDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		pwAuth bool
		want   string
		bad    string
	}{
		{false, "PasswordAuthentication no\n", "PasswordAuthentication yes"},
		{true, "PasswordAuthentication yes\n", "PasswordAuthentication no"},
	} {
		p, err := m.writeSSHDConfig(2222, []string{"/k"}, c.pwAuth)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(p)
		if !strings.Contains(string(b), c.want) || strings.Contains(string(b), c.bad) {
			t.Errorf("pwAuth=%v için yapılandırma yanlış:\n%s", c.pwAuth, b)
		}
		if !strings.Contains(string(b), "PermitEmptyPasswords no\n") {
			t.Error("PermitEmptyPasswords no kayboldu")
		}
	}
}
