package vnc

import "testing"

// Piksel biçimi dönüşümünün testleri.
//
// NEDEN: bu kod yalnızca istemci DÜŞÜK RENK istediğinde çalışır (RealVNC'nin
// "Otomatik" renk ayarı yavaş bağlantıda 16 bite düşer). Yani hata, ancak
// belirli bir istemci belirli bir ağda bağlandığında görünür — geliştirici
// makinesinde asla. Testler o yolu burada kilitliyor.

func TestNativeFormatIsFastPath(t *testing.T) {
	if !nativeFormat.isNative() {
		t.Fatal("doğal biçim kendini doğal saymıyor — her kare boşuna dönüştürülür")
	}
	// Sunucunun ServerInit'te bildirdiği değerlerle AYNI olmalı; ayrışırsa
	// istemci bizim göndermediğimiz bir biçim bekler.
	if nativeFormat.bpp != 32 || nativeFormat.rShift != 0 ||
		nativeFormat.gShift != 8 || nativeFormat.bShift != 16 {
		t.Errorf("doğal biçim ServerInit ile uyuşmuyor: %+v", nativeFormat)
	}
}

func TestParsePixelFormat(t *testing.T) {
	body := []byte{
		16, 16, 0, 1, // bpp, depth, bigEndian, trueColour
		0, 31, 0, 63, 0, 31, // rMax, gMax, bMax (RGB565)
		11, 5, 0, // shifts
		0, 0, 0, // padding
	}
	got := parsePixelFormat(body)
	if got.bpp != 16 || got.gMax != 63 || got.rShift != 11 {
		t.Fatalf("RGB565 yanlış çözüldü: %+v", got)
	}
	if got.isNative() {
		t.Error("RGB565 doğal biçim sayıldı — dönüşüm atlanırdı")
	}
	if !got.usable() {
		t.Error("RGB565 kullanılamaz sayıldı")
	}
}

// 16 bitlik dönüşümde beyaz BEYAZ kalmalı: kanalları yalnızca kaydırmak
// (v<<3 gibi) en parlak değeri griye düşürür.
func TestEncodeRow565KeepsWhiteWhite(t *testing.T) {
	f := pixelFormat{bpp: 16, depth: 16, trueColour: true,
		rMax: 31, gMax: 63, bMax: 31, rShift: 11, gShift: 5, bShift: 0}

	src := []byte{255, 255, 255, 255} // bir piksel: beyaz
	dst := make([]byte, 2)
	f.encodeRow(dst, src, 1)

	v := uint16(dst[0]) | uint16(dst[1])<<8
	if v != 0xFFFF {
		t.Errorf("beyaz 565'te %#04x oldu, 0xFFFF olmalıydı", v)
	}
}

// Siyah siyah kalmalı ve kanallar KARIŞMAMALI.
func TestEncodeRowChannelsDoNotMix(t *testing.T) {
	f := pixelFormat{bpp: 32, depth: 24, trueColour: true,
		rMax: 255, gMax: 255, bMax: 255, rShift: 16, gShift: 8, bShift: 0}

	src := []byte{10, 20, 30, 255} // R=10 G=20 B=30
	dst := make([]byte, 4)
	f.encodeRow(dst, src, 1)

	v := uint32(dst[0]) | uint32(dst[1])<<8 | uint32(dst[2])<<16 | uint32(dst[3])<<24
	if r := (v >> 16) & 0xFF; r != 10 {
		t.Errorf("kırmızı %d, 10 olmalıydı", r)
	}
	if g := (v >> 8) & 0xFF; g != 20 {
		t.Errorf("yeşil %d, 20 olmalıydı", g)
	}
	if b := v & 0xFF; b != 30 {
		t.Errorf("mavi %d, 30 olmalıydı", b)
	}
}

// Paletli (true-colour olmayan) kip desteklenmiyor ve bunu AÇIKÇA söylemeli:
// sessizce kabul etmek, istemcide renkli gürültü demekti.
func TestPalettedFormatRejected(t *testing.T) {
	f := pixelFormat{bpp: 8, depth: 8, trueColour: false}
	if f.usable() {
		t.Error("paletli kip kullanılabilir sayıldı")
	}
}
