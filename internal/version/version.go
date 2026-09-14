// Package version is the single source of truth for the MCOS version string.
//
// NEDEN AYRI BİR PAKET: sürüm numarası daha önce SEKİZ ayrı yerde elle
// yazılıydı (daemon, model varsayılanları, panel kenar çubuğu, katalog
// User-Agent, demo verisi, testler). Bir sürüm yükseltmesinde bunların
// bazıları güncelleniyor, bazıları unutuluyordu; kullanıcı panelde "v0.1.0"
// görürken daemon kendini "1.0.0" diye tanıtıyordu.
//
// Artık tek yer burası. VERSION dosyası ile AYNI değeri taşımalı; bunu
// scripts/test-version.sh doğrular.
package version

// Version is the MCOS release version.
//
// 1.0.1: fare/touchpad desteği, açılış animasyonu, geçiş efektleri,
// playit tünel akışı, PC eşleştirme + MCOS Link dünya paylaşımı.
const Version = "1.0.1"

// UserAgent is what MCOS sends to third-party APIs (Modrinth, Adoptium…).
//
// Gerçek bir tanıtıcı göndermek nezaket değil, ZORUNLULUK: Modrinth
// belirsiz User-Agent taşıyan istekleri hız sınırına takar.
const UserAgent = "mcos/" + Version + " (Minecraft Server OS)"

// Display returns the version as shown in the interface ("v1.0.1").
func Display() string { return "v" + Version }
