package fbpanel

import (
	"mcos/internal/fbui"
	"mcos/internal/ipc"
	"mcos/internal/ipcclient"
	"mcos/internal/model"
	"mcos/internal/version"
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
		Version:      version.Version,
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

	a.SetConfig(&model.Config{
		Theme: "graphite-teal",
		UI:    model.DefaultUI(),
		Cluster: model.ClusterConfig{
			Enabled: true, NodeName: "mcos-lab", Port: 27890,
		},
	})

	// ── PC eşleştirme örneği ────────────────────────────────────────────
	a.mu.Lock()
	a.clusterID = peersIdentity{
		Secret:   "9f3c1a77e2b04d58",
		NodeName: "mcos-lab",
		Port:     27890,
		Address:  "192.168.1.42:27890",
	}
	a.peers = []model.Peer{
		{ID: "mcos-oda@192.168.1.57", Name: "mcos-oda", IP: "192.168.1.57",
			Cores: 4, RAMMB: 8192, State: model.PeerAvailable, Paired: true},
		{ID: "mcos-salon@192.168.1.63", Name: "mcos-salon", IP: "192.168.1.63",
			Cores: 2, RAMMB: 4096, State: model.PeerAvailable},
	}
	a.link = model.LinkStatus{
		Mode:         model.LinkSharedWorld,
		ServerName:   "Yaratıcı",
		Difficulty:   model.DifficultyNormal,
		SlabChunks:   model.DefaultSlabChunks,
		ModInstalled: true,
		Handoffs:     12,
		Territories:  model.Territories([]string{"mcos-lab", "mcos-oda"}, 0),
	}
	a.playit = ipcclient.PlayitStatus{
		Installed: true,
		Claimed:   true,
		Running:   true,
		Address:   "mavi-kedi.craft.ply.gg:31417",
		Note:      "Tünel açık: mavi-kedi.craft.ply.gg:31417",
		Log: []string{
			"playit: tunnel established",
			"playit: mavi-kedi.craft.ply.gg:31417 -> 127.0.0.1:25565",
		},
	}
	a.pointerDevices = []string{
		"Logitech USB Optical Mouse (fare)",
		"SynPS/2 Synaptics TouchPad (touchpad)",
	}
	a.mu.Unlock()

	a.Emit(fbui.EventOK, "Survival çalışıyor")
	a.Emit(fbui.EventBusy, "Yaratıcı açılıyor — Fabric yükleniyor…")
}

// DemoView positions the panel for a screenshot.
//
// YALNIZCA --screenshot kipinde kullanilir: hangi bolumun ve hangi odagin
// cizilecegini secer. Boylece arayuzun her ekrani donanim olmadan gozden
// gecirilebilir - bir ekranin bos halde iyi, dolu halde kotu gorunmesi cok
// yaygin bir hatadir.
// DemoSetup positions the wizard on a given page for screenshots.
//
// Sihirbazın her sayfası donanım olmadan gözden geçirilebilmeli: bir
// kurulum akışının kötü görünen tek bir sayfası, tüm ürünün ilk izlenimini
// bozar.
func DemoSetup(a *App, page int) {
	FillDemo(a)
	a.StartSetup()
	s := a.setupState()
	if s == nil {
		return
	}
	if page < 0 {
		page = 0
	}
	if page >= int(setupStepCount) {
		page = int(setupStepCount) - 1
	}
	s.step = setupStep(page)
	s.javaOK = true
	s.javaNote = "kurulu: 17, 21"
	s.playitOK = true
	s.playitNote = "playit ajanı hazır."
	s.ssid = "MCOS-Lab"
	s.password = "gizli"
	s.disks = []ipc.DiskTarget{
		{Device: "/dev/sda", Model: "Samsung SSD 860 EVO",
			SizeBytes: 500 << 30, Removable: false},
		{Device: "/dev/sdb", Model: "SanDisk Ultra",
			SizeBytes: 32 << 30, Removable: true},
	}
	a.Invalidate()
}

// DemoWizard positions the create-server wizard for screenshots.
func DemoWizard(a *App, page int) {
	FillDemo(a)
	a.StartWizard()
	w := a.wizardState()
	if w == nil {
		return
	}
	if page < 0 {
		page = 0
	}
	if page >= int(wizStepCount) {
		page = int(wizStepCount) - 1
	}
	w.step = wizStep(page)
	// Gerçek veriyle dolu bir sayfa, boş bir sayfadan çok daha yararlı
	// gözden geçirilir.
	w.versions = []string{"1.21.11", "1.21.8", "1.21.4", "1.21.1", "1.20.6"}
	w.verLoaded = true
	w.name = "Survival"
	w.desc = "Arkadaş sunucusu"
	w.motd = "MCOS üzerinde çalışıyor"
	w.eula = true
	w.autostart = true
	w.gpuInfo = "Intel UHD Graphics 630"
	a.Invalidate()
}

func DemoView(a *App, section int, contentFocus bool) {
	if section < 0 || Section(section) >= secCount {
		return
	}
	a.gotoSection(Section(section))
	if contentFocus {
		a.setFocus(FocusContent)
		a.loadSection()
	}
}

// DemoModal opens a sample picker for screenshots.
//
// Amac, bulanik arka planin GERCEKTEN calistigini gozle dogrulamak.
func DemoModal(a *App, kind string) {
	switch kind {
	case "list":
		a.OpenModal(NewListModal("Kablosuz Ag Sec", "3 ag bulundu.", []ListItem{
			{Label: "MCOS-Lab", Detail: "%92", Badge: "korumali", BadgeKind: fbui.EventWarn, Current: true},
			{Label: "Ofis-5G", Detail: "%74", Badge: "korumali", BadgeKind: fbui.EventWarn},
			{Label: "Misafir", Detail: "%61", Badge: "acik", BadgeKind: fbui.EventInfo},
		}, nil))
	case "password":
		m := NewPasswordModal("MCOS-Lab", nil)
		for _, r := range "parola123" {
			m.Key(a, string(r))
		}
		a.OpenModal(m)
	case "pointer":
		a.OpenModal(newPointerModal(a))
	case "text":
		a.OpenModal(NewTextModal("Elle eşleştir",
			"Öbür MCOS cihazının IP adresini girin.", nil).
			WithValue("192.168.1.57").
			WithOK("Eşleştir").
			WithHint("Adresi öbür cihazın MCOS Paylaşım ekranında görebilirsiniz."))
	case "confirm":
		a.OpenModal(NewConfirmModal("Kapat?",
			[]string{"Calisan sunucular duzgunce durdurulacak.",
				"Bagli oyuncular baglantiyi kaybedecek."},
			"Kapat", true, nil))
	case "lock":
		// Kilit ekranı bir PENCERE değil, tam ekrandır — ama ekran görüntüsü
		// üretiminde aynı anahtardan sürülmesi pratik: yoksa gözden geçirme
		// setinde HİÇ görünmezdi ve parola alanının hareketi (yazma, silme,
		// sarsılma) kimsenin bakmadığı tek yerde kalırdı.
		DemoLock(a, 9)
	}
}

// DemoLock arms the lock screen with n typed characters, for screenshots.
func DemoLock(a *App, n int) {
	_, _, cfg := a.Snapshot()
	if cfg == nil {
		cfg = model.DefaultConfig()
	}
	// Kilit ekranı parola KURULU değilken hiç açılmaz; örnek bir parola kur.
	_ = cfg.Security.SetPassword("ornek-parola")
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()

	a.Lock()
	for i := 0; i < n; i++ {
		a.lockKey("x")
	}
	// Animasyon damgalarını temizle: ekran görüntüsü DURAĞAN hâli göstermeli,
	// yoksa son işaret yarı saydam yakalanır ve "eksik çizilmiş" görünür.
	a.mu.Lock()
	a.lockAn.reset()
	a.mu.Unlock()
}
