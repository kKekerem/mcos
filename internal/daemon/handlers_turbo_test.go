package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcos/internal/model"
)

// Turbo tanısı /data/log/turbo.log'a EKLENMELİ ve dosya sınırı aşınca
// turbo.log.1'e dönmeli: kullanıcı bir sonraki gerçek PC denemesinde bu
// dosyayı gönderebilsin, /data da şişmesin. Sahte kök (MCOS_TURBO_KOK):
// geliştirme makinesinin /data'sına yazılmaz.
func TestTurboLogAppendsAndRotates(t *testing.T) {
	root := t.TempDir()
	t.Setenv(turboKokEnv, root)
	d := &Daemon{}
	st := model.TurboStatus{Active: true, Summary: "performans kipi", Limiter: "PL1 güç sınırı (15,0 W)",
		Diag: []model.TurboItem{{Name: "RAPL package-0 (MSR)", State: model.TurboInfo, Detail: "PL1 15,0 W, tüketim 14,8 W"}}}
	d.turboLog("turbo açıldı", st)
	d.turboLog("turbo açık — dönemsel ölçüm", st)
	p := filepath.Join(root, "data/log/turbo.log")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Count(s, "Sınırlayan: PL1 güç sınırı (15,0 W)") != 2 || !strings.Contains(s, "tüketim 14,8 W") ||
		!strings.Contains(s, "— turbo açıldı ===") {
		t.Errorf("günlük eksik ya da eklenmiyor:\n%s", s)
	}
	if err := os.WriteFile(p, make([]byte, turboLogMax+1), 0o644); err != nil {
		t.Fatal(err)
	}
	d.turboLog("turbo kapatıldı", st)
	if fi, err := os.Stat(p + ".1"); err != nil || fi.Size() != turboLogMax+1 {
		t.Errorf("büyük günlük turbo.log.1'e dönmedi: %v", err)
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), "turbo kapatıldı") || len(b) > turboLogMax {
		t.Errorf("döndürmeden sonra yeni günlük yanlış (%d bayt)", len(b))
	}
}
