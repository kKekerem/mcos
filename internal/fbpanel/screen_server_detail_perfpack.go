package fbpanel

import (
	"strings"
	"time"

	"mcos/internal/fbui"
	"mcos/internal/model"
)

// perfPackPoll / perfPackWait: panel daemon'daki paket işinin bitişini
// sunucu kaydından izler. server.perfPack HEMEN döner (panelin RPC zaman
// aşımı 10 sn, mod indirmek dakikalar sürebilir); bitiş, kayıttaki
// PerfPack.At'in ilerlemesinden anlaşılır. Daemon hata durumunu da kayda
// yazar, yani bekleme yalnızca daemon çökerse üst sınıra dayanır.
var (
	perfPackPoll = 2 * time.Second
	perfPackWait = 16 * time.Minute
)

// detailPerfPack installs/updates the performance pack as a background job.
func (a *App) detailPerfPack(d *ServerDetail, s *model.Server) {
	if a.offline() {
		a.Emit(fbui.EventWarn, "mcosd bağlantısı yok")
		return
	}
	id, name := s.ID, s.Name
	var since time.Time
	if s.PerfPack != nil {
		since = s.PerfPack.At
	}
	a.runJob("perfpack:"+id, name+": performans paketi kuruluyor", func() (string, error) {
		res, err := a.cl.ServerPerfPack(id)
		if err != nil {
			return "performans paketi başlatılamadı", err
		}
		if !res.Started {
			return "Performans paketi: " + res.Message, nil
		}
		if len(res.Items) > 0 {
			a.Emit(fbui.EventBusy, name+": "+strings.Join(res.Items, ", ")+" kuruluyor…")
		}
		deadline := time.Now().Add(perfPackWait)
		for time.Now().Before(deadline) {
			time.Sleep(perfPackPoll)
			srv, err := a.cl.Server(id)
			if err != nil {
				continue // geçici bağlantı kopması işi bitmiş saymaz
			}
			if srv.PerfPack != nil && srv.PerfPack.At.After(since) {
				return "Performans paketi: " + srv.PerfPack.Note, nil
			}
		}
		return "Performans paketi hâlâ sürüyor; sonucu sunucu günlüğünde görün", nil
	}, func() { a.detailLoadInstalled(d) })
}
