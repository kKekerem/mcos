package ipc

// ── Sistem güncellemesi (USB'deki yeni ISO) ─────────────────────────────────
//
// Güncelleme yalnızca açılış bölümündeki (p1) bzImage/initrd.img'yi
// değiştirir; yeni sistem açılışta kök bölümüne kopyalanır, /data'ya
// dokunulmaz. Ayrıntı: rootfs-overlay/usr/bin/mcos-update.

// Karşılaştırma sonuçları (UpdateISO.Compare). Panel ve mcosctl bunları
// "daha yeni / aynı / eski" diye yazar.
const (
	UpdateNewer   = "yeni"
	UpdateSame    = "ayni"
	UpdateOlder   = "eski"
	UpdateInvalid = "gecersiz" // MCOS değil ya da derleme kimliği yok
)

// UpdateISO is one ISO file found on a USB drive.
type UpdateISO struct {
	Device    string `json:"device"`
	Path      string `json:"path"` // bölüm köküne göre, "/" ayraçlı
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
	BuildID   string `json:"buildId,omitempty"`
	Version   string `json:"version,omitempty"`
	Compare   string `json:"compare"`
	Error     string `json:"error,omitempty"` // gecersiz ise sebebi
}

// UpdateScanResult is the reply to system.updateScan.
type UpdateScanResult struct {
	// Live: sistem RAM'den (USB/ISO) çalışıyor; güncellenecek disk yok.
	Live           bool        `json:"live"`
	CurrentBuild   string      `json:"currentBuild,omitempty"`
	CurrentVersion string      `json:"currentVersion,omitempty"`
	Items          []UpdateISO `json:"items"`
}

// UpdateParams selects the ISO for system.update.
type UpdateParams struct {
	Device string `json:"device"`
	Path   string `json:"path"`
}

// UpdateStatusResult is the reply to system.updateStatus.
type UpdateStatusResult struct {
	Running bool   `json:"running"`
	Done    bool   `json:"done"`   // başarıyla bitti: yeniden başlatınca kurulacak
	Failed  bool   `json:"failed"` // hata; ayrıntı Lines'ta
	Percent int    `json:"percent"`
	Message string `json:"message,omitempty"`
	// Lines: hatada kullanıcıya gösterilecek satırlar (Hata/Neden/Ne yapmalı).
	Lines []string `json:"lines,omitempty"`
}
