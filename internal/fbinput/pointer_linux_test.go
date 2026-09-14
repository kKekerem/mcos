//go:build linux

package fbinput

import (
	"testing"
	"time"
)

// evdev → imleç çözücüsünün testleri.
//
// ── Neden burada sınanmalı? ─────────────────────────────────────────────────
// Bu kod GERÇEK donanımdan gelen ham baytları yorumluyor. Bir hata, "fare
// ters çalışıyor", "touchpad zıplıyor" ya da "imleç hiç kıpırdamıyor" olarak
// ortaya çıkar ve hata ayıklaması çok pahalıdır — kullanıcının makinesine
// erişim gerekir.
//
// Burada olay akışlarını elle üretip çıktıyı doğruluyoruz; donanım gerekmez.

// newTestActivity builds an Activity with a known screen size.
func newTestActivity(w, h int) *Activity {
	a := &Activity{
		wake: make(chan struct{}, 1),
		ptr:  make(chan PointerEvent, pointerQueue),
	}
	a.scrW.Store(int64(w))
	a.scrH.Store(int64(h))
	return a
}

// drain collects every queued pointer event.
func drain(a *Activity) []PointerEvent {
	var out []PointerEvent
	for {
		select {
		case e := <-a.ptr:
			out = append(out, e)
		default:
			return out
		}
	}
}

func mouseCaps() caps {
	return caps{Kind: KindMouse, Name: "Test Mouse", HasBtnL: true, HasWhl: true}
}

func padCaps() caps {
	return caps{
		Kind:   KindTouchpad,
		Name:   "Test Touchpad",
		HasMT:  true,
		HasDbl: true,
		RangeX: absInfo{Min: 0, Max: 1000},
		RangeY: absInfo{Min: 0, Max: 500},
	}
}

func TestMouseMotionAccumulatesPerPacket(t *testing.T) {
	a := newTestActivity(1920, 1080)
	d := newPointerDecoder(a, mouseCaps())

	// Tek bir paket: üç REL olayı + SYN. Çıkışta TEK olay olmalı.
	d.feed(evRel, relX, 3)
	d.feed(evRel, relX, 4)
	d.feed(evRel, relY, -2)
	d.feed(evSyn, 0, 0)

	got := drain(a)
	if len(got) != 1 {
		t.Fatalf("bir paketten %d olay çıktı (1 olmalı): %+v", len(got), got)
	}
	if got[0].DX != 7 || got[0].DY != -2 {
		t.Errorf("hareket yanlış birleştirildi: %+v", got[0])
	}
}

// Hiç hareket yoksa olay üretilmemeli: boş olaylar ana döngüyü boşuna
// uyandırır ve her karede yeniden çizim yaptırır.
func TestMouseNoMotionNoEvent(t *testing.T) {
	a := newTestActivity(1920, 1080)
	d := newPointerDecoder(a, mouseCaps())

	d.feed(evSyn, 0, 0)
	d.feed(evSyn, 0, 0)

	if got := drain(a); len(got) != 0 {
		t.Errorf("hareketsizken %d olay üretildi: %+v", len(got), got)
	}
}

func TestMouseButtons(t *testing.T) {
	a := newTestActivity(1920, 1080)
	d := newPointerDecoder(a, mouseCaps())

	d.feed(evKey, btnLeft, 1)
	d.feed(evSyn, 0, 0)
	d.feed(evKey, btnLeft, 0)
	d.feed(evSyn, 0, 0)

	got := drain(a)
	if len(got) != 2 {
		t.Fatalf("iki düğme olayı bekleniyordu, %d geldi: %+v", len(got), got)
	}
	if !got[0].Press || got[0].Button != ButtonLeft {
		t.Errorf("basma olayı yanlış: %+v", got[0])
	}
	if !got[1].Release || got[1].Button != ButtonLeft {
		t.Errorf("bırakma olayı yanlış: %+v", got[1])
	}
}

func TestMouseWheel(t *testing.T) {
	a := newTestActivity(1920, 1080)
	d := newPointerDecoder(a, mouseCaps())

	d.feed(evRel, relWheel, 1)
	d.feed(evSyn, 0, 0)

	got := drain(a)
	if len(got) != 1 || got[0].Wheel != 1 {
		t.Fatalf("tekerlek olayı yanlış: %+v", got)
	}
}

// Touchpad: parmak indiğinde imleç ZIPLAMAMALI.
//
// Bu, touchpad kodunda yapılan klasik hatadır: parmağın yeni indiği yer ile
// ÖNCEKİ dokunuşun bittiği yer arasındaki fark hareket sanılır ve imleç
// ekranın öbür ucuna fırlar.
func TestTouchpadFirstTouchDoesNotJump(t *testing.T) {
	a := newTestActivity(1920, 1080)
	d := newPointerDecoder(a, padCaps())

	// İlk dokunuş: pad'in sol üstünde.
	d.feed(evKey, btnTouch, 1)
	d.feed(evAbs, absMTPosX, 10)
	d.feed(evAbs, absMTPosY, 10)
	d.feed(evSyn, 0, 0)

	if got := drain(a); len(got) != 0 {
		t.Fatalf("ilk dokunuşta hareket üretildi: %+v", got)
	}

	// İkinci paket: 100 birim sağa.
	d.feed(evAbs, absMTPosX, 110)
	d.feed(evAbs, absMTPosY, 10)
	d.feed(evSyn, 0, 0)

	got := drain(a)
	if len(got) != 1 {
		t.Fatalf("hareket olayı bekleniyordu, %d geldi", len(got))
	}
	// Pad genişliği 1000, ekran 1920 → 100 birim ≈ 192 piksel.
	if got[0].DX < 150 || got[0].DX > 240 {
		t.Errorf("touchpad ölçekleme yanlış: DX=%d (≈192 beklenir)", got[0].DX)
	}

	// Parmak kalkıp pad'in ÖBÜR UCUNA insin: sıçrama OLMAMALI.
	d.feed(evKey, btnTouch, 0)
	d.feed(evSyn, 0, 0)
	drain(a)

	d.feed(evKey, btnTouch, 1)
	d.feed(evAbs, absMTPosX, 900)
	d.feed(evAbs, absMTPosY, 400)
	d.feed(evSyn, 0, 0)

	if got := drain(a); len(got) != 0 {
		t.Errorf("yeni dokunuşta imleç zıpladı: %+v", got)
	}
}

// Dokunarak tıklama: kısa ve az hareketli bir dokunuş sol tık üretmeli.
func TestTouchpadTapToClick(t *testing.T) {
	a := newTestActivity(1920, 1080)
	d := newPointerDecoder(a, padCaps())

	d.feed(evKey, btnTouch, 1)
	d.feed(evAbs, absMTPosX, 500)
	d.feed(evAbs, absMTPosY, 250)
	d.feed(evSyn, 0, 0)
	d.feed(evKey, btnTouch, 0)
	d.feed(evSyn, 0, 0)

	got := drain(a)
	press, release := false, false
	for _, e := range got {
		if e.Press && e.Button == ButtonLeft {
			press = true
		}
		if e.Release && e.Button == ButtonLeft {
			release = true
		}
	}
	if !press || !release {
		t.Errorf("dokunma tıklama üretmedi: %+v", got)
	}
}

// Uzun süren bir dokunuş tıklama SAYILMAMALI: sürükleme başlangıcını
// tıklama saymak, kullanıcının istemediği eylemleri tetikler.
func TestTouchpadLongPressIsNotTap(t *testing.T) {
	a := newTestActivity(1920, 1080)
	d := newPointerDecoder(a, padCaps())

	d.feed(evKey, btnTouch, 1)
	d.feed(evAbs, absMTPosX, 500)
	d.feed(evAbs, absMTPosY, 250)
	d.feed(evSyn, 0, 0)

	// Dokunuşun başlangıcını geriye al: süre sınırını aşmış gibi yap.
	d.touchAt = time.Now().Add(-2 * tapMaxDuration)

	d.feed(evKey, btnTouch, 0)
	d.feed(evSyn, 0, 0)

	for _, e := range drain(a) {
		if e.Press || e.Release {
			t.Errorf("uzun basış tıklama sayıldı: %+v", e)
		}
	}
}

// Çok hareketli bir dokunuş da tıklama sayılmamalı (sürükleme).
func TestTouchpadDragIsNotTap(t *testing.T) {
	a := newTestActivity(1920, 1080)
	d := newPointerDecoder(a, padCaps())

	d.feed(evKey, btnTouch, 1)
	d.feed(evAbs, absMTPosX, 100)
	d.feed(evAbs, absMTPosY, 100)
	d.feed(evSyn, 0, 0)
	for x := 150; x <= 900; x += 50 {
		d.feed(evAbs, absMTPosX, int32(x))
		d.feed(evAbs, absMTPosY, 100)
		d.feed(evSyn, 0, 0)
	}
	drain(a)

	d.feed(evKey, btnTouch, 0)
	d.feed(evSyn, 0, 0)

	for _, e := range drain(a) {
		if e.Press || e.Release {
			t.Errorf("sürükleme tıklama sayıldı: %+v", e)
		}
	}
}

// İki parmakla dikey hareket, imleci değil TEKERLEĞİ sürmeli.
func TestTouchpadTwoFingerScroll(t *testing.T) {
	a := newTestActivity(1920, 1080)
	d := newPointerDecoder(a, padCaps())

	d.feed(evKey, btnTouch, 1)
	d.feed(evKey, btnToolDoubl, 1)
	d.feed(evAbs, absMTPosX, 500)
	d.feed(evAbs, absMTPosY, 100)
	d.feed(evSyn, 0, 0)
	drain(a)

	// Aşağı doğru belirgin bir hareket.
	for y := 150; y <= 450; y += 50 {
		d.feed(evAbs, absMTPosX, 500)
		d.feed(evAbs, absMTPosY, int32(y))
		d.feed(evSyn, 0, 0)
	}

	got := drain(a)
	wheel, motion := 0, 0
	for _, e := range got {
		wheel += e.Wheel
		if e.DX != 0 || e.DY != 0 {
			motion++
		}
	}
	if wheel == 0 {
		t.Fatalf("iki parmakla kaydırma tekerlek üretmedi: %+v", got)
	}
	if motion != 0 {
		t.Errorf("iki parmak sırasında imleç de hareket etti (%d olay)", motion)
	}
}

// İkinci parmağın olayları (slot 1) imleci sürmemeli.
func TestTouchpadIgnoresSecondSlot(t *testing.T) {
	a := newTestActivity(1920, 1080)
	d := newPointerDecoder(a, padCaps())

	d.feed(evKey, btnTouch, 1)
	d.feed(evAbs, absMTSlot, 0)
	d.feed(evAbs, absMTPosX, 100)
	d.feed(evAbs, absMTPosY, 100)
	d.feed(evSyn, 0, 0)
	drain(a)

	// İkinci parmak pad'in öbür ucunda: yok sayılmalı.
	d.feed(evAbs, absMTSlot, 1)
	d.feed(evAbs, absMTPosX, 900)
	d.feed(evAbs, absMTPosY, 400)
	d.feed(evSyn, 0, 0)

	if got := drain(a); len(got) != 0 {
		t.Errorf("ikinci parmak imleci hareket ettirdi: %+v", got)
	}
}

// Dokunmatik ekran: parmağın değdiği nokta imlecin gittiği yer olmalı.
func TestTouchscreenMapsAbsolutePosition(t *testing.T) {
	a := newTestActivity(1920, 1080)
	c := caps{
		Kind:   KindTouchscreen,
		HasMT:  true,
		RangeX: absInfo{Min: 0, Max: 4095},
		RangeY: absInfo{Min: 0, Max: 4095},
	}
	d := newPointerDecoder(a, c)

	d.feed(evKey, btnTouch, 1)
	d.feed(evAbs, absMTPosX, 2048) // tam orta
	d.feed(evAbs, absMTPosY, 2048)
	d.feed(evSyn, 0, 0)

	got := drain(a)
	if len(got) != 1 || !got[0].HasAbs {
		t.Fatalf("mutlak konum olayı üretilmedi: %+v", got)
	}
	if got[0].AbsX < 900 || got[0].AbsX > 1020 {
		t.Errorf("X eşlemesi yanlış: %d (≈959 beklenir)", got[0].AbsX)
	}
	if got[0].AbsY < 500 || got[0].AbsY > 580 {
		t.Errorf("Y eşlemesi yanlış: %d (≈539 beklenir)", got[0].AbsY)
	}
}

// Klavye gibi aygıtlar için çözücü kurulmamalı.
func TestNonPointerDeviceHasNoDecoder(t *testing.T) {
	a := newTestActivity(1920, 1080)
	if d := newPointerDecoder(a, caps{Kind: KindOther}); d != nil {
		t.Error("klavye için imleç çözücüsü kuruldu")
	}
}

// Yavaş touchpad hareketinde yuvarlama kaybı imleci dondurmamalı.
//
// Her paket 0.4 piksel taşıyorsa ve kesir atılırsa imleç HİÇ kıpırdamaz.
// Kesir saklanmalı ve biriktikçe piksele dönüşmeli.
func TestSubPixelMotionAccumulates(t *testing.T) {
	a := newTestActivity(1920, 1080)
	d := newPointerDecoder(a, padCaps())

	d.feed(evKey, btnTouch, 1)
	d.feed(evAbs, absMTPosX, 500)
	d.feed(evAbs, absMTPosY, 250)
	d.feed(evSyn, 0, 0)
	drain(a)

	// Her pakette 1 birim (≈1.92 piksel) — ama yönü değişmeden.
	total := 0
	for i := 1; i <= 10; i++ {
		d.feed(evAbs, absMTPosX, int32(500+i))
		d.feed(evAbs, absMTPosY, 250)
		d.feed(evSyn, 0, 0)
		for _, e := range drain(a) {
			total += e.DX
		}
	}
	// 10 birim ≈ 19 piksel. Kesir atılsaydı 10 piksel olurdu.
	if total < 15 {
		t.Errorf("kesirli hareket kayboldu: toplam %d piksel (≈19 beklenir)", total)
	}
}

// Kuyruk dolduğunda EN ESKİ olay düşmeli: imleçte önemli olan son konumdur.
func TestPointerQueueDropsOldest(t *testing.T) {
	a := newTestActivity(1920, 1080)
	for i := 0; i < pointerQueue*2; i++ {
		a.emit(PointerEvent{Kind: KindMouse, DX: i})
	}
	got := drain(a)
	if len(got) == 0 {
		t.Fatal("kuyruk tamamen boşaldı")
	}
	// En son gönderilen olay kuyrukta OLMALI.
	last := got[len(got)-1]
	if last.DX != pointerQueue*2-1 {
		t.Errorf("en yeni olay kaybolmuş: DX=%d", last.DX)
	}
}
