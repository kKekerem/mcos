package model

// TurboStatus, turbo kipinin GERÇEKTE ne yaptığını anlatır.
//
// ── Neden ayrı bir tür ─────────────────────────────────────────────────────
// Eskiden durumda yalnızca "TurboOn bool" vardı ve panel "Turbo AÇIK"
// yazıyordu. Oysa turbo yalnızca nice -10 veriyordu: gerçek bir PC'de
// işlemci 400 MHz'de beklemeye, fanlar sessiz kipte dönmeye devam ediyordu.
// Kullanıcı "turbo hiçbir işe yaramıyor" dedi — haklıydı, ve arayüz bunu
// göremesin diye kurulmuş gibiydi. Artık her kolun sonucu madde madde
// taşınıyor; desteklenmeyen bir kol "desteklenmiyor" olarak GÖRÜNÜR.
type TurboStatus struct {
	Active bool `json:"active"`
	// Supported: en az bir donanım kolu (frekans, platform profili, fan)
	// bu makinede var. Sanal makinelerde çoğunlukla false olur; o zaman
	// turbo yalnızca öncelik ve çekirdek sabitlemesi yapabilir.
	Supported bool   `json:"supported"`
	Summary   string `json:"summary"`
	// Items: her kolun sonucu, kullanıcıya gösterilecek biçimde.
	Items []TurboItem `json:"items,omitempty"`

	Driver   string `json:"driver,omitempty"`   // cpufreq sürücüsü (intel_pstate, amd-pstate-epp, acpi-cpufreq)
	Governor string `json:"governor,omitempty"` // tüm politikalarda ortaksa adı, değilse "karışık"

	Hybrid bool   `json:"hybrid,omitempty"`
	PCores string `json:"pCores,omitempty"` // sunucuların sabitlendiği çekirdekler, "0-7"
	ECores string `json:"eCores,omitempty"` // hibrit işlemcide verimlilik çekirdekleri
	// CoreMethod: P/E ayrımının nasıl bulunduğu (kanıt olarak gösterilir).
	CoreMethod string `json:"coreMethod,omitempty"`

	FansFull  int `json:"fansFull,omitempty"`  // tam güce alınabilen fan kanalı
	FansTotal int `json:"fansTotal,omitempty"` // denetlenebilir görünen fan kanalı

	BaseMHz int `json:"baseMHz,omitempty"` // temel (turbo olmayan) frekans
	MaxMHz  int `json:"maxMHz,omitempty"`  // en yüksek turbo frekansı
	// TargetMHz: turbonun İSTEDİĞİ frekans (scaling_min_freq =
	// cpuinfo_max_freq). Bir istek; ısı/güç sınırı gelirse işlemci kendisi
	// düşürür, bu yüzden CurMHz ile yan yana gösterilir.
	TargetMHz int `json:"targetMHz,omitempty"`
	CurMHz    int `json:"curMHz,omitempty"` // P-çekirdeklerinde şu anki en yüksek frekans
	// CStatesOff: /dev/cpu_dma_latency=0 açık tutuluyor (derin uyku kapalı).
	// ARTIK HEP false: bu istek boştaki çekirdekleri C0'da (poll) tutuyor,
	// "etkin çekirdek" sayısını tavana çıkarıp tek çekirdek turbosunu
	// (ör. 4,4 GHz) engelliyor ve güç bütçesini boşa yakıyordu (bkz.
	// internal/turbo/turbo.go, cstateItem). Alan eski istemciler için duruyor.
	CStatesOff bool `json:"cStatesOff,omitempty"`

	// Limiter: frekansı ŞU AN sınırlayan etkenin tek satırlık tahmini,
	// ör. "PL1 güç sınırı (15 W)". Kanıtı Diag maddelerindedir.
	Limiter string `json:"limiter,omitempty"`
	// Diag: TANI — turbo açık da kapalı da her kolun donanımdan GERİ
	// OKUNAN gerçek durumu (sürücü/kip, çekirdek başına hedef ve ölçülen
	// MHz, PL1/PL2 önce/sonra, kısıtlama sayaçları, sıcaklık, fan RPM/PWM,
	// "yazıldı ama geri okuyunca farklı" uyarıları). Gerçek PC'de turbo
	// "4,4 GHz yerine 2,4 GHz" verdi ve sebebi hiçbir yerde görünmüyordu;
	// kullanıcı bu listenin ekran görüntüsünü atabilsin diye var.
	Diag []TurboItem `json:"diag,omitempty"`
	// DiagAt: tanının alındığı an (Unix saniye).
	DiagAt int64 `json:"diagAt,omitempty"`

	// PinnedThreads: çalışan sunucularda P-çekirdeklerine sabitlenen iş
	// parçacığı sayısı (yalnızca JVM süreci değil, TÜM iş parçacıkları).
	PinnedThreads int `json:"pinnedThreads,omitempty"`
}

// TurboItem bir turbo kolunun sonucudur.
type TurboItem struct {
	Name   string `json:"name"`
	State  string `json:"state"` // TurboOK | TurboPartial | TurboUnsupported | TurboFailed
	Detail string `json:"detail,omitempty"`
}

// TurboItem.State değerleri. Türkçe, çünkü panel ve telefon uygulaması
// bunları doğrudan gösteriyor.
const (
	TurboOK          = "tamam"
	TurboPartial     = "kısmen"
	TurboUnsupported = "desteklenmiyor"
	TurboFailed      = "hata"
	// TurboInfo yalnızca tanı maddelerinde: ölçüm/bilgi, başarı ya da
	// hata değil.
	TurboInfo = "bilgi"
)
