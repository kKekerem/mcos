//go:build linux

package drm

import (
	"encoding/binary"
	"testing"
)

// Firmware'in ŞU AN sürdüğü çıkış seçilmeli: kullanıcının açılışta baktığı
// ekran odur (dizüstünde kapak açıkken eDP, masaüstünde takılı monitör).
func TestBirincilCikisSurulen(t *testing.T) {
	c := []outputCand{
		{conn: connector{Type: 11}}, // HDMI, sürülmüyor
		{conn: connector{Type: 14}, activeCrtc: 41, activeArea: 1920 * 1080}, // eDP, sürülüyor
	}
	if got := pickPrimary(c); got != 1 {
		t.Fatalf("sürülen eDP seçilmeli, %d seçildi", got)
	}
}

func TestBirincilCikisSanalDegil(t *testing.T) {
	c := []outputCand{
		{conn: connector{Type: connVirtual}},
		{conn: connector{Type: 10}}, // DP
	}
	if got := pickPrimary(c); got != 1 {
		t.Fatalf("gerçek monitör seçilmeli, %d seçildi", got)
	}
}

// Mod reddedilirse aynı çözünürlükte DÜŞÜK tazeleme, sonra küçük çözünürlük.
func TestGeriDususSirasi(t *testing.T) {
	all := []Mode{
		{Width: 3840, Height: 2160, MilliHz: 144000},
		{Width: 3840, Height: 2160, MilliHz: 60000},
		{Width: 2560, Height: 1440, MilliHz: 144000},
		{Width: 1920, Height: 1080, MilliHz: 60000},
	}
	got := fallbackOrder(all[0], all)
	if len(got) != 4 || got[0].MilliHz != 144000 || got[1].MilliHz != 60000 ||
		got[1].Width != 3840 || got[2].Width != 2560 || got[3].Width != 1920 {
		t.Fatalf("sıra yanlış: %v", got)
	}
}

// olay builds a DRM event whose length field says ln (tampon en az 8 bayt).
func olay(typ uint32, ln int) []byte {
	b := make([]byte, max(ln, 8))
	binary.LittleEndian.PutUint32(b[0:], typ)
	binary.LittleEndian.PutUint32(b[4:], uint32(ln))
	return b
}

// Tek read() birden çok olay getirebilir; yalnızca FLIP_COMPLETE sayılır.
func TestCevirmeOlaylariSayilir(t *testing.T) {
	var buf []byte
	buf = append(buf, olay(0x02, 32)...) // FLIP_COMPLETE
	buf = append(buf, olay(0x01, 32)...) // VBLANK
	buf = append(buf, olay(0x02, 32)...)
	if n := countFlipEvents(buf); n != 2 {
		t.Fatalf("2 çevirme olayı bekleniyordu, %d", n)
	}
	// Bozuk uzunluk sonsuz döngüye sokmamalı.
	if n := countFlipEvents(olay(0x02, 4)); n != 0 {
		t.Fatalf("bozuk olay sayıldı: %d", n)
	}
}
