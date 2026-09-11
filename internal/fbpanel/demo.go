package fbpanel

import (
	"mcos/internal/fbui"
	"mcos/internal/model"
)

// FillDemo populates the panel with representative data.
//
// YALNIZCA ekran görüntüsü kipinde kullanılır (mcos-panel-fb --screenshot).
// Çalışan sistemde her zaman daemon'dan gelen GERÇEK veri gösterilir; bu
// fonksiyon oraya hiç ulaşmaz.
//
// Amacı, arayüzü donanım olmadan gözden geçirebilmek: bir ekran tasarımının
// boş halde iyi, dolu halde kötü görünmesi çok yaygındır.
func FillDemo(a *App) {
	a.SetStatus(&model.SystemStatus{
		SystemName:   "mcos-lab",
		Version:      "0.1.0",
		Uptime:       3*24*3600 + 7*3600 + 12*60,
		ServersTotal: 3,
		ServersUp:    2,
		CPU: model.CPUInfo{
			Model: "Intel Core i5-9400F", Cores: 6, Threads: 6,
			UsagePct: 37.4, MHz: 2900, TempC: 52,
		},
		Memory: model.MemInfo{
			TotalBytes: 16 << 30, UsedBytes: 9<<30 + 512<<20,
			AvailableBytes: 6 << 30, UsagePct: 59.4,
		},
		Disks: []model.DiskInfo{
			{Mount: "/data", Filesystem: "ext4", TotalBytes: 460 << 30,
				UsedBytes: 118 << 30, FreeBytes: 342 << 30, UsagePct: 25.6},
		},
		Net: model.NetStatus{
			LocalIP: "192.168.1.42", Internet: true, Hostname: "mcos-lab",
			NICs: []model.NICInfo{
				{Name: "enp3s0", Kind: "wired", IPv4: "192.168.1.42", Up: true, Link: true},
				{Name: "wlan0", Kind: "wireless", Up: true, Link: false},
			},
		},
		JavaVersions: []int{17, 21},
	})

	a.SetServers([]*model.Server{
		{ID: "a", Name: "Survival", Software: model.Software("paper"),
			MCVersion: "1.21.1", State: model.StateRunning, Port: 25565, RAMMB: 4096},
		{ID: "b", Name: "Yaratıcı", Software: model.Software("fabric"),
			MCVersion: "1.21.11", State: model.StateStarting, Port: 25566, RAMMB: 2048},
		{ID: "c", Name: "Test", Software: model.Software("vanilla"),
			MCVersion: "1.21.1", State: model.StateStopped, Port: 25567, RAMMB: 2048},
	})

	a.SetConfig(&model.Config{Theme: "graphite-teal"})

	a.Emit(fbui.EventOK, "Survival çalışıyor")
	a.Emit(fbui.EventBusy, "Yaratıcı açılıyor — Fabric yükleniyor…")
}
