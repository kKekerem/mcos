package fbpanel

import (
	"path/filepath"
	"testing"
)

// Ses penceresi: bipçi satırı eklendi, pencere bütün satırları sığdırmalı.
// MCOS_SHOT_DIR verilirse PNG oraya yazılır (gözle bakmak için).
func TestSoundModalShot(t *testing.T) {
	a, _ := newTestApp(t)
	a.gotoSection(SecSettings)
	a.setFocus(FocusContent)
	a.OpenModal(newSoundModal())
	a.Draw()
	img := a.ui.Canvas()
	if ink := countInk(img); ink < 20000 {
		t.Fatalf("ses penceresi neredeyse boş (%d piksel)", ink)
	}
	writePNG(t, filepath.Join(peersShotDir(t), "sound_modal.png"), img)
}
