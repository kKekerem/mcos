package fbpanel

import (
	"errors"
	"image"
	"sync"
	"testing"
	"time"

	"mcos/internal/fbinput"
	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// GÜÇ EYLEMİ ÇIKIŞ YOLU
// ════════════════════════════════════════════════════════════════════════════
//
// ── Neden bu test var ───────────────────────────────────────────────────────
//
// Kapatma yolu QEMU'da iki kez BOZUK bulundu ve ikisini de hiçbir test
// yakalamamıştı, çünkü Run döngüsünün güç dalı hiç sınanmamıştı:
//
//  1. Onay penceresi Enter ile İPTAL oluyordu (düğmede "Enter" yazdığı hâlde).
//  2. Panel çıktıktan sonra mcos-launch'ın kurtarma menüsü kapanış
//     animasyonunun üstüne basılıyor ve 15 saniye sonra paneli KAPANMAKTA
//     OLAN sistemde yeniden açıyordu.
//
// İkincisinin çözümü, panelin "bilerek kapandım" diyebilmesi: Run artık
// ErrPowerPending döndürüyor ve mcos-panel-fb bunu 65 çıkış koduna çeviriyor.
// Bu test o sözleşmeyi kilitler.

type fakeDisplay struct {
	mu     sync.Mutex
	blanks int
}

func (d *fakeDisplay) Flip(*image.RGBA) error { return nil }
func (d *fakeDisplay) Blank(bool) bool {
	d.mu.Lock()
	d.blanks++
	d.mu.Unlock()
	return true
}

type fakeHost struct {
	keys chan fbinput.Key
	act  chan struct{}
	ptr  chan fbinput.PointerEvent

	mu   sync.Mutex
	got  []string
	fail error
}

func newFakeHost() *fakeHost {
	return &fakeHost{
		keys: make(chan fbinput.Key),
		act:  make(chan struct{}),
		ptr:  make(chan fbinput.PointerEvent),
	}
}

func (h *fakeHost) Keys() <-chan fbinput.Key             { return h.keys }
func (h *fakeHost) Activity() <-chan struct{}            { return h.act }
func (h *fakeHost) Pointer() <-chan fbinput.PointerEvent { return h.ptr }
func (h *fakeHost) Power(action string) error {
	h.mu.Lock()
	h.got = append(h.got, action)
	err := h.fail
	h.mu.Unlock()
	return err
}

func (h *fakeHost) actions() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.got...)
}

// runPower starts the loop, queues a power action and waits for the loop to
// return. Animasyonlar KAPALI: bu test kapanış animasyonunu değil, ÇIKIŞ
// SÖZLEŞMESİNİ sınıyor (animasyonun kendi testi var: power_anim_test.go).
func runPower(t *testing.T, act Action, hostErr error) (error, []string) {
	t.Helper()
	a, img := newTestApp(t)
	cfg := model.DefaultConfig()
	cfg.UI.Animations = false
	a.SetConfig(cfg)

	host := newFakeHost()
	host.fail = hostErr

	done := make(chan error, 1)
	go func() {
		done <- a.Run(img, &fakeDisplay{}, host, Options{
			Poll:       time.Hour, // yoklama bu testte gürültü
			Anim:       10 * time.Millisecond,
			EscTimeout: 10 * time.Millisecond,
			Frame:      10 * time.Millisecond,
		})
	}()

	// Onay penceresinin geri çağrısının yaptığı şey: eylemi sıraya koy.
	a.mu.Lock()
	a.pending = act
	a.mu.Unlock()

	select {
	case err := <-done:
		return err, host.actions()
	case <-time.After(10 * time.Second):
		t.Fatal("Run güç eyleminden sonra dönmedi — panel kapanmıyor")
		return nil, nil
	}
}

func TestPoweroffReturnsPowerPending(t *testing.T) {
	err, acts := runPower(t, ActPoweroff, nil)
	if !errors.Is(err, ErrPowerPending) {
		t.Fatalf("Run %v döndü; ErrPowerPending bekleniyordu — mcos-launch "+
			"bunu 'panel normal kapandı' sanıp kurtarma menüsünü açar", err)
	}
	if len(acts) != 1 || acts[0] != "poweroff" {
		t.Errorf("konağa iletilen eylemler %v; [poweroff] bekleniyordu", acts)
	}
}

func TestRebootReturnsPowerPending(t *testing.T) {
	err, acts := runPower(t, ActReboot, nil)
	if !errors.Is(err, ErrPowerPending) {
		t.Fatalf("Run %v döndü; ErrPowerPending bekleniyordu", err)
	}
	if len(acts) != 1 || acts[0] != "reboot" {
		t.Errorf("konağa iletilen eylemler %v; [reboot] bekleniyordu", acts)
	}
}

// İstek İLETİLEMEZSE bu GERÇEK bir hatadır: panel yeniden açılmalı, yoksa
// kullanıcı kapanmayan bir makinede kara ekranla kalır.
func TestPowerFailureIsNotPowerPending(t *testing.T) {
	boom := errors.New("poweroff bulunamadı")
	err, _ := runPower(t, ActPoweroff, boom)
	if errors.Is(err, ErrPowerPending) {
		t.Fatal("güç isteği BAŞARISIZ olduğu hâlde ErrPowerPending döndü — " +
			"ekran siyah kalır ve panel bir daha açılmaz")
	}
	if !errors.Is(err, boom) {
		t.Errorf("Run %v döndü; konağın hatası bekleniyordu", err)
	}
}
