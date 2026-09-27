package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mcos/internal/ipc"
)

func TestCompareBuild(t *testing.T) {
	cur := "20260927-1715-aaaaaaaa"
	for _, c := range []struct{ cand, want string }{
		{"", ipc.UpdateInvalid},
		{cur, ipc.UpdateSame},
		{"20261001-0900-bbbbbbbb", ipc.UpdateNewer},
		{"20260101-0900-cccccccc", ipc.UpdateOlder},
		{"20260927-1715-dddddddd", ipc.UpdateNewer}, // aynı dakika, farklı derleme
	} {
		if got := compareBuild(cur, c.cand); got != c.want {
			t.Errorf("compareBuild(%q, %q) = %q, want %q", cur, c.cand, got, c.want)
		}
	}
	// Kimliksiz (güncelleme sisteminden önceki) kurulum: her kimlikli ISO yeni.
	if got := compareBuild("", "20260101-0000-x"); got != ipc.UpdateNewer {
		t.Errorf("kimliksiz sistem: %q", got)
	}
}

func TestRootIsLive(t *testing.T) {
	dir := t.TempDir()
	old := mountInfoFile
	defer func() { mountInfoFile = old }()
	mountInfoFile = filepath.Join(dir, "mountinfo")

	write := func(s string) {
		if err := os.WriteFile(mountInfoFile, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("1 1 0:2 / / rw - rootfs rootfs rw\n22 1 0:20 / /proc rw - proc proc rw\n")
	if !rootIsLive() {
		t.Error("rootfs kök canlı sayılmalı")
	}
	write("20 1 8:2 / / rw,noatime - ext4 /dev/sda2 rw\n21 20 8:3 / /data rw - ext4 /dev/sda3 rw\n")
	if rootIsLive() {
		t.Error("ext4 (8:2) kök kurulu sayılmalı")
	}
}

func TestParseUpdateStatus(t *testing.T) {
	if st := parseUpdateStatus("25|3/4 Yeni sistem yazılıyor...\n"); st.Percent != 25 || st.Message != "3/4 Yeni sistem yazılıyor..." {
		t.Errorf("ilerleme: %+v", st)
	}
	if st := parseUpdateStatus("TAMAM|Güncelleme hazır"); !st.Done || st.Percent != 100 {
		t.Errorf("tamam: %+v", st)
	}
	if st := parseUpdateStatus("HATA|yer yok"); !st.Failed || st.Message != "yer yok" {
		t.Errorf("hata: %+v", st)
	}
}

func TestUpdateFailureLines(t *testing.T) {
	out := ">> mcos-update: 1/4 ISO doğrulanıyor...\nHATA: açılış bölümünde yer yok\nNEDEN: boş 10 MiB\nÇÖZÜM: yeniden kurun\n"
	got := updateFailureLines(out, errors.New("exit status 1"))
	want := []string{"Hata: açılış bölümünde yer yok", "Neden: boş 10 MiB", "Ne yapmalı: yeniden kurun"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("satırlar = %q", got)
	}
	// Çıktı yoksa "exit status 1" TEK BAŞINA değil, bağlamıyla.
	got = updateFailureLines("", errors.New("dosya bulunamadı"))
	if len(got) != 1 || !strings.Contains(got[0], "dosya bulunamadı") {
		t.Errorf("çıktısız hata = %q", got)
	}
}

func TestSortUpdateItems(t *testing.T) {
	items := []ipc.UpdateISO{
		{Name: "bozuk.iso", Compare: ipc.UpdateInvalid},
		{Name: "eski.iso", Compare: ipc.UpdateOlder, BuildID: "20250101-0000-a"},
		{Name: "yeni1.iso", Compare: ipc.UpdateNewer, BuildID: "20261001-0000-a"},
		{Name: "yeni2.iso", Compare: ipc.UpdateNewer, BuildID: "20261101-0000-a"},
		{Name: "ayni.iso", Compare: ipc.UpdateSame, BuildID: "20260927-0000-a"},
	}
	sortUpdateItems(items)
	var names []string
	for _, it := range items {
		names = append(names, it.Name)
	}
	if got := strings.Join(names, ","); got != "yeni2.iso,yeni1.iso,ayni.iso,eski.iso,bozuk.iso" {
		t.Errorf("sıra = %s", got)
	}
}

// TestInspectISO: betiğin --denetle çıktısı ayrıştırılır; hata satırı
// kullanıcıya gösterilecek sebep olur.
func TestInspectISO(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh betiği")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "mcos-update")
	body := "#!/bin/sh\ncase \"$2\" in\n*iyi.iso) printf 'KIMLIK=20261001-1200-abcd\\nSURUM=1.0.2\\n' ;;\n" +
		"*) echo 'HATA: Bu ISO güncellemeyi desteklemiyor' >&2; exit 1 ;;\nesac\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	old := updateScript
	defer func() { updateScript = old }()
	updateScript = script

	id, ver, err := inspectISO(context.Background(), "/x/iyi.iso")
	if err != nil || id != "20261001-1200-abcd" || ver != "1.0.2" {
		t.Errorf("iyi: id=%q ver=%q err=%v", id, ver, err)
	}
	_, _, err = inspectISO(context.Background(), "/x/eski.iso")
	if err == nil || !strings.Contains(err.Error(), "desteklemiyor") {
		t.Errorf("eski: err=%v", err)
	}
}
