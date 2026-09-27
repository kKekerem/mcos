package store

import (
	"fmt"
	"log"
	"os"
	"time"

	"mcos/internal/model"
	"mcos/internal/sysmon"
)

// LoadConfig reads the global config from path, returning defaults if missing.
//
// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
//
// Burada `var cfg model.Config` vardı: dosyada BULUNMAYAN her alan Go'nun
// SIFIR değeriyle kalıyordu. Varsayılanlar yalnızca dosya HİÇ YOKKEN
// uygulanıyordu.
//
// Oysa MCOS ilk açılışta EKSİK bir dosya yazar — etc/init.d/S03mcosdata,
// küme kimliği kaybolmasın diye:
//
//	{"setupComplete":false,"nodeId":"node_…"}
//
// Yani dosya VARDI ama içinde "ui" yoktu. Sonuç, ilk açılan panelde:
//
//	UI.Animations   = false   -> açılış yakınlaşması ve TÜM ekran geçişleri
//	                             sessizce kapalı
//	UI.Mouse        = false   -> fare ölü
//	UI.Touchpad     = false   -> touchpad ölü
//	UI.PointerSpeed = 0
//	Theme           = ""      -> tema adı boş
//	Tier.Mode       = ""      -> katman kipi yok
//
// Belirtisi tam olarak şuydu: açılış animasyonu bitiyor ama panele
// "yakınlaşarak" geçmiyordu; ekran TEK KAREDE atlıyordu. QEMU'da 100 ms
// aralıkla ölçüldü — 900 ms'lik geçişin tek bir ara karesi bile yoktu, çünkü
// animasyonlar hiç açılmamıştı. Kullanıcı bunu "efekt yok" diye görüyordu,
// ama aynı sıfırlama farenin de hiç çalışmamasının sebebiydi.
//
// Çözüm: dosya varsayılanların ÜSTÜNE çözülüyor. encoding/json, JSON'da
// bulunmayan alanlara DOKUNMAZ; yani eksik anahtarlar varsayılanını korur,
// yazılı olanlar ezer. Kullanıcı bir ayarı açıkça false yaptıysa o false
// kalır — istenen davranış budur.
func LoadConfig(path string) (*model.Config, error) {
	cfg := model.DefaultConfig()
	if err := readJSON(path, cfg); err != nil {
		if err == ErrNotFound {
			return model.DefaultConfig(), nil
		}
		// ── Düzeltilen gerçek hata: BOZUK AYAR DOSYASI SİSTEMİ AÇILMAZ
		//    YAPIYORDU ────────────────────────────────────────────────────
		//
		// Kullanıcının VMware'de aldığı ekran birebir şuydu:
		//
		//	mcosd: init failed: store: decode /data/config.json:
		//	       unexpected end of JSON input
		//	Hata: mcosd soketi hazırlanamadı: /run/mcos/mcosd.sock
		//
		// Yani /data/config.json SIFIR BAYT (ya da yarım) kalmış, mcosd
		// açılmayı REDDETMİŞ, soket hiç oluşmamış ve panel de açılamamış.
		// Makine tamamen kullanılamaz hâle geldi — hem de kaybedilen şey
		// yalnızca TERCİHLERDİ.
		//
		// Yazma yolu zaten atomik (geçici dosya + fsync + rename + dizin
		// fsync, bkz. writeJSON). Ama atomiklik dosyanın BAŞKA yollarla
		// bozulmasını engellemez:
		//
		//   - Sanal diskin yazma önbelleği yalan söyler; sanal makine
		//     "sıfırla" ile kesilince günlük yarım kalır.
		//   - Dosya bir kurulum/kurtarma adımında `touch` ile yaratılır.
		//   - Disk dolar ve yazma yarıda kesilir.
		//
		// Bir ev aletinde doğru tepki ASLA açılmamak olamaz. Bozuk dosya
		// KENARA ALINIR (kanıt olarak durur), varsayılanlarla devam edilir
		// ve kullanıcı kurulum sihirbazına düşer — kara ekrana değil.
		quarantineCorrupt(path, err)
		return model.DefaultConfig(), nil
	}
	// Elle düzenlenmiş ya da eski bir dosyadan gelen aralık dışı değerler
	// (ör. pointerSpeed: 0) burada bir kez düzeltilir; her okuyanın ayrıca
	// Normalize çağırmasına güvenmek, bir gün unutulacak bir kuraldır.
	cfg.UI = cfg.UI.Normalize()
	checkHardwareChange(cfg)
	return cfg, nil
}

// quarantineCorrupt moves an unreadable config aside so boot can continue.
//
// Dosya SİLİNMEZ: kullanıcının Wi-Fi parolası, teması ve düğüm kimliği orada
// olabilir ve bir insan onu kurtarabilir. Yeni ad zaman damgalı, böylece
// tekrarlanan bozulmalar birbirini ezmez.
//
// Taşıma başarısız olursa bile açılış SÜRER: buradaki hiçbir hata, sistemin
// açılmamasına gerekçe değildir — düzeltmeye çalıştığımız şey tam olarak buydu.
func quarantineCorrupt(path string, cause error) {
	bad := fmt.Sprintf("%s.bozuk-%s", path, time.Now().Format("20060102-150405"))
	if err := os.Rename(path, bad); err != nil {
		log.Printf("store: bozuk ayar dosyası kenara alınamadı (%s): %v", path, err)
		return
	}
	log.Printf("store: %s okunamadı (%v) — %s olarak saklandı, "+
		"varsayılan ayarlarla devam ediliyor", path, cause, bad)
}

// checkHardwareChange forces the first-boot wizard to re-run when the MCOS
// media has been moved to different hardware. The previous implementation
// compared a node-id file that travels with the disk (so it could never detect
// a move); we now compare a MAC fingerprint, which is bound to the machine.
func checkHardwareChange(cfg *model.Config) {
	if cfg.HardwareID == "" {
		return // never recorded (pre-setup); SetupComplete already governs
	}
	current := sysmon.HardwareID()
	if current != "" && current != cfg.HardwareID {
		cfg.SetupComplete = false
	}
}

// SaveConfig atomically persists the global config to path.
func SaveConfig(path string, cfg *model.Config) error {
	return writeJSON(path, cfg)
}
