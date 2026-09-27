package providers

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ════════════════════════════════════════════════════════════════════════════
// FORGE MAVEN SÜRÜM ADI
// ════════════════════════════════════════════════════════════════════════════
//
// ── Yakalanan gerçek hata: eski Forge sürümlerinde kurucu 404 ───────────────
// forgeInstaller kurucu adresini promosyondan "<mc>-<forge>" diye kuruyordu.
// Eski dallarda maven sürüm adı bir DAL EKİ taşıyor. 2026-09-27'de
// promotions_slim.json'daki 1.5.2 ve sonrası 62 sürümün hepsi denendi; beşi
// 404 verdi, maven-metadata.xml'de adları şöyle:
//
//	1.7.2  -> 1.7.2-10.12.2.1161-mc172     1.9.4 -> 1.9.4-12.17.0.2317-1.9.4
//	1.7.10 -> 1.7.10-10.13.4.1614-1.7.10   1.10  -> 1.10-12.18.0.2000-1.10.0
//	1.8.9  -> 1.8.9-11.15.1.2318-1.8.9
//
// Mod paketlerinin en yaygın iki sürümü (1.7.10, 1.8.9) sihirbazda listelenip
// kurulumda "indirilemedi" diye düşüyordu. Ekli adla indirilen 1.7.10
// kurucusu --installServer ile sorunsuz kuruldu (aynı gün, JDK 21).
//
// Ek bir kurala bağlanamıyor ("-mc172", "-1.10.0"): gerçek ad Forge'un
// maven dizininden okunur.

const forgeMavenMetadata = forgeMaven + "/maven-metadata.xml"

// forgeMavenSurumu returns the maven version name for full ("<mc>-<forge>").
//
// Tam ad dizinde varsa (ölçülen 62 sürümün 57'si, 26.x'in hepsi) o döner;
// yoksa "<full>-" ile başlayan TEK ad. Dizin okunamazsa full olduğu gibi
// döner: eski davranış, yeni sürümlerde zaten doğru.
func forgeMavenSurumu(ctx context.Context, client *http.Client, full string) string {
	surumler, err := forgeMavenSurumleri(ctx, client)
	if err != nil {
		return full
	}
	var ekli []string
	for _, v := range surumler {
		if v == full {
			return full
		}
		if strings.HasPrefix(v, full+"-") {
			ekli = append(ekli, v)
		}
	}
	// Birden çok ekli ad belirsizdir (ölçülen listede hiç yok); tahmin
	// etmek yerine eski adla dene, hata iletisi ne denendiğini gösterir.
	if len(ekli) == 1 {
		return ekli[0]
	}
	return full
}

// forgeMavenSurumleri reads <versioning><versions><version> from Forge's
// maven-metadata.xml (~210 KB, 5000+ sürüm).
func forgeMavenSurumleri(ctx context.Context, client *http.Client) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, forgeMavenMetadata, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "mcos/0.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %s", forgeMavenMetadata, resp.Status)
	}
	var meta struct {
		Versions []string `xml:"versioning>versions>version"`
	}
	// 8 MB: bugünkü dosyanın 40 katı; bozuk ya da sonsuz bir yanıt belleği
	// doldurmasın.
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&meta); err != nil {
		return nil, fmt.Errorf("forge: maven-metadata.xml: %w", err)
	}
	return meta.Versions, nil
}
