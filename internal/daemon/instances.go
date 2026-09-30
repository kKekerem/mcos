package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"mcos/internal/cluster"
	"mcos/internal/model"
	"mcos/internal/portmgr"
	"mcos/internal/server"
	"mcos/internal/sysmon"
)

// ════════════════════════════════════════════════════════════════════════════
// Aynı makinede bölünmüş dünya: kardeş kopyaların yaşam döngüsü
// ════════════════════════════════════════════════════════════════════════════
//
// Ana sunucu Instances > 1 ile açılırken MCOS gizli kardeş kayıtları
// ("<ad> #2" …, ParentID = ana) kurar, onları ana sunucuyla eşitler ve
// birlikte açar/kapatır. Dünyanın bölünmesi PC eşleştirmesindeki koordinatörle
// aynı yoldan yürür: kardeşler topolojiye "<makine>-2" … düğümleri olarak
// girer (bkz. LinkSpec, cluster.withInstances).

// firstSiblingLinkPort, kardeşlerin mod portlarının başladığı yerdir.
//
// 27894: ana sunucu varsayılan 27893'ü (model.DefaultLinkPort) kullanır;
// kardeşler hemen üstünden devam eder, koordinatörün 27892'siyle çakışmaz.
const firstSiblingLinkPort = model.DefaultLinkPort + 1

// hostCapacity returns logical cores and total RAM (MB) for the auto count.
func hostCapacity() (int, int) {
	return runtime.NumCPU(), int(sysmon.Memory().TotalBytes >> 20)
}

// instanceCount is how many copies srv runs on this machine (≥ 1).
func (d *Daemon) instanceCount(srv *model.Server) int {
	cores, ram := hostCapacity()
	return srv.InstanceCount(cores, ram)
}

// siblingsOf returns main's copies from list, ordered by index.
func siblingsOf(mainID string, list []*model.Server) []*model.Server {
	var out []*model.Server
	for _, s := range list {
		if s.ParentID == mainID {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].InstanceIndex < out[j].InstanceIndex })
	return out
}

// activeSiblings keeps the copies that belong to the current count.
//
// Sayı düşünce fazla kardeşler SİLİNMEZ, yalnızca açılmaz: o dilimde
// kurulmuş yapılar kardeşin dünya klasöründedir ve sayı yeniden
// artırıldığında geri gelmelidir. Ana sunucu silinince onlar da gider.
func activeSiblings(sibs []*model.Server, count int) []*model.Server {
	var out []*model.Server
	for _, s := range sibs {
		if s.InstanceIndex >= 2 && s.InstanceIndex <= count {
			out = append(out, s)
		}
	}
	return out
}

// planSiblings builds the records still missing for copies 2..count.
//
// Port: ana portun +1'inden başlayarak boş olan; mod portu 27894'ten başlayarak
// boş olan. "Boş" = başka bir MCOS sunucusunun kaydında yok VE işletim
// sisteminde bağlanabilir (bindable; testte sahte). Saf fonksiyon: kayıt
// yazmaz, yalnızca ne yazılacağını söyler.
func planSiblings(main *model.Server, list []*model.Server, count int,
	bindable func(int) bool, newID func() string) []*model.Server {
	have := map[int]bool{}
	for _, s := range siblingsOf(main.ID, list) {
		have[s.InstanceIndex] = true
	}
	used := portmgr.UsedPorts(list, "")
	usedLink := map[int]bool{model.CoordinatorPort: true}
	for _, s := range list {
		p := s.Link.LinkPort
		if p <= 0 {
			p = model.DefaultLinkPort
		}
		usedLink[p] = true
	}
	next := func(from int, taken map[int]bool) int {
		for p := from; p < 65535; p++ {
			if !taken[p] && bindable(p) {
				taken[p] = true
				return p
			}
		}
		return 0
	}
	mcFrom, linkFrom := main.Port+1, firstSiblingLinkPort
	var out []*model.Server
	for idx := 2; idx <= count; idx++ {
		if have[idx] {
			continue
		}
		port := next(mcFrom, used)
		linkPort := next(linkFrom, usedLink)
		if port == 0 || linkPort == 0 {
			break // boş port kalmadı: eksik kopyayla açmak, hiç açmamaktan iyi
		}
		mcFrom, linkFrom = port+1, linkPort+1
		out = append(out, &model.Server{
			ID:            newID(),
			Name:          model.SiblingName(main.Name, idx),
			ParentID:      main.ID,
			InstanceIndex: idx,
			Port:          port,
			Link:          model.LinkConfig{LinkPort: linkPort},
			CreatedAt:     time.Now(),
		})
	}
	return out
}

// syncSibling copies everything that must match the main into a copy.
//
// Her açılışta çalışır: kullanıcı ana sunucuda sürümü, tohumu ya da kuralları
// değiştirdiyse kardeş eski ayarla açılsaydı sınırı geçen oyuncu başka bir
// oyuna düşerdi. Port, mod portu ve playit tünel kimliği kardeşin KENDİSİNİNDİR.
func syncSibling(main, sib *model.Server, rules *model.LinkRules) {
	sib.Name = model.SiblingName(main.Name, sib.InstanceIndex)
	sib.Software, sib.MCVersion = main.Software, main.MCVersion
	sib.JavaMajor, sib.JavaPath = main.JavaMajor, main.JavaPath
	sib.AllowOldVersions = main.AllowOldVersions
	// RAM: ana sunucununki; bütçe kısıtı açılışta ayrıca uygulanır.
	sib.RAMMB, sib.CPUQuota, sib.Priority = main.RAMMB, main.CPUQuota, main.Priority
	sib.FullPerf, sib.JVMFlags = main.FullPerf, main.JVMFlags
	sib.ViewDistance, sib.SimDistance = main.ViewDistance, main.SimDistance
	sib.MaxPlayers, sib.MOTD = main.MaxPlayers, main.MOTD
	sib.Gamemode, sib.Difficulty = main.Gamemode, main.Difficulty
	sib.OnlineMode, sib.PVP, sib.Hardcore = main.OnlineMode, main.PVP, main.Hardcore
	sib.Whitelist, sib.LevelSeed = main.Whitelist, main.LevelSeed
	sib.RestartOnCrash = main.RestartOnCrash
	sib.SupportsPlugins, sib.SupportsMods = main.SupportsPlugins, main.SupportsMods
	// Kardeş kendi başına açılmaz: ana sunucu açılırken onu açar. Kendi
	// başına açılsaydı ana kapalıyken dünyanın yalnızca bir dilimi çalışırdı.
	sib.Autostart = false
	// Aynı çekirdeklere sabitlenmesin: bölmenin amacı başka çekirdeği
	// kullanmaktır.
	sib.CPUAffinity = nil
	// WAN ana sunucudan gelir: playit işçisi WAN açık her sunucuya tünel
	// açtığı için kardeş de kendi tünelini alır.
	sib.WAN.Enabled = main.WAN.Enabled
	linkPort := sib.Link.LinkPort
	// Ana sunucu Velocity arkasındaysa kardeş de arka uçtur (aynı anahtar);
	// genel port YALNIZCA anadadır (proxy'yi o çalıştırır).
	var px *model.LinkProxy
	if main.Link.BehindProxy() {
		px = &model.LinkProxy{Secret: main.Link.Proxy.Secret, OnlineMode: main.Link.Proxy.OnlineMode}
	}
	sib.Link = model.LinkConfig{
		Mode:       model.LinkSharedWorld,
		Difficulty: main.Link.Difficulty,
		SlabChunks: main.Link.SlabChunks,
		Seed:       main.LevelSeed,
		LinkPort:   linkPort,
		// Kurallar ana sunucunun server.properties'inden: kardeşin
		// server.properties'ine her açılışta yazılır (WriteLinkProperties).
		Rules: rules,
		Proxy: px,
	}
	sib.UpdatedAt = time.Now()
}

// ensureSplitLink turns the main into a shared world when it is split.
//
// PC eşleştirmesinin bütün düzeneği (topoloji, mod, server.properties)
// "ortak dünya" kipine bakıyor; bölünmüş dünya da bir ortak dünyadır. Kip
// kendiliğinden açıldıysa (Auto) sayı 1'e inince yine kendiliğinden kapanır.
func (d *Daemon) ensureSplitLink(main *model.Server, count int) error {
	if count <= 1 {
		if main.Link.Auto && main.Link.Mode == model.LinkSharedWorld {
			main.Link.Mode, main.Link.Auto = model.LinkOff, false
			// Bölme bitti: proxy de kalkar, sunucu genel portuna döner.
			restoreFromProxy(main)
			return d.store.SaveServer(main)
		}
		return nil
	}
	if main.Link.Mode == model.LinkSharedWorld {
		return nil
	}
	if _, _, ok := linkArtifact(main.Software); !ok {
		return fmt.Errorf("dünyayı bölmek yalnızca Fabric ve Paper/Purpur sunucularında çalışır")
	}
	if err := d.installLinkMod(main); err != nil {
		return err
	}
	// Tohum ŞART: her kopya kendi dilimini üretir; tohum farklıysa sınırda
	// arazi kopar (bkz. handleLinkEnable'daki aynı gerekçe).
	seed := strings.TrimSpace(main.LevelSeed)
	if have, err := d.liveWorldSeed(main, d.serverDataDir(main)); err == nil {
		seed = have
	} else if !errors.Is(err, cluster.ErrNoWorld) {
		return fmt.Errorf("dünyanın tohumu okunamadı: %w", err)
	}
	if seed == "" {
		seed = strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	diff := model.LinkDifficulty(strings.TrimSpace(main.Difficulty))
	if diff == "" {
		diff = model.DifficultyNormal
	}
	main.Link = model.LinkConfig{
		Mode:       model.LinkSharedWorld,
		Difficulty: diff,
		Seed:       seed,
		LinkPort:   model.DefaultLinkPort,
		Auto:       true,
	}
	main.LevelSeed = seed
	main.UpdatedAt = time.Now()
	return d.store.SaveServer(main)
}

// ensureInstances prepares main's copies and returns the ones to start.
//
// Ana sunucu AÇILMADAN önce çağrılır: ortak dünya kipi ve tohum ana
// sunucunun server.properties'ine açılışta yazılır (Manager.Start). Sayı 1
// ise hiçbir şey yapmaz ve nil döner.
func (d *Daemon) ensureInstances(main *model.Server) ([]*model.Server, error) {
	if main.IsSibling() {
		return nil, nil
	}
	count := d.instanceCount(main)
	if err := d.ensureSplitLink(main, count); err != nil {
		return nil, err
	}
	// Tek adres: ortak dünyanın kurucusu Velocity arkasına alınır (bölünmüş
	// dünyada da, PC'lerle paylaşılanda da). AÇILMADAN önce: port taşıması
	// ancak açılışta etkili olur. Başarısızsa eski transfer yolu çalışır.
	// Süre sınırı: internet yoksa ya da yavaşsa sunucunun açılışı Velocity
	// indirmesini dakikalarca beklemesin.
	pctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	_, perr := d.ensureOriginProxy(pctx, main)
	cancel()
	if err := perr; err != nil {
		d.log.Warnf("proxy: %s tek adrese alınamadı, eski aktarım kullanılıyor: %v", main.Name, err)
		d.setProxyProblem("tek adres (proxy) açılamadı: " + err.Error())
	}
	if count <= 1 {
		return nil, nil
	}
	if !d.cluster.Running() {
		d.log.Warnf("instances: %s bölünecek ama eşleştirme hizmeti kapalı; "+
			"koordinatör çalışmadan kopyalar dünyayı paylaşamaz (Ayarlar → Eşleştirme)", main.Name)
	}
	list, err := d.store.ListServers()
	if err != nil {
		return nil, err
	}
	mainDir := d.serverDataDir(main)
	for _, sib := range planSiblings(main, list, count, portmgr.IsBindable, generateID) {
		if err := d.store.SaveServer(sib); err != nil {
			return nil, err
		}
		// Var olan dünya kardeşe BİR KEZ kopyalanır: kopyalanmasaydı
		// kardeşin dilimindeki yapılar yeni üretilmiş arazinin altında
		// kaybolurdu. Sonraki açılışlarda her kopya kendi dilimini saklar.
		if err := copyWorlds(mainDir, d.serverDataDir(sib)); err != nil {
			d.log.Warnf("instances: %s dünyası kopyalanamadı: %v", sib.Name, err)
		}
		d.log.Infof("instances: %s oluşturuldu (port %d, mod portu %d)",
			sib.Name, sib.Port, sib.Link.LinkPort)
		list = append(list, sib)
	}
	rules := server.ReadLinkRules(mainDir, main)
	active := activeSiblings(siblingsOf(main.ID, list), count)
	for _, sib := range active {
		syncSibling(main, sib, rules)
		if err := d.store.SaveServer(sib); err != nil {
			return nil, err
		}
		if err := mirrorAddons(mainDir, d.serverDataDir(sib)); err != nil {
			d.log.Warnf("instances: %s modları eşitlenemedi: %v", sib.Name, err)
		}
	}
	return active, nil
}

// startSiblings launches the copies (arka planda çağrılır).
//
// Kurulum (sunucu jar'ı indirme) ilk açılışta uzun sürebilir; ana sunucunun
// RPC yanıtını bekletmemek için çağıran bunu gorutinde çalıştırır.
func (d *Daemon) startSiblings(ctx context.Context, sibs []*model.Server) {
	for _, sib := range sibs {
		if isLive(d.servers.State(sib.ID)) {
			continue
		}
		if err := d.launchServer(ctx, sib.Clone()); err != nil {
			d.log.Errorf("instances: %s başlatılamadı: %v", sib.Name, err)
		}
	}
}

// startWithInstances prepares the copies, then starts the main and them.
func (d *Daemon) startWithInstances(ctx context.Context, main *model.Server) error {
	if isLive(d.servers.State(main.ID)) {
		// Zaten açık: kopyaları yeniden hazırlamanın anlamı yok (tohum
		// okumak için çalışan sunucuya kaydettirme komutu giderdi).
		return d.launchServer(ctx, main)
	}
	sibs, err := d.ensureInstances(main)
	if err != nil {
		// Bölme kurulamadı: sunucu yine TEK kopya olarak açılır. Hiç
		// açılmaması kullanıcı için daha kötüdür; neden konsolda görünür.
		d.log.Warnf("instances: %s bölünemedi, tek sunucu açılıyor: %v", main.Name, err)
		d.servers.Note(main.ID, "[MCOS] Dünya bölünemedi, tek sunucu açılıyor: "+err.Error())
	}
	if err := d.launchServer(ctx, main); err != nil {
		return err
	}
	if len(sibs) > 0 {
		go d.startSiblings(context.Background(), sibs)
	}
	return nil
}

// hasInstances reports whether main is split now or still has live copies
// (sayı düşürülmüşken çalışan fazla kopya da yeniden başlatmada kapanmalı).
func (d *Daemon) hasInstances(main *model.Server) bool {
	if d.instanceCount(main) > 1 {
		return true
	}
	list, _ := d.store.ListServers()
	for _, sib := range siblingsOf(main.ID, list) {
		if isLive(d.servers.State(sib.ID)) {
			return true
		}
	}
	return false
}

// stopSiblings stops every running copy of mainID.
func (d *Daemon) stopSiblings(mainID string) {
	list, _ := d.store.ListServers()
	for _, sib := range siblingsOf(mainID, list) {
		if isLive(d.servers.State(sib.ID)) {
			_ = d.servers.Stop(sib.ID)
		}
	}
}

// deleteSiblings stops and removes every copy of mainID.
//
// Yedekler ana sunucunun kaydıdır ve silinmez; kardeşin dünya klasörü
// gider — ana silinince o dilimleri çalıştıracak bir sunucu kalmaz.
func (d *Daemon) deleteSiblings(mainID string) {
	list, _ := d.store.ListServers()
	for _, sib := range siblingsOf(mainID, list) {
		if isLive(d.servers.State(sib.ID)) {
			d.stopAndWait(sib.ID)
		}
		if err := d.store.DeleteServer(sib.ID); err != nil {
			d.log.Warnf("instances: %s silinemedi: %v", sib.Name, err)
		}
	}
}

// localInstances lists main's active copies for the topology.
func (d *Daemon) localInstances(main *model.Server, list []*model.Server) []model.LinkInstance {
	count := d.instanceCount(main)
	if count <= 1 {
		return nil
	}
	var out []model.LinkInstance
	for _, sib := range activeSiblings(siblingsOf(main.ID, list), count) {
		out = append(out, model.LinkInstance{
			Index:    sib.InstanceIndex,
			MCPort:   sib.Port,
			LinkPort: sib.Link.LinkPort,
			// Yalnızca AÇIK kopya çevrimiçidir: mod çevrimdışı düğüme
			// aktarmaz, oyuncu açılmakta olan bir sunucuya düşmez.
			Online:     d.servers.State(sib.ID) == model.StateRunning,
			PublicAddr: sib.WAN.Hostname,
		})
	}
	return out
}

// instanceEnv tells a copy's Minecraft process which node it is.
//
// Topolojinin "self" alanı makinenin adıdır (ana sunucu); kardeş kendi
// dilimini bu değişken olmadan bulamazdı ve ana sunucunun dilimini
// sahiplenirdi.
func (d *Daemon) instanceEnv(srv *model.Server) []string {
	if !srv.IsSibling() || srv.InstanceIndex < 2 {
		return nil
	}
	return []string{"MCOS_LINK_SELF=" + model.InstanceNodeName(d.cluster.NodeName(), srv.InstanceIndex)}
}

// decorateInstances hides copies from a listing and folds them into the main.
//
// Menüde tek sunucu: oyuncu sayısı tüm kopyaların toplamıdır, satırda
// "×N sunucu" yazması için RunningInstances doldurulur.
func (d *Daemon) decorateInstances(list []*model.Server) []*model.Server {
	byParent := map[string][]*model.Server{}
	for _, s := range list {
		if s.IsSibling() {
			byParent[s.ParentID] = append(byParent[s.ParentID], s)
		}
	}
	out := model.HideSiblings(list)
	for _, m := range out {
		count := d.instanceCount(m)
		if count <= 1 {
			continue
		}
		m.RunningInstances = count
		for _, sib := range activeSiblings(byParent[m.ID], count) {
			d.servers.FillRuntime(sib)
			m.Players += sib.Players
		}
	}
	return out
}

// copyWorlds copies every world folder (level.dat taşıyan klasör) once.
func copyWorlds(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(src, e.Name(), "level.dat")); err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(dst, e.Name())); err == nil {
			continue // kardeşin kendi dünyası var: ezilmez
		}
		if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()), 0,
			func(rel string, _ fs.FileInfo) bool { return filepath.Base(rel) != "session.lock" }); err != nil {
			return err
		}
	}
	return nil
}

// mirrorAddons makes a copy's mods/ and plugins/ match the main's.
//
// Jar'lar birebir aynalanır (ana sunucudan kaldırılan mod kardeşten de
// kalkar); alt klasörlerden ve config/'den yalnızca küçük ayar dosyaları
// alınır. Eklentilerin veri klasörleri (harita karoları, veritabanları)
// gigabaytlarca olabilir ve her açılışta kopyalanmamalı.
func mirrorAddons(src, dst string) error {
	for _, dir := range []string{"mods", "plugins"} {
		if err := mirrorJars(filepath.Join(src, dir), filepath.Join(dst, dir)); err != nil {
			return err
		}
	}
	for _, dir := range []string{"mods", "plugins", "config"} {
		s := filepath.Join(src, dir)
		if _, err := os.Stat(s); err != nil {
			continue
		}
		if err := copyTree(s, filepath.Join(dst, dir), 2, isSmallConfig); err != nil {
			return err
		}
	}
	return nil
}

// mirrorJars makes dst's top-level jars exactly src's.
func mirrorJars(src, dst string) error {
	want := map[string]bool{}
	entries, err := os.ReadDir(src)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
			continue
		}
		want[e.Name()] = true
		si, err1 := e.Info()
		di, err2 := os.Stat(filepath.Join(dst, e.Name()))
		if err1 == nil && err2 == nil && si.Size() == di.Size() && !si.ModTime().After(di.ModTime()) {
			continue // aynı dosya: her açılışta yüzlerce MB kopyalamayalım
		}
		if err := copyFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	old, _ := os.ReadDir(dst)
	for _, e := range old {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".jar") && !want[e.Name()] {
			_ = os.Remove(filepath.Join(dst, e.Name()))
		}
	}
	return nil
}

// isSmallConfig selects plugin/mod settings files worth syncing.
func isSmallConfig(rel string, fi fs.FileInfo) bool {
	if fi.Size() > 1<<20 {
		return false
	}
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".yml", ".yaml", ".json", ".json5", ".toml", ".properties", ".conf", ".cfg", ".txt":
		return true
	}
	return false
}

// copyTree copies src into dst, keeping files keep() accepts.
//
// maxDepth > 0 ise o kadar klasör derinliğinden aşağı inilmez: ayar
// dosyaları "plugins/<eklenti>/config.yml" düzeyindedir; harita karosu gibi
// binlerce klasörlük ağaçları her açılışta gezmek boşa iştir. Klasörler
// yalnızca içine dosya kopyalanırken oluşturulur.
func copyTree(src, dst string, maxDepth int, keep func(rel string, fi fs.FileInfo) bool) error {
	return filepath.WalkDir(src, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if e.IsDir() {
			if maxDepth > 0 && rel != "." && strings.Count(rel, string(filepath.Separator))+1 > maxDepth {
				return fs.SkipDir
			}
			return nil
		}
		fi, err := e.Info()
		if err != nil || !fi.Mode().IsRegular() || !keep(rel, fi) {
			return nil
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(path, target)
	})
}
