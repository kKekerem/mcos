package providers

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"mcos/internal/log"
	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// PAPERMC AİLESİ (Paper, Folia)
// ════════════════════════════════════════════════════════════════════════════
//
// ── Düzeltilen gerçek hata: v2 API KAPATILDI ────────────────────────────────
//
// Kullanıcı "sunucuyu kuruyorum, sonra 'sunucu yazılımı indirilemedi' deyip
// açmıyor" dedi. Sebep ölçüldü — PaperMC'nin v2 API'si artık YOK:
//
//	GET https://api.papermc.io/v2/projects/paper/versions/1.21.1
//	-> 410 Gone
//
// Aynı anda Vanilla sorunsuz iniyordu, yani ağ ya da TLS değil, TEK BİR
// sağlayıcının API'si ölmüştü. Paper en yaygın seçim olduğu için kullanıcı
// bunu "sunucu hiç kurulmuyor" diye görüyordu.
//
// ── Yeni API (v3, fill.papermc.io) ──────────────────────────────────────────
//
//	GET https://fill.papermc.io/v3/projects/<proje>/versions/<sürüm>/builds
//
// Doğrudan derleme LİSTESİ döner ve her derleme indirme bağlantısını KENDİ
// İÇİNDE taşır:
//
//	[{ "id": 133, "channel": "STABLE",
//	   "downloads": { "server:default": {
//	       "name": "paper-1.21.1-133.jar",
//	       "checksums": {"sha256": "..."},
//	       "url": "https://fill-data.papermc.io/v1/objects/<sha>/<ad>.jar" }}}]
//
// Bu, v2'ye göre bir çağrı daha az: eskiden önce sürüm, sonra derleme, sonra
// indirme adresi kuruluyordu. Artık tek çağrı yetiyor.
//
// ── Neden STABLE tercih ediliyor ────────────────────────────────────────────
//
// Liste deneysel (EXPERIMENTAL) derlemeler de içerebiliyor. Bir Minecraft
// sunucusu appliance'ında varsayılan olarak deneysel yazılım kurmak doğru
// değil; kararlı bir derleme varsa o seçiliyor, yoksa en yeniye düşülüyor
// (yeni çıkmış bir Minecraft sürümünde yalnızca deneysel derleme olabilir ve
// o durumda "hiç kurma" demek kullanıcıya yardımcı olmaz).

const paperAPI = "https://fill.papermc.io/v3/projects"

// paperLikeProvider serves PaperMC-family projects (Paper, Folia) which share
// the same v3 API shape.
type paperLikeProvider struct {
	software model.Software
	project  string // "paper" | "folia"
}

func (p paperLikeProvider) Software() model.Software { return p.software }

// paperBuild is one entry of the v3 builds list.
type paperBuild struct {
	ID        int    `json:"id"`
	Channel   string `json:"channel"`
	Downloads map[string]struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"downloads"`
}

// sunucuIndirmesi returns the server jar download for a build.
//
// Anahtar "server:default". Sabit yazmak yerine önce ona bakıp sonra
// herhangi bir girdiye düşüyoruz: API yeni bir anahtar eklerse (ör.
// "server:mojmap") kurulum sessizce kırılmasın.
func (b paperBuild) sunucuIndirmesi() (ad, url string, ok bool) {
	if d, var_ := b.Downloads["server:default"]; var_ && d.URL != "" {
		return d.Name, d.URL, true
	}
	for _, d := range b.Downloads {
		if d.URL != "" {
			return d.Name, d.URL, true
		}
	}
	return "", "", false
}

func (p paperLikeProvider) Install(ctx context.Context, mcVersion, dir, javaBin string, client *http.Client, lg *log.Logger) (*InstallResult, error) {
	var builds []paperBuild
	url := fmt.Sprintf("%s/%s/versions/%s/builds", paperAPI, p.project, mcVersion)
	if err := getJSON(ctx, client, url, &builds); err != nil {
		// ── Neden burada özel bir mesaj ─────────────────────────────────
		//
		// 404, "ağ koptu" değil "bu proje bu sürümü desteklemiyor" demek.
		// Ölçüldü: Folia 1.21.1'i hiç yayınlamamış (1.21.4 ve üstü var).
		// Kullanıcıya ham bir 404 ya da "indirilemedi" göstermek, sihirbazda
		// yanlış bir eşleşme seçtiğini anlamasına YARDIM ETMİYOR.
		if surumler := p.desteklenenSurumler(ctx, client); len(surumler) > 0 {
			return nil, fmt.Errorf(
				"%s %s sürümünü desteklemiyor. Desteklenen sürümler: %s",
				p.project, mcVersion, strings.Join(surumler, ", "))
		}
		return nil, fmt.Errorf("%s %s: derleme listesi alınamadı: %w",
			p.project, mcVersion, err)
	}
	if len(builds) == 0 {
		return nil, fmt.Errorf("%s: %s sürümü için derleme yok",
			p.project, mcVersion)
	}

	// En yeni önce: API sırası garanti değil, id'ye göre kendimiz sıralıyoruz.
	sort.Slice(builds, func(i, j int) bool { return builds[i].ID > builds[j].ID })

	sec := builds[0]
	for _, b := range builds {
		if b.Channel == "STABLE" {
			sec = b
			break
		}
	}

	jarName, dlURL, ok := sec.sunucuIndirmesi()
	if !ok {
		return nil, fmt.Errorf("%s: derleme %d için indirme bağlantısı yok",
			p.project, sec.ID)
	}
	if lg != nil {
		lg.Infof("%s: %s indiriliyor (derleme %d, %s)",
			p.project, jarName, sec.ID, sec.Channel)
	}
	if _, err := downloadTo(ctx, client, dlURL, dir, "server.jar"); err != nil {
		return nil, err
	}
	return &InstallResult{JarFile: "server.jar", LaunchArgs: []string{"nogui"}}, nil
}

// desteklenenSurumler lists the versions this project actually publishes.
//
// Yalnızca HATA YOLUNDA çağrılıyor: normal kurulumda gereksiz bir istek
// yapmamak için. Kendisi de başarısız olabilir (ağ gerçekten kopmuş olabilir);
// o zaman boş döner ve çağıran asıl hatayı gösterir.
func (p paperLikeProvider) desteklenenSurumler(ctx context.Context, client *http.Client) []string {
	// Liste sihirbazın gösterdiğiyle AYNI yoldan gelir (projeSurumleri,
	// versions.go): yalnızca tam sürümler, ortak mcver sırasıyla en yeni
	// önce. Kullanıcının aradığı genelde güncel sürümdür; ilk sekizi yeter.
	//
	// Metin sıralaması BURADA YANLIŞ olurdu: "1.21.8" > "1.21.11" çıkar, çünkü
	// '8' > '1'. Ölçüldü — düz sort.Strings ile liste "1.21.8, 1.21.6, 1.21.5,
	// 1.21.4, 1.21.11" sırasıyla geliyordu ve en yeni sürüm sonda kalıyordu.
	//
	// ── Yakalanan gerçek hata: ön sürümler ve 26.x ─────────────────────────
	// Buradaki eski sayısal karşılaştırma sayıya çevrilemeyen parçayı metin
	// olarak kıyaslıyordu. Gerçek Paper listesiyle (2026-09-27) "paper 26.1"
	// hatası şunu öneriyordu: "26.3-rc-3, 26.3, 26.2-rc-2, 26.2, 26.1.2,
	// 26.1.1, 1.21.9-rc1, 1.21.9-pre4" — rc tam sürümden önce, 1.21.11 hiç
	// yok ("9-rc1" > "11" metin olarak). Artık ortak mcver sırası.
	out, err := p.projeSurumleri(ctx, client)
	if err != nil {
		return nil
	}
	if len(out) > 8 {
		out = append(out[:8], "…")
	}
	return out
}
