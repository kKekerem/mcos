package files

// USB varlık tespiti — sidebar'da "USB" bölümünü göstermek/gizlemek için.
//
// ScanUSBJars()'tan AYRI tutuluyor çünkü o fonksiyon her bölümü geçici olarak
// BAĞLAR; panel durum döngüsünde saniyede bir çağrılamaz. Buradaki tespit
// yalnızca /sys ve /proc okur, hiçbir şeyi bağlamaz — sürekli yoklama için
// güvenlidir.

// USBInfo summarises attached removable storage without mounting anything.
type USBInfo struct {
	// Present, en az bir çıkarılabilir/USB bölümü takılıysa true.
	Present bool `json:"present"`
	// Partitions, bulunan bölüm aygıtlarının sayısı.
	Partitions int `json:"partitions"`
	// Devices, bölüm aygıt yolları (ör. /dev/sdb1). Panel bunu göstermez ama
	// tanılama için taşınır.
	Devices []string `json:"devices,omitempty"`
}
