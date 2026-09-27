package daemon

import (
	"context"
	"io"
	"testing"

	"mcos/internal/ipc"
	"mcos/internal/log"
	"mcos/internal/mcver"
)

// Mojang'a ulaşılamayınca sunulan liste: en yeni önce, yalnızca tam
// sürümler, 26.x takvim sürümleri ve 1.x'in son sürümü dahil; ilk öğe en
// yeni sürüm (26.3, 2026-09-27'deki manifest).
func TestYedekSurumListesi(t *testing.T) {
	if len(fallbackVersions) == 0 || fallbackVersions[0] != "26.3" {
		t.Fatalf("ilk (varsayılan) sürüm 26.3 olmalı: %v", fallbackVersions)
	}
	var var263, var12111 bool
	for i, v := range fallbackVersions {
		if !mcver.IsRelease(v) {
			t.Errorf("%q tam sürüm değil", v)
		}
		if i > 0 && !mcver.Newer(fallbackVersions[i-1], v) {
			t.Errorf("sıra bozuk: %q, %q'den yeni değil", fallbackVersions[i-1], v)
		}
		// Paper/Purpur/Folia düz "26.1"i yayımlamıyor: varsayılan bir
		// listede olursa o altyapılarda kurulum 404 ile düşer.
		if v == "26.1" {
			t.Error(`"26.1" listede olmamalı (Paper/Purpur 404); 26.1.2 kullanılmalı`)
		}
		var263 = var263 || v == "26.3"
		var12111 = var12111 || v == "1.21.11"
	}
	if !var263 || !var12111 {
		t.Fatalf("26.3 ve 1.21.11 listede olmalı: %v", fallbackVersions)
	}
}

// Hiçbir liste alınamazsa (bağlam iptal: istek hiç çıkmaz) işleyici yedek
// listeyi, onun ilk öğesini "latest" olarak ve YEDEK işaretiyle döndürür.
// Ağ sahte (surumAgiKur): sınama gerçek ağa asla çıkmaz.
func TestSurumIsleyicisiYedegeDuser(t *testing.T) {
	surumAgiKur(t)
	ctx, iptal := context.WithCancel(context.Background())
	iptal()
	d := &Daemon{log: log.New(io.Discard, log.LevelError, 8)}
	out, err := d.handleServerVersions(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := out.(ipc.ServerVersionsResult)
	if res.Latest != "26.3" || len(res.Versions) != len(fallbackVersions) ||
		!res.Fallback || res.Source != yerlesikKaynak {
		t.Fatalf("yedek yanıt yanlış: %+v", res)
	}
}
