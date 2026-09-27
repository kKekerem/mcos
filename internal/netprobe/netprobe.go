// Package netprobe answers "is there internet?" — and says WHY when there isn't.
//
// ════════════════════════════════════════════════════════════════════════════
// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının iki ayrı şikâyeti aynı satırdan geliyordu:
//
//	"wifiye baglaniyom hala internet yok dio"
//	"javanin hicbi surumu indirilmiyor internette bagli olsa bile"
//
// Eski sınama şuydu (sysmon.checkInternet):
//
//	1.1.1.1:53 ve 8.8.8.8:53 adreslerine TCP, 1500 ms
//
// Yani "internet var mı?" sorusu YALNIZCA TCP PORT 53 ile ölçülüyordu. Oysa
// DNS normalde UDP kullanır; TCP/53 çoğunlukla yalnızca bölge aktarımı için
// açıktır ve pek çok ISS, kurumsal ağ, otel ağı ve VPN onu ENGELLER.
//
// Sonuç: HTTPS pekâlâ çalışan bir ağda MCOS "internet yok" diyordu. Dahası
// bu bayrak Java indirmesini KAPIDA reddediyordu (fbpanel/keys.go):
//
//	if st != nil && !st.Net.Internet {
//	    a.Emit(fbui.EventError, "İnternet yok — Java indirilemez")
//	    return
//	}
//
// Yani indirme HİÇ DENENMİYORDU. Kullanıcı "internete bağlı olsa bile
// indirilmiyor" derken tam olarak bunu görüyordu.
//
// ── Yeni sınama ─────────────────────────────────────────────────────────────
//
// Ölçülen şey artık İŞE YARAYAN şey: dışarıya HTTPS (443) çıkışı. Birden
// fazla sağlayıcıya PARALEL denenir ve ilk başarı yeter. 443 kapalıysa 53'e
// de bakılır — bazı kapalı ağlarda yalnızca DNS açıktır ve bunu bilmek
// kullanıcıya doğru şeyi söyletir.
//
// Sonuç ÖNBELLEKLENİR: durum çubuğu saniyede bir yenileniyor ve her seferinde
// beş soket açmak hem gereksiz hem de yavaş.
package netprobe

import (
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// Result is what a probe found.
type Result struct {
	// Online: dışarıya gerçekten çıkılabiliyor mu.
	Online bool
	// Reason: çıkılamıyorsa NEDEN. Kullanıcıya doğrudan gösterilir.
	//
	// "İnternet yok" tek başına işe yaramaz: kullanıcı ne yapacağını
	// bilemez. "Ağ kablosu/Wi-Fi bağlı değil" ile "ağa bağlısınız ama
	// dışarı çıkış engelli" tamamen farklı iki sorundur.
	Reason string
	// At: bu sonucun alındığı an.
	At time.Time
}

// hedefler, paralel denenen çıkış noktaları.
//
// Neden 443: indirme, güncelleme ve uzaktan kontrol HTTPS kullanıyor. Ölçülen
// şey, kullanılacak şey olmalı.
// Neden birden fazla sağlayıcı: biri kapalıysa ("Cloudflare engelli") ağ hâlâ
// çalışıyor olabilir.
// Neden 53 de var: yalnızca DNS'in açık olduğu ağları AYIRT ETMEK için —
// oradan "dışarı çıkış engelli" demek, "internet yok" demekten dürüsttür.
var hedefler = []struct {
	adres string
	https bool
}{
	{"1.1.1.1:443", true},
	{"8.8.8.8:443", true},
	{"9.9.9.9:443", true},
	{"1.1.1.1:53", false},
	{"8.8.8.8:53", false},
}

// probeTimeout, tek bir denemenin üst sınırı.
//
// 2500 ms: 1500 ms mobil/uydu bağlantılarda sınırdaydı ve yanlış "internet
// yok" üretiyordu. Denemeler PARALEL olduğu için toplam süre yine 2,5 sn.
const probeTimeout = 2500 * time.Millisecond

// cacheTTL, sonucun geçerli kalma süresi.
//
// Durum çubuğu saniyede bir yenileniyor; her yenilemede beş soket açmak hem
// israf hem de ağda gereksiz gürültü.
const cacheTTL = 10 * time.Second

var (
	mu      sync.Mutex
	son     Result
	sonAt   time.Time
	sürüyor bool
)

// Check returns the current connectivity state, using a short cache.
func Check() Result {
	mu.Lock()
	if time.Since(sonAt) < cacheTTL && !sonAt.IsZero() {
		r := son
		mu.Unlock()
		return r
	}
	if sürüyor {
		// Başka bir çağrı ölçüyor: son bilinen sonucu ver. Beklemek, panelin
		// çizim döngüsünü 2,5 saniye dondurmak demekti.
		r := son
		mu.Unlock()
		return r
	}
	sürüyor = true
	mu.Unlock()

	r := olc()

	mu.Lock()
	son, sonAt, sürüyor = r, time.Now(), false
	mu.Unlock()
	return r
}

// Invalidate forces the next Check to measure again.
//
// Ağ ayarı değiştiğinde çağrılır: kullanıcı Wi-Fi'ye bağlandıktan sonra on
// saniye "internet yok" görmemeli.
func Invalidate() {
	mu.Lock()
	sonAt = time.Time{}
	mu.Unlock()
}

// olc runs the actual probes in parallel.
func olc() Result {
	type sonuc struct {
		ok    bool
		https bool
	}
	ch := make(chan sonuc, len(hedefler))
	for _, h := range hedefler {
		go func(adres string, https bool) {
			var ok bool
			if https {
				ok = tlsUlasiyor(adres)
			} else {
				c, err := net.DialTimeout("tcp", adres, probeTimeout)
				if err == nil {
					_ = c.Close()
					ok = true
				}
			}
			ch <- sonuc{ok: ok, https: https && ok}
		}(h.adres, h.https)
	}

	dnsVar := false
	for i := 0; i < len(hedefler); i++ {
		s := <-ch
		if s.ok && s.https {
			// HTTPS çıkışı var: gerisini beklemeye gerek yok.
			return Result{Online: true, At: time.Now()}
		}
		if s.ok {
			dnsVar = true
		}
	}

	// Buraya gelindiyse HTTPS çıkışı YOK. Sebebi ayırt et.
	return Result{Online: false, Reason: sebep(dnsVar), At: time.Now()}
}

// tlsUlasiyor reports whether a real TLS server answers at adres.
//
// ── Yakalanan gerçek hata (kullanıcı, VirtualBox) ───────────────────────────
//
// "İnternete bağlı olmamasına rağmen bazı yerlerde bağlı diyor." Sınama
// yalnızca TCP bağlantısının AÇILMASINA bakıyordu. Sanal makinelerin NAT
// katmanı (ve bazı proxy/güvenlik duvarları) konuğun bağlantısını dışarıya
// ulaşmadan KABUL edip sonra kapatabilir; TCP el sıkışması "başarılı" görünür,
// internet yoktur. Artık HTTPS hedeflerinde TLS el sıkışması TAMAMLANMALI:
// sahte kabul eden bir katman gerçek bir TLS sunucusu gibi yanıt veremez.
//
// Sertifika DOĞRULANMAZ: amaç kimlik değil ulaşılabilirlik; imajda kök
// sertifika deposu eksik kalırsa her yer yanlışlıkla "internet yok" derdi.
func tlsUlasiyor(adres string) bool {
	d := &net.Dialer{Timeout: probeTimeout}
	c, err := tls.DialWithDialer(d, "tcp", adres, &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // ulaşılabilirlik sınaması, kimlik değil
		MinVersion:         tls.VersionTLS12,
	})
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// sebep explains a failed probe in terms the user can act on.
func sebep(dnsVar bool) string {
	switch {
	case !rotaVar():
		return "Ağa bağlı değilsiniz (varsayılan yol yok)"
	case !dnsSunucusuVar():
		return "Ağa bağlısınız ama DNS sunucusu ayarlanmamış"
	case dnsVar:
		return "Ağa bağlısınız ama dışarı çıkış engelli (HTTPS kapalı)"
	default:
		return "Ağa bağlısınız ama dışarıya ulaşılamıyor"
	}
}

// rotaVar reports whether a default route exists.
func rotaVar() bool {
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return true // okuyamıyorsak suçlamayalım
	}
	for i, satir := range strings.Split(string(b), "\n") {
		if i == 0 || strings.TrimSpace(satir) == "" {
			continue
		}
		f := strings.Fields(satir)
		// Alan 1 hedef: "00000000" varsayılan yol demek.
		if len(f) > 1 && f[1] == "00000000" {
			return true
		}
	}
	return false
}

// dnsSunucusuVar reports whether resolv.conf names a nameserver.
func dnsSunucusuVar() bool {
	b, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return true
	}
	for _, satir := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(satir), "nameserver") {
			return true
		}
	}
	return false
}

// String makes a Result printable in logs.
func (r Result) String() string {
	if r.Online {
		return "internet: var"
	}
	return fmt.Sprintf("internet: yok (%s)", r.Reason)
}
