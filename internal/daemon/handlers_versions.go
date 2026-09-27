package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/model"
	"mcos/internal/server/providers"
)

// ════════════════════════════════════════════════════════════════════════════
// server.versions — YAZILIMA GÖRE Minecraft sürüm listesi
// ════════════════════════════════════════════════════════════════════════════
//
// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
// İşleyici ServerVersionsParams.Software'i hiç okumuyor, her yazılıma
// Mojang'ın listesini veriyordu (tek, ortak bir önbellekle). 2026-09-27'de
// gerçek API'lerle ölçüldü: sihirbaz Paper/Purpur/Folia için düz "26.1"i
// öneriyordu (üçü de 404 veriyor; Paper yalnızca 26.1.1 ve 26.1.2'yi
// yayımlıyor), Folia için "en yeni" diye 26.3'ü sunuyordu (Folia'nın en
// yenisi 26.2). Seçilen sürüm kurulumda "Desteklenen sürümler: …" hatasıyla
// düşüyordu — kullanıcı sihirbazda listeden seçtiği bir sürüm için.
//
// ── Yedek zinciri ───────────────────────────────────────────────────────────
//
//  1. Yazılımın kendi kaynağı (providers.ListVersions: fill.papermc.io,
//     api.purpurmc.org, meta.fabricmc.net, hub.spigotmc.org, …). Yazılım
//     başına 1 saat önbellek.
//  2. Olmazsa Mojang manifesti. Yanıt Fallback=true: listedeki bazı
//     sürümler o yazılımda olmayabilir ve arayüz bunu söylemeli.
//  3. O da olmazsa gömülü fallbackVersions (Source="yerleşik").
//
// Yedek yanıtlar yalnızca surumYedekSure boyunca tutulur: ağ geldiğinde
// gerçek liste bir saat beklemeden gelsin, ama internetsiz bir makinede
// sihirbazda altyapıyı her değiştirmek yeni bir zaman aşımı beklemesin.
//
// ── Anlık görüntüler ────────────────────────────────────────────────────────
// ServerVersionsParams'ta anlık görüntü seçeneği YOK (tek alan: Software).
// Liste bu yüzden her zaman yalnızca TAM sürümlerdir; LatestSnapshot,
// eskisi gibi, Mojang listesinde dolu gelir.

const (
	// surumTazeSure: gerçek bir listenin önbellekte kalma süresi. Sürümler
	// haftada bir bile değişmiyor; saatte bir yeterince taze.
	surumTazeSure = time.Hour
	// surumYedekSure: yedek yanıtların önbellekte kalma süresi.
	surumYedekSure = time.Minute
	// surumKaynakSure + surumMojangSure = 20 sn, eski tek isteğin zaman
	// aşımı. Yazılımın API'si asılı kalırsa Mojang yedeğine yine vakit
	// kalsın; panelin ipc bağlantısı da eskisinden uzun beklemesin.
	surumKaynakSure = 12 * time.Second
	surumMojangSure = 8 * time.Second
)

// yerlesikKaynak is ServerVersionsResult.Source for the built-in list.
const yerlesikKaynak = "yerleşik"

// surumIstemcisi fetches version lists. Testler bunu httptest'e yönlendirir.
var surumIstemcisi = &http.Client{Timeout: 20 * time.Second}

// surumKaydi is one cached reply.
type surumKaydi struct {
	at  time.Time
	ttl time.Duration
	res ipc.ServerVersionsResult
}

// versionsCache holds one reply PER SOFTWARE.
//
// Eskiden tek bir kayıt vardı: yazılım parametresi okunsa bile ilk gelen
// yanıt (ör. Paper'ınki) bir saat boyunca Fabric'e de verilirdi.
type versionsCache struct {
	mu    sync.Mutex
	kayit map[model.Software]surumKaydi
}

var verCache versionsCache

// al returns a cached reply that has not expired.
func (c *versionsCache) al(sw model.Software, now time.Time) (ipc.ServerVersionsResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k, ok := c.kayit[sw]
	if !ok || now.Sub(k.at) >= k.ttl || len(k.res.Versions) == 0 {
		return ipc.ServerVersionsResult{}, false
	}
	return k.res, true
}

// gercek returns a cached reply only if it came from the real source.
func (c *versionsCache) gercek(sw model.Software, now time.Time) (ipc.ServerVersionsResult, bool) {
	res, ok := c.al(sw, now)
	if !ok || res.Fallback {
		return ipc.ServerVersionsResult{}, false
	}
	return res, true
}

func (c *versionsCache) koy(sw model.Software, res ipc.ServerVersionsResult, ttl time.Duration, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.kayit == nil {
		c.kayit = map[model.Software]surumKaydi{}
	}
	c.kayit[sw] = surumKaydi{at: now, ttl: ttl, res: res}
}

func (d *Daemon) handleServerVersions(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ipc.ServerVersionsParams
	if err := decode(raw, &p); err != nil {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	sw := p.Software
	if sw == "" {
		sw = model.SoftwareVanilla // eski istemciler: parametresiz = vanilla
	}
	if !sw.Valid() {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams,
			Message: fmt.Sprintf("unknown software %q", p.Software)}
	}
	if res, ok := verCache.al(sw, time.Now()); ok {
		return res, nil
	}
	res, ttl := d.surumListesi(ctx, sw)
	// Çağıran vazgeçtiyse (bağlantı koptu, istek iptal) yedek yanıt ağın
	// durumunu değil iptali yansıtır: önbelleğe yazılsaydı bir sonraki
	// istek, ağ sağlamken bir dakika boyunca yedek listeyi alırdı.
	if ctx.Err() == nil {
		verCache.koy(sw, res, ttl, time.Now())
	}
	return res, nil
}

// surumListesi walks the fallback chain and returns the reply plus how long
// it may be cached.
func (d *Daemon) surumListesi(ctx context.Context, sw model.Software) (ipc.ServerVersionsResult, time.Duration) {
	kctx, iptal := context.WithTimeout(ctx, surumKaynakSure)
	l, err := providers.ListVersions(kctx, surumIstemcisi, sw)
	iptal()
	if err == nil {
		return surumSonucu(l, false), surumTazeSure
	}
	d.log.Warnf("versions: %s sürüm listesi alınamadı (%v); yedek liste kullanılıyor", sw, err)

	// Mojang'ın gerçek listesi önbellekte tazeyse ağa hiç çıkma.
	if m, ok := verCache.gercek(model.SoftwareVanilla, time.Now()); ok {
		m.Fallback = true
		return m, surumYedekSure
	}
	// Vanilla'nın kendi kaynağı zaten Mojang: az önce düştü, aynı adrese
	// ikinci kez gidip bir zaman aşımı daha beklemenin anlamı yok. Spigot ve
	// CraftBukkit'in kaynağı BuildTools'un sürüm dizini (hub.spigotmc.org);
	// o düşünce Mojang listesi onlar için de gerçek bir yedektir.
	if sw != model.SoftwareVanilla {
		mctx, iptal := context.WithTimeout(ctx, surumMojangSure)
		m, merr := providers.ListVersions(mctx, surumIstemcisi, model.SoftwareVanilla)
		iptal()
		if merr == nil {
			// Bu, vanilla'nın GERÇEK listesi: onun kaydı da tazelenir.
			verCache.koy(model.SoftwareVanilla, surumSonucu(m, false), surumTazeSure, time.Now())
			return surumSonucu(m, true), surumYedekSure
		}
		d.log.Warnf("versions: Mojang manifesti de alınamadı (%v); gömülü liste", merr)
	}
	return ipc.ServerVersionsResult{
		Versions: fallbackVersions,
		Latest:   fallbackVersions[0],
		Fallback: true,
		Source:   yerlesikKaynak,
	}, surumYedekSure
}

// surumSonucu converts a provider list into the RPC reply.
func surumSonucu(l providers.VersionList, yedek bool) ipc.ServerVersionsResult {
	return ipc.ServerVersionsResult{
		Versions:       l.Versions,
		Latest:         l.Versions[0], // ListVersions boş liste döndürmez
		LatestSnapshot: l.LatestSnapshot,
		Fallback:       yedek,
		Source:         l.Source,
	}
}
