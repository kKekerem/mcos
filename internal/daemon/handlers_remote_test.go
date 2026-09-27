package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"mcos/internal/log"
)

// newRemoteTestDaemon, veri kökü geçici klasörde olan gerçek bir Daemon.
// Uzaktan köprü gerçek bir TCP portu açıyor; sertifika geçici klasöre yazılır.
func newRemoteTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	root := t.TempDir()
	d, err := New(filepath.Join(root, "config.json"), root, log.New(io.Discard, log.LevelError, 16))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.stopRemote)
	return d
}

func enableRemote(t *testing.T, d *Daemon, port int) (RemoteStatus, error) {
	t.Helper()
	raw := json.RawMessage(fmt.Sprintf(`{"port":%d}`, port))
	res, err := d.handleRemoteEnable(context.Background(), raw)
	if err != nil {
		return RemoteStatus{}, err
	}
	return res.(RemoteStatus), nil
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return p
}

// Port doluyken remote.enable HATA döndürmeli. Eskiden port goroutine'de
// açıldığı için "running": true ve parmak izi dönüyordu; panel QR'ı
// gösteriyor, telefon "bağlantıyı reddetti" alıyordu.
func TestRemoteEnableBusyPortReportsError(t *testing.T) {
	busy, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port

	d := newRemoteTestDaemon(t)
	st, err := enableRemote(t, d, port)
	if err == nil {
		t.Fatalf("dolu portta remote.enable başarılı döndü: %+v", st)
	}
	if !strings.Contains(err.Error(), "başka bir program") {
		t.Errorf("hata Türkçe/anlaşılır değil: %v", err)
	}
	if got := d.remoteStatus(); got.Running {
		t.Errorf("dolu portta durum hâlâ running: %+v", got)
	}
}

// Çalışırken port değişirse köprü YENİ portta dinlemeli; durum ve QR yeni
// portu söylüyor, eskiden köprü eski portta kalıyordu.
func TestRemoteEnablePortChangeRestartsBridge(t *testing.T) {
	d := newRemoteTestDaemon(t)
	p1 := freePort(t)
	st, err := enableRemote(t, d, p1)
	if err != nil || !st.Running || st.Fingerprint == "" {
		t.Fatalf("ilk açılış: %+v %v", st, err)
	}
	p2 := freePort(t)
	st, err = enableRemote(t, d, p2)
	if err != nil {
		t.Fatal(err)
	}
	d.remoteSt.mu.Lock()
	gercek := d.remoteSt.srv.Port()
	d.remoteSt.mu.Unlock()
	if st.Port != p2 || gercek != p2 {
		t.Fatalf("durum portu %d, köprünün dinlediği port %d; beklenen %d", st.Port, gercek, p2)
	}
	c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", p2))
	if err != nil {
		t.Fatalf("yeni porta bağlanılamadı: %v", err)
	}
	_ = c.Close()
}
