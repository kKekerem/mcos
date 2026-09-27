package providers

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"mcos/internal/model"
)

// Gercek indirme denemesi. Ag gerektirir; MCOS_LIVE_DL=1 olmadan atlanir.
func TestCanliIndirme(t *testing.T) {
	if os.Getenv("MCOS_LIVE_DL") == "" {
		t.Skip("MCOS_LIVE_DL=1 ile calistirin")
	}
	// TUM saglayicilar: birinin API'si olduginde otekilerin de olmus
	// olabilecegini varsaymak yerine HEPSINI deniyoruz.
	// Her saglayicinin GERCEKTEN yayinladigi bir surum: Folia 1.21.1'i hic
	// cikarmamis, o yuzden hepsine ayni surumu vermek yanlis negatif uretir.
	for _, tc := range []struct {
		sw  model.Software
		ver string
	}{
		{model.SoftwareVanilla, "1.21.1"},
		{model.SoftwarePaper, "1.21.1"},
		{model.SoftwareFolia, "1.21.4"},
		{model.SoftwarePurpur, "1.21.1"},
		{model.SoftwareFabric, "1.21.1"},
		// Takvim sürümleri (26.x): 2026-09-27'de en yeni 26.3. Folia 26.3'ü
		// henüz yayımlamadı (en yeni 26.2), Paper'da 26.3 yalnızca ALPHA.
		{model.SoftwareVanilla, "26.3"},
		{model.SoftwarePaper, "26.3"},
		{model.SoftwareFolia, "26.2"},
		{model.SoftwarePurpur, "26.3"},
		{model.SoftwareFabric, "26.3"},
	} {
		sw := tc.sw
		t.Run(string(sw)+"-"+tc.ver, func(t *testing.T) {
			// İndirmeler ana makinenin /data/artifacts'ına değil geçici bir
			// depoya yazılsın.
			cevrimdisiDepoGecici(t)
			p, ok := Get(sw)
			if !ok {
				t.Fatalf("%s saglayicisi yok", sw)
			}
			ctx, iptal := context.WithTimeout(context.Background(), 120*time.Second)
			defer iptal()
			res, err := p.Install(ctx, tc.ver, t.TempDir(), "",
				&http.Client{Timeout: 120 * time.Second}, nil)
			if err != nil {
				t.Fatalf("%s INDIRME HATASI: %v", sw, err)
			}
			t.Logf("%s basarili: %+v", sw, *res)
		})
	}
}
