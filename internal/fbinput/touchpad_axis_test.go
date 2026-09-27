//go:build linux

package fbinput

import "testing"

// ════════════════════════════════════════════════════════════════════════════
// TOUCHPAD: ÇEKİRDEK DEĞİŞMEYEN EKSENİ GÖNDERMEZ
// ════════════════════════════════════════════════════════════════════════════
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
//
// Kullanıcı "bazı PC'lerde touchpad çalışmıyor" dedi. Ölçüldü ki sorun
// "bazı"dan çok daha geniş: DÜZ hareket HİÇBİR makinede çalışmıyordu.
//
// Çözücü, hareketi yalnızca X ve Y AYNI pakette geldiğinde hesaplıyordu
// (gotX && gotY). Ama Linux girdi katmanı, değeri DEĞİŞMEYEN bir ekseni
// göndermez — parmak tam yatay kayıyorsa ABS_Y hiç gelmez.
//
// Ölçülen (ELAN1200 aralıkları, 1920x1080):
//
//	saf yatay sürükleme, 30 kare   -> 0 olay, 0 piksel
//	saf dikey sürükleme,  30 kare  -> 0 olay, 0 piksel
//	karışık (X her kare, Y her 4.) -> paketlerin %77,5'i düşüyor
//
// Aşağıdaki testler bu davranışı kilitler. Biri "iki eksen de gelsin"
// koşulunu geri koyarsa test anında kırılır.

// ilkTemas performs the first touch so a movement reference exists.
func ilkTemas(d *pointerDecoder, a *Activity, x, y int32) {
	d.feed(evKey, btnTouch, 1)
	d.feed(evAbs, absMTPosX, x)
	d.feed(evAbs, absMTPosY, y)
	d.feed(evSyn, 0, 0)
	drain(a)
}

func TestTouchpadSafYatayHareketCalisir(t *testing.T) {
	a := newTestActivity(1920, 1080)
	d := newPointerDecoder(a, padCaps())
	ilkTemas(d, a, 100, 250)

	// YALNIZCA X gelir: parmak tam yatay kayıyor, çekirdek Y'yi göndermez.
	d.feed(evAbs, absMTPosX, 200)
	d.feed(evSyn, 0, 0)

	got := drain(a)
	if len(got) == 0 {
		t.Fatal("saf yatay harekette HİÇ olay üretilmedi — " +
			"imleç kıpırdamaz (çekirdek değişmeyen Y'yi göndermez)")
	}
	// Pad genişliği 1000, ekran 1920 -> 100 birim ≈ 192 piksel.
	if got[0].DX < 150 || got[0].DX > 240 {
		t.Errorf("DX=%d; ≈192 bekleniyordu", got[0].DX)
	}
	if got[0].DY != 0 {
		t.Errorf("DY=%d; Y hiç gelmediği için 0 olmalı", got[0].DY)
	}
}

func TestTouchpadSafDikeyHareketCalisir(t *testing.T) {
	a := newTestActivity(1920, 1080)
	d := newPointerDecoder(a, padCaps())
	ilkTemas(d, a, 100, 250)

	d.feed(evAbs, absMTPosY, 300)
	d.feed(evSyn, 0, 0)

	got := drain(a)
	if len(got) == 0 {
		t.Fatal("saf dikey harekette HİÇ olay üretilmedi")
	}
	// Pad yüksekliği 500, ekran 1080 -> 50 birim ≈ 108 piksel.
	if got[0].DY < 80 || got[0].DY > 135 {
		t.Errorf("DY=%d; ≈108 bekleniyordu", got[0].DY)
	}
	if got[0].DX != 0 {
		t.Errorf("DX=%d; X hiç gelmediği için 0 olmalı", got[0].DX)
	}
}

// Asıl ölçüm: karışık akışta paketlerin çoğu DÜŞÜYORDU.
func TestTouchpadKarisikAkistaPaketDusmez(t *testing.T) {
	a := newTestActivity(1920, 1080)
	d := newPointerDecoder(a, padCaps())
	ilkTemas(d, a, 100, 250)

	const kare = 40
	x, y := int32(100), int32(250)
	olay := 0
	for i := 0; i < kare; i++ {
		x += 5
		d.feed(evAbs, absMTPosX, x)
		if i%4 == 0 { // Y yalnızca dört karede bir değişiyor
			y += 2
			d.feed(evAbs, absMTPosY, y)
		}
		d.feed(evSyn, 0, 0)
		olay += len(drain(a))
	}

	// Her paket hareket taşıyor; hepsi olaya dönüşmeli.
	if olay < kare-2 {
		t.Errorf("%d pakette yalnızca %d olay üretildi — "+
			"paketlerin %%%.0f'i düştü (eskiden %%77,5 düşüyordu)",
			kare, olay, 100*float64(kare-olay)/float64(kare))
	}
}

// Toplam yer değiştirme DOĞRU olmalı: eksen ayrı izlenince biriken fark
// kaybolmamalı.
func TestTouchpadToplamMesafeDogru(t *testing.T) {
	a := newTestActivity(1920, 1080)
	d := newPointerDecoder(a, padCaps())
	ilkTemas(d, a, 100, 250)

	toplam := 0
	x := int32(100)
	for i := 0; i < 20; i++ {
		x += 10 // toplam 200 birim
		d.feed(evAbs, absMTPosX, x)
		d.feed(evSyn, 0, 0)
		for _, e := range drain(a) {
			toplam += e.DX
		}
	}
	// 200 birim / 1000 pad genişliği * 1920 ekran = 384 piksel.
	if toplam < 340 || toplam > 430 {
		t.Errorf("toplam DX=%d; ≈384 bekleniyordu", toplam)
	}
}

// Yeni dokunuşta sıçrama OLMAMALI — eski davranış korunuyor.
func TestTouchpadYeniTemastaSicramaYok(t *testing.T) {
	a := newTestActivity(1920, 1080)
	d := newPointerDecoder(a, padCaps())
	ilkTemas(d, a, 100, 250)

	d.feed(evAbs, absMTPosX, 150)
	d.feed(evSyn, 0, 0)
	drain(a)

	d.feed(evKey, btnTouch, 0)
	d.feed(evSyn, 0, 0)
	drain(a)

	// Parmak pad'in öbür ucuna iniyor: tek eksen gelse bile sıçramamalı.
	d.feed(evKey, btnTouch, 1)
	d.feed(evAbs, absMTPosX, 900)
	d.feed(evSyn, 0, 0)
	if got := drain(a); len(got) != 0 {
		t.Errorf("yeni dokunuşta imleç zıpladı: %+v", got)
	}
}
