package ipcclient

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/log"
	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// link.enable PANELİ DONDURMAMALI
// ════════════════════════════════════════════════════════════════════════════
//
// link.enable artık ortak dünya modunu ÖNCE kurar ve kurulamazsa nedeniyle
// reddeder (kullanıcının raporu: "ortak dünyayı açınca 'mcos link kurulu
// değil' diyor"). Kurulum fabric-api'yi Modrinth'ten indirebilir (90 sn'ye
// kadar) ve dünya tohumu için sunucunun kaydetmesini bekleyebilir. ipc.Client
// çağrıları TEK kilitle sıraya dizer: paylaşılan bağlantıda bu süre boyunca
// panelin yoklama döngüsü (link.status, system.status …), tuşlar ve fare
// donardı. Bu sınama LinkEnable sürerken başka bir çağrının yanıt aldığını
// denetler.
func TestLinkEnableDoesNotBlockOtherCalls(t *testing.T) {
	ln, err := ipc.Listen("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := ipc.NewServer(ln, log.New(io.Discard, log.LevelError, 8))
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	srv.Handle(ipc.MethodLinkEnable, func(context.Context, json.RawMessage) (any, error) {
		<-release // yavaş kurulum (fabric-api indirmesi)
		return map[string]any{"message": "tamam", "seed": "1"}, nil
	})
	srv.Handle(ipc.MethodLinkStatus, func(context.Context, json.RawMessage) (any, error) {
		return model.LinkStatus{Mode: model.LinkOff}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Serve(ctx) }()

	cl, err := Dial("tcp://" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cl.c.Close() })

	enabled := make(chan error, 1)
	go func() {
		_, _, err := cl.LinkEnable("s1", model.DifficultyNormal, 0, "")
		enabled <- err
	}()
	time.Sleep(150 * time.Millisecond) // link.enable daemon'da beklesin

	status := make(chan error, 1)
	go func() {
		_, err := cl.LinkStatus()
		status <- err
	}()
	select {
	case err := <-status:
		if err != nil {
			t.Fatalf("link.status: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("link.enable sürerken link.status yanıt vermedi: panelin bağlantısı kilitli kaldı")
	}

	free()
	select {
	case err := <-enabled:
		if err != nil {
			t.Fatalf("link.enable: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("link.enable dönmedi")
	}
}
