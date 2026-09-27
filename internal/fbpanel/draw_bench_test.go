package fbpanel

import (
	"image"
	"testing"

	"mcos/internal/fbfont"
	"mcos/internal/fbui"
	"mcos/internal/model"
)

// App.Draw'un tam kare maliyeti.
//
// Kullanıcının "kasıyor arayüz" şikâyetinin ölçüldüğü yer burası: 1920x1080'de
// bir karenin çizilmesi 33 ms'lik tikin altında kalmalı, yoksa animasyon
// takılır.
func BenchmarkAppDrawTamKare(b *testing.B) {
	a, _ := newBenchApp(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Invalidate()
		a.Draw()
	}
}

func BenchmarkAppDrawPencereli(b *testing.B) {
	a, _ := newBenchApp(b)
	a.OpenModal(NewListModal("Kıyas", "", []ListItem{
		{Label: "Bir"}, {Label: "İki"}, {Label: "Üç"},
	}, nil))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Invalidate()
		a.Draw()
	}
}

func newBenchApp(b *testing.B) (*App, int) {
	b.Helper()
	f, err := fbfont.Load(16)
	if err != nil {
		b.Fatalf("font: %v", err)
	}
	b.Cleanup(func() { f.Close() })
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	ui := fbui.NewUI(img, f, fbui.DefaultPalette)
	a := New(ui, nil)
	a.SetHeadless(true)
	FillDemo(a)
	a.SetScreenSize(1920, 1080)
	cfg := model.DefaultConfig()
	cfg.UI.Animations = true
	a.SetConfig(cfg)
	a.gotoSection(SecServers)
	a.setFocus(FocusContent)
	a.Draw()
	return a, 0
}
