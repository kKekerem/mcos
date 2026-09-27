package fbpanel

import (
	"strings"
	"testing"

	"mcos/internal/ipc"
)

// TestGuncellemeListesi: rozetler "daha yeni/aynı/eski" olarak çıkar, kimliği
// olmayan ISO seçilemez ve sebebi görünür; eski ISO'nun onayı uyarır.
func TestGuncellemeListesi(t *testing.T) {
	m := newUpdatePicker(ipc.UpdateScanResult{
		CurrentBuild: "20260927-1715-aaaa", CurrentVersion: "1.0.1",
		Items: []ipc.UpdateISO{
			{Name: "yeni.iso", Device: "/dev/sdb1", Compare: ipc.UpdateNewer, BuildID: "20261001-1200-bbbb", Version: "1.0.2"},
			{Name: "ayni.iso", Device: "/dev/sdb1", Compare: ipc.UpdateSame, BuildID: "20260927-1715-aaaa"},
			{Name: "eski.iso", Device: "/dev/sdb1", Compare: ipc.UpdateOlder, BuildID: "20250101-0000-cccc"},
			{Name: "bozuk.iso", Device: "/dev/sdb1", Compare: ipc.UpdateInvalid, Error: "derleme kimliği yok"},
		},
	})
	want := []string{"daha yeni", "aynı", "eski", "uygun değil"}
	for i, it := range m.items {
		if it.Badge != want[i] {
			t.Errorf("%s: rozet %q, beklenen %q", it.Label, it.Badge, want[i])
		}
	}
	if !m.items[3].Disabled || m.items[3].Detail != "derleme kimliği yok" {
		t.Errorf("geçersiz ISO seçilebilir ya da sebebi yok: %+v", m.items[3])
	}
	if m.items[0].Detail != "1.0.2 · 20261001-1200 · sdb1" {
		t.Errorf("ayrıntı = %q", m.items[0].Detail)
	}
	lines := strings.Join(updateConfirmLines(m.items[2].Value.(ipc.UpdateISO)), "\n")
	if !strings.Contains(lines, "ESKİ") || !strings.Contains(lines, "KORUNUR") {
		t.Errorf("eski ISO onayı:\n%s", lines)
	}
}
