package model

import "time"

// PerfPackItem is one mod the performance pack put on disk.
//
// Kayıt, paketin KENDİ koyduğu dosyaları kullanıcının eklediklerinden ayırmak
// için tutulur: güncellemede yalnızca paketin eski dosyası değiştirilir,
// kullanıcının kendi indirdiği Lithium'a asla dokunulmaz.
type PerfPackItem struct {
	Slug    string `json:"slug"`
	Name    string `json:"name,omitempty"`
	File    string `json:"file"`
	Version string `json:"version,omitempty"`
	// MC, dosyanın hangi Minecraft sürümü için kurulduğudur. Sürüm
	// değiştirilince (ör. 1.21.1 -> 1.21.11) eski sürümün modu sunucuyu
	// açılışta düşürür; internet olmasa bile bayat dosya bununla tanınıp
	// kaldırılır.
	MC string `json:"mc,omitempty"`
}

// PerfPackState is what the performance pack last did on a server.
//
// Sunucu kaydında (manifest.json) durur; yoksa (nil) paket bu sunucuya hiç
// kurulmamıştır ya da kullanıcı sihirbazda kapatmıştır. Sürüm değiştirmede
// paket yalnızca kayıt varsa yeniden uygulanır.
type PerfPackState struct {
	Items []PerfPackItem `json:"items,omitempty"`
	// Settings, uygulanan ayarlardır ("spigot.yml: settings.x = true").
	Settings []string `json:"settings,omitempty"`
	// Pending, dosyası henüz olmadığı için yazılamamış ayarlardır
	// (spigot.yml / paper-world-defaults.yml ilk açılışta oluşur). Doluysa
	// daemon sunucuyu başlatmadan önce onları yazar.
	Pending []string `json:"pending,omitempty"`
	// MC, paketin uygulandığı Minecraft sürümüdür.
	MC string    `json:"mc,omitempty"`
	At time.Time `json:"at,omitempty"`
	// Note, son uygulamanın tek satırlık özeti ("internet yok, ...").
	Note string `json:"note,omitempty"`
}
