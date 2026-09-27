package fbpanel

import (
	"fmt"
	"strings"
	"time"

	"mcos/internal/fbui"
	"mcos/internal/ipc"
	"mcos/internal/model"
	"mcos/internal/sound"
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

// ── SES SEVİYESİ ────────────────────────────────────────────────────────────
//
// Kullanıcı: "ses arttırma f3 kısma f4 olsun".
//
//	F3  / volumeup    sesi aç
//	F4  / volumedown  sesi kıs
//	F9  / mute        sessiz aç/kapa
//
// "volumeup/volumedown/mute", dizüstülerin MEDYA tuşlarıdır (Fn+F3 vb.).
// Bunlar TTY'ye hiçbir karakter göndermez; fbinput onları evdev'den okuyup
// bu adlarla panele iletir. Çoğu dizüstüde Fn kilidi kapalıyken F3'e basmak
// zaten KEY_VOLUMEUP üretir — yalnızca "f3"e bakmak o makinelerde hiçbir şey
// yapmazdı.
//
// F12 ESKİ PANELE ayrılmış (uzun süredir öyle ve belgeli), dokunulmadı.
func (a *App) volumeKey(key string) bool {
	switch key {
	case "f3", "volumeup":
		v := sound.VolumeUp()
		a.showVolume(v, false)
		// Yeni seviyeyi DUYURUYORUZ: sayıyı görmek yetmez, kullanıcı sesin
		// ne kadar yükseldiğini kulakla ölçer.
		a.playSound(sound.Nav)
		return true
	case "f4", "volumedown":
		v := sound.VolumeDown()
		a.showVolume(v, v == 0)
		if v > 0 {
			a.playSound(sound.Nav)
		}
		return true
	case "f9", "mute":
		v, sessiz := sound.ToggleMute()
		if sessiz {
			a.Emit(fbui.EventInfo, "Ses kapatıldı")
		} else {
			a.Emit(fbui.EventOK, fmt.Sprintf("Ses açıldı — %%%d", v))
		}
		a.showVolume(v, sessiz)
		return true
	}
	return false
}

// Key handles one keystroke and returns the action the host must perform.
//
// Tuş atamaları ESKİ PANELLE AYNI (panel/app.go): kullanıcı kas hafızasını
// kaybetmemeli.
func (a *App) Key(key string) Action {
	// Ses tuşları HER ŞEYDEN ÖNCE: kilit ekranı, açık pencere, kurulum
	// sihirbazı — hiçbiri sesi ayarlamayı engellememeli.
	//
	// ── Yakalanan gerçek hata ─────────────────────────────────────────────
	// Ses tuşları eskiden aşağıdaki switch'in İÇİNDEYDİ; kurulum sihirbazı,
	// sunucu sihirbazı ve her açık pencere tuşu ondan ÖNCE yutuyordu. Yorum
	// "tuşlar HER EKRANDA çalışıyor" diyordu ama ilk açılışta — kullanıcının
	// sesi en çok ayarlamak istediği anda — hiçbiri çalışmıyordu.
	//
	// Kilit ekranından önce olması güvenli: F tuşları ve medya tuşları bir
	// parolanın parçası olamaz, yani kilidi atlatmanın bir yolu açılmıyor.
	if a.volumeKey(key) {
		return ActNone
	}

	// Kilit ekranı kısayollardan önce gelir: parola girilmeden hiçbir
	// kısayol çalışmamalı. (Aksi halde "q" ile panelden çıkıp kilidi atlamak
	// mümkün olurdu.)
	if a.Locked() {
		return a.lockKey(key)
	}

	// Açılır pencere varsa tuşlar ÖNCE ona gider: arkadaki ekranın
	// kısayolları pencere açıkken tetiklenmemeli.
	if m := a.ActiveModal(); m != nil {
		if m.Key(a, key) {
			// Geri çağrı YENİ bir pencere açmış olabilir; o zaman onu
			// kapatmıyoruz (bkz. closeModalIf).
			a.closeModalIf(m)
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
	// Sunucu detayı açıksa tuşlar ONA gider: detayda "r" yeniden başlatmak,
	// "s" başlatmak demek; listenin "r = yenile" anlamı orada yanlış olurdu.
	// Kullanıcı fareyle başka bir bölüme geçtiyse detay artık görünmüyor;
	// görünmeyen bir ekranın tuşları yutması en kötü hata olurdu, kapatılır.
	if d := a.detailState(); d != nil {
		if a.Section() == SecServers {
			return a.detailKey(d, key)
		}
		a.dropServerDetail()
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

	case "d", "D":
		// Turbo tanısı: "neden 4,4 değil 2,4 GHz" sorusunun ölçülmüş
		// cevabı (bkz. screen_turbo.go, turboDiagModal).
		if a.Section() == SecPerformance {
			a.openTurboDiag()
		}
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

	case "u", "U":
		// USB'den sunucu aktar (dünya, modlar, eklentiler, ayarlar).
		a.importUSBServer()
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
	// Fare tekerleği de buraya gelir. Detay açıkken LİSTE imleci
	// kıpırdamamalı: detayın kendi imleci var ve Esc listeye dönerken eski
	// satırı geri koyar.
	if d := a.detailState(); d != nil && a.Section() == SecServers {
		a.detailMove(d, delta)
		return
	}
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
		return a.displayRowCount()
	case SecTunnel:
		return a.tunnelRowCount() // adımlar + sunucu satırları (screen_tunnel.go)
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
		// Ekran kartı modu çalışırken değiştirebiliyorsa ANINDA uygula;
		// değiştiremiyorsa (yalnızca firmware framebuffer'ı) eski yol:
		// GRUB'a yaz, yeniden başlatınca etkin olsun.
		if _, ok := a.liveSelectable(); ok {
			a.applyDisplayRow(a.Cursor())
		} else {
			a.applyDisplayMode(a.Cursor())
		}
	case SecSoftware:
		a.installJava(a.Cursor())
	case SecServers:
		// Kullanıcının isteği: "sunucuya enter e basınca o ekrana gitsin".
		// Enter eskiden sunucuyu başlatıp durduruyordu — yanlışlıkla
		// basılınca oyuncuları atan bir tuş. Başlat/durdur s/x'te kaldı.
		a.openServerDetail()
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
	// Gömülü sürüm için "indiriliyor…" göstermek yanlış olurdu: hiçbir şey
	// inmiyor, daemon aynı gömülü çalışma zamanını geri veriyor.
	a.mu.Lock()
	gomulu := false
	for _, rt := range a.javaRuntimes {
		if rt.Major == major && rt.Builtin {
			gomulu = true
		}
	}
	a.mu.Unlock()
	if gomulu {
		a.Emit(fbui.EventOK, fmt.Sprintf("Java %d sistemle gömülü geldi — kurulum gerekmez", major))
		return
	}
	// ── Düzeltilen gerçek hata: İNDİRME KAPIDA REDDEDİLİYORDU ───────────
	//
	// Burada şu vardı:
	//
	//	if st != nil && !st.Net.Internet {
	//	    a.Emit(fbui.EventError, "İnternet yok — Java indirilemez")
	//	    return
	//	}
	//
	// Yani indirme HİÇ DENENMİYORDU. Ve o bayrak yalnızca TCP PORT 53 ile
	// ölçülüyordu — DNS normalde UDP kullanır, TCP/53 ise pek çok ISS ve
	// kurumsal ağda ENGELLİDİR. Sonuç: HTTPS'in sorunsuz çalıştığı bir ağda
	// kullanıcı "İnternet yok — Java indirilemez" görüyordu.
	//
	// Kullanıcının "javanın hiçbir sürümü indirilmiyor, internete bağlı olsa
	// bile" şikâyeti tam olarak buydu.
	//
	// Artık DENENİYOR. Sınama (netprobe) da gerçek çıkışı ölçüyor ama tek
	// başına yetki sahibi değil: bir sınama yanılabilir, indirmenin kendisi
	// yanılamaz. Ağ gerçekten yoksa hata zaten gelir ve NEDENİ söylenir.
	// Arka plan İŞİ olarak: kullanıcı başka ekranlara gidebilir, durum
	// çubuğu süreyi gösterir, ikinci basış ikinci kurulum başlatmaz
	// (bkz. jobs.go — kullanıcının "aynı ekranda kalmak zorundayım" şikâyeti).
	a.runJob(fmt.Sprintf("java-%d", major), fmt.Sprintf("Java %d indirilip kuruluyor", major),
		func() (string, error) {
			rt, err := a.cl.JavaInstall(major)
			if err != nil {
				// Ağ sorunuysa NEDENİNİ ekle: "kurulamadı" tek başına
				// kullanıcıya ne yapacağını söylemiyor.
				mesaj := fmt.Sprintf("Java %d kurulamadı", major)
				if st, _, _ := a.Snapshot(); st != nil && st.Net.Reason != "" {
					mesaj += " — " + st.Net.Reason
				}
				return mesaj, err
			}
			return fmt.Sprintf("Java %d kuruldu (%s)", rt.Major, rt.Version), nil
		}, a.loadSection)
}

// ── Ağ ──────────────────────────────────────────────────────────────────────

// openWiFiPicker opens the live scan dialog and starts a scan behind it.
//
// ── Neden artık pencere ÖNCE açılıyor ───────────────────────────────────────
//
// Eski akış taramayı başlatıp SONUCU bekliyordu; pencere ancak tarama bitince
// (ölçülen 4-12 sn) açılıyordu. O süre boyunca ekranda yalnızca arka plandaki
// küçük radar vardı ve kullanıcı "bir şey olmuyor" diye tekrar tuşa basıyordu.
//
// Kullanıcının isteği: "kablosuz tara deyince üste bir menü gelecek ama
// animasyon o tarama animasyonu orada olacak ve canlı listelenecek bulduğunda."
//
// Yeni akış: pencere ANINDA açılır (boş ve "aranıyor" hâlinde), daemon'da
// tarama başlatılır (net.wifiScanStart — bloklamaz) ve pencere her 400 ms'de
// kısmi sonucu yoklar. Bulunan her ağ satır satır düşer.
//
// wifiListWanted/takeWiFiPickerWant artık GEREKMİYOR: eskiden tarama biterken
// kullanıcının açtığı başka bir pencerenin üzerine liste açılmasın diye niyet
// kaydediliyordu. Pencere baştan açık olduğu için böyle bir yarış yok — açık
// olan pencere zaten bu.
func (a *App) openWiFiPicker() {
	if a.offline() {
		return
	}

	m := NewScanModal("Kablosuz Ağ Seç", "Kablosuz ağlar aranıyor…",
		func(app *App, _ int, it ListItem) bool {
			net, ok := it.Value.(ipc.WiFiNetwork)
			if !ok {
				return false
			}
			app.connectWiFi(net.SSID, net.Secured)
			return true
		}).
		WithEmpty("Ağ bulunamadı. Kablolu bağlantı için e tuşu.").
		WithRescan(func(app *App, sm *ScanModal) { app.startWiFiScan(sm) }).
		WithCancel(func(app *App) {
			app.setScanning(false)
			app.setScanNote("")
		})

	a.OpenModal(m)
	a.startWiFiScan(m)
}

// wifiPollInterval, canlı taramanın yoklama aralığı.
//
// 400 ms: gözün "anında" saydığı üst sınıra yakın, ama daemon'a saniyede iki
// buçuk istekten fazlasını yollamıyor. Her istek yalnızca bir anlık görüntü
// kopyalar (tarama işini YAPMAZ), yani maliyeti ihmal edilebilir.
const wifiPollInterval = 400 * time.Millisecond

// startWiFiScan kicks off a daemon-side scan and feeds m until it finishes.
func (a *App) startWiFiScan(m *ScanModal) { a.startWiFiScanFor(m, "") }

// startWiFiScanFor is startWiFiScan with a "currently selected" SSID marked.
func (a *App) startWiFiScanFor(m *ScanModal, current string) {
	m.SetScanning(true)
	a.setScanning(true)
	a.setScanNote("Kablosuz ağlar aranıyor…")
	a.Emit(fbui.EventBusy, "Kablosuz ağlar taranıyor…")
	a.Invalidate()

	go func() {
		defer func() {
			a.setScanning(false)
			a.setScanNote("")
			a.Invalidate()
		}()

		st, err := a.cl.WiFiScanStart()
		if err != nil {
			m.SetError("Tarama başlatılamadı: " + err.Error())
			a.Fail("tarama yapılamadı", err)
			return
		}
		gen := st.Gen
		m.Replace(wifiItems(st.Networks, current))

		for {
			if m.Closed() {
				return // kullanıcı pencereyi kapattı; yoklamayı sürdürme
			}
			time.Sleep(wifiPollInterval)

			st, err = a.cl.WiFiScanStatus()
			if err != nil {
				m.SetError("Tarama durumu alınamadı: " + err.Error())
				return
			}
			// Başka bir tarama oturumu başladıysa (ör. kurulum sihirbazı)
			// bizim penceremiz onun sonuçlarını göstermemeli.
			if st.Gen != gen {
				m.SetScanning(false)
				return
			}
			m.Replace(wifiItems(st.Networks, current))
			a.Invalidate()

			if !st.Scanning {
				m.SetScanning(false)
				if st.Error != "" {
					m.SetError(st.Error)
					a.Emit(fbui.EventWarn, "Tarama: "+st.Error)
					return
				}
				a.Emit(fbui.EventOK,
					fmt.Sprintf("%d ağ bulundu", len(st.Networks)))
				return
			}
		}
	}()
}

// wifiItems converts scan results to dialog rows. current (may be empty) is
// the SSID marked as "şu an bağlı".
func wifiItems(nets []ipc.WiFiNetwork, current string) []ListItem {
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
			Current:   current != "" && n.SSID == current,
			Value:     n,
		})
	}
	return items
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
	// Arka plan işi (bkz. jobs.go): bağlanma 10-30 sn sürebilir; kullanıcı
	// beklerken başka ekrana geçebilmeli ve sonucu yine görmeli.
	a.runJob("wifi", ssid+" ağına bağlanılıyor", func() (string, error) {
		if err := a.cl.WiFiApply(ssid, pass); err != nil {
			return ssid + " bağlantısı başarısız", err
		}
		return ssid + " ağına bağlanıldı", nil
	}, nil)
}

func (a *App) connectWired() {
	if a.offline() {
		return
	}
	a.runJob("kablolu", "Kablolu bağlantı kuruluyor", func() (string, error) {
		if err := a.cl.WiredUp(); err != nil {
			return "kablolu bağlantı başarısız", err
		}
		return "Kablolu bağlantı kuruldu", nil
	}, nil)
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
			app.runJob("usbjar-"+id+"-"+jar.Name, jar.Name+" kuruluyor", func() (string, error) {
				msg, err := app.cl.InstallUSBMods(id, []model.USBJar{jar})
				if err != nil {
					return jar.Name + " kurulamadı", err
				}
				return msg, nil
			}, nil)
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
	a.confirmPairPeer(peers[idx])
}

// confirmPairPeer asks before pairing (or unpairing) one device.
//
// pairPeer'dan AYRILDI: canlı tarama penceresi cihazı DOĞRUDAN veriyor
// (a.peers dizinine göre değil). İki yol tek onay penceresini paylaşıyor;
// ayrı yazmak, birinde düzeltilen bir hatanın ötekinde kalması demekti.
func (a *App) confirmPairPeer(p model.Peer) {
	if !p.Paired {
		a.startCodePairing(p)
		return
	}
	a.legacyPairConfirm(p)
}

// startCodePairing pairs WITHOUT typing a key: iki ekranda aynı 6 haneli
// kod çıkar, düğümde "Kabul et", burada onay (bkz. cluster/pairoffer.go).
//
// Kullanıcı: "eşleştirme anahtarını elle girme gerekmesin, oto tarasın
// doğrulasın". Karşı taraf bunu bilmiyorsa (eski düğüm, başka bir MCOS)
// eski onay yoluna düşülür.
func (a *App) startCodePairing(p model.Peer) {
	if a.offline() {
		return
	}
	a.Emit(fbui.EventBusy, p.Name+" ile eşleştirme başlatılıyor…")
	go func() {
		code, ok, err := a.cl.ClusterPairOffer(p.ID)
		if err != nil {
			a.Fail(p.Name+" ile eşleştirme başlatılamadı", err)
			return
		}
		a.mu.Lock()
		a.clearBusyLocked()
		a.mu.Unlock()
		if !ok {
			a.legacyPairConfirm(p)
			return
		}
		a.OpenModal(NewConfirmModal("Eşleştirme kodu: "+code,
			[]string{
				p.Name + " (" + p.IP + ") bilgisayarındaki MCOS uygulamasında",
				"AYNI kod görünüyor: " + code,
				"Orada \"Kabul et\"e basın; kodlar aynıysa burada onaylayın.",
				"Kodlar farklıysa İPTAL edin: araya başka biri girmiş olabilir.",
			},
			"Kodlar aynı, onayla", false,
			func(app *App) {
				app.runJob("esle-"+p.ID, p.Name+" eşleştiriliyor (kabul bekleniyor)",
					func() (string, error) { return app.waitCodePairing(p) }, app.loadSection)
			}))
	}()
}

// waitCodePairing sends the sealed key until the node accepts (en çok 2 dk).
func (a *App) waitCodePairing(p model.Peer) (string, error) {
	son := nowFunc().Add(2 * time.Minute)
	for {
		st, msg, err := a.cl.ClusterPairConfirm(p.ID)
		if err != nil {
			return p.Name + " eşleştirilemedi", err
		}
		switch st {
		case "tamam":
			return p.Name + " eşleşti — anahtar otomatik aktarıldı", nil
		case "bekliyor":
			if nowFunc().After(son) {
				_ = a.cl.ClusterPairCancel(p.ID)
				return p.Name + " eşleştirilemedi",
					fmt.Errorf("uygulamada 2 dakika içinde \"Kabul et\"e basılmadı")
			}
			time.Sleep(time.Second)
		case "reddedildi":
			return p.Name + " eşleştirilemedi", fmt.Errorf("%s", "karşı tarafta reddedildi")
		default:
			if msg == "" {
				msg = "teklifin süresi doldu — yeniden deneyin"
			}
			return p.Name + " eşleştirilemedi", fmt.Errorf("%s", msg)
		}
	}
}

// legacyPairConfirm is the old key-based pair/unpair confirmation.
func (a *App) legacyPairConfirm(p model.Peer) {
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
	a.serverActionOn(a.selectedServer(), what)
}

// serverActionOn issues start/stop/restart for a given server.
//
// Listeden AYRI, çünkü sunucu detayı sunucuyu imleçle değil KİMLİKLE tutar:
// detay açıkken liste sırası değişirse (bir sunucu silindi) imleçteki satır
// artık başka bir sunucudur ve "yeniden başlat" yanlış sunucuyu vururdu.
func (a *App) serverActionOn(s *model.Server, what string) {
	if a.offline() || s == nil {
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
