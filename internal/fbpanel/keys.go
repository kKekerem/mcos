package fbpanel

import (
	"fmt"
	"strings"

	"mcos/internal/fbui"
	"mcos/internal/ipc"
	"mcos/internal/model"
)

// Focus is which column the cursor is in.
type Focus int

const (
	// FocusSidebar: ok tuşları bölümler arasında gezer.
	FocusSidebar Focus = iota
	// FocusContent: ok tuşları o bölümün satırları arasında gezer.
	FocusContent
)

// Action is what the caller must do after a keystroke.
//
// Tuş işleyicisi doğrudan yeniden başlatma veya kapatma YAPMAZ: bunlar geri
// alınamaz işlemlerdir ve ekranı devralmış bir programın onları yaparken
// konsolu geri vermesi gerekir. Karar burada, uygulaması cmd katmanındadır.
type Action int

const (
	// ActNone: yapılacak bir şey yok.
	ActNone Action = iota
	// ActQuit: panelden çık.
	ActQuit
	// ActSleep: ekranı uykuya al.
	ActSleep
	// ActReboot: sistemi yeniden başlat.
	ActReboot
	// ActPoweroff: sistemi kapat.
	ActPoweroff
	// ActLegacyPanel: eski Bubble Tea panelini aç (acil durum çıkışı).
	ActLegacyPanel
)

// Key handles one keystroke and returns the action the host must perform.
//
// Tuş atamaları ESKİ PANELLE AYNI (panel/app.go): kullanıcı kas hafızasını
// kaybetmemeli.
func (a *App) Key(key string) Action {
	// Kilit ekranı HER ŞEYDEN ÖNCE gelir: parola girilmeden hiçbir kısayol
	// çalışmamalı. (Aksi halde "q" ile panelden çıkıp kilidi atlamak
	// mümkün olurdu.)
	if a.Locked() {
		return a.lockKey(key)
	}

	// Açılır pencere varsa tuşlar ÖNCE ona gider: arkadaki ekranın
	// kısayolları pencere açıkken tetiklenmemeli.
	if m := a.ActiveModal(); m != nil {
		if m.Key(a, key) {
			a.CloseModal()
		}
		a.Invalidate()
		return ActNone
	}

	// Kurulum sihirbazı etkinse panelin kısayolları çalışmaz: sihirbazın
	// ortasında "g" ile güç menüsüne düşmek, yarım yapılandırılmış bir
	// sistem bırakırdı.
	if a.setupState() != nil {
		return a.setupKey(key)
	}
	// Sunucu oluşturma sihirbazı da aynı şekilde tuşları kendi alır:
	// ad yazarken "q" basmak panelden çıkmamalı.
	if a.wizardState() != nil {
		return a.wizardKey(key)
	}

	switch key {
	case "ctrl+c", "q":
		return ActQuit
	case "f12":
		return ActLegacyPanel

	case "g":
		a.gotoSection(SecPower)
		a.setFocus(FocusContent)
		return ActNone

	case "t":
		a.toggleTurbo()
		return ActNone

	case "tab", "shift+tab":
		a.toggleFocus()
		return ActNone

	case "up", "k":
		a.moveCursor(-1)
		return ActNone
	case "down", "j":
		a.moveCursor(1)
		return ActNone

	case "enter", "right", "l":
		return a.activate()

	case "esc", "left", "h":
		a.setFocus(FocusSidebar)
		return ActNone

	case "n", "N":
		// Yeni sunucu: artık YENİ arayüzde (internal/fbpanel/wizard.go).
		// Hangi bölümde olursak olalım çalışır — kullanıcı sunucu eklemek
		// için önce Sunucular bölümüne gitmek zorunda kalmamalı.
		a.StartWizard()
		return ActNone

	case "w":
		if a.Section() == SecNetwork {
			a.openWiFiPicker()
		}
		return ActNone
	case "e":
		if a.Section() == SecNetwork {
			a.connectWired()
		}
		return ActNone

	case "r", "R":
		a.refreshSection()
		return ActNone

	case "i", "I":
		// Elle IP girişi: taramanın bulamadığı cihazlar için.
		if a.Section() == SecPeers {
			a.openManualPair()
		}
		return ActNone

	case "s", "S":
		// Eşleştirme ekranında "s" TARAMA demektir; sunucu başlatmak
		// oraya ait değil ve yanlışlıkla sunucu açardı.
		if a.Section() == SecPeers {
			a.startPeerScan()
			return ActNone
		}
		a.serverAction("start")
		return ActNone
	case "x", "X":
		a.serverAction("stop")
		return ActNone
	}
	return ActNone
}

func (a *App) setFocus(f Focus) {
	a.mu.Lock()
	a.focus = f
	a.dirty = true
	a.mu.Unlock()
}

// Focus returns the current column focus.
func (a *App) Focus() Focus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.focus
}

func (a *App) toggleFocus() {
	a.mu.Lock()
	if a.focus == FocusSidebar {
		a.focus = FocusContent
	} else {
		a.focus = FocusSidebar
	}
	a.cursor = 0
	a.dirty = true
	a.mu.Unlock()
}

// moveCursor moves within the sidebar or the content list, wrapping around.
func (a *App) moveCursor(delta int) {
	if a.Focus() == FocusSidebar {
		vis := a.visibleSections()
		if len(vis) == 0 {
			return
		}
		cur := a.Section()
		idx := 0
		for i, s := range vis {
			if s == cur {
				idx = i
				break
			}
		}
		idx = (idx + delta + len(vis)) % len(vis)
		a.gotoSection(vis[idx])
		return
	}

	n := a.contentRows()
	if n <= 0 {
		return
	}
	a.mu.Lock()
	a.cursor = (a.cursor + delta + n) % n
	a.dirty = true
	a.mu.Unlock()
}

// contentRows is how many selectable rows the current section has.
//
// Bu sayı ÇİZİMLE aynı kaynaktan gelmeli; yoksa imleç listenin dışına çıkar.
// Eski panelde Wi-Fi listesi 8 satır çiziyordu ama imleç daha aşağı
// inebiliyordu — tam olarak bu hatayı önlemek için tek fonksiyon.
func (a *App) contentRows() int {
	st, servers, _ := a.Snapshot()
	switch a.Section() {
	case SecServers:
		return len(servers)
	case SecUSB:
		a.mu.Lock()
		defer a.mu.Unlock()
		return len(a.usbJars)
	case SecSoftware:
		return len(javaOffer)
	case SecPerformance:
		return 1 // turbo satırı
	case SecNetwork:
		if st == nil {
			return 0
		}
		return len(st.Net.NICs)
	case SecDisplay:
		return len(displayModes)
	case SecTunnel:
		return int(tunnelStepCount)
	case SecPeers:
		return len(a.peersRows())
	case SecPower:
		return len(powerItems)
	case SecSettings:
		return int(settingCount)
	}
	return 0
}

// activate performs the primary action for the focused row.
func (a *App) activate() Action {
	if a.Focus() == FocusSidebar {
		a.setFocus(FocusContent)
		a.loadSectionAsync()
		return ActNone
	}

	switch a.Section() {
	case SecPower:
		switch a.Cursor() {
		case 0:
			return ActSleep
		case 1:
			a.confirmPower("Yeniden Başlat", ActReboot)
		case 2:
			a.confirmPower("Kapat", ActPoweroff)
		}
	case SecSettings:
		a.activateSetting(a.Cursor())
	case SecDisplay:
		a.applyDisplayMode(a.Cursor())
	case SecSoftware:
		a.installJava(a.Cursor())
	case SecServers:
		a.toggleServer()
	case SecPerformance:
		a.toggleTurbo()
	case SecNetwork:
		a.openWiFiPicker()
	case SecUSB:
		a.installUSBJar(a.Cursor())
	case SecPeers:
		a.peersActivate(a.Cursor())
	case SecTunnel:
		a.tunnelActivate(a.Cursor())
	}
	return ActNone
}

// pendingPower remembers which power action a confirmation dialog approved.
//
// Modal geri çağrısı Action döndüremez (arayüz bool döner), bu yüzden karar
// burada saklanır ve ana döngü bir sonraki turda okur.
func (a *App) confirmPower(label string, act Action) {
	a.OpenModal(NewConfirmModal(
		label+"?",
		[]string{
			"Çalışan sunucular düzgünce durdurulacak.",
			"Bağlı oyuncular bağlantıyı kaybedecek.",
		},
		label, true,
		func(app *App) {
			app.mu.Lock()
			app.pending = act
			app.mu.Unlock()
		}))
}

// TakePending returns and clears a queued power action.
func (a *App) TakePending() Action {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.pending
	a.pending = ActNone
	return p
}

// offline reports whether there is no daemon connection.
//
// Ekran goruntusu kipinde istemci nil olabilir; ayrica daemon dusmusse de
// cagri yapmamak gerekir. Her RPC kullanan yerin basinda kontrol edilir -
// nil isaretciyle cagri yapmak paniğe yol acar (olculdu: DemoView cokmesi).
func (a *App) offline() bool { return a.cl == nil }

// ── Bölüm verisi ────────────────────────────────────────────────────────────

// loadSection fetches the data a section needs, once, on entry.
//
// Her saniye HER bölümün verisini çekmek israf olurdu: bir Minecraft
// sunucusunun üstünde çalışıyoruz. Veri yalnızca o bölüme girilince ve
// r tuşuna basılınca yenilenir.
func (a *App) loadSection() {
	if a.offline() {
		return
	}
	switch a.Section() {
	case SecSoftware:
		if rt, err := a.cl.JavaList(); err == nil {
			a.mu.Lock()
			a.javaRuntimes = rt
			a.dirty = true
			a.mu.Unlock()
		}
	case SecPeers:
		a.loadPeersSection()
	case SecTunnel:
		a.loadTunnelSection()
	}
}

// refreshSection re-fetches the current section's data (the r key).
func (a *App) refreshSection() {
	if a.offline() {
		return
	}
	switch a.Section() {
	case SecUSB:
		if a.scanning() {
			return
		}
		a.setScanning(true)
		a.setScanNote("USB bellek taranıyor…")
		a.Emit(fbui.EventBusy, "USB bellek taranıyor…")
		// ARKA PLANDA: tarama saniyeler sürebilir ve ana döngüde yapmak
		// radar animasyonunu dondururdu — yani "bekleniyor" göstergesi
		// tam da beklerken donardı.
		go func() {
			jars, err := a.cl.ScanUSBMods()
			a.setScanning(false)
			a.setScanNote("")
			if err != nil {
				a.Fail("USB taranamadı", err)
				return
			}
			a.mu.Lock()
			a.usbJars = jars
			a.usbScanned = true
			a.cursor = 0
			a.dirty = true
			a.mu.Unlock()
			// Radar ekranından dosya listesine yumuşak geçiş.
			a.beginTransition(transFade)
			a.Emit(fbui.EventOK, fmt.Sprintf("USB: %d dosya bulundu", len(jars)))
		}()
	case SecNetwork, SecDevices:
		// ── Yakalanan gerçek hata ───────────────────────────────────
		// Bu dal Status()'ü ANA DÖNGÜDE çağırıyordu. ipc.Client tüm
		// çağrıları tek bir kilitle sıraya dizer; bir Wi-Fi ya da ağ
		// taraması sürerken (25 saniyeye kadar) o kilit tutuludur.
		//
		// Sonuç: kullanıcı tarama sürerken "r"ye bastığında çalışma
		// döngüsü Key() içinde kilitleniyordu — çizim de, giriş de
		// durur, dönen tarama göstergesi tam da beklerken DONARDI.
		// Yani "meşgulüm" göstergesi meşgulken çalışmıyordu.
		//
		// Hemen üstteki SecUSB dalı bunu baştan doğru yapıyordu; bu
		// iki dal ona çevrilmeden kalmış.
		if !a.refreshing.CompareAndSwap(false, true) {
			return
		}
		a.Emit(fbui.EventBusy, "donanım yeniden taranıyor…")
		go func() {
			defer a.refreshing.Store(false)
			st, err := a.cl.Status()
			if err != nil {
				a.Fail("durum alınamadı", err)
				return
			}
			a.SetStatus(st)
			a.Emit(fbui.EventOK, "yenilendi")
		}()
	default:
		// loadSection da RPC yapar; aynı nedenle ana döngüde olamaz.
		a.loadSectionAsync()
		a.Emit(fbui.EventInfo, "yenilendi")
	}
}

// ── Ayarlar ─────────────────────────────────────────────────────────────────

// openThemePicker shows the theme list in a blurred dialog.
func (a *App) openThemePicker() {
	_, _, cfg := a.Snapshot()
	active := ""
	if cfg != nil {
		active = cfg.Theme
	}
	items := make([]ListItem, 0, len(fbui.ThemeOrder))
	for _, name := range fbui.ThemeOrder {
		items = append(items, ListItem{
			Label:   fbui.ThemeLabel(name),
			Detail:  name,
			Current: name == active,
			Value:   name,
		})
	}
	a.OpenModal(NewListModal("Tema", "Arayüz vurgu rengini seçin.", items,
		func(app *App, _ int, it ListItem) bool {
			app.applyTheme(it.Value.(string))
			return true
		}))
}

// applyTheme switches the accent colour and persists it.
func (a *App) applyTheme(name string) {
	if !fbui.ValidTheme(name) {
		return
	}
	a.mu.Lock()
	a.ui.Pal = fbui.DefaultPalette.WithAccent(name)
	cfg := a.cfg
	a.dirty = true
	a.mu.Unlock()

	a.Emit(fbui.EventOK, "Tema: "+fbui.ThemeLabel(name))
	if cfg == nil || a.offline() {
		return
	}
	// Yapılandırmanın KOPYASI üzerinde çalış: kaydetme başarısız olursa
	// bellekteki durum diskle uyuşmaz kalırdı.
	next := *cfg
	next.Theme = name
	a.saveConfigAsync(&next, "tema kaydedilemedi", func() {
		a.mu.Lock()
		a.cfg = &next
		a.mu.Unlock()
	})
}

// ── Ekran ───────────────────────────────────────────────────────────────────

// SetScreenSize records the real framebuffer resolution.
func (a *App) SetScreenSize(w, h int) {
	a.mu.Lock()
	a.screenW, a.screenH = w, h
	a.dirty = true
	a.mu.Unlock()
}

// SetDisplayPref records the saved boot resolution preference.
func (a *App) SetDisplayPref(mode string) {
	a.mu.Lock()
	a.displayPref = mode
	a.dirty = true
	a.mu.Unlock()
}

// applyDisplayMode saves a boot resolution preference.
//
// Çözünürlük ÇALIŞIRKEN değiştirilemez: bu çekirdekte gerçek ekran kartı
// sürücüsü yok, framebuffer'ı firmware kuruyor. Bu yüzden seçim kaydedilir
// ve önyükleyici yapılandırması güncellenir; yeniden başlatınca etkin olur.
func (a *App) applyDisplayMode(idx int) {
	if idx < 0 || idx >= len(displayModes) {
		return
	}
	mode := displayModes[idx]
	a.OpenModal(NewConfirmModal(
		"Çözünürlüğü değiştir",
		[]string{
			"Yeni çözünürlük: " + strings.Replace(mode, "x", " x ", 1),
			"Bu değişiklik yeniden başlatmadan sonra etkin olur.",
			"Seçilen mod bu ekranda yoksa sistem otomatik olarak",
			"desteklenen bir moda döner.",
		},
		"Kaydet", false,
		func(app *App) { app.saveDisplayMode(mode) }))
}

// saveDisplayMode writes the preference and updates the bootloader config.
func (a *App) saveDisplayMode(mode string) {
	a.mu.Lock()
	a.displayPref = mode
	a.dirty = true
	a.mu.Unlock()

	// mcos-display hedef sistemde boot bölümünü BAĞLAR ve grub.cfg içindeki
	// "set gfxmode=" satırını yeniden yazar. Panel bunu doğrudan yapmaz:
	// bölüm bağlamak ve önyükleyici dosyası yazmak ayrı bir programın işi.
	//
	// Bağlama saniyeler sürebilir; ana döngüde beklemek paneli dondururdu.
	a.Emit(fbui.EventBusy, "Çözünürlük kaydediliyor…")
	go func() {
		out, err := runHelper("mcos-display", "set", mode)
		if err != nil {
			a.Fail("çözünürlük kaydedilemedi", err)
			return
		}
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = "Çözünürlük kaydedildi: " + mode
		}
		a.Emit(fbui.EventOK, msg+" — yeniden başlatınca etkin olacak")
	}()
}

// ── Yazılım ─────────────────────────────────────────────────────────────────

func (a *App) installJava(idx int) {
	if a.offline() {
		return
	}
	if idx < 0 || idx >= len(javaOffer) {
		return
	}
	major := javaOffer[idx].major
	st, _, _ := a.Snapshot()
	if st != nil && !st.Net.Internet {
		a.Emit(fbui.EventError, "İnternet yok — Java indirilemez")
		return
	}
	a.Emit(fbui.EventBusy, fmt.Sprintf("Java %d indiriliyor…", major))
	go func() {
		rt, err := a.cl.JavaInstall(major)
		if err != nil {
			a.Fail(fmt.Sprintf("Java %d kurulamadı", major), err)
			return
		}
		a.Emit(fbui.EventOK, fmt.Sprintf("Java %d kuruldu (%s)", rt.Major, rt.Version))
		a.loadSection()
	}()
}

// ── Ağ ──────────────────────────────────────────────────────────────────────

// openWiFiPicker scans and shows the network list in a blurred dialog.
func (a *App) openWiFiPicker() {
	if a.offline() {
		return
	}
	if a.scanning() {
		return // zaten sürüyor; ikinci tarama kartı boşuna yorar
	}
	a.mu.Lock()
	a.wifiNote = "taranıyor…"
	a.wifiListWanted = true
	a.dirty = true
	a.mu.Unlock()
	// Radar animasyonu: eş taramasıyla AYNI gösterge.
	a.setScanning(true)
	a.setScanNote("Kablosuz ağlar aranıyor…")
	a.Emit(fbui.EventBusy, "Kablosuz ağlar taranıyor…")

	go func() {
		nets, err := a.cl.WiFiScan()
		a.setScanning(false)
		a.setScanNote("")
		a.mu.Lock()
		a.wifiNote = ""
		a.mu.Unlock()
		if err != nil {
			a.Fail("tarama yapılamadı", err)
			return
		}
		items := make([]ListItem, 0, len(nets))
		for _, n := range nets {
			badge, kind := "açık", fbui.EventInfo
			if n.Secured {
				badge, kind = "korumalı", fbui.EventWarn
			}
			items = append(items, ListItem{
				Label:     n.SSID,
				Detail:    fmt.Sprintf("%%%d", n.Signal),
				Badge:     badge,
				BadgeKind: kind,
				Value:     n,
			})
		}
		// ── Yakalanan gerçek hata ───────────────────────────────────
		// Tarama saniyeler sürüyor ve kullanıcı o sırada başka bir şey
		// açabiliyor (tema listesi, parola kutusu, onay penceresi). Burası
		// eskiden KOŞULSUZ OpenModal çağırıyordu: kullanıcının açtığı
		// pencere, ağ listesi tarafından habersizce yok ediliyordu.
		//
		// Artık taramayı BAŞLATAN niyet kaydediliyor; o sırada başka bir
		// pencere açıldıysa liste sessizce atlanır (kullanıcı "w" ile
		// yeniden açabilir).
		if !a.takeWiFiPickerWant() {
			a.Emit(fbui.EventInfo, fmt.Sprintf("%d ağ bulundu (w ile listele)",
				len(nets)))
			return
		}
		a.OpenModal(NewListModal("Kablosuz Ağ Seç",
			fmt.Sprintf("%d ağ bulundu.", len(nets)), items,
			func(app *App, _ int, it ListItem) bool {
				net := it.Value.(ipc.WiFiNetwork)
				app.connectWiFi(net.SSID, net.Secured)
				return true
			}).WithEmpty("Ağ bulunamadı. Kablolu bağlantı için e tuşu."))
		a.Emit(fbui.EventOK, fmt.Sprintf("%d ağ bulundu", len(nets)))
	}()
}

// connectWiFi applies a network. Secured networks need a password first.
func (a *App) connectWiFi(ssid string, secured bool) {
	if secured {
		a.OpenModal(NewPasswordModal(ssid, func(app *App, pass string) {
			app.doWiFiApply(ssid, pass)
		}))
		return
	}
	a.doWiFiApply(ssid, "")
}

func (a *App) doWiFiApply(ssid, pass string) {
	if a.offline() {
		return
	}
	a.Emit(fbui.EventBusy, ssid+" ağına bağlanılıyor…")
	go func() {
		if err := a.cl.WiFiApply(ssid, pass); err != nil {
			a.Fail(ssid+" bağlantısı başarısız", err)
			return
		}
		a.Emit(fbui.EventOK, ssid+" ağına bağlanıldı")
	}()
}

func (a *App) connectWired() {
	if a.offline() {
		return
	}
	a.Emit(fbui.EventBusy, "Kablolu bağlantı kuruluyor…")
	go func() {
		if err := a.cl.WiredUp(); err != nil {
			a.Fail("kablolu bağlantı başarısız", err)
			return
		}
		a.Emit(fbui.EventOK, "Kablolu bağlantı kuruldu")
	}()
}

// ── USB ─────────────────────────────────────────────────────────────────────

func (a *App) installUSBJar(idx int) {
	if a.offline() {
		return
	}
	a.mu.Lock()
	jars := a.usbJars
	a.mu.Unlock()
	if idx < 0 || idx >= len(jars) {
		return
	}
	_, servers, _ := a.Snapshot()
	if len(servers) == 0 {
		a.Emit(fbui.EventWarn, "Önce bir sunucu oluşturun")
		return
	}
	jar := jars[idx]

	items := make([]ListItem, 0, len(servers))
	for _, s := range servers {
		items = append(items, ListItem{
			Label:  s.Name,
			Detail: string(s.Software) + " " + s.MCVersion,
			Value:  s.ID,
		})
	}
	a.OpenModal(NewListModal("Hangi sunucuya?",
		jar.Name+" kurulacak sunucuyu seçin.", items,
		func(app *App, _ int, it ListItem) bool {
			id := it.Value.(string)
			app.Emit(fbui.EventBusy, jar.Name+" kuruluyor…")
			go func() {
				msg, err := app.cl.InstallUSBMods(id, []model.USBJar{jar})
				if err != nil {
					app.Fail(jar.Name+" kurulamadı", err)
					return
				}
				app.Emit(fbui.EventOK, msg)
			}()
			return true
		}))
}

// ── Eşler ───────────────────────────────────────────────────────────────────

// pairPeer toggles pairing for the device at index idx in a.peers.
//
// idx, EKRAN satırı değil CİHAZ dizinidir: eşleştirme ekranında satırların
// başında eylemler (tara, IP gir) var ve ikisini karıştırmak yanlış cihazı
// eşleştirirdi. Dönüşümü peersActivate yapar.
func (a *App) pairPeer(idx int) {
	if a.offline() {
		return
	}
	a.mu.Lock()
	peers := a.peers
	a.mu.Unlock()
	if idx < 0 || idx >= len(peers) {
		return
	}
	p := peers[idx]
	verb := "Eşleştir"
	if p.Paired {
		verb = "Eşleşmeyi kaldır"
	}
	a.OpenModal(NewConfirmModal(verb,
		[]string{p.Name + " (" + p.IP + ")",
			fmt.Sprintf("%d çekirdek · %d MB bellek", p.Cores, p.RAMMB)},
		verb, false,
		func(app *App) {
			// ARKA PLANDA: modal geri çağrıları ANA DÖNGÜDE çalışır.
			// Burada doğrudan RPC yapmak, bir tarama sürerken paneli
			// dondururdu. Bu dosyadaki diğer modal geri çağrıları
			// (ör. screen_peers.go) baştan böyle yazılmıştı.
			go func() {
				if err := app.cl.ClusterPair(p.ID, p.Paired); err != nil {
					app.Fail(verb+" başarısız", err)
					return
				}
				app.Emit(fbui.EventOK, p.Name+" — "+verb)
				app.loadSection()
			}()
		}))
}

// ── Turbo / sunucu eylemleri ────────────────────────────────────────────────

// toggleTurbo flips turbo mode via the daemon.
func (a *App) toggleTurbo() {
	if a.offline() {
		return
	}
	_, _, cfg := a.Snapshot()
	want := true
	if cfg != nil {
		want = !cfg.Turbo
	}
	a.Emit(fbui.EventBusy, "Turbo uygulanıyor…")
	go func() {
		on, err := a.cl.Turbo(want)
		if err != nil {
			a.Fail("turbo değiştirilemedi", err)
			return
		}
		a.mu.Lock()
		if a.cfg != nil {
			a.cfg.Turbo = on
		}
		a.dirty = true
		a.mu.Unlock()
		if on {
			a.Emit(fbui.EventWarn, "Turbo açıldı — kaynak sınırları yok sayılıyor")
		} else {
			a.Emit(fbui.EventInfo, "Turbo kapatıldı")
		}
	}()
}

// selectedServer returns the server under the content cursor, or nil.
func (a *App) selectedServer() *model.Server {
	if a.Section() != SecServers || a.Focus() != FocusContent {
		return nil
	}
	_, servers, _ := a.Snapshot()
	c := a.Cursor()
	if c < 0 || c >= len(servers) {
		return nil
	}
	return servers[c]
}

// toggleServer starts a stopped server or stops a running one.
func (a *App) toggleServer() {
	s := a.selectedServer()
	if s == nil {
		return
	}
	if s.State == model.StateRunning || s.State == model.StateStarting {
		a.serverAction("stop")
	} else {
		a.serverAction("start")
	}
}

// serverAction issues start/stop/restart for the selected server.
//
// Her eylem ANINDA bir olay üretir: RPC dönene kadar arayüz sessiz kalmasın.
// Gerçek durum değişimi yoklama ile gelir ve ikinci bir olay yazar.
func (a *App) serverAction(what string) {
	if a.offline() {
		return
	}
	s := a.selectedServer()
	if s == nil {
		return
	}
	// ARKA PLANDA ÇALIŞTIRILIR.
	//
	// ── Neden şart ───────────────────────────────────────────────────────
	// "start", sunucu yazılımı kurulu değilse onu İNDİRİR (54 MB'lık bir
	// jar dakikalar sürebilir). Ana döngüde beklemek, tam da "başlatılıyor"
	// göstergesinin dönmesi gereken anda paneli DONDURUR.
	name := s.Name
	id := s.ID
	switch what {
	case "start":
		a.Emit(fbui.EventBusy, name+" başlatılıyor…")
		go func() {
			if err := a.cl.Start(id); err != nil {
				a.Fail(name, err)
			}
		}()
	case "stop":
		a.Emit(fbui.EventBusy, name+" durduruluyor…")
		go func() {
			if err := a.cl.Stop(id); err != nil {
				a.Fail(name, err)
			}
		}()
	case "restart":
		a.Emit(fbui.EventBusy, name+" yeniden başlatılıyor…")
		go func() {
			if err := a.cl.Restart(id); err != nil {
				a.Fail(name, err)
			}
		}()
	}
}

// takeWiFiPickerWant reports whether the Wi-Fi list should still be shown.
//
// TEK SEFERLİK: niyet okunduğunda sıfırlanır, böylece geciken ikinci bir
// tarama yanıtı pencereyi yeniden açmaz.
//
// Kullanıcı tarama sürerken başka bir pencere açtıysa niyet iptal edilir:
// altındaki pencereyi habersizce değiştirmek, kullanıcının az önce yaptığı
// seçimi kaybettirir.
func (a *App) takeWiFiPickerWant() bool {
	a.mu.Lock()
	want := a.wifiListWanted
	a.wifiListWanted = false
	other := a.modal != nil
	a.mu.Unlock()
	return want && !other
}
