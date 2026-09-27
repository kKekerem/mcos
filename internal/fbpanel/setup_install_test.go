package fbpanel

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ════════════════════════════════════════════════════════════════════════════
// PANEL ↔ mcos-install ARGÜMAN SÖZLEŞMESİ
// ════════════════════════════════════════════════════════════════════════════
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
//
// Panel `mcos-install --device /dev/sda --yes` çağırıyordu; betik ise hedefi
// KONUMSAL okuyordu (`TARGET="${1:-}"`). Hedef aygıt "--device" oluyor, betik
// hemen "geçersiz blok aygıtı: --device" deyip 1 ile çıkıyordu. Diske hiçbir
// şey yazılmıyordu ve kullanıcının gördüğü tek şey "hata kodu 1" idi.
//
// Bu iki taraf ayrı ayrı yazıldığı için derleyici hiçbir şey yakalayamaz.
// Sınama bu yüzden BETİĞİ GERÇEKTEN ÇALIŞTIRIYOR: tek gerçek kanıt budur.
//
// Yıkıcı bir şey olmaz — var olmayan bir aygıt veriliyor ve betik daha ilk
// `[ -b ]` denetiminde, tek bir bayt yazmadan çıkıyor.

const sahteAygit = "/dev/mcos-boyle-bir-aygit-yok"

func installScriptPath(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "os", "buildroot", "external", "board",
		"mcos", "rootfs-overlay", "usr", "bin", "mcos-install")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("mcos-install betiği yok (%v)", err)
	}
	return p
}

// TestKurulumArgumanlariBetikleUyumlu, panelin verdiği argümanların betik
// tarafından HEDEF AYGIT olarak anlaşıldığını kanıtlıyor.
func TestKurulumArgumanlariBetikleUyumlu(t *testing.T) {
	betik := installScriptPath(t)

	args := append([]string{betik}, installArgs(sahteAygit)...)
	out, err := exec.Command("sh", args...).CombinedOutput()
	metin := string(out)

	if err == nil {
		t.Fatalf("var olmayan aygıtla başarı bekleniyordu değil: %s", metin)
	}
	// Betik hedefi DOĞRU okuduysa şikâyeti aygıtın kendisi hakkındadır.
	if !strings.Contains(metin, sahteAygit) {
		t.Fatalf("betik hedefi okuyamadı.\nargümanlar: %v\nçıktı: %s",
			installArgs(sahteAygit), metin)
	}
	// Bayrak adının hedef sanılması TAM OLARAK eski hataydı.
	for _, bayrak := range []string{"--device", "--yes", "bilinmeyen seçenek"} {
		if strings.Contains(metin, "geçersiz blok aygıtı: "+bayrak) ||
			strings.Contains(metin, "bilinmeyen seçenek") {
			t.Fatalf("betik argümanı bayrak olarak reddetti (%s): %s", bayrak, metin)
		}
	}
}

// TestKurulumBetigiHerIkiBicimiKabulEder, panel ile betiğin ayrı ayrı
// güncellenebildiği durumu koruyor: eski panel bayraklı, yeni panel konumsal
// çağırıyor ve İKİSİ DE çalışmalı.
func TestKurulumBetigiHerIkiBicimiKabulEder(t *testing.T) {
	betik := installScriptPath(t)

	for _, tc := range []struct {
		ad   string
		args []string
	}{
		{"konumsal", []string{sahteAygit}},
		{"eski panel bayrakları", []string{"--device", sahteAygit, "--yes"}},
		{"eşittirli", []string{"--device=" + sahteAygit}},
	} {
		out, err := exec.Command("sh",
			append([]string{betik}, tc.args...)...).CombinedOutput()
		if err == nil {
			t.Fatalf("%s: hata bekleniyordu", tc.ad)
		}
		if !strings.Contains(string(out), sahteAygit) {
			t.Errorf("%s: hedef okunamadı: %s", tc.ad, out)
		}
	}
}

// TestKurulumBetigiHedefsizCagriyiReddeder: argümansız çağrı diski SİLMEMELİ.
// Betik burada bir aygıt "tahmin" etmeye kalkarsa yanlış diski biçimlendirir.
func TestKurulumBetigiHedefsizCagriyiReddeder(t *testing.T) {
	betik := installScriptPath(t)
	out, err := exec.Command("sh", betik).CombinedOutput()
	if err == nil {
		t.Fatalf("argümansız çağrı başarılı oldu: %s", out)
	}
	if !strings.Contains(string(out), "Kullanım") {
		t.Errorf("kullanım mesajı bekleniyordu: %s", out)
	}
}

// ════════════════════════════════════════════════════════════════════════════
// BAŞARISIZ KURULUMDA GERÇEK SEBEP (yalnızca "exit status 1" değil)
// ════════════════════════════════════════════════════════════════════════════

// betikCalistirici, installRunner'ı depodaki GERÇEK mcos-install betiğine
// yönlendirir (sh ile; PATH'e kurulu olması gerekmez).
func betikCalistirici(t *testing.T) {
	t.Helper()
	betik := installScriptPath(t)
	eski := installRunner
	installRunner = func(_ string, args ...string) *exec.Cmd {
		return exec.Command("sh", append([]string{betik}, args...)...)
	}
	t.Cleanup(func() { installRunner = eski })
	eskiDurum := installStatusFile
	installStatusFile = filepath.Join(t.TempDir(), "durum")
	t.Cleanup(func() { installStatusFile = eskiDurum })
}

// TestBasarisizKurulumGercekSebebiVerir: gerçek betik sahte aygıtla
// çalıştırılır; panelin göstereceği satırlar betiğin HATA ve ÇÖZÜM
// satırlarıdır, "exit status" DEĞİL.
func TestBasarisizKurulumGercekSebebiVerir(t *testing.T) {
	betikCalistirici(t)
	out, err := runInstaller(sahteAygit, nil)
	if err == nil {
		t.Fatalf("sahte aygıtla başarı: %s", out)
	}
	satirlar := parseInstallFailure(out, err)
	hepsi := strings.Join(satirlar, "\n")
	if !strings.HasPrefix(satirlar[0], "Hata: ") || !strings.Contains(satirlar[0], sahteAygit) {
		t.Fatalf("ilk satır betiğin HATA satırı olmalı:\n%s\nçıktı:\n%s", hepsi, out)
	}
	if !strings.Contains(hepsi, "Ne yapmalı: ") {
		t.Errorf("ne yapılacağı yok:\n%s", hepsi)
	}
	if strings.Contains(hepsi, "exit status") {
		t.Errorf("kullanıcıya yine 'exit status' gösteriliyor:\n%s", hepsi)
	}
}

// TestKurulumHataAyiklama: betiğin tam hata bloğu (QEMU'da ölçülen parted
// hatası) dört satıra ayrılır.
func TestKurulumHataAyiklama(t *testing.T) {
	out := `>> mcos-install: [1/7] Bölüm tablosu oluşturuluyor: /dev/sda
>> mcos-install: HATA: bölüm tablosu oluşturulamadı
mcos-install başarısız
HATA: bölüm tablosu oluşturulamadı
NEDEN: Error: Partition(s) 1 on /dev/sda have been written, but we have been unable to inform the kernel of the change
ÇÖZÜM: Diski çıkarıp yeniden takın ve tekrar deneyin.
GÜNLÜK: /data/log/install-20260926-101010.log
`
	got := parseInstallFailure(out, &exec.ExitError{})
	want := []string{
		"Hata: bölüm tablosu oluşturulamadı",
		"Neden: Error: Partition(s) 1 on /dev/sda have been written, but we have been unable to inform the kernel of the change",
		"Ne yapmalı: Diski çıkarıp yeniden takın ve tekrar deneyin.",
		"Tam günlük: /data/log/install-20260926-101010.log",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestKurulumHataYedekYolu: yapılandırılmış satır yoksa (eski betik) son
// satırlar ve çıkış kodu gösterilir; boş çıktıda bile açıklama vardır.
func TestKurulumHataYedekYolu(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 1").Run()
	got := strings.Join(parseInstallFailure(">> mcos-install: a\nb\nc\nmcos-install başarısız: kök bölümü oluşturulamadı\n", err), "\n")
	if !strings.Contains(got, "kök bölümü oluşturulamadı") || !strings.Contains(got, "çıkış kodu 1") {
		t.Fatalf("yedek yol: %s", got)
	}
	if got := parseInstallFailure("", err); len(got) == 0 || got[0] == "" {
		t.Fatalf("boş çıktıda açıklama yok: %v", got)
	}
}

// TestKurulumIlerlemesiOkunur: betiğin durum dosyası ilerleme olarak iletilir.
func TestKurulumIlerlemesiOkunur(t *testing.T) {
	durum := filepath.Join(t.TempDir(), "durum")
	eskiDurum, eskiR := installStatusFile, installRunner
	t.Cleanup(func() { installStatusFile, installRunner = eskiDurum, eskiR })
	installStatusFile = durum
	installRunner = func(string, ...string) *exec.Cmd {
		return exec.Command("sh", "-c", "printf '35|[4/7] Çekirdek kopyalanıyor\\n' > '"+durum+"'; sleep 2")
	}
	var gorulen []string
	if _, err := runInstaller("/dev/x", func(m string) { gorulen = append(gorulen, m) }); err != nil {
		t.Fatal(err)
	}
	if len(gorulen) == 0 || gorulen[0] != "%35 · [4/7] Çekirdek kopyalanıyor" {
		t.Fatalf("ilerleme: %q", gorulen)
	}
	if formatInstallStatus("HATA|x") != "" {
		t.Errorf("HATA satırı ilerleme sayıldı")
	}
}

// TestBasarisizKurulumSayfadaCizilir: sebep satırları sayfada ÇİZİLİYOR mu?
// Eskiden installMsg başarısızlıkta hiç çizilmiyordu. Hata rengi pikselleri
// sayılır: hata yokken (neredeyse) sıfır, varken belirgin.
func TestBasarisizKurulumSayfadaCizilir(t *testing.T) {
	a, img := newTestApp(t)
	a.StartSetup()
	s := a.setupState()
	a.mu.Lock()
	s.step = stepInstall
	a.mu.Unlock()

	say := func() int {
		for i := range img.Pix {
			img.Pix[i] = 0
		}
		a.Draw()
		e := a.ui.Pal.Error
		n := 0
		for i := 0; i < len(img.Pix); i += 4 {
			if img.Pix[i] == e.R && img.Pix[i+1] == e.G && img.Pix[i+2] == e.B {
				n++
			}
		}
		return n
	}
	once := say()
	a.mu.Lock()
	s.installErr = []string{"Hata: bölüm tablosu oluşturulamadı", "Ne yapmalı: Diski çıkarıp takın."}
	a.dirty = true
	a.mu.Unlock()
	sonra := say()
	if sonra < once+200 {
		t.Fatalf("hata satırları çizilmedi (hata rengi piksel: önce %d, sonra %d)", once, sonra)
	}
}
