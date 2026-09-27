package fbpanel

import (
	"path/filepath"
	"testing"

	"mcos/internal/model"
)

// Eşleştirme portu yanıt veren ama modu yanıt vermeyen düğüm satırı uyarı
// rengiyle "mod yanıt vermiyor" göstermeli (iki VM'li sınamada eşte mod
// kurulamamıştı; ekran "2 düğüm çevrimiçi" diyordu).
func TestPeersNodeModNotReadyShown(t *testing.T) {
	render := func(ready, note bool) int {
		a := peersShotAppSized(t, 1280, 800)
		a.mu.Lock()
		v := ready
		for i := range a.link.Nodes {
			if !a.link.Nodes[i].Self {
				a.link.Nodes[i].Online = true
				a.link.Nodes[i].ModReady = &v
			}
		}
		if note {
			a.link.Note = "ortak dünya modu yanıt vermiyor: PC-B (port 27893) — o makinede sunucu açık mı, mod kurulu mu, güvenlik duvarı izin veriyor mu?"
		}
		a.dirty = true
		a.mu.Unlock()
		a.Draw()
		img := a.ui.Canvas()
		if note {
			writePNG(t, filepath.Join(peersShotDir(t), "esleme-mod-yanitsiz.png"), img)
		}
		return warnPixels(img, a.ui.Pal.Warn, a.sidebarWidth(), 0, a.ui.Bounds().Max.Y-a.ui.StatusBarH())
	}
	var nodes []model.LinkNode
	a := peersShotAppSized(t, 1280, 800)
	a.mu.Lock()
	nodes = a.link.Nodes
	a.mu.Unlock()
	if len(nodes) < 2 {
		t.Skipf("tanıtım verisinde %d düğüm", len(nodes))
	}
	// Yalnızca SATIR (not olmadan): uyarı renkli nokta çizilmeli.
	ok, row := render(true, false), render(false, false)
	if row-ok < 20 {
		t.Errorf("düğüm satırı mod yanıtsızken uyarı rengine dönmedi (uyarı pikseli %d -> %d)", ok, row)
	}
	// Not + satır: gerekçe metni de uyarı renginde görünmeli.
	if full := render(false, true); full-row < 200 {
		t.Errorf("mod yanıtsız notu çizilmedi (uyarı pikseli %d -> %d)", row, full)
	}
}
