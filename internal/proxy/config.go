// Package proxy runs MCOS's Velocity proxy for shared worlds.
//
// ── Neden bir proxy ─────────────────────────────────────────────────────────
// Kullanıcının isteği: "çoklu PC bağlama çok yanlış, bildiğin yeni sunucuya
// aktarıyorsun. DonutSMP gibi modern sunucular böyle yapmıyor, sessizce
// geçiriyor. Tek bir IP'den çıkış versin hepsi, proxy olsun onları yöneten."
//
// Eski yol Minecraft'ın transfer paketiydi: istemci BAŞKA bir adrese yeniden
// bağlanır (yükleme ekranı görünür) ve her PC'nin portu dışarı açık olmalıdır.
// DonutSMP gibi ağlar Paper sunucularını bir Velocity proxy'sinin arkasına
// koyar: oyuncu yalnızca proxy'ye bağlanır, sunucular arası geçişi proxy
// kendi içinde yapar ve oyuncunun bağlantısı hiç kopmaz. MCOS artık aynısını
// yapıyor: ortak dünyayı kuran makinede Velocity sunucunun genel portunu
// alır, her düğüm (bu makinedeki kopyalar, eşleşmiş PC'ler) onun arkasında
// bir arka uçtur. Mod sınırda proxy'ye "Connect <arka uç>" der.
package proxy

import (
	"fmt"
	"strings"
)

// Backend is one server behind the proxy.
type Backend struct {
	// Name, model.ProxyBackendName kuralıyla üretilmiş addır; mod aynı adı
	// topolojinin "backend" alanından okur.
	Name string
	// Addr "host:port".
	Addr string
}

// Config is what velocity.toml says.
type Config struct {
	// Bind "0.0.0.0:<genel port>".
	Bind string
	// OnlineMode: hesabı PROXY doğrular (arka uçlar online-mode=false).
	OnlineMode bool
	// SecretFile, modern yönlendirme anahtarının dosyası (mutlak yol).
	SecretFile string
	// MOTD, arka uç yanıt vermezken sunucu listesinde görünen ad.
	MOTD string
	// MaxPlayers, listede görünen üst sınır.
	MaxPlayers int
	Backends   []Backend
	// Try, oyuncunun ilk bağlanacağı sunucular (kurucunun kendi sunucusu).
	Try []string
	// Legacy: Velocity 3.x biçimi (config-version 2.8, ping-passthrough
	// dizge). 4.x (config-version 2.9) ping-passthrough'u tabloya çevirdi;
	// yanlış biçim Velocity'nin açılışta düşmesine yol açar.
	Legacy bool
}

// Render produces velocity.toml.
//
// Yalnızca MCOS'un karar verdiği anahtarlar yazılır; gerisini Velocity kendi
// varsayılanıyla doldurur (dosyayı açılışta kendisi tamamlar). Her
// değişiklikte dosya BAŞTAN yazılır: Velocity yapılandırmayı canlı yeniden
// okumaz, zaten yeniden başlatılması gerekir.
func Render(c Config) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w("# MCOS tarafından üretildi — elle değiştirmeyin, her açılışta yeniden yazılır.")
	if c.Legacy {
		w(`config-version = "2.8"`)
	} else {
		w(`config-version = "2.9"`)
	}
	w("bind = %s", quote(c.Bind))
	motd := c.MOTD
	if strings.TrimSpace(motd) == "" {
		motd = "MCOS Ortak Dünya"
	}
	w("motd = %s", quote(miniSafe(motd)))
	maxp := c.MaxPlayers
	if maxp <= 0 {
		maxp = 20
	}
	w("show-max-players = %d", maxp)
	w("online-mode = %t", c.OnlineMode)
	// Çevrimdışı hesapların imza anahtarı yok; zorlanırsa hiçbiri giremez.
	w("force-key-authentication = %t", c.OnlineMode)
	w(`player-info-forwarding-mode = "MODERN"`)
	w("forwarding-secret-file = %s", quote(c.SecretFile))
	w("announce-forge = false")
	w("kick-existing-players = false")
	if c.Legacy {
		w(`ping-passthrough = "DESCRIPTION"`)
	}
	w("")
	if !c.Legacy {
		// Sunucu listesinde kurucunun kendi MOTD'si ve simgesi görünsün;
		// oyuncu sayısı proxy'nin (tüm düğümlerin toplamı).
		w("[ping-passthrough]")
		w("description = true")
		w("favicon = true")
		w("")
	}
	w("[servers]")
	for _, s := range c.Backends {
		w("%s = %s", quote(s.Name), quote(s.Addr))
	}
	try := make([]string, 0, len(c.Try))
	for _, t := range c.Try {
		try = append(try, quote(t))
	}
	w("try = [%s]", strings.Join(try, ", "))
	w("")
	w("[forced-hosts]")
	w("")
	w("[advanced]")
	// Modun "Connect <arka uç>" iletisi bu kanaldan gelir (sözleşme).
	w("bungee-plugin-message-channel = true")
	// Bir düğüm çökerse oyuncu atılmasın, kurucunun sunucusuna düşsün.
	w("failover-on-unexpected-server-disconnect = true")
	w("")
	w("[query]")
	w("enabled = false")
	return b.String()
}

// quote renders a TOML basic string.
func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ", "\r", " ", "\t", " ")
	return `"` + r.Replace(s) + `"`
}

// miniSafe strips MiniMessage tags: Velocity MOTD'yi MiniMessage olarak
// okur, sunucu adındaki "<" bir etiket sanılıp yapılandırmayı bozabilirdi.
func miniSafe(s string) string {
	return strings.NewReplacer("<", "", ">", "").Replace(s)
}
