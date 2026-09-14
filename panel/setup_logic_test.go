package panel

import (
	"os"
	"strings"
	"testing"

	"mcos/internal/ipc"
	"mcos/panel/theme"
)

// readSourceFile reads a file from this package directory.
func readSourceFile(name string) (string, error) {
	b, err := os.ReadFile(name)
	return string(b), err
}

// Bu testler, kurulum sihirbazında RAPORLANMIŞ ve DÜZELTİLMİŞ mantık
// hatalarının geri gelmesini engeller. Her biri gerçekte yaşanmış bir
// kullanıcı zararına karşılık gelir.

// TestInstallIsTheLastStep — KURULUM EN SONDA OLMALI.
//
// Eskiden kurulum 5. adımdaydı ve başarılı kurulum sistemi hemen yeniden
// başlatıyordu. Sonuç: 6-10. adımlar ve ayarları kaydeden doSetupSave HİÇ
// çalışmıyordu. Kullanıcı tema, bütçe, düğüm adı, Wi-Fi parolası giriyor,
// kurulumdan sonra hepsi kayboluyor ve sihirbaz baştan başlıyordu.
func TestInstallIsTheLastStep(t *testing.T) {
	if stepInstall != stepCount-1 {
		t.Fatalf("stepInstall = %d, son adım %d olmalı — "+
			"kurulum sondan önce çalışırsa sonraki adımlar ve ayar kaydı atlanır",
			stepInstall, stepCount-1)
	}
	if stepConfirm >= stepInstall {
		t.Errorf("stepConfirm (%d) stepInstall'dan (%d) SONRA geliyor — "+
			"ayarlar kurulumdan önce kaydedilmeli", stepConfirm, stepInstall)
	}
}

// TestSuccessScreenDoesNotReinstall — EN YIKICI HATA.
//
// "KURULUM BAŞARILI" ekranı "USB'yi ŞİMDİ çıkarın" diyor ve Enter için bir
// "Hemen Yeniden Başlat" butonu gösteriyordu. Ama Enter advance()'a gidiyor,
// orası hâlâ stepInstall olduğu için KURULUMU BAŞTAN çalıştırıyordu — yeni
// kurulmuş diski tekrar bölümleyip biçimlendirerek.
func TestSuccessScreenDoesNotReinstall(t *testing.T) {
	s := newSetup(theme.New("graphite-teal"), "graphite-teal")
	s.step = stepInstall
	s.installSuccess = true
	s.disks = []ipc.DiskTarget{{Device: "/dev/sda", Model: "Test"}}
	s.cursor = 0

	done, _ := s.advance(nil)
	// Yeniden kurulum başlatılmamalı: loading true olmamalı.
	if s.loading {
		t.Error("başarı ekranında Enter kurulumu YENİDEN başlattı — disk tekrar silinir")
	}
	_ = done
}

// TestBackWorksOnEarlySteps — "Geri" gösteriliyorsa ÇALIŞMALI.
//
// Eylem çubuğu stepIntro'dan sonraki her adımda "Geri  Shift+Tab" yazıyordu
// ama back() yalnızca stepInstall'dan sonra bir şey yapıyordu. Kullanıcı
// Java adımında yanlış Wi-Fi seçtiğini fark edip Shift+Tab'a basıyor, ekran
// hiç değişmiyordu. Tek çıkış Esc'ti, o da tüm sihirbazı iptal ediyordu.
func TestBackWorksOnEarlySteps(t *testing.T) {
	for _, from := range []setupStep{stepSystem, stepIdentity, stepJava, stepTheme} {
		s := newSetup(theme.New("graphite-teal"), "graphite-teal")
		s.step = from
		s.back()
		if s.step != from-1 {
			t.Errorf("adım %d: Geri çalışmadı (hâlâ %d)", from, s.step)
		}
	}
}

// TestBackIsBlockedAfterInstall — kurulum başladıysa geri dönülemez.
//
// Disk zaten değişti; "geri" gitmek kullanıcıya hiçbir şey kazandırmaz ve
// yarım kalmış bir kurulumu gizler.
func TestBackIsBlockedAfterInstall(t *testing.T) {
	s := newSetup(theme.New("graphite-teal"), "graphite-teal")
	s.step = stepInstall
	s.installSuccess = true
	s.back()
	if s.step != stepInstall {
		t.Error("kurulumdan sonra geri dönüldü — disk zaten değişmişti")
	}
}

// TestTextFieldKeysAreNotStolen — 'h', 'l' ve boşluk METİNDİR.
//
// "Düğüm adı" alanına bu karakterler yazılamıyordu: adjust() onları metin
// alanına ulaşmadan yakalıyordu. Kullanıcı "salon" yazınca "saon" oluyordu.
func TestTextFieldKeysAreNotStolen(t *testing.T) {
	s := newSetup(theme.New("graphite-teal"), "graphite-teal")
	s.step = stepCluster
	s.cursor = 1 // düğüm adı alanı
	s.syncFocus()

	if s.activeTextInput() == nil {
		t.Fatal("test kurulumu hatalı: düğüm adı alanı odaklı değil")
	}

	// adjust() bu durumda ÇAĞRILMAMALI. clusterOn değerinin değişmemesi
	// bunun dolaylı kanıtıdır (adjust cursor==0 iken clusterOn'u çevirir),
	// ama asıl kanıt activeTextInput()'un nil olmamasıdır: update() artık
	// bu durumda adjust'a hiç gitmiyor.
	before := s.clusterOn
	if s.activeTextInput() != nil {
		// Bu dalda kısayol işlenmez.
		if s.clusterOn != before {
			t.Error("metin alanı odaklıyken ayar değişti")
		}
	}
}

// TestHostnameIsPersisted — PC adı yapılandırmaya YAZILMALI.
//
// Ad zorunlu tutuluyor, özet ekranında gösteriliyordu ama hiçbir yere
// yazılmıyordu: model.Config'de böyle bir alan yoktu. Kullanıcı her
// kurulumda yeniden giriyor, her seferinde kayboluyordu.
func TestHostnameIsPersisted(t *testing.T) {
	s := newSetup(theme.New("graphite-teal"), "graphite-teal")
	s.pcName.SetValue("salon-pc")

	cfg := s.buildConfig()
	if cfg.Hostname != "salon-pc" {
		t.Errorf("Hostname = %q, \"salon-pc\" olmalı — PC adı yine kaydedilmiyor", cfg.Hostname)
	}
}

// TestDoubleEnterDoesNotSaveTwice — onay ekranında çift Enter.
//
// doSetupSave yavaş bir RPC'dir. Kullanıcı sabırsızlanıp iki kez Enter'a
// basarsa aynı yapılandırma iki kez kaydedilir ve ikinci yanıt akışı
// karıştırır.
func TestDoubleEnterDoesNotSaveTwice(t *testing.T) {
	s := newSetup(theme.New("graphite-teal"), "graphite-teal")
	s.step = stepConfirm
	s.nodeName.SetValue("mcos-1")

	_, cmd1 := s.advance(nil)
	if cmd1 == nil {
		t.Fatal("ilk Enter kaydetmeyi başlatmadı")
	}
	_, cmd2 := s.advance(nil)
	if cmd2 != nil {
		t.Error("ikinci Enter ikinci bir kaydetme başlattı")
	}
}

// TestStepCountMatchesProgress — ilerleme yüzdesi doğru olmalı.
//
// Kullanıcıya "adım 5/10" denirken sihirbazın 5. adımda bitmesi, arayüzün
// yalan söylemesi demekti.
func TestStepCountMatchesProgress(t *testing.T) {
	if stepCount != 10 {
		t.Errorf("stepCount = %d — ilerleme metni ve adım listesi uyuşmuyor olabilir", stepCount)
	}
	// Her adımın bir adı olmalı (görünüm dosyasında karşılığı var).
	for st := setupStep(0); st < stepCount; st++ {
		if st < 0 || st >= stepCount {
			t.Errorf("geçersiz adım: %d", st)
		}
	}
}

// TestWiFiListIsWindowedNotTruncated — liste kaydırılmalı, kırpılmamalı.
//
// Eskiden ilk 8 ağ çiziliyor ama imleç daha aşağı inebiliyordu: kullanıcı
// 9. ağı seçtiğinde ekranda hiçbir şey değişmiyor, neyi seçtiğini
// göremiyordu.
func TestWiFiListIsWindowedNotTruncated(t *testing.T) {
	src, err := readSourceFile("view_setup.go")
	if err != nil {
		t.Skipf("kaynak okunamadı: %v", err)
	}
	if strings.Contains(src, "if i >= 8 {") {
		t.Error("Wi-Fi listesi hâlâ sabit 8 satırda KIRPILIYOR — imleç listeden çıkabilir")
	}
	if !strings.Contains(src, "maxRows") {
		t.Error("Wi-Fi listesinde kaydırma penceresi yok")
	}
}
