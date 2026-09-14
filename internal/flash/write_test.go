package flash

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Bu testler, ham diske yazma yolunu GERÇEK BİR DİSKE DOKUNMADAN sınar.
//
// Sahte bir aygıt (bellekte yaşayan memDevice) takılıyor; böylece "bozuk
// bellek yakalanıyor mu?", "sync çağrılıyor mu?", "doğrulama gerçekten geri
// okuyor mu?" sorularının hepsi yanıtlanabiliyor.
//
// Bu testler OLMADAN, yazma kodundaki bir hata ancak kullanıcının USB'si
// açılmadığında ortaya çıkardı — ve nedeni asla anlaşılmazdı.

// memDevice is a deviceFile backed by a byte slice.
type memDevice struct {
	buf    []byte
	pos    int64
	synced int
	closed bool
	// corruptAt, geri okumada bozulma taklidi yapar (bozuk bellek sınaması).
	corruptAt int
	failWrite error
}

func (m *memDevice) Write(p []byte) (int, error) {
	if m.failWrite != nil {
		return 0, m.failWrite
	}
	need := int(m.pos) + len(p)
	if need > len(m.buf) {
		m.buf = append(m.buf, make([]byte, need-len(m.buf))...)
	}
	copy(m.buf[m.pos:], p)
	m.pos += int64(len(p))
	return len(p), nil
}

func (m *memDevice) Read(p []byte) (int, error) {
	if m.pos >= int64(len(m.buf)) {
		return 0, io.EOF
	}
	n := copy(p, m.buf[m.pos:])
	m.pos += int64(n)
	return n, nil
}

func (m *memDevice) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		m.pos = off
	case io.SeekCurrent:
		m.pos += off
	case io.SeekEnd:
		m.pos = int64(len(m.buf)) + off
	}
	return m.pos, nil
}

func (m *memDevice) Sync() error {
	m.synced++
	// Bozulma taklidi: veri diske "indikten" sonra bir baytı çevir.
	if m.corruptAt >= 0 && m.corruptAt < len(m.buf) {
		m.buf[m.corruptAt] ^= 0xFF
	}
	return nil
}

func (m *memDevice) Close() error {
	m.closed = true
	return nil
}

// install swaps in the fake backend and returns a restore function.
func install(t *testing.T, dev *memDevice) {
	t.Helper()
	oldOpen, oldDrop := openDeviceFn, dropCachesFn
	openDeviceFn = func(string) (deviceFile, error) { return dev, nil }
	dropCachesFn = func(deviceFile) error { return nil }
	t.Cleanup(func() {
		openDeviceFn, dropCachesFn = oldOpen, oldDrop
	})
}

// writableDevice returns a Device that passes Validate.
func writableDevice() Device {
	return Device{
		Path:      "/dev/fake0",
		Name:      "fake0",
		SizeBytes: MinSizeBytes * 2,
		Removable: true,
		Model:     "Sahte Bellek",
		Bus:       "usb",
	}
}

// tempImage writes n random bytes to a temp file and returns its path.
func tempImage(t *testing.T, n int) string {
	t.Helper()
	data := make([]byte, n)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("rastgele veri üretilemedi: %v", err)
	}
	p := filepath.Join(t.TempDir(), "test.img")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("imaj yazılamadı: %v", err)
	}
	return p
}

func TestWriteCopiesImageAndVerifies(t *testing.T) {
	const size = 9 << 20 // birden çok 4 MiB parçaya yayılsın
	img := tempImage(t, size)
	want, err := os.ReadFile(img)
	if err != nil {
		t.Fatal(err)
	}

	dev := &memDevice{corruptAt: -1}
	install(t, dev)

	w := Writer{ImagePath: img, Device: writableDevice(), Verify: true}
	var last Progress
	if err := w.Write(context.Background(), func(p Progress) { last = p }); err != nil {
		t.Fatalf("yazma başarısız: %v", err)
	}

	if !bytes.Equal(dev.buf[:size], want) {
		t.Error("aygıta yazılan veri imajla aynı değil")
	}
	if dev.synced == 0 {
		t.Error("Sync() hiç çağrılmadı — veri bellekte kalmış olabilir")
	}
	if !dev.closed {
		t.Error("aygıt kapatılmadı")
	}
	if !last.Done || last.Err != nil {
		t.Errorf("son ilerleme bildirimi yanlış: %+v", last)
	}
	if !strings.Contains(last.Stage, "doğrulandı") {
		t.Errorf("doğrulama aşaması bildirilmedi: %q", last.Stage)
	}
}

// Bu testin yakaladığı hata, bu aracın VAROLUŞ NEDENİDİR: ucuz bir USB
// bellek yazmayı "başarılı" bildirip veriyi yanlış yazabilir. Doğrulama
// olmadan kullanıcı açılmayan bir diskle kalır ve nedenini asla bilemez.
func TestWriteDetectsCorruptedMedia(t *testing.T) {
	img := tempImage(t, 5<<20)

	dev := &memDevice{corruptAt: 1234} // Sync sırasında bir bayt bozulsun
	install(t, dev)

	w := Writer{ImagePath: img, Device: writableDevice(), Verify: true}
	err := w.Write(context.Background(), nil)
	if err == nil {
		t.Fatal("bozuk aygıt yakalanmadı — doğrulama işe yaramıyor")
	}
	if !strings.Contains(err.Error(), "doğrulama") {
		t.Errorf("hata doğrulama hatası gibi görünmüyor: %v", err)
	}
}

func TestWriteRefusesSystemDisk(t *testing.T) {
	img := tempImage(t, 1<<20)
	dev := &memDevice{corruptAt: -1}
	install(t, dev)

	target := writableDevice()
	target.System = true

	w := Writer{ImagePath: img, Device: target}
	if err := w.Write(context.Background(), nil); err == nil {
		t.Fatal("sistem diskine yazma engellenmedi")
	}
	if len(dev.buf) != 0 {
		t.Error("sistem diskine BAYT YAZILDI — bu felaket olurdu")
	}
}

func TestWriteRefusesImageLargerThanDevice(t *testing.T) {
	img := tempImage(t, 4<<20)
	dev := &memDevice{corruptAt: -1}
	install(t, dev)

	target := writableDevice()
	target.SizeBytes = MinSizeBytes // Validate'i geçer
	// İmaj aygıttan büyük olsun.
	big := Writer{ImagePath: img, Device: target}
	big.Device.SizeBytes = 1 << 20

	if err := big.Write(context.Background(), nil); err == nil {
		t.Fatal("aygıttan büyük imaj kabul edildi")
	}
	if len(dev.buf) != 0 {
		t.Error("reddedilen yazmada aygıta dokunuldu")
	}
}

// Deneme kipi HİÇBİR aygıtı açmamalı: geliştirme sırasında akışı sınamanın
// tek güvenli yolu budur ve "dry-run yazdı" hatası onarılamaz.
func TestDryRunNeverOpensDevice(t *testing.T) {
	img := tempImage(t, 2<<20)

	oldOpen := openDeviceFn
	opened := false
	openDeviceFn = func(string) (deviceFile, error) {
		opened = true
		return nil, errors.New("açılmamalıydı")
	}
	t.Cleanup(func() { openDeviceFn = oldOpen })

	w := Writer{ImagePath: img, Device: writableDevice(), DryRun: true}
	if err := w.Write(context.Background(), nil); err != nil {
		t.Fatalf("deneme kipi hata verdi: %v", err)
	}
	if opened {
		t.Fatal("deneme kipinde aygıt AÇILDI")
	}
}

func TestWriteStopsOnCancel(t *testing.T) {
	img := tempImage(t, 16<<20)
	dev := &memDevice{corruptAt: -1}
	install(t, dev)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // daha başlamadan iptal

	w := Writer{ImagePath: img, Device: writableDevice()}
	if err := w.Write(ctx, nil); err == nil {
		t.Fatal("iptal edilen yazma hata döndürmedi")
	}
}

func TestValidateRejectsTooSmall(t *testing.T) {
	d := writableDevice()
	d.SizeBytes = MinSizeBytes - 1
	err := Validate(d)
	if err == nil {
		t.Fatal("küçük aygıt kabul edildi")
	}
	if !strings.Contains(err.Error(), "küçük") {
		t.Errorf("hata mesajı boyutu anlatmıyor: %v", err)
	}
}
