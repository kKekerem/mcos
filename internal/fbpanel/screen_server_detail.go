package fbpanel

// ── Sunucu detayı ───────────────────────────────────────────────────────────
//
// Kullanıcının isteği: "eski paneldeki sunucu detayları panelini de ekler
// misin, sunucuya enter'e basınca o ekrana gitsin". Eski panelin
// (panel/view_detail.go) on iki sekmesinin HEPSİ burada, aynı sırayla ve aynı
// tuşlarla: kas hafızası korunmalı.
//
// Bir Section DEĞİL, Sunucular bölümünün ALT DURUMU (sihirbaz gibi): kenar
// çubuğunda "Sunucular" seçili kalır, Esc listeye ve AYNI satıra döner.
//
// ── Eşzamanlılık ────────────────────────────────────────────────────────────
// Her RPC bir goroutine'de yapılır; ana döngü (tuş + çizim) hiçbir zaman
// ağ beklemez. Goroutine'lerin yazdığı alanlar d.mu ile korunur; yalnızca ana
// döngünün dokunduğu alanlar (sekme, imleçler, görünüm) kilitsizdir.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"mcos/internal/fbui"
	"mcos/internal/ipc"
	"mcos/internal/model"
)

// detailTab is one tab of the server detail screen.
type detailTab int

// Sıra eski panelle (panel/view_detail.go detailTabs) AYNI.
const (
	dtGeneral detailTab = iota
	dtConsole
	dtSettings
	dtPlayers
	dtSoftware
	dtFiles
	dtWorlds
	dtBackups
	dtAccess
	dtWAN
	dtPerf
	dtNetwork
	dtCount
)

var detailTabNames = [dtCount]string{
	dtGeneral:  "Genel",
	dtConsole:  "Konsol",
	dtSettings: "Ayarlar",
	dtPlayers:  "Oyuncular",
	dtSoftware: "Yazılım",
	dtFiles:    "Dosyalar",
	dtWorlds:   "Dünyalar",
	dtBackups:  "Yedekler",
	dtAccess:   "Erişim",
	dtWAN:      "İnternete Aç",
	dtPerf:     "Performans",
	dtNetwork:  "Ağ",
}

// Name returns the tab label.
func (t detailTab) Name() string {
	if t < 0 || t >= dtCount {
		return ""
	}
	return detailTabNames[t]
}

// Yazılım sekmesinin iki görünümü: kurulu dosyalar ve Modrinth sonuçları.
const (
	swInstalled = iota
	swResults
)

// consoleMax, bellekte tutulan konsol satırı sınırıdır (eski panelle aynı).
const consoleMax = 500

// consolePoll, konsol sekmesi açıkken yoklama aralığıdır (eski panelle aynı).
const consolePoll = 700 * time.Millisecond

// ServerDetail is the per-server screen opened from the server list.
type ServerDetail struct {
	// id, sunucunun KİMLİĞİDİR. Liste imleci değil: detay açıkken liste
	// sırası değişebilir (bir sunucu silindi, biri eklendi).
	id string
	// listCursor, açılıştaki liste satırı. Esc'te sunucu artık listede
	// yoksa buna en yakın satıra dönülür.
	listCursor int

	tab detailTab
	// cursor, detayın KENDİ imleçleridir — sekme başına bir tane. Sekme
	// değiştirip geri gelen kullanıcı kaldığı satırı bulmalı.
	cursor [dtCount]int
	// consoleBack, konsolda en alttan kaç satır geri kaydırıldığıdır.
	consoleBack int
	// swView: swInstalled | swResults.
	swView int
	// filesPath, Dosyalar sekmesinde bulunulan klasör ("." = kök).
	filesPath string

	// consoleOn, konsol yoklayıcısının çalıştığını söyler: aynı anda iki
	// yoklayıcı aynı imleçle satırları İKİ KEZ eklerdi.
	consoleOn atomic.Bool

	mu      sync.Mutex
	loading [dtCount]bool
	errs    [dtCount]string

	console    []string
	consoleCur int64

	players   ipc.PlayersListResult
	installed []model.FileEntry
	search    detailSearch
	files     []model.FileEntry
	worlds    []model.World
	backups   []model.Backup
	// backupPol, otomatik yedek planı ve sonraki yedeğin zamanı (bkz.
	// screen_server_detail_backup.go).
	backupPol detailBackupPolicy
	access    detailAccess
}

// detailSearch is the Modrinth search state of the software tab.
type detailSearch struct {
	query      string
	anyVersion bool
	busy       bool
	done       bool
	err        string
	res        ipc.CatalogSearchResult
}

// detailAccess holds the names read from ops/whitelist/banned-players.json.
type detailAccess struct {
	ops, whitelist, banned []string
}

func newServerDetail(id string, listCursor int) *ServerDetail {
	return &ServerDetail{id: id, listCursor: listCursor, filesPath: "."}
}

// ── Aç / kapat ──────────────────────────────────────────────────────────────

// detailState returns the open server detail, or nil.
func (a *App) detailState() *ServerDetail {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.detail
}

// openServerDetail opens the detail of the server under the list cursor.
func (a *App) openServerDetail() {
	s := a.selectedServer()
	if s == nil {
		return
	}
	d := newServerDetail(s.ID, a.Cursor())
	a.beginTransition(transFade)
	a.mu.Lock()
	a.detail = d
	a.focus = FocusContent
	a.dirty = true
	a.mu.Unlock()
	a.detailEnterTab(d)
}

// closeServerDetail returns to the list and puts the cursor back on the
// server's row.
//
// Satır KİMLİKLE bulunur: detay açıkken liste sırası değiştiyse eski dizin
// artık başka bir sunucuyu gösterir. Sunucu silindiyse eski dizine en yakın
// geçerli satıra dönülür.
func (a *App) closeServerDetail() {
	d := a.detailState()
	if d == nil {
		return
	}
	d.consoleOn.Store(false)
	a.beginTransition(transFade)
	a.mu.Lock()
	a.detail = nil
	cur := -1
	for i, s := range a.servers {
		if s.ID == d.id {
			cur = i
			break
		}
	}
	if cur < 0 {
		cur = d.listCursor
		if cur >= len(a.servers) {
			cur = len(a.servers) - 1
		}
		if cur < 0 {
			cur = 0
		}
	}
	a.cursor = cur
	a.focus = FocusContent
	a.dirty = true
	a.mu.Unlock()
}

// dropServerDetail forgets the detail without touching the cursor (used when
// another section took over the screen).
func (a *App) dropServerDetail() {
	a.mu.Lock()
	d := a.detail
	a.detail = nil
	a.dirty = true
	a.mu.Unlock()
	if d != nil {
		d.consoleOn.Store(false)
	}
}

// detailServer returns the live server record for the detail, or nil.
//
// Kopya DEĞİL, yoklamanın son kaydı: durum, oyuncu sayısı ve çalışma süresi
// her saniye tazelenir ve detay ekstra RPC yapmadan güncel kalır.
func (a *App) detailServer(d *ServerDetail) *model.Server {
	_, servers, _ := a.Snapshot()
	for _, s := range servers {
		if s.ID == d.id {
			return s
		}
	}
	return nil
}

// ── Tuşlar ──────────────────────────────────────────────────────────────────

// detailKey handles a keystroke while the server detail is open.
func (a *App) detailKey(d *ServerDetail, key string) Action {
	defer a.Invalidate()

	switch key {
	case "f12":
		// Eski panele acil çıkış HER EKRANDA çalışmalı (setup.go ile aynı
		// desen): detay tuşları yutsaydı, yeni arayüz bozulduğunda kullanıcının
		// elinde hiçbir çıkış kalmazdı.
		return ActLegacyPanel
	case "ctrl+c", "q":
		return ActQuit
	}

	s := a.detailServer(d)
	if s == nil {
		// Sunucu silinmiş (başka bir istemciden). Boş bir detayda kalmak
		// yerine listeye dön ve söyle.
		a.closeServerDetail()
		a.Emit(fbui.EventWarn, "Sunucu artık listede yok")
		return ActNone
	}

	// Fare: sekme ve satır bölgeleri sentetik tuş gönderir (bkz. çizim).
	if n, ok := strings.CutPrefix(key, "detail-tab-"); ok {
		if i, err := strconv.Atoi(n); err == nil && i >= 0 && i < int(dtCount) {
			a.detailSetTab(d, detailTab(i))
		}
		return ActNone
	}
	if n, ok := strings.CutPrefix(key, "detail-row-"); ok {
		i, err := strconv.Atoi(n)
		if err != nil {
			return ActNone
		}
		// İlk tık seçer, seçili satıra tık çalıştırır (listelerle aynı kural).
		if d.cursor[d.tab] == i {
			a.detailActivate(d, s)
		} else {
			d.cursor[d.tab] = i
		}
		return ActNone
	}

	// Sekmeye özgü tuşlar ÖNCE: Oyuncular'da "d" deop, Yazılım'da "d"
	// kaldır demektir.
	if a.detailTabKey(d, s, key) {
		return ActNone
	}

	switch key {
	case "esc":
		if d.tab == dtSoftware && d.swView == swResults {
			// Aramadan kurulu listeye dön; ikinci Esc detaydan çıkar.
			d.swView = swInstalled
			d.cursor[dtSoftware] = 0
			return ActNone
		}
		a.closeServerDetail()
	case "backspace", "b":
		if d.tab == dtFiles && d.filesPath != "." {
			a.detailFilesOpen(d, parentDir(d.filesPath))
			return ActNone
		}
		if d.tab == dtSoftware && d.swView == swResults {
			d.swView = swInstalled
			d.cursor[dtSoftware] = 0
			return ActNone
		}
		a.closeServerDetail()
	case "left", "h", "[", "shift+tab":
		a.detailSetTab(d, (d.tab-1+dtCount)%dtCount)
	case "right", "l", "]", "tab":
		a.detailSetTab(d, (d.tab+1)%dtCount)
	case "up", "k":
		a.detailMove(d, -1)
	case "down", "j":
		a.detailMove(d, 1)
	case "enter":
		a.detailActivate(d, s)
	case "s", "S":
		a.serverActionOn(s, "start")
	case "x", "X":
		a.serverActionOn(s, "stop")
	case "r", "R":
		// Eski panelle aynı: r = yeniden başlat. Listede r "yenile"dir; detay
		// zaten kendiliğinden tazelenir, yenileme F5'te.
		a.serverActionOn(s, "restart")
	case "f5":
		a.detailEnterTab(d)
		a.Emit(fbui.EventInfo, d.tab.Name()+" yenilendi")
	case "n", "N":
		a.StartWizard()
	case "g":
		a.dropServerDetail()
		a.gotoSection(SecPower)
		a.setFocus(FocusContent)
	}
	return ActNone
}

// detailSetTab switches tabs and loads the new tab's data.
func (a *App) detailSetTab(d *ServerDetail, t detailTab) {
	if t < 0 || t >= dtCount || t == d.tab {
		return
	}
	if d.tab == dtConsole {
		d.consoleOn.Store(false)
	}
	d.tab = t
	a.Invalidate()
	a.detailEnterTab(d)
}

// detailMove moves the detail's own cursor (or scrolls the console).
func (a *App) detailMove(d *ServerDetail, delta int) {
	defer a.Invalidate()
	if d.tab == dtConsole {
		// Konsolda ok tuşları geçmişte kaydırır: yukarı = eskiye.
		d.mu.Lock()
		n := len(d.console)
		d.mu.Unlock()
		d.consoleBack -= delta
		if d.consoleBack < 0 {
			d.consoleBack = 0
		}
		if d.consoleBack > n {
			d.consoleBack = n
		}
		return
	}
	s := a.detailServer(d)
	if s == nil {
		return
	}
	n := len(a.detailViewFor(d, s).items)
	if n == 0 {
		d.cursor[d.tab] = 0
		return
	}
	d.cursor[d.tab] = (d.cursor[d.tab] + delta + n) % n
}

// detailTabKey handles letter keys that belong to one tab. Returns true if
// the key was consumed.
func (a *App) detailTabKey(d *ServerDetail, s *model.Server, key string) bool {
	switch d.tab {
	case dtConsole:
		switch key {
		case "i", "/", ":":
			a.detailOpenCommand(d, s)
			return true
		case "end":
			d.consoleBack = 0
			return true
		}
	case dtPlayers:
		if cmd, ok := playerKeyCommand(key); ok {
			if name, has := a.detailSelectedPlayer(d); has {
				a.detailPlayerCommand(d, s, cmd, name)
			}
			return true
		}
	case dtSoftware:
		switch key {
		case "/":
			a.detailOpenSearch(d, s)
			return true
		case "v":
			// Sürüm süzgecini aç/kapat; bir arama varsa hemen yenile.
			d.mu.Lock()
			q, anyV := d.search.query, !d.search.anyVersion
			d.search.anyVersion = anyV
			d.mu.Unlock()
			if q != "" {
				a.detailSearch(d, s, q, anyV)
			} else if anyV {
				a.Emit(fbui.EventInfo, "Sürüm süzgeci kapalı — tüm Minecraft sürümleri aranacak")
			} else {
				a.Emit(fbui.EventInfo, "Sürüm süzgeci açık — yalnızca "+s.MCVersion)
			}
			return true
		case "u", "U":
			a.detailUSB(d, s)
			return true
		case "p", "P":
			a.detailPerfPack(d, s)
			return true
		case "d", "delete":
			if d.swView == swInstalled {
				a.detailConfirmRemove(d, s)
				return true
			}
		}
	case dtBackups:
		if key == "c" || key == "C" {
			a.detailBackupCreate(d, s)
			return true
		}
		if key == "o" || key == "O" {
			a.detailBackupPolicyMenu(d, s)
			return true
		}
	case dtWAN:
		if key == "t" || key == " " || key == "space" {
			a.detailToggle(d, s, "wan")
			return true
		}
	}
	return false
}

// playerKeyCommand maps the old panel's moderation keys to console commands.
//
// Harfler eski panelle AYNI (o/d/K/B/u/w/W); büyük harfler yıkıcı olanlar
// (at, yasakla) — yanlışlıkla basılmasın diye Shift ister.
func playerKeyCommand(key string) (string, bool) {
	switch key {
	case "o":
		return "op", true
	case "d":
		return "deop", true
	case "K":
		return "kick", true
	case "B":
		return "ban", true
	case "u":
		return "pardon", true
	case "w":
		return "whitelist add", true
	case "W":
		return "whitelist remove", true
	}
	return "", false
}

// detailActivate runs Enter on the current tab's selected row.
func (a *App) detailActivate(d *ServerDetail, s *model.Server) {
	if d.tab == dtConsole {
		a.detailOpenCommand(d, s)
		return
	}
	v := a.detailViewFor(d, s)
	c := d.cursor[d.tab]
	if c < 0 || c >= len(v.items) {
		return
	}
	it := v.items[c]
	if it.disabled {
		return
	}
	switch it.act {
	case "set-ram":
		a.detailEditNumber(d, s, "RAM (MB)", "ram", s.RAMMB, 512, 262144)
	case "set-view":
		a.detailEditNumber(d, s, "Görüş uzaklığı", "view", orInt(s.ViewDistance, 10), 2, 32)
	case "set-sim":
		a.detailEditNumber(d, s, "Simülasyon uzaklığı", "sim", orInt(s.SimDistance, 10), 2, 32)
	case "set-maxplayers":
		a.detailEditNumber(d, s, "Maksimum oyuncu", "maxplayers", orInt(s.MaxPlayers, 20), 1, 1000)
	case "toggle-fullperf":
		a.detailToggle(d, s, "fullperf")
	case "toggle-autostart":
		a.detailToggle(d, s, "autostart")
	case "toggle-wan":
		a.detailToggle(d, s, "wan")
	case "player":
		a.detailPlayerMenu(d, s, it.value.(string))
	case "installed":
		a.detailInstalledMenu(d, s, it.value.(model.FileEntry))
	case "result":
		a.detailInstall(d, s, it.value.(ipc.CatalogItem))
	case "search-any":
		d.mu.Lock()
		q := d.search.query
		d.mu.Unlock()
		a.detailSearch(d, s, q, true)
	case "search-new":
		a.detailOpenSearch(d, s)
	case "dir":
		a.detailFilesOpen(d, it.value.(string))
	case "backup-create":
		a.detailBackupCreate(d, s)
	case "backup-policy":
		a.detailBackupPolicyMenu(d, s)
	case "backup":
		a.detailBackupMenu(d, s, it.value.(model.Backup))
	case "access":
		a.detailAccessMenu(d, s, it.value.(accessEntry))
	case "access-op":
		a.detailAskPlayer(d, s, "Op yetkisi ver", "op")
	case "access-wl":
		a.detailAskPlayer(d, s, "Beyaz listeye ekle", "whitelist add")
	}
}

// ── Veri yükleme (hepsi arka planda) ────────────────────────────────────────

// detailEnterTab fetches the data the current tab needs.
//
// Veri YALNIZCA sekmeye girilince çekilir: üzerinde Minecraft sunucusu koşan
// bir makinede, bakılmayan on sekmenin her saniye yoklanması israftır.
func (a *App) detailEnterTab(d *ServerDetail) {
	switch d.tab {
	case dtConsole:
		a.detailStartConsole(d)
	case dtPlayers:
		a.detailLoadPlayers(d)
	case dtSoftware:
		a.detailLoadInstalled(d)
	case dtFiles:
		a.detailFilesOpen(d, d.filesPath)
	case dtWorlds:
		a.detailLoad(d, dtWorlds, func() error {
			w, err := a.cl.WorldsList(d.id)
			if err == nil {
				d.mu.Lock()
				d.worlds = w
				d.mu.Unlock()
			}
			return err
		})
	case dtBackups:
		a.detailLoadBackups(d)
	case dtAccess:
		a.detailLoad(d, dtAccess, func() error {
			acc := detailAccess{
				ops:       a.readNames(d.id, "ops.json"),
				whitelist: a.readNames(d.id, "whitelist.json"),
				banned:    a.readNames(d.id, "banned-players.json"),
			}
			d.mu.Lock()
			d.access = acc
			d.mu.Unlock()
			return nil
		})
	}
}

func (a *App) detailLoadPlayers(d *ServerDetail) {
	a.detailLoad(d, dtPlayers, func() error {
		r, err := a.cl.PlayersList(d.id)
		if err == nil {
			d.mu.Lock()
			d.players = r
			d.mu.Unlock()
		}
		return err
	})
}

// detailLoad runs fetch in the background with the tab's loading flag set.
func (a *App) detailLoad(d *ServerDetail, t detailTab, fetch func() error) {
	if a.offline() {
		return
	}
	d.mu.Lock()
	if d.loading[t] {
		d.mu.Unlock()
		return
	}
	d.loading[t] = true
	d.errs[t] = ""
	d.mu.Unlock()
	go func() {
		err := fetch()
		d.mu.Lock()
		d.loading[t] = false
		if err != nil {
			d.errs[t] = err.Error()
		}
		d.mu.Unlock()
		a.Invalidate()
	}()
}

// contentDir is where the server keeps plugins/mods, relative to its root.
//
// daemon/handlers_catalog.go contentDirFor ile AYNI kural. Vanilla için boş:
// orada kurulabilecek bir eklenti klasörü yok.
func contentDir(s *model.Server) string {
	switch {
	case s.Software.SupportsPlugins():
		return "plugins"
	case s.Software.SupportsMods():
		return "mods"
	}
	return ""
}

// contentKind is the badge shown for content on this server.
//
// Rozet SUNUCUNUN yazılımından türer, Modrinth'in proje türünden DEĞİL:
// Modrinth WorldEdit'e bile "mod" der, oysa Paper sunucusuna kurulan şey bir
// eklentidir. Kullanıcı rozete bakıp "mod mu kuruyorum, Paper'da çalışır mı?"
// diye tereddüt etmemeli.
func contentKind(s *model.Server) string {
	switch {
	case s.Software.SupportsPlugins():
		return "eklenti"
	case s.Software.SupportsMods():
		return "mod"
	}
	return ""
}

// canInstallContent reports whether the server can load plugins or mods.
func canInstallContent(s *model.Server) bool { return contentDir(s) != "" }

func (a *App) detailLoadInstalled(d *ServerDetail) {
	s := a.detailServer(d)
	if s == nil || !canInstallContent(s) {
		return
	}
	dir := contentDir(s)
	a.detailLoad(d, dtSoftware, func() error {
		es, err := a.cl.FilesList(d.id, dir)
		if err != nil {
			// Klasör henüz yok (sunucu hiç açılmadı): hata değil, boş liste.
			if strings.Contains(strings.ToLower(err.Error()), "no such file") {
				err = nil
			}
		}
		jars := jarEntries(es)
		d.mu.Lock()
		d.installed = jars
		d.mu.Unlock()
		return err
	})
}

// jarEntries keeps the loadable files, sorted by name.
func jarEntries(es []model.FileEntry) []model.FileEntry {
	var out []model.FileEntry
	for _, e := range es {
		if e.IsDir {
			continue
		}
		low := strings.ToLower(e.Name)
		if strings.HasSuffix(low, ".jar") || strings.HasSuffix(low, ".jar.disabled") {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func (a *App) detailFilesOpen(d *ServerDetail, p string) {
	if p == "" {
		p = "."
	}
	if p != d.filesPath {
		d.filesPath = p
		d.cursor[dtFiles] = 0
	}
	a.detailLoad(d, dtFiles, func() error {
		es, err := a.cl.FilesList(d.id, p)
		if err == nil {
			sortFiles(es)
			d.mu.Lock()
			d.files = es
			d.mu.Unlock()
		}
		return err
	})
}

// sortFiles lists folders first, then files, each alphabetically.
func sortFiles(es []model.FileEntry) {
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].IsDir != es[j].IsDir {
			return es[i].IsDir
		}
		return strings.ToLower(es[i].Name) < strings.ToLower(es[j].Name)
	})
}

func parentDir(p string) string {
	p = path.Clean(strings.TrimPrefix(p, "./"))
	if p == "." || p == "/" || p == "" {
		return "."
	}
	return path.Dir(p)
}

func (a *App) detailLoadBackups(d *ServerDetail) {
	a.detailLoad(d, dtBackups, func() error {
		b, err := a.cl.BackupList(d.id)
		if err == nil {
			sort.Slice(b, func(i, j int) bool { return b[i].CreatedAt.After(b[j].CreatedAt) })
			d.mu.Lock()
			d.backups = b
			d.mu.Unlock()
		}
		// Yeni bir yedek "sonraki" zamanını değiştirir: plan listeyle birlikte.
		a.fetchBackupPolicy(d)
		return err
	})
}

// readNames reads a Minecraft access list (ops.json & co.) and returns names.
//
// Dosya yoksa (sunucu hiç açılmadı) boş liste: bu bir hata değil.
func (a *App) readNames(id, file string) []string {
	res, err := a.cl.FilesRead(id, file)
	if err != nil {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(res.ContentBase64)
	if err != nil {
		return nil
	}
	return parseNames(raw)
}

// parseNames extracts "name" fields from a Minecraft JSON access list.
func parseNames(raw []byte) []string {
	var rows []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &rows) != nil {
		return nil
	}
	var out []string
	for _, r := range rows {
		if r.Name != "" {
			out = append(out, r.Name)
		}
	}
	sort.Strings(out)
	return out
}

// ── Konsol ──────────────────────────────────────────────────────────────────

// detailStartConsole polls the console while its tab is visible.
//
// Tek yoklayıcı: consoleOn bayrağı hem "çalışıyor" hem "devam et" demek.
// Sekme değişince ya da detay kapanınca bayrak düşer ve goroutine bir sonraki
// turda kendiliğinden biter.
func (a *App) detailStartConsole(d *ServerDetail) {
	if a.offline() {
		return
	}
	if !d.consoleOn.CompareAndSwap(false, true) {
		return
	}
	go func() {
		for d.consoleOn.Load() {
			d.mu.Lock()
			cur := d.consoleCur
			d.mu.Unlock()
			res, err := a.cl.Console(d.id, cur)
			d.mu.Lock()
			if err != nil {
				d.errs[dtConsole] = err.Error()
			} else {
				d.errs[dtConsole] = ""
				d.appendConsoleLocked(res)
			}
			d.mu.Unlock()
			if err != nil || len(res.Lines) > 0 {
				a.Invalidate()
			}
			time.Sleep(consolePoll)
			if a.detailState() != d {
				d.consoleOn.Store(false)
			}
		}
	}()
}

// appendConsoleLocked adds polled lines, keeping the last consoleMax.
// d.mu TUTULARAK çağrılır.
func (d *ServerDetail) appendConsoleLocked(res ipc.ConsoleResult) {
	for _, ln := range res.Lines {
		d.console = append(d.console, ln.Text)
	}
	if len(d.console) > consoleMax {
		d.console = append([]string(nil), d.console[len(d.console)-consoleMax:]...)
	}
	d.consoleCur = res.Cursor
}

func (a *App) detailOpenCommand(d *ServerDetail, s *model.Server) {
	if s.State != model.StateRunning {
		a.Emit(fbui.EventWarn, "Komut göndermek için sunucu çalışıyor olmalı (s ile başlatın)")
		return
	}
	id, name := s.ID, s.Name
	a.OpenModal(NewTextModal("Konsol komutu", name+" konsoluna gönderilecek komut",
		func(app *App, text string) {
			cmd := strings.TrimPrefix(strings.TrimSpace(text), "/")
			if cmd == "" || app.offline() {
				return
			}
			d.consoleBack = 0
			go func() {
				if err := app.cl.Command(id, cmd); err != nil {
					app.Fail("komut gönderilemedi", err)
					return
				}
				app.Emit(fbui.EventOK, "Komut gönderildi: "+cmd)
			}()
		}).WithPlaceholder("say Merhaba").WithOK("Gönder").WithMaxLen(256))
}

// ── Oyuncular / erişim ──────────────────────────────────────────────────────

func (a *App) detailSelectedPlayer(d *ServerDetail) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c := d.cursor[dtPlayers]
	if c < 0 || c >= len(d.players.Players) {
		return "", false
	}
	return d.players.Players[c].Name, true
}

// detailPlayerCommand sends a moderation command; kick and ban ask first.
func (a *App) detailPlayerCommand(d *ServerDetail, s *model.Server, cmd, name string) {
	run := func(app *App) {
		if app.offline() {
			return
		}
		full := cmd + " " + name
		id := s.ID
		go func() {
			if err := app.cl.PlayersCommand(id, full); err != nil {
				app.Fail(full, err)
				return
			}
			app.Emit(fbui.EventOK, "Oyuncu komutu: "+full)
			// Liste değişmiş olabilir (atılan oyuncu çıkar). detailEnterTab
			// DEĞİL: o d.tab'ı okur ve d.tab yalnızca ana döngüye aittir.
			app.detailLoadPlayers(d)
		}()
	}
	switch cmd {
	case "kick", "ban":
		verb := "Oyundan atılsın mı?"
		if cmd == "ban" {
			verb = "Yasaklansın mı?"
		}
		a.OpenModal(NewConfirmModal(name+" — "+verb,
			[]string{name + " sunucudan " + map[string]string{"kick": "atılacak.", "ban": "yasaklanacak."}[cmd],
				"Yasak, Erişim sekmesinden kaldırılabilir."},
			map[string]string{"kick": "At", "ban": "Yasakla"}[cmd], true, run))
	default:
		run(a)
	}
}

func (a *App) detailPlayerMenu(d *ServerDetail, s *model.Server, name string) {
	items := []ListItem{
		{Label: "Op yetkisi ver", Detail: "o", Value: "op"},
		{Label: "Op yetkisini al", Detail: "d", Value: "deop"},
		{Label: "Beyaz listeye ekle", Detail: "w", Value: "whitelist add"},
		{Label: "Beyaz listeden çıkar", Detail: "W", Value: "whitelist remove"},
		{Label: "Oyundan at", Detail: "K", Value: "kick"},
		{Label: "Yasakla", Detail: "B", Value: "ban"},
		{Label: "Yasağı kaldır", Detail: "u", Value: "pardon"},
	}
	a.OpenModal(NewListModal(name, "Oyuncu için bir işlem seçin.", items,
		func(app *App, _ int, it ListItem) bool {
			app.detailPlayerCommand(d, s, it.Value.(string), name)
			return true
		}))
}

// accessEntry is one row of the access tab.
type accessEntry struct {
	name string
	list string // "op" | "whitelist" | "banned"
}

func (a *App) detailAccessMenu(d *ServerDetail, s *model.Server, e accessEntry) {
	var label, cmd string
	switch e.list {
	case "op":
		label, cmd = "Op yetkisini al", "deop"
	case "whitelist":
		label, cmd = "Beyaz listeden çıkar", "whitelist remove"
	default:
		label, cmd = "Yasağı kaldır", "pardon"
	}
	a.OpenModal(NewListModal(e.name, "", []ListItem{{Label: label, Value: cmd}},
		func(app *App, _ int, it ListItem) bool {
			app.detailPlayerCommand(d, s, it.Value.(string), e.name)
			return true
		}))
}

func (a *App) detailAskPlayer(d *ServerDetail, s *model.Server, title, cmd string) {
	a.OpenModal(NewTextModal(title, "Oyuncu adı", func(app *App, text string) {
		name := strings.TrimSpace(text)
		if name != "" {
			app.detailPlayerCommand(d, s, cmd, name)
		}
	}).WithMaxLen(16).WithValidate(func(v string) string {
		v = strings.TrimSpace(v)
		if v == "" {
			return "Bir oyuncu adı yazın"
		}
		for _, r := range v {
			if !(r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
				return "Minecraft adları yalnızca harf, rakam ve _ içerir"
			}
		}
		return ""
	}))
}

// ── Ayarlar ─────────────────────────────────────────────────────────────────

func orInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// detailEditNumber asks for a number and saves it through server.update.
func (a *App) detailEditNumber(d *ServerDetail, s *model.Server, label, field string, cur, lo, hi int) {
	id := s.ID
	a.OpenModal(NewTextModal(label, fmt.Sprintf("%s (%d – %d)", label, lo, hi),
		func(app *App, text string) {
			v, _ := strconv.Atoi(strings.TrimSpace(text))
			p := ipc.ServerUpdateParams{ID: id}
			switch field {
			case "ram":
				p.RAMMB = v
			case "view":
				p.ViewDistance = v
			case "sim":
				p.SimDistance = v
			case "maxplayers":
				p.MaxPlayers = v
			}
			app.detailUpdate(p, label+" = "+strconv.Itoa(v))
		}).WithValue(strconv.Itoa(cur)).WithMaxLen(6).WithOK("Kaydet").
		WithHint("Sunucu yeniden başlatılınca geçerli olur.").
		WithValidate(func(t string) string {
			v, err := strconv.Atoi(strings.TrimSpace(t))
			if err != nil {
				return "Bir sayı yazın"
			}
			if v < lo || v > hi {
				return fmt.Sprintf("%d ile %d arasında olmalı", lo, hi)
			}
			return ""
		}))
}

// detailToggle flips a boolean server setting.
func (a *App) detailToggle(d *ServerDetail, s *model.Server, field string) {
	p := ipc.ServerUpdateParams{ID: s.ID}
	var label string
	switch field {
	case "fullperf":
		v := !s.FullPerf
		p.FullPerf, label = &v, "Tam performans "+onOff(v)
	case "autostart":
		v := !s.Autostart
		p.Autostart, label = &v, "Otomatik başlatma "+onOff(v)
	case "wan":
		v := !s.WAN.Enabled
		p.WAN, label = &v, "İnternete açma "+onOff(v)
	default:
		return
	}
	a.detailUpdate(p, label)
}

// detailUpdate saves settings in the background and patches the local copy.
//
// Yerel kopya HEMEN güncellenir: yoklama bir saniye sonra aynı şeyi getirir,
// ama o saniye boyunca eski değeri göstermek "kaydedilmedi mi?" dedirtir.
func (a *App) detailUpdate(p ipc.ServerUpdateParams, label string) {
	if a.offline() {
		a.Emit(fbui.EventWarn, "mcosd bağlantısı yok")
		return
	}
	a.Emit(fbui.EventBusy, "Kaydediliyor: "+label+"…")
	go func() {
		srv, err := a.cl.UpdateServer(p)
		if err != nil {
			a.Fail("ayar kaydedilemedi", err)
			return
		}
		if srv != nil {
			a.mu.Lock()
			list := make([]*model.Server, len(a.servers))
			copy(list, a.servers)
			for i, s := range list {
				if s.ID == srv.ID {
					// Çalışma zamanı alanları (durum, oyuncu) güncellemede
					// gelmeyebilir; yoklamanın bildiğini koru.
					srv.State, srv.Players, srv.UptimeSec, srv.PID, srv.LastLog =
						s.State, s.Players, s.UptimeSec, s.PID, s.LastLog
					list[i] = srv
				}
			}
			a.servers = list
			a.dirty = true
			a.mu.Unlock()
		}
		a.Emit(fbui.EventOK, label)
	}()
}

// ── Yazılım: arama, kurma, kaldırma ─────────────────────────────────────────

// explainNoContent tells the user why a flavour cannot take plugins or mods.
//
// Vanilla'da arama AÇILMAZ: Modrinth bir şey bulsa bile sunucu onu
// yüklemeyecek. Sessizce hiçbir şey yapmamak da yanlış — kullanıcı "/"a basıp
// tepki görmezse tuşun bozuk olduğunu sanır.
func (a *App) explainNoContent(s *model.Server) {
	a.Emit(fbui.EventWarn, string(s.Software)+" eklenti/mod yükleyemez")
	a.OpenModal(NewInfoModal("Eklenti kurulamaz", noContentLines(s)))
}

func noContentLines(s *model.Server) []string {
	return []string{
		softwareLabel(s.Software) + " sunucusu eklenti ya da mod yüklemez.",
		"",
		"Eklenti için Paper veya Purpur,",
		"mod için Fabric, Quilt, Forge veya NeoForge",
		"yazılımıyla yeni bir sunucu oluşturun (n).",
	}
}

func (a *App) detailOpenSearch(d *ServerDetail, s *model.Server) {
	if !canInstallContent(s) {
		a.explainNoContent(s)
		return
	}
	d.mu.Lock()
	prev, anyV := d.search.query, d.search.anyVersion
	d.mu.Unlock()
	hint := "Minecraft " + s.MCVersion + " ile uyumlu sonuçlar"
	if anyV {
		hint = "Sürüm süzgeci kapalı (v ile açın)"
	}
	a.OpenModal(NewTextModal("Modrinth'te ara", contentKindTitle(s)+" adı",
		func(app *App, text string) {
			app.detailSearch(d, s, text, anyV)
		}).WithValue(prev).WithPlaceholder("worldedit").WithOK("Ara").WithMaxLen(64).WithHint(hint))
}

func contentKindTitle(s *model.Server) string {
	switch contentKind(s) {
	case "eklenti":
		return "Eklenti"
	case "mod":
		return "Mod"
	}
	return "İçerik"
}

// detailSearch runs a Modrinth search in the background.
func (a *App) detailSearch(d *ServerDetail, s *model.Server, q string, anyVersion bool) {
	q = strings.TrimSpace(q)
	if q == "" {
		return
	}
	// İKİNCİ koruma: arama anahtarı bir gün başka bir yoldan (fare, yeni bir
	// kısayol) çağrılsa bile Vanilla'da ağa çıkılmaz.
	if !canInstallContent(s) {
		a.explainNoContent(s)
		return
	}
	d.mu.Lock()
	d.search = detailSearch{query: q, anyVersion: anyVersion, busy: true}
	d.mu.Unlock()
	d.swView = swResults
	d.cursor[dtSoftware] = 0
	a.Invalidate()
	if a.offline() {
		d.mu.Lock()
		d.search.busy = false
		d.search.err = "mcosd bağlantısı yok"
		d.mu.Unlock()
		return
	}
	a.Emit(fbui.EventBusy, "Modrinth'te aranıyor: "+q+"…")
	id := s.ID
	go func() {
		res, err := a.cl.CatalogSearchWith(ipc.CatalogSearchParams{ServerID: id, Query: q, AnyVersion: anyVersion})
		d.mu.Lock()
		// Kullanıcı bu arada yeni bir arama başlattıysa eski yanıt yazılmaz.
		stale := d.search.query != q || d.search.anyVersion != anyVersion
		if !stale {
			d.search.busy = false
			d.search.done = true
			d.search.res = res
			if err != nil {
				d.search.err = err.Error()
			}
		}
		d.mu.Unlock()
		if stale {
			return
		}
		if err != nil {
			a.Fail("Modrinth araması", err)
			return
		}
		a.Emit(fbui.EventOK, searchHeader(res, len(res.Items)))
	}()
}

// searchHeader is the filter line above the results: "paper · 1.21.1 · 21 sonuç".
//
// Yalnızca sunucunun KENDİ yükleyicisi yazılır: purpur sunucusunda zincir
// purpur/paper/spigot/bukkit'tir ama başlıkta dört ad okunmaz. Hangi sonucun
// geri düşüşle bulunduğu satırın kendisinde yazar ("paper uyumlu").
func searchHeader(res ipc.CatalogSearchResult, shown int) string {
	var parts []string
	if len(res.Loaders) > 0 {
		parts = append(parts, res.Loaders[0])
	}
	if res.GameVersion != "" {
		parts = append(parts, res.GameVersion)
	} else {
		parts = append(parts, "tüm sürümler")
	}
	n := res.Total
	if n < shown {
		n = shown
	}
	parts = append(parts, fmt.Sprintf("%d sonuç", n))
	return strings.Join(parts, " · ")
}

func (a *App) detailInstall(d *ServerDetail, s *model.Server, it ipc.CatalogItem) {
	if !canInstallContent(s) {
		a.explainNoContent(s)
		return
	}
	if a.offline() {
		a.Emit(fbui.EventWarn, "mcosd bağlantısı yok")
		return
	}
	d.mu.Lock()
	anyV := d.search.anyVersion
	d.mu.Unlock()
	title := it.Title
	if title == "" {
		title = it.Slug
	}
	a.Emit(fbui.EventBusy, title+" kuruluyor…")
	id := s.ID
	go func() {
		res, err := a.cl.CatalogInstallWith(ipc.CatalogInstallParams{ServerID: id, Slug: it.Slug, AnyVersion: anyV})
		if err != nil {
			a.Fail(title+" kurulamadı", err)
			return
		}
		sum := res.Summary
		if sum == "" {
			// Eski daemon: yalnızca dosya adını döner.
			sum = it.Slug + " kuruldu"
		}
		a.detailAfterInstall(d, id, sum)
	}()
}

// detailAfterInstall reports an install and offers the restart it needs.
//
// Kullanıcının isteği: "eklenti kurulduktan sonra yüklenmesi için sunucu
// yeniden başlatılsın mı?" Bukkit/Fabric eklentileri yalnızca AÇILIŞTA
// yüklenir; sormazsak kullanıcı eklentiyi kurup oyunda bulamaz ve kurulumun
// başarısız olduğunu sanır. Kapalı sunucuya sormanın anlamı yok: bir sonraki
// açılışta zaten yüklenecek.
func (a *App) detailAfterInstall(d *ServerDetail, serverID, summary string) {
	a.Emit(fbui.EventOK, summary)
	if a.detailState() == d {
		a.detailLoadInstalled(d)
	}
	var s *model.Server
	_, servers, _ := a.Snapshot()
	for _, sv := range servers {
		if sv.ID == serverID {
			s = sv
		}
	}
	if s == nil {
		return
	}
	if s.State != model.StateRunning && s.State != model.StateStarting {
		a.Emit(fbui.EventInfo, s.Name+" başlatıldığında yüklenecek")
		return
	}
	a.OpenModal(restartConfirm(s, summary))
}

// restartConfirm is the "restart now?" dialog shown after an install.
func restartConfirm(s *model.Server, summary string) *ConfirmModal {
	m := NewConfirmModal("Yeniden başlatılsın mı?",
		[]string{
			summary + ".",
			"Eklentinin yüklenmesi için sunucu yeniden başlatılsın mı?",
			"Bağlı oyuncular kısa süre bağlantıyı kaybeder.",
		},
		"Yeniden başlat", false,
		func(app *App) { app.serverActionOn(s, "restart") })
	return m
}

func (a *App) detailInstalledMenu(d *ServerDetail, s *model.Server, f model.FileEntry) {
	a.OpenModal(NewListModal(f.Name, bytesShort(uint64(f.Size)), []ListItem{
		{Label: "Kaldır", Detail: "d", Value: "remove"},
	}, func(app *App, _ int, _ ListItem) bool {
		app.detailConfirmRemoveFile(d, s, f)
		return false // onay penceresi açıldı; o kapanınca biter
	}))
}

func (a *App) detailConfirmRemove(d *ServerDetail, s *model.Server) {
	// Satır ÇİZİMLE aynı listeden bulunur: listenin başında "Modrinth'te
	// ara…" eylem satırı var ve d.installed'a doğrudan imleçle bakmak bir
	// kaydırmayla YANLIŞ dosyayı silerdi.
	v := a.detailViewFor(d, s)
	c := d.cursor[dtSoftware]
	if c < 0 || c >= len(v.items) || v.items[c].act != "installed" {
		return
	}
	a.detailConfirmRemoveFile(d, s, v.items[c].value.(model.FileEntry))
}

func (a *App) detailConfirmRemoveFile(d *ServerDetail, s *model.Server, f model.FileEntry) {
	dir := contentDir(s)
	if dir == "" {
		return
	}
	id := s.ID
	a.OpenModal(NewConfirmModal(f.Name+" kaldırılsın mı?",
		[]string{
			"Dosya " + dir + "/ klasöründen silinir.",
			"Çalışan sunucuda etkisi yeniden başlatınca görülür.",
		}, "Kaldır", true,
		func(app *App) {
			if app.offline() {
				return
			}
			go func() {
				if err := app.cl.FilesDelete(id, dir+"/"+f.Name); err != nil {
					app.Fail(f.Name+" kaldırılamadı", err)
					return
				}
				app.Emit(fbui.EventOK, f.Name+" kaldırıldı — yeniden başlatınca etkin olur")
				app.detailLoadInstalled(d)
			}()
		}))
}

// detailUSB installs a .jar from a USB stick into this server.
func (a *App) detailUSB(d *ServerDetail, s *model.Server) {
	if !canInstallContent(s) {
		a.explainNoContent(s)
		return
	}
	if a.offline() {
		a.Emit(fbui.EventWarn, "mcosd bağlantısı yok")
		return
	}
	a.Emit(fbui.EventBusy, "USB bellek taranıyor…")
	id, name := s.ID, s.Name
	go func() {
		jars, err := a.cl.ScanUSBMods()
		if err != nil {
			a.Fail("USB taranamadı", err)
			return
		}
		items := make([]ListItem, 0, len(jars))
		for _, j := range jars {
			items = append(items, ListItem{Label: j.Name, Detail: bytesShort(uint64(j.SizeBytes)), Value: j})
		}
		a.OpenModal(NewListModal("USB'den kur", name+" sunucusuna kurulacak dosyayı seçin.", items,
			func(app *App, _ int, it ListItem) bool {
				jar := it.Value.(model.USBJar)
				app.Emit(fbui.EventBusy, jar.Name+" kuruluyor…")
				go func() {
					msg, err := app.cl.InstallUSBMods(id, []model.USBJar{jar})
					if err != nil {
						app.Fail(jar.Name+" kurulamadı", err)
						return
					}
					app.detailAfterInstall(d, id, msg)
				}()
				return true
			}).WithEmpty("USB bellekte .jar dosyası yok."))
	}()
}

// ── Yedekler ────────────────────────────────────────────────────────────────

func (a *App) detailBackupCreate(d *ServerDetail, s *model.Server) {
	if a.offline() {
		a.Emit(fbui.EventWarn, "mcosd bağlantısı yok")
		return
	}
	a.Emit(fbui.EventBusy, s.Name+" yedekleniyor…")
	id := s.ID
	go func() {
		if err := a.cl.BackupCreate(ipc.BackupCreateParams{ServerID: id}); err != nil {
			a.Fail("yedek alınamadı", err)
			return
		}
		a.Emit(fbui.EventOK, "Yedek alındı")
		a.detailLoadBackups(d)
	}()
}

func (a *App) detailBackupMenu(d *ServerDetail, s *model.Server, b model.Backup) {
	id := s.ID
	a.OpenModal(NewListModal(b.Name, b.CreatedAt.Format("2006-01-02 15:04")+" · "+bytesShort(uint64(b.SizeBytes)),
		[]ListItem{
			{Label: "Bu yedeğe geri dön", Value: "restore"},
			{Label: "Yedeği sil", Value: "delete"},
		}, func(app *App, _ int, it ListItem) bool {
			if it.Value.(string) == "restore" {
				app.OpenModal(NewConfirmModal("Geri yüklensin mi?",
					[]string{"Sunucu " + b.CreatedAt.Format("2006-01-02 15:04") + " anına döner.",
						"Bu andan sonraki değişiklikler kaybolur.", "Sunucu önce durdurulmalıdır."},
					"Geri yükle", true, func(app *App) {
						go func() {
							if err := app.cl.BackupRestore(id, b.ID); err != nil {
								app.Fail("geri yüklenemedi", err)
								return
							}
							app.Emit(fbui.EventOK, "Yedek geri yüklendi")
						}()
					}))
				return false
			}
			app.OpenModal(NewConfirmModal(b.Name+" silinsin mi?",
				[]string{"Yedek kalıcı olarak silinir."}, "Sil", true, func(app *App) {
					go func() {
						if err := app.cl.BackupDelete(id, b.ID); err != nil {
							app.Fail("yedek silinemedi", err)
							return
						}
						app.Emit(fbui.EventOK, "Yedek silindi")
						app.detailLoadBackups(d)
					}()
				}))
			return false
		}))
}

// ── Kısayollar ──────────────────────────────────────────────────────────────

// detailShortcuts are the status-bar hints for the detail's current tab.
//
// Beşten fazla kapak alt çubuğa sığmaz ve olay metninin üstüne biner; her
// sekme yalnızca en önemli tuşlarını gösterir.
func (a *App) detailShortcuts(d *ServerDetail) []fbui.Shortcut {
	tabs := fbui.Shortcut{Key: "←→", Label: "Sekme"}
	back := fbui.Shortcut{Key: "Esc", Label: "Liste"}
	switch d.tab {
	case dtConsole:
		return []fbui.Shortcut{{Key: "Enter", Label: "Komut"}, {Key: "↑↓", Label: "Kaydır"}, tabs, back}
	case dtSettings:
		return []fbui.Shortcut{{Key: "Enter", Label: "Değiştir"}, tabs, back}
	case dtPlayers:
		return []fbui.Shortcut{{Key: "Enter", Label: "İşlem"}, {Key: "o", Label: "Op"}, {Key: "K", Label: "At"}, tabs, back}
	case dtSoftware:
		if d.swView == swResults {
			return []fbui.Shortcut{{Key: "Enter", Label: "Kur"}, {Key: "v", Label: "Sürüm süzgeci"}, {Key: "/", Label: "Ara"}, {Key: "Esc", Label: "Kurulular"}}
		}
		return []fbui.Shortcut{{Key: "/", Label: "Modrinth'te ara"}, {Key: "u", Label: "USB"}, {Key: "p", Label: "Performans paketi"}, {Key: "d", Label: "Kaldır"}, tabs, back}
	case dtFiles:
		return []fbui.Shortcut{{Key: "Enter", Label: "Aç"}, {Key: "b", Label: "Üst klasör"}, tabs, back}
	case dtBackups:
		return []fbui.Shortcut{{Key: "c", Label: "Yedek al"}, {Key: "o", Label: "Otomatik"}, {Key: "Enter", Label: "İşlem"}, tabs, back}
	case dtWAN:
		return []fbui.Shortcut{{Key: "t", Label: "Aç/Kapat"}, tabs, back}
	case dtAccess:
		return []fbui.Shortcut{{Key: "Enter", Label: "İşlem"}, tabs, back}
	}
	return []fbui.Shortcut{
		{Key: "s", Label: "Başlat"}, {Key: "x", Label: "Durdur"}, {Key: "r", Label: "Yeniden başlat"},
		tabs, back,
	}
}
