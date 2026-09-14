package store

import (
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
		return nil, err
	}
	// Elle düzenlenmiş ya da eski bir dosyadan gelen aralık dışı değerler
	// (ör. pointerSpeed: 0) burada bir kez düzeltilir; her okuyanın ayrıca
	// Normalize çağırmasına güvenmek, bir gün unutulacak bir kuraldır.
	cfg.UI = cfg.UI.Normalize()
	checkHardwareChange(cfg)
	return cfg, nil
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
