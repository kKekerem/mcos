package providers

import (
	"context"
	"testing"
)

// Eski Forge dallarında maven adı ek taşıyor; kurucu adresi o adla kurulmalı.
// Fikstür (forge_maven_metadata.xml) gerçek maven-metadata.xml'den
// 2026-09-27'de kırpıldı: aynı biçim, aynı sıra, yanıltıcı komşular dahil
// ("1.7.10-10.13.4.1558-1.7.10", "1.7.10_pre4-…-prerelease").
func TestForgeEskiDalKurucuAdresi(t *testing.T) {
	ag := yeniAg()
	ag.ekle(forgePromos, "@liste_forge_promos.json")
	ag.ekle(forgeMavenMetadata, "@forge_maven_metadata.xml")
	cl := ag.istemci(t)

	// 1.7.2 ve 1.10'un eki Minecraft sürümü değil ("-mc172", "-1.10.0"):
	// ek bir kurala bağlanamaz, dizinden okunmalı.
	ag2 := yeniAg()
	ag2.ekle(forgePromos, `{"promos":{"1.7.2-latest":"10.12.2.1161","1.10-latest":"12.18.0.2000"}}`)
	ag2.ekle(forgeMavenMetadata, "@forge_maven_metadata.xml")
	cl2 := ag2.istemci(t)

	for _, tc := range []struct {
		ag   *sahteAg
		mc   string
		want string
	}{
		{ag, "1.7.10", "1.7.10-10.13.4.1614-1.7.10"},
		{ag, "1.8.9", "1.8.9-11.15.1.2318-1.8.9"},
		{ag2, "1.7.2", "1.7.2-10.12.2.1161-mc172"},
		{ag2, "1.10", "1.10-12.18.0.2000-1.10.0"},
		// Eksiz adlar olduğu gibi kalır.
		{ag, "1.12.2", "1.12.2-14.23.5.2859"}, // recommended
		{ag, "1.5.2", "1.5.2-7.8.1.738"},
		{ag, "26.3", "26.3-66.0.5"},
	} {
		c := cl
		if tc.ag == ag2 {
			c = cl2
		}
		url, full, err := forgeInstaller(context.Background(), c, tc.mc)
		wantURL := forgeMaven + "/" + tc.want + "/forge-" + tc.want + "-installer.jar"
		if err != nil || full != tc.want || url != wantURL {
			t.Errorf("forge %s: %q %s %v; istenen %q", tc.mc, full, url, err, tc.want)
		}
	}
	if !ag.istendi(forgeMavenMetadata) {
		t.Error("maven-metadata.xml istenmedi")
	}
}

// Dizin okunamazsa ya da ek BELİRSİZSE eski ad kullanılır: yeni sürümler
// (26.x) dizin olmadan da doğru kurulmaya devam eder.
func TestForgeMavenAdiCozulemezse(t *testing.T) {
	ag := yeniAg() // maven-metadata.xml yok: 404
	ag.ekle(forgePromos, "@liste_forge_promos.json")
	cl := ag.istemci(t)
	for mc, want := range map[string]string{
		"26.3":   "26.3-66.0.5",
		"1.7.10": "1.7.10-10.13.4.1614",
	} {
		if _, full, err := forgeInstaller(context.Background(), cl, mc); err != nil || full != want {
			t.Errorf("dizinsiz forge %s: %q %v; istenen %q", mc, full, err, want)
		}
	}

	belirsiz := yeniAg()
	belirsiz.ekle(forgeMavenMetadata, `<metadata><versioning><versions>`+
		`<version>1.7.10-10.13.4.1614-a</version><version>1.7.10-10.13.4.1614-b</version>`+
		`</versions></versioning></metadata>`)
	if got := forgeMavenSurumu(context.Background(), belirsiz.istemci(t), "1.7.10-10.13.4.1614"); got != "1.7.10-10.13.4.1614" {
		t.Errorf("iki ekli ad varken %q seçildi; tahmin edilmemeli", got)
	}

	bozuk := yeniAg()
	bozuk.ekle(forgeMavenMetadata, `<metadata><versioning><versions><version>1.7.10-10.13.4.1614-1.7.10`)
	if got := forgeMavenSurumu(context.Background(), bozuk.istemci(t), "1.7.10-10.13.4.1614"); got != "1.7.10-10.13.4.1614" {
		t.Errorf("bozuk dizinde %q seçildi", got)
	}
}
