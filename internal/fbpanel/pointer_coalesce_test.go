package fbpanel

import (
	"testing"

	"mcos/internal/fbinput"
)

// 1000 Hz farenin bir kare içinde biriktirdiği olaylar TEK harekete iner;
// toplam yer değiştirme korunur (imleç doğru yere gider, geriden gelmez).
func TestBirikenHareketTekOlayaIner(t *testing.T) {
	ch := make(chan fbinput.PointerEvent, 512)
	for i := 0; i < 299; i++ {
		ch <- fbinput.PointerEvent{Kind: fbinput.KindMouse, DX: 2, DY: -1}
	}
	out := coalescePointer(fbinput.PointerEvent{Kind: fbinput.KindMouse, DX: 2, DY: -1}, ch)
	if len(out) != 1 {
		t.Fatalf("300 hareket %d olaya indi, 1 bekleniyordu", len(out))
	}
	if out[0].DX != 600 || out[0].DY != -300 {
		t.Fatalf("toplam hareket kayboldu: DX=%d DY=%d (600, -300 bekleniyordu)", out[0].DX, out[0].DY)
	}
	if len(ch) != 0 {
		t.Fatalf("kuyrukta %d olay kaldı", len(ch))
	}
}

// Tıklama, ondan ÖNCE biriken hareketin sonuna düşmeli; sıra korunmalı.
func TestTiklamaDogruYereDuser(t *testing.T) {
	ch := make(chan fbinput.PointerEvent, 16)
	ch <- fbinput.PointerEvent{Kind: fbinput.KindMouse, DX: 5}
	ch <- fbinput.PointerEvent{Kind: fbinput.KindMouse, Button: fbinput.ButtonLeft, Press: true}
	ch <- fbinput.PointerEvent{Kind: fbinput.KindMouse, DX: 3}
	ch <- fbinput.PointerEvent{Kind: fbinput.KindMouse, DX: 4}
	ch <- fbinput.PointerEvent{Kind: fbinput.KindMouse, Button: fbinput.ButtonLeft, Release: true}
	out := coalescePointer(fbinput.PointerEvent{Kind: fbinput.KindMouse, DX: 1}, ch)
	if len(out) != 4 {
		t.Fatalf("olay sayısı %d, 4 bekleniyordu: %+v", len(out), out)
	}
	if out[0].DX != 6 || !out[1].Press || out[2].DX != 7 || !out[3].Release {
		t.Fatalf("sıra/toplam yanlış: %+v", out)
	}
}

// Mutlak konumda (VNC, VirtualBox tableti) son konum kalır; göreli
// hareketle karışmaz.
func TestMutlakKonumSonuncuKalir(t *testing.T) {
	ch := make(chan fbinput.PointerEvent, 16)
	ch <- fbinput.PointerEvent{Kind: fbinput.KindTouchscreen, HasAbs: true, AbsX: 20, AbsY: 30}
	ch <- fbinput.PointerEvent{Kind: fbinput.KindTouchscreen, HasAbs: true, AbsX: 400, AbsY: 300}
	ch <- fbinput.PointerEvent{Kind: fbinput.KindMouse, DX: 9}
	out := coalescePointer(fbinput.PointerEvent{Kind: fbinput.KindTouchscreen, HasAbs: true, AbsX: 1, AbsY: 1}, ch)
	if len(out) != 2 || out[0].AbsX != 400 || out[0].AbsY != 300 || out[1].DX != 9 {
		t.Fatalf("mutlak/göreli birleştirme yanlış: %+v", out)
	}
}
