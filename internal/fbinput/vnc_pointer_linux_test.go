//go:build linux

package fbinput

import "testing"

// VNC'nin sanal faresi ("MCOS VNC", internal/vnc/uinput_linux.go) ve
// QEMU/VirtualBox USB tableti gibi TEMASSIZ mutlak işaretçilerin sınamaları.
//
// Ölçülen hata: bu aygıtlar touchpad sınıflandırılıyordu; touchpad çözücüsü
// hareketi yalnızca BTN_TOUCH=1 iken işlediği için VNC'de imleç HİÇ
// kıpırdamıyordu ve tıklamalar imlecin eski yerine düşüyordu.

// bits builds a kernel capability bitmap with the given bits set.
func bits(n ...int) []byte {
	b := make([]byte, 0x300/8)
	for _, i := range n {
		b[i/8] |= 1 << uint(i%8)
	}
	return b
}

func TestAbsPointerKindSiniflandirma(t *testing.T) {
	const btnSouth = 0x130 // gamepad "A" düğmesi
	cases := []struct {
		ad    string
		keys  []byte
		kind  Kind
		hover bool
	}{
		// internal/vnc'nin gerçekte açtığı bitler: klavye tuşları + 3 fare düğmesi.
		{"VNC sanal faresi", bits(1, 2, 30, 57, btnLeft, btnRight, btnMiddle), KindTouchscreen, true},
		{"QEMU usb-tablet", bits(btnLeft, btnRight, btnMiddle), KindTouchscreen, true},
		{"dizüstü touchpad", bits(btnLeft, btnTouch, btnToolFing, btnToolDoubl), KindTouchpad, false},
		{"dokunmatik ekran", bits(btnTouch), KindTouchscreen, false},
		{"gamepad (ABS + BTN_SOUTH)", bits(btnSouth), KindTouchpad, false},
		{"düğmesiz mutlak aygıt", bits(), KindTouchpad, false},
	}
	for _, c := range cases {
		k, h := absPointerKind(c.keys)
		if k != c.kind || h != c.hover {
			t.Errorf("%s: kind=%v hover=%v, beklenen kind=%v hover=%v", c.ad, k, h, c.kind, c.hover)
		}
	}
}

// vncCaps mirrors what probe() reports for the VNC device on a w×h screen.
func vncCaps(w, h int32) caps {
	return caps{
		Kind:    KindTouchscreen,
		Hover:   true,
		Name:    "MCOS VNC",
		HasBtnL: true,
		HasWhl:  true,
		RangeX:  absInfo{Min: 0, Max: w - 1},
		RangeY:  absInfo{Min: 0, Max: h - 1},
	}
}

func TestVNCImleciTemassizHareketEder(t *testing.T) {
	a := newTestActivity(1280, 800)
	d := newPointerDecoder(a, vncCaps(1280, 800))

	// VNC PointerEvent (640,360), düğme yok: tek paket.
	d.feed(evAbs, absX, 640)
	d.feed(evAbs, absY, 360)
	d.feed(evSyn, 0, 0)
	got := drain(a)
	if len(got) != 1 || !got[0].HasAbs || got[0].AbsX != 640 || got[0].AbsY != 360 {
		t.Fatalf("temassız hareket imleci taşımadı: %+v (beklenen tek olay, mutlak 640,360)", got)
	}

	// Yalnızca Y değişti: çekirdek X'i GÖNDERMEZ, son X geçerli kalmalı.
	d.feed(evAbs, absY, 100)
	d.feed(evSyn, 0, 0)
	got = drain(a)
	if len(got) != 1 || got[0].AbsX != 640 || got[0].AbsY != 100 {
		t.Fatalf("tek eksen hareketi yanlış: %+v (beklenen 640,100)", got)
	}
}

// Tıklama, AYNI paketteki hareketten SONRA gelmeli: panel düğmeyi imlecin
// o anki yerine uygular. Sıra ters olsaydı tık eski konuma düşerdi.
func TestVNCTiklamasiYeniKonumaDuser(t *testing.T) {
	a := newTestActivity(1280, 800)
	d := newPointerDecoder(a, vncCaps(1280, 800))

	d.feed(evAbs, absX, 200)
	d.feed(evAbs, absY, 300)
	d.feed(evKey, btnLeft, 1)
	d.feed(evSyn, 0, 0)
	d.feed(evKey, btnLeft, 0)
	d.feed(evSyn, 0, 0)

	got := drain(a)
	if len(got) != 3 {
		t.Fatalf("%d olay (3 olmalı: hareket, bas, bırak): %+v", len(got), got)
	}
	if !got[0].HasAbs || got[0].AbsX != 200 || got[0].AbsY != 300 {
		t.Errorf("ilk olay hareket olmalı: %+v", got[0])
	}
	if !got[1].Press || got[1].Button != ButtonLeft {
		t.Errorf("ikinci olay sol basma olmalı: %+v", got[1])
	}
	if !got[2].Release || got[2].Button != ButtonLeft {
		t.Errorf("üçüncü olay sol bırakma olmalı: %+v", got[2])
	}
}

// İlk hareket yalnızca dikeyse X hiç gelmez (çekirdek, aygıtın başlangıç
// değeriyle aynı olan ekseni bastırır). Başlangıç değeri bilinen konum
// sayılmalı; yoksa imleç yerinde sayar.
func TestVNCIlkHareketTekEksenliKaybolmaz(t *testing.T) {
	a := newTestActivity(1280, 800)
	c := vncCaps(1280, 800)
	c.RangeX.Value = 0
	c.RangeY.Value = 0
	d := newPointerDecoder(a, c)

	d.feed(evAbs, absY, 500)
	d.feed(evSyn, 0, 0)
	got := drain(a)
	if len(got) != 1 || got[0].AbsX != 0 || got[0].AbsY != 500 {
		t.Fatalf("ilk tek eksenli hareket kayboldu: %+v (beklenen 0,500)", got)
	}
}

func TestVNCTekerlegiIletilir(t *testing.T) {
	a := newTestActivity(1280, 800)
	d := newPointerDecoder(a, vncCaps(1280, 800))

	d.feed(evRel, relWheel, 1)
	d.feed(evSyn, 0, 0)
	got := drain(a)
	if len(got) != 1 || got[0].Wheel != 1 {
		t.Fatalf("VNC tekerleği iletilmedi: %+v (beklenen Wheel=1)", got)
	}
}

// VNC ekranı paneldekinden farklı boyuttaysa (panel çözünürlüğü değiştirdi,
// VNC henüz yeniden başlamadı) konum ORANTILI ölçeklenmeli.
func TestVNCOlceklemeModDegisince(t *testing.T) {
	a := newTestActivity(1920, 1080)
	d := newPointerDecoder(a, vncCaps(1280, 720))

	d.feed(evAbs, absX, 1279)
	d.feed(evAbs, absY, 719)
	d.feed(evSyn, 0, 0)
	got := drain(a)
	if len(got) != 1 || got[0].AbsX != 1919 || got[0].AbsY != 1079 {
		t.Fatalf("sağ alt köşe ölçeklenmedi: %+v (beklenen 1919,1079)", got)
	}
}

// Temas bildiren GERÇEK dokunmatik ekran davranışı değişmemeli: parmak
// değmeden gelen konum imleci taşımaz.
func TestDokunmatikEkranTemassizHareketEtmez(t *testing.T) {
	a := newTestActivity(1280, 800)
	c := vncCaps(1280, 800)
	c.Hover = false
	d := newPointerDecoder(a, c)

	d.feed(evAbs, absX, 640)
	d.feed(evAbs, absY, 360)
	d.feed(evSyn, 0, 0)
	if got := drain(a); len(got) != 0 {
		t.Fatalf("temassız dokunmatik ekran imleci taşıdı: %+v", got)
	}
}
