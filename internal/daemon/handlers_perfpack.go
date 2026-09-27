package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/model"
	"mcos/internal/perfpack"
	"mcos/internal/store"
)

// ════════════════════════════════════════════════════════════════════════════
// PERFORMANS PAKETİ (server.perfPack ve kurulum sonrası otomatik kurulum)
// ════════════════════════════════════════════════════════════════════════════
//
// Neyin kurulduğunu internal/perfpack seçer; burası yalnızca NE ZAMAN
// kurulacağını ve sonucun sunucu kaydına yazılmasını yönetir. Ayrı dosyada:
// handlers.go ve daemon.go başka özelliklerle aynı anda düzenleniyor.

// perfPackBusy holds the ids of servers whose pack is being applied.
//
// Aynı sunucuya iki paket işi aynı anda koşarsa (sihirbazın otomatik
// kurulumu sürerken kullanıcı panelden "kur/güncelle"ye basarsa) ikisi aynı
// mods/ klasörüne aynı dosyayı indirir ve kayıtlardan biri ötekini ezer.
// Daemon yapısına alan eklemek yerine paket düzeyinde: daemon.go ortak dosya.
var perfPackBusy sync.Map

// errPerfPackBusy: bu sunucuda bir paket işi zaten sürüyor.
var errPerfPackBusy = errors.New("performans paketi zaten kuruluyor")

// perfPackTimeout, bir paket işinin üst sınırıdır: yavaş bağlantıda altı
// modun indirilmesi dakikalar sürebilir ama asılı kalan bir istek kilidi
// (perfPackBusy) sonsuza dek tutmamalı.
const perfPackTimeout = 15 * time.Minute

func (d *Daemon) perfPackInstaller() *perfpack.Installer {
	return &perfpack.Installer{
		Catalog: d.catalog,
		Logf:    func(format string, args ...any) { d.log.Infof(format, args...) },
	}
}

// runPerfPack applies the performance pack to a server and records the result.
//
// Engelleyici: çağıran bir goroutine'de çalıştırır.
func (d *Daemon) runPerfPack(id string) error {
	if _, busy := perfPackBusy.LoadOrStore(id, struct{}{}); busy {
		return errPerfPackBusy
	}
	defer perfPackBusy.Delete(id)

	srv, err := d.store.GetServer(id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), perfPackTimeout)
	defer cancel()
	res, applyErr := d.perfPackInstaller().Apply(ctx, perfpack.Request{
		Software: srv.Software,
		MC:       srv.MCVersion,
		DataDir:  d.serverDataDir(srv),
		Prev:     srv.PerfPack,
	})
	st := res.State
	if applyErr != nil {
		// Hata da kayda yazılır: panel işin bittiğini PerfPack.At'in
		// değişmesinden anlıyor; yazılmazsa zaman aşımına kadar beklerdi.
		// Önceki mod kayıtları korunur, yoksa sonraki çalışma kendi
		// koyduğu dosyaları tanımaz.
		st = model.PerfPackState{MC: srv.MCVersion, At: time.Now(),
			Note: "performans paketi kurulamadı: " + applyErr.Error()}
		if srv.PerfPack != nil {
			st.Items, st.Settings, st.Pending = srv.PerfPack.Items, srv.PerfPack.Settings, srv.PerfPack.Pending
		}
	}
	if err := d.savePerfPackState(id, &st); err != nil {
		return err
	}
	d.log.Infof("perfpack: %s (%s %s): %s", srv.Name, srv.Software, srv.MCVersion, st.Note)
	return applyErr
}

// savePerfPackState writes st into the server record.
//
// Kayıt YENİDEN okunur: paket dakikalar sürebilir, bu arada kullanıcı başka
// bir ayarı değiştirmiş olabilir; baştaki kopyayı yazmak onu ezerdi.
func (d *Daemon) savePerfPackState(id string, st *model.PerfPackState) error {
	cur, err := d.store.GetServer(id)
	if err != nil {
		return err
	}
	cur.PerfPack = st
	return d.store.SaveServer(cur)
}

// perfPackBeforeStart writes the pack settings that were waiting for the
// server's first start to create their files.
//
// Hızlıdır (yalnızca yerel YAML), ağ yok; başlatma RPC'sini bekletmez. İlk
// açılışta dosyalar henüz yoktur, ayar bekler; sonraki başlatmada yazılır ve
// o açılıştan itibaren geçerli olur.
func (d *Daemon) perfPackBeforeStart(srv *model.Server) {
	if srv == nil || srv.PerfPack == nil || len(srv.PerfPack.Pending) == 0 {
		return
	}
	if _, busy := perfPackBusy.LoadOrStore(srv.ID, struct{}{}); busy {
		return // bir paket işi zaten sürüyor; o bitince kaydı kendisi yazar
	}
	defer perfPackBusy.Delete(srv.ID)
	res := d.perfPackInstaller().ApplySettings(perfpack.Request{
		Software: srv.Software,
		MC:       srv.MCVersion,
		DataDir:  d.serverDataDir(srv),
		Prev:     srv.PerfPack,
	})
	st := res.State
	if len(res.Changed) == 0 && srv.PerfPack.Note != "" && len(st.Pending) == len(srv.PerfPack.Pending) {
		st.Note = srv.PerfPack.Note // hiçbir şey değişmedi; mod özetini silme
	}
	if err := d.savePerfPackState(srv.ID, &st); err != nil {
		d.log.Warnf("perfpack: %s kaydı yazılamadı: %v", srv.Name, err)
		return
	}
	srv.PerfPack = &st
}

// handleServerPerfPack starts the pack job in the background and returns.
func (d *Daemon) handleServerPerfPack(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.ServerPerfPackParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "id is required"}
	}
	srv, err := d.store.GetServer(p.ID)
	if err == store.ErrNotFound {
		return nil, &ipc.Error{Code: ipc.CodeNotFound, Message: "server not found"}
	} else if err != nil {
		return nil, err
	}
	pack := perfpack.For(srv.Software, srv.MCVersion)
	if pack.Empty() {
		msg := pack.Note
		if msg == "" {
			msg = "bu sunucu için performans paketi yok"
		}
		// Kayıtta eski paket dosyası varsa (yazılım değiştirilmiş) yine de
		// çalıştırılır: süpürme onları kaldırır.
		if srv.PerfPack == nil || len(srv.PerfPack.Items) == 0 {
			return ipc.ServerPerfPackResult{Started: false, Message: msg}, nil
		}
	}
	if _, busy := perfPackBusy.Load(p.ID); busy {
		return ipc.ServerPerfPackResult{Started: false, Message: errPerfPackBusy.Error()}, nil
	}
	go func(id string) {
		if err := d.runPerfPack(id); err != nil && !errors.Is(err, errPerfPackBusy) {
			d.log.Errorf("perfpack: %s: %v", id, err)
		}
	}(srv.ID)
	return ipc.ServerPerfPackResult{Started: true, Items: pack.Names()}, nil
}
