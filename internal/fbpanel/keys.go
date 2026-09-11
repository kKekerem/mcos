package fbpanel

import (
	"mcos/internal/fbui"
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
// Tuş işleyicisi doğrudan yeniden başlatma veya kapatma YAPMAZ: bunlar
// geri alınamaz işlemlerdir ve ekranı devralmış bir programın onları
// yaparken konsolu geri vermesi gerekir. Karar burada, uygulaması
// cmd katmanındadır.
type Action int

const (
	// ActNone: yapılacak bir şey yok.
	ActNone Action = iota
	// ActQuit: panelden çık (eski panele dön).
	ActQuit
	// ActSleep: ekranı uykuya al.
	ActSleep
	// ActReboot: sistemi yeniden başlat.
	ActReboot
	// ActPoweroff: sistemi kapat.
	ActPoweroff
	// ActLegacyPanel: eski Bubble Tea panelini aç.
	ActLegacyPanel
)

// Key handles one keystroke and returns the action the host must perform.
//
// Tuş atamaları ESKİ PANELLE AYNI (panel/app.go): kullanıcı kas hafızasını
// kaybetmemeli. Yeni olan tek şey F12 (eski panele dönüş).
func (a *App) Key(key string) Action {
	// Açılır pencere varsa tuşlar ÖNCE ona gider: arkadaki ekranın kısayolları
	// pencere açıkken tetiklenmemeli.
	if m := a.ActiveModal(); m != nil {
		if m.Key(a, key) {
			a.CloseModal()
		}
		a.Invalidate()
		return ActNone
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

	case "s", "S":
		a.serverAction("start")
		return ActNone
	case "x", "X":
		a.serverAction("stop")
		return ActNone
	case "r", "R":
		a.serverAction("restart")
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
// Bu sayı çizimle AYNI kaynaktan gelmeli; yoksa imleç listenin dışına çıkar.
// Eski panelde Wi-Fi listesi 8 satır çiziyordu ama imleç daha aşağı
// inebiliyordu — tam olarak bu hatayı önlemek için tek fonksiyon.
func (a *App) contentRows() int {
	switch a.Section() {
	case SecServers:
		_, servers, _ := a.Snapshot()
		return len(servers)
	case SecPower:
		return len(powerItems)
	case SecSettings:
		return len(fbui.ThemeOrder)
	}
	return 0
}

// activate performs the primary action for the focused row.
func (a *App) activate() Action {
	if a.Focus() == FocusSidebar {
		a.setFocus(FocusContent)
		return ActNone
	}

	switch a.Section() {
	case SecPower:
		switch a.Cursor() {
		case 0:
			return ActSleep
		case 1:
			return ActReboot
		case 2:
			return ActPoweroff
		}
	case SecSettings:
		a.applyTheme(a.Cursor())
	case SecServers:
		a.toggleServer()
	}
	return ActNone
}

// applyTheme switches the accent colour and persists it.
func (a *App) applyTheme(idx int) {
	if idx < 0 || idx >= len(fbui.ThemeOrder) {
		return
	}
	name := fbui.ThemeOrder[idx]

	a.mu.Lock()
	a.ui.Pal = fbui.DefaultPalette.WithAccent(name)
	cfg := a.cfg
	a.dirty = true
	a.mu.Unlock()

	a.Emit(fbui.EventOK, "Tema: "+fbui.ThemeLabel(name))

	if cfg == nil {
		return
	}
	// Yapılandırmanın KOPYASI üzerinde çalış: doğrudan değiştirip kaydetme
	// başarısız olursa bellekteki durum diskle uyuşmaz kalırdı.
	next := *cfg
	next.Theme = name
	if err := a.cl.UpdateConfig(&next); err != nil {
		a.Fail("tema kaydedilemedi", err)
		return
	}
	a.mu.Lock()
	a.cfg = &next
	a.mu.Unlock()
}

// toggleTurbo flips turbo mode via the daemon.
func (a *App) toggleTurbo() {
	_, _, cfg := a.Snapshot()
	want := true
	if cfg != nil {
		want = !cfg.Turbo
	}
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
	s := a.selectedServer()
	if s == nil {
		return
	}
	var err error
	switch what {
	case "start":
		a.Emit(fbui.EventBusy, s.Name+" başlatılıyor…")
		err = a.cl.Start(s.ID)
	case "stop":
		a.Emit(fbui.EventBusy, s.Name+" durduruluyor…")
		err = a.cl.Stop(s.ID)
	case "restart":
		a.Emit(fbui.EventBusy, s.Name+" yeniden başlatılıyor…")
		err = a.cl.Restart(s.ID)
	default:
		return
	}
	if err != nil {
		a.Fail(s.Name, err)
	}
}
