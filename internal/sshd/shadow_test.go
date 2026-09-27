package sshd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildrootShadow, gerçek imajdaki /etc/shadow'un BİREBİR kopyası
// (os/buildroot/output/images/rootfs.cpio.gz içinden çıkarıldı).
const buildrootShadow = `root:$5$Gx8KRqxFYYLq7$zqX2/eZh52nivR14AaBNRNAYMseLgHFCzAqHJgMXUY5:::::::
daemon:*:::::::
bin:*:::::::
sys:*:::::::
sync:*:::::::
mail:*:::::::
www-data:*:::::::
operator:*:::::::
nobody:*:::::::
sshd:*:::::::
`

var testNow = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

func TestReplaceShadowEntryKeepsEverythingElse(t *testing.T) {
	hash := sha512Vectors[0].want
	out, err := replaceShadowEntry(buildrootShadow, "root", hash, testNow)
	if err != nil {
		t.Fatal(err)
	}
	inLines := strings.Split(buildrootShadow, "\n")
	outLines := strings.Split(out, "\n")
	if len(inLines) != len(outLines) {
		t.Fatalf("satır sayısı değişti: %d -> %d", len(inLines), len(outLines))
	}
	for i := 1; i < len(inLines); i++ {
		if inLines[i] != outLines[i] {
			t.Errorf("başka bir satır değişti: %q -> %q", inLines[i], outLines[i])
		}
	}
	f := strings.Split(outLines[0], ":")
	if len(f) != shadowFields {
		t.Fatalf("root satırı %d alan, %d olmalı: %q", len(f), shadowFields, outLines[0])
	}
	if f[0] != "root" || f[1] != hash {
		t.Fatalf("root satırı yanlış: %q", outLines[0])
	}
	// 2026-09-26 = 20722. gün (1970-01-01'den beri).
	if f[2] != "20722" {
		t.Fatalf("son değişim günü %q, 20722 olmalı", f[2])
	}
	for _, x := range f[3:] {
		if x != "" {
			t.Fatalf("yaşlanma alanları boş kalmalı: %q", outLines[0])
		}
	}
	if !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") {
		t.Fatalf("dosya sonu bozuldu: %q", out[len(out)-5:])
	}
}

// OpenSSH (auth-shadow.c) sp_lstchg == 0'ı "kök zorla değiştirtiyor" sayar ve
// girişte parola değişimi ister. Saati 1970'te duran bir makinede alan 0
// yerine BOŞ yazılmalı.
func TestLastChangeNeverZero(t *testing.T) {
	for _, now := range []time.Time{time.Unix(0, 0), time.Unix(3600, 0), time.Unix(-86400*3, 0)} {
		out, err := replaceShadowEntry("root:x:::::::\n", "root", "h", now)
		if err != nil {
			t.Fatal(err)
		}
		if f := strings.Split(strings.TrimSpace(out), ":"); f[2] != "" {
			t.Errorf("saat %v iken son değişim %q yazıldı; OpenSSH zorunlu değişim ister", now, f[2])
		}
	}
}

func TestReplaceShadowEntryPadsAndAppends(t *testing.T) {
	// Eksik alanlı satır 9 alana tamamlanmalı; min/max gibi mevcut alanlar
	// korunmalı.
	out, err := replaceShadowEntry("root:eski:1:0:99999", "root", "yeni", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if out != "root:yeni:20722:0:99999::::\n" {
		t.Fatalf("beklenmeyen satır: %q", out)
	}
	// Kullanıcı yoksa sona eklenmeli.
	out, err = replaceShadowEntry("daemon:*:::::::\n", "root", "h", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if out != "daemon:*:::::::\nroot:h:20722::::::\n" {
		t.Fatalf("beklenmeyen içerik: %q", out)
	}
	// Boş dosya.
	out, _ = replaceShadowEntry("", "root", "h", testNow)
	if out != "root:h:20722::::::\n" {
		t.Fatalf("boş dosyadan beklenmeyen içerik: %q", out)
	}
}

func TestReplaceShadowEntryRejectsFieldBreakers(t *testing.T) {
	for _, c := range []struct{ user, hash string }{
		{"root", "a:b"}, {"root", "a\nb"}, {"root", ""}, {"ro:ot", "h"}, {"", "h"},
	} {
		if _, err := replaceShadowEntry(buildrootShadow, c.user, c.hash, testNow); err == nil {
			t.Errorf("%q/%q kabul edildi — shadow alanları kayardı", c.user, c.hash)
		}
	}
}

func TestWriteShadowHashAtomicAndKeepsMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "shadow")
	if err := os.WriteFile(p, []byte(buildrootShadow), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := writeShadowHash(p, "root", "h", testNow); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o640 {
		t.Fatalf("izin değişti: %v", st.Mode().Perm())
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("geçici dosya arkada kaldı: %v", ents)
	}

	// Dosya yoksa 0600 ile oluşturulmalı.
	p2 := filepath.Join(dir, "yeni-shadow")
	if err := writeShadowHash(p2, "root", "h", testNow); err != nil {
		t.Fatal(err)
	}
	st, _ = os.Stat(p2)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("yeni shadow izni %v, 0600 olmalı", st.Mode().Perm())
	}
}

func TestVolatileMount(t *testing.T) {
	const live = `rootfs / rootfs rw,size=1498460k,nr_inodes=374615 0 0
proc /proc proc rw,relatime 0 0
tmpfs /run tmpfs rw,nosuid,nodev,mode=755 0 0
`
	const persisted = live + "/dev/vda /data ext4 rw,relatime 0 0\n"
	const tmpData = live + "tmpfs /data tmpfs rw 0 0\n"
	const spaced = live + "/dev/sdb2 /media/my\\040disk ext4 rw 0 0\n"

	for _, c := range []struct {
		mounts, dir string
		want        bool
	}{
		{live, "/data", true},                                     // MCOS-DATA bulunamadı: /data RAM'de
		{persisted, "/data", false},                               // kalıcı bölüm bağlı
		{persisted, "/data/ssh", false},                           // alt klasör de kalıcı
		{persisted, "/data2/ssh", true},                           // önek benzerliği eşleşme sayılmamalı
		{tmpData, "/data/ssh", true},                              // /data'ya tmpfs bağlanmış
		{spaced, "/media/my disk/x", false},                       // \040 kaçışı çözülmeli
		{persisted + "tmpfs /data tmpfs rw 0 0\n", "/data", true}, // üst üste bağlamada sonuncusu geçerli
	} {
		if got := volatileMount(c.mounts, c.dir); got != c.want {
			t.Errorf("%s: %v, %v olmalı", c.dir, got, c.want)
		}
	}
}

func TestTailBufferKeepsCause(t *testing.T) {
	var sink strings.Builder
	tb := newTailBuffer(&sink, 128)
	tb.Write([]byte(strings.Repeat("Accepted password for root\n", 20)))
	tb.Write([]byte("Bind to port 22 on 0.0.0.0 failed: Address already in use.\r\n"))
	tb.Write([]byte("Cannot bind any address.\n\n"))
	want := "Bind to port 22 on 0.0.0.0 failed: Address already in use. / Cannot bind any address."
	if got := tb.LastLines(2); got != want {
		t.Fatalf("\n got  %q\n want %q", got, want)
	}
	if len(tb.buf) > 128 {
		t.Fatalf("tampon sınırı aşıldı: %d", len(tb.buf))
	}
	if !strings.Contains(sink.String(), "Cannot bind") {
		t.Fatal("çıktı asıl hedefe (mcosd günlüğü) iletilmedi")
	}
}
