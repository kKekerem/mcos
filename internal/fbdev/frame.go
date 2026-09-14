package fbdev

import (
	"encoding/binary"
	"fmt"
	"image"
	"os"
	"path/filepath"
)

// Bu dosya BİR KAREYİ iki program arasında taşır.
//
// ── Neden? ──────────────────────────────────────────────────────────────────
// Açılış ekranı (mcos-splash) ve panel (mcos-panel-fb) AYRI programlardır.
// Kullanıcının istediği geçiş — "boot animasyonu bitince içeri zoomlanarak
// blur ile OOBE'nin ilk ekranı gelsin" — panelin, açılış ekranının SON
// KARESİNİ bilmesini gerektirir. Aksi halde geçiş siyahtan başlar ve
// "içeri dalma" hissi kaybolur.
//
// ── Neden PNG değil? ────────────────────────────────────────────────────────
// PNG sıkıştırması 1080p'de ~80 ms sürer ve açılışta her milisaniye görünür.
// Ham RGBA yazmak bir memcpy'dir: 8.3 MB, tmpfs'e ~15 ms. Dosya /run
// altındadır (RAM diski), yani diske hiç dokunmaz ve yeniden başlatınca
// kendiliğinden kaybolur — tam olarak istediğimiz ömür.
//
// ── Biçim ───────────────────────────────────────────────────────────────────
//
//	sihirli sayı  "MCFB"   4 bayt
//	genişlik      uint32   küçük sonlu
//	yükseklik     uint32   küçük sonlu
//	piksel        w*h*4    RGBA
//
// Sihirli sayı olmadan, yarım yazılmış veya alakasız bir dosya çöp piksel
// olarak ekrana basılırdı.

const frameMagic = "MCFB"

// frameHeaderSize is magic + width + height.
const frameHeaderSize = 4 + 4 + 4

// SaveFrame writes an RGBA image to path in the raw MCFB format.
//
// Önce geçici dosyaya yazıp taşır: panel, açılış ekranı hâlâ yazarken
// dosyayı okursa yarım bir kare görürdü.
func SaveFrame(img *image.RGBA, path string) error {
	if img == nil {
		return fmt.Errorf("kare yok")
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return fmt.Errorf("geçersiz kare boyutu %dx%d", w, h)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	hdr := make([]byte, frameHeaderSize)
	copy(hdr, frameMagic)
	binary.LittleEndian.PutUint32(hdr[4:], uint32(w))
	binary.LittleEndian.PutUint32(hdr[8:], uint32(h))
	if _, err := f.Write(hdr); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if _, err := f.Write(img.Pix); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// LoadFrame reads an MCFB file, returning nil if it is missing or malformed.
//
// HATA DÖNDÜRMEZ, nil döndürür: açılış karesinin bulunmaması normaldir
// (açılış ekranı kapalıysa, ya da panel elle başlatıldıysa). Panel bunu bir
// hata olarak göstermemeli, yalnızca geçiş animasyonunu atlamalıdır.
func LoadFrame(path string, want image.Rectangle) *image.RGBA {
	data, err := os.ReadFile(path)
	if err != nil || len(data) < frameHeaderSize {
		return nil
	}
	if string(data[:4]) != frameMagic {
		return nil
	}
	w := int(binary.LittleEndian.Uint32(data[4:]))
	h := int(binary.LittleEndian.Uint32(data[8:]))
	if w <= 0 || h <= 0 {
		return nil
	}
	need := frameHeaderSize + w*h*4
	if len(data) < need {
		return nil
	}
	// Çözünürlük DEĞİŞTİYSE kareyi kullanma: farklı boyutta bir kareyi
	// karıştırmak, ekranın çapraz kaymış bir kopyasını üretir.
	if want.Dx() != w || want.Dy() != h {
		return nil
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	copy(img.Pix, data[frameHeaderSize:need])
	return img
}
