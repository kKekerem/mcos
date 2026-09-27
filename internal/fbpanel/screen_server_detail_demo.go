package fbpanel

import (
	"time"

	"mcos/internal/ipc"
	"mcos/internal/model"
)

// DemoServerDetail opens the server detail on a tab with sample data.
//
// YALNIZCA ekran görüntüsü ve testler içindir (FillDemo gibi). Her sekme
// DOLU haliyle gözden geçirilebilmeli: boş bir sekmenin iyi, dolu bir
// sekmenin taşan göründüğü çok sık olur.
func DemoServerDetail(a *App, tab int) *ServerDetail {
	FillDemo(a)
	// FillDemo'daki Survival'a canlı bir sunucunun çalışma zamanı alanlarını
	// ekle: Genel sekmesi "0 / 20 oyuncu, —" ile gözden geçirilemez.
	_, servers, _ := a.Snapshot()
	list := make([]*model.Server, 0, len(servers))
	for _, s := range servers {
		cp := s.Clone()
		if cp.ID == "a" {
			cp.Players, cp.MaxPlayers = 3, 20
			cp.UptimeSec = 5*3600 + 17*60
			cp.PID = 4812
			cp.JavaMajor = 21
			cp.ViewDistance, cp.SimDistance = 12, 8
			cp.MOTD = "MCOS üzerinde çalışıyor"
			cp.Gamemode, cp.Difficulty = "survival", "normal"
			cp.OnlineMode, cp.PVP, cp.Autostart = true, true, true
			cp.LevelSeed = "-4172144997902289642"
			cp.JVMFlags = "aikar"
			cp.Backup = model.BackupPolicy{Auto: true, Schedule: "6h", Keep: 5}
			cp.LastLog = "[12:41:07 INFO]: Steve joined the game"
		}
		list = append(list, cp)
	}
	a.SetServers(list)

	a.gotoSection(SecServers)
	a.setFocus(FocusContent)
	a.SetCursor(0)
	a.openServerDetail()
	d := a.detailState()
	if d == nil {
		return nil
	}
	if tab < 0 || tab >= int(dtCount) {
		tab = 0
	}
	d.tab = detailTab(tab)

	d.mu.Lock()
	d.console = []string{
		"[12:30:01 INFO]: Starting minecraft server version 1.21.1",
		"[12:30:01 INFO]: Loading properties",
		"[12:30:02 INFO]: This server is running Paper version 1.21.1-130",
		"[12:30:04 WARN]: Using offline mode is not recommended",
		"[12:30:09 INFO]: Preparing level \"world\"",
		"[12:30:14 INFO]: [WorldEdit] Enabling WorldEdit v7.3.9",
		"[12:30:15 INFO]: Done (13.482s)! For help, type \"help\"",
		"[12:41:07 INFO]: Steve joined the game",
		"[12:41:30 INFO]: Alex joined the game",
		"[12:44:02 ERROR]: Could not pass event PlayerMoveEvent to BrokenPlugin v0.1",
		"[12:45:11 INFO]: <Steve> merhaba 👋 herkese",
	}
	d.players = ipc.PlayersListResult{Online: 3, Max: 20, Players: []model.PlayerInfo{
		{Name: "Steve"}, {Name: "Alex"}, {Name: "Kerem_07"},
	}}
	d.installed = []model.FileEntry{
		{Name: "EssentialsX-2.20.1.jar", Size: 1_214_000},
		{Name: "LuckPerms-Bukkit-5.4.141.jar", Size: 2_050_000},
		{Name: "worldedit-bukkit-7.3.9.jar", Size: 6_900_000},
		{Name: "OldPlugin-1.0.jar.disabled", Size: 48_000},
	}
	d.files = []model.FileEntry{
		{Name: "logs", Path: "logs", IsDir: true},
		{Name: "plugins", Path: "plugins", IsDir: true},
		{Name: "world", Path: "world", IsDir: true},
		{Name: "world_nether", Path: "world_nether", IsDir: true},
		{Name: "banned-players.json", Size: 2},
		{Name: "eula.txt", Size: 158},
		{Name: "ops.json", Size: 112},
		{Name: "paper-1.21.1-130.jar", Size: 49_800_000},
		{Name: "server.properties", Size: 1_402},
		{Name: "whitelist.json", Size: 2},
	}
	d.worlds = []model.World{
		{Name: "world", SizeBytes: 412 << 20},
		{Name: "world_nether", SizeBytes: 38 << 20},
		{Name: "world_the_end", SizeBytes: 9 << 20},
	}
	now := time.Date(2026, 9, 24, 14, 5, 0, 0, time.Local)
	d.backups = []model.Backup{
		{ID: "b3", Name: "gunluk-2026-09-24", CreatedAt: now, SizeBytes: 461 << 20, Type: "auto"},
		{ID: "b2", Name: "pre-version-change", CreatedAt: now.Add(-26 * time.Hour), SizeBytes: 455 << 20, Type: "restore-point"},
		{ID: "b1", Name: "elle-alinan", CreatedAt: now.Add(-72 * time.Hour), SizeBytes: 402 << 20, Type: "manual"},
	}
	// Otomatik yedek planı: "Sonraki" satırı gerçek saate göre (bugün/yarın)
	// yazıldığı için demo da gerçek saatten iki saat sonrasını verir.
	next := nowFunc().Add(2 * time.Hour).Truncate(time.Hour)
	d.backupPol = detailBackupPolicy{ok: true, res: ipc.BackupPolicyResult{ServerID: "a",
		Policy: model.BackupPolicy{Auto: true, Schedule: "6h", Keep: 5}, Next: &next}}
	d.access = detailAccess{
		ops:       []string{"Kerem_07"},
		whitelist: []string{"Alex", "Steve"},
		banned:    []string{"griefer42"},
	}
	d.mu.Unlock()

	// Yazılım sekmesi arama SONUÇLARIYLA gösterilir: arayüzün en yoğun hali
	// ve ASCII dışı başlıkların sığdırılması orada görülür.
	if d.tab == dtSoftware {
		d.swView = swResults
		d.mu.Lock()
		d.search = detailSearch{query: "worldedit", done: true, res: ipc.CatalogSearchResult{
			Total: 21, Loaders: []string{"paper", "spigot", "bukkit"}, GameVersion: "1.21.1", ProjectType: "plugin",
			Items: []ipc.CatalogItem{
				{Slug: "worldedit", Title: "WorldEdit", Downloads: 10_767_750, Type: "mod", Loader: "paper"},
				{Slug: "fastasyncworldedit", Title: "FastAsyncWorldEdit", Downloads: 2_140_000, Type: "plugin", Loader: "paper"},
				{Slug: "worldedit-selection-viewer", Title: "WorldEdit Selection Viewer", Downloads: 41_200, Type: "plugin", Loader: "paper"},
				{Slug: "we-cui", Title: "WorldEditCUI 世界編集 🚀 çok uzun bir başlık ki satıra sığmasın diye", Downloads: 980, Type: "plugin", Loader: "bukkit"},
				{Slug: "worldguard", Title: "WorldGuard", Downloads: 6_400_000, Type: "plugin", Loader: "paper"},
			},
		}}
		d.mu.Unlock()
	}

	// Ekran görüntüsü DURAĞAN hali göstermeli: açılış geçişi yarıda
	// yakalanırsa her şey yarı saydam çıkar.
	a.mu.Lock()
	a.trans = nil
	a.dirty = true
	a.mu.Unlock()
	return d
}
