package fbpanel

import (
	"fmt"
	"image"
	"strconv"
	"strings"

	"mcos/internal/fbui"
)

// Sunucu oluşturma sihirbazının sayfa gövdeleri.
//
// Düzen, satır çizimi ve adım göstergesi ilk kurulum sihirbazıyla ORTAK
// (bkz. drawFlow). Burada yalnızca her sayfanın kendine özgü bilgisi var.

// drawWizardBody renders page-specific content above the row list.
func (a *App) drawWizardBody(w *Wizard, body image.Rectangle, y int) int {
	u := a.ui

	switch w.step {
	case wizTemplate:
		return a.hint(body, y,
			"Şablon, sonraki sayfalardaki alanları doldurur.",
			"Hiçbir şeyi kilitlemez — her değeri sonra değiştirebilirsiniz.") +
			u.M.PadY

	case wizVersion:
		// ── Yakalanan gerçek hata ───────────────────────────────────────
		// Bu üç alan (verLoaded, verNote, versions) arka plan sürüm
		// isteğinin goroutine'i tarafından KİLİTSİZ yazılıyordu. Liste
		// beklenirken Tick her karede yeniden çizim istediği için,
		// göstergenin döndüğü sırada w.verNote'un 2 kelimelik string
		// başlığı ve w.versions'ın 3 kelimelik dilim başlığı tam yazma
		// anında okunabiliyordu: uyuşmayan işaretçi/uzunluk çifti ekranda
		// bozuk metin, kötü durumda da metin çizicide sınır dışı okuma
		// demekti. Artık hepsi TEK kilitte kopyalanıp öyle çiziliyor.
		note, loaded, count := a.wizVersionView(w)
		if !loaded && note == "" {
			u.Spinner(body.Min.X, y, a.Spin(), u.Pal.Accent)
			u.Text(body.Min.X+u.F.CellW+u.M.Gap, y,
				"Mojang sürüm listesi alınıyor…", u.Pal.TextDim)
			return y + u.F.CellH + u.M.PadY*2
		}
		if note != "" {
			u.WarnTriangle(body.Min.X, y, u.Pal.Warn)
			u.Text(body.Min.X+u.F.CellW+u.M.Gap, y, note, u.Pal.Warn)
			return y + u.F.CellH + u.M.PadY*2
		}
		u.Text(body.Min.X, y,
			fmt.Sprintf("%d sürüm listelendi.", count), u.Pal.TextFaint)
		return y + u.F.CellH + u.M.PadY

	case wizSoftware:
		sw := w.software()
		rows := [][3]any{
			{"Eklenti (plugins/)", yesNo(sw.SupportsPlugins()),
				okColor(a, sw.SupportsPlugins())},
			{"Mod (mods/)", yesNo(sw.SupportsMods()),
				okColor(a, sw.SupportsMods())},
		}
		y = a.kvList(body, y, 24, rows)
		y += u.M.PadY
		if !sw.SupportsMods() {
			y = a.hint(body, y,
				"Ortak dünya (MCOS Link) yalnızca mod yükleyen sürümlerde",
				"çalışır — Fabric önerilir.")
		}
		return y + u.M.PadY

	case wizGameplay:
		if !w.onlineMode {
			u.WarnTriangle(body.Min.X, y, u.Pal.Warn)
			u.Text(body.Min.X+u.F.CellW+u.M.Gap, y,
				"Online mode kapalı: doğrulanmamış istemciler bağlanabilir.",
				u.Pal.Warn)
			y += u.F.CellH + u.M.PadY
		}
		if w.hardcore {
			u.WarnTriangle(body.Min.X, y, u.Pal.Error)
			u.Text(body.Min.X+u.F.CellW+u.M.Gap, y,
				"Hardcore: ölüm kalıcıdır ve sonradan kapatılamaz.", u.Pal.Error)
			y += u.F.CellH + u.M.PadY
		}
		return y

	case wizResources:
		st, _, cfg := a.Snapshot()
		if st != nil {
			total := int(st.Memory.TotalBytes >> 20)
			free := int(st.Memory.AvailableBytes >> 20)
			y = a.hint(body, y,
				fmt.Sprintf("Bu makinede %d MB bellek var, %d MB boş.", total, free))
			y += u.M.PadY / 2
		}
		// Kaynak bütçesi kurulumda belirlendiyse SÖYLE: kullanıcı 16 GB
		// seçip sonra sessizce kısıldığında nedenini anlayamazdı.
		if cfg != nil && cfg.Budget.MaxServerRAMMB > 0 {
			want := wizRAMChoices[w.ramIdx]
			if want > cfg.Budget.MaxServerRAMMB {
				u.WarnTriangle(body.Min.X, y, u.Pal.Warn)
				u.Text(body.Min.X+u.F.CellW+u.M.Gap, y,
					fmt.Sprintf("Kaynak bütçesi %d MB ile sınırlı — istek kısılacak.",
						cfg.Budget.MaxServerRAMMB), u.Pal.Warn)
				y += u.F.CellH + u.M.PadY
			}
		}
		return y + u.M.PadY

	case wizEULA:
		y = a.hint(body, y,
			"Minecraft sunucusunu çalıştırmak için Mojang'ın Son Kullanıcı",
			"Lisans Sözleşmesi'ni kabul etmeniz gerekir. Kabul ettiğinizde",
			"sunucu klasörüne eula.txt yazılır.")
		y += u.M.PadY
		u.Text(body.Min.X, y, "https://aka.ms/MinecraftEULA", u.Pal.Accent)
		return y + u.F.CellH + u.M.PadY*2

	case wizConfirm:
		return a.drawWizardSummary(w, body, y)
	}
	return y
}

// drawWizardSummary lists every choice before creating.
func (a *App) drawWizardSummary(w *Wizard, body image.Rectangle, y int) int {
	u := a.ui

	port := strings.TrimSpace(w.port)
	if port == "" || port == "0" {
		port = "otomatik"
	}
	cpu := strings.TrimSpace(w.cpuQuota)
	if cpu == "" {
		cpu = "sınırsız"
	} else {
		cpu = "%" + cpu
	}
	dir := strings.TrimSpace(w.dataDir)
	if dir == "" {
		dir = "varsayılan"
	}

	// Sürüm, arka plan isteğiyle paylaşılan alanlardan okunur: kilitli
	// sarmalayıcıdan alınır (bkz. wizard.go "KİLİT SÖZLEŞMESİ").
	ver := a.wizardVersion(w)

	y = a.kvList(body, y, 22, [][3]any{
		{"Ad", w.name, u.Pal.Text},
		{"Yazılım", string(w.software()) + " " + ver, u.Pal.Text},
		{"ViaVersion", onOff(w.via), u.Pal.Text},
		{"Port", port, u.Pal.Text},
		{"RAM / CPU", strconv.Itoa(wizRAMChoices[w.ramIdx]) + " MB · " + cpu,
			u.Pal.Text},
		{"Oyun modu", wizGamemodes[w.gamemodeIdx] + " · " +
			wizDifficulties[w.difficultyIdx], u.Pal.Text},
		{"PvP / Beyaz liste", onOff(w.pvp) + " / " + onOff(w.whitelist), u.Pal.Text},
		{"Hardcore", onOff(w.hardcore), u.Pal.Text},
		{"Online mode", onOff(w.onlineMode), u.Pal.Text},
		{"PC paylaşımı", onOff(w.clusterShare), u.Pal.Text},
		{"Kurulum yeri", dir, u.Pal.Text},
		{"Açılışta başlat", onOff(w.autostart), u.Pal.Text},
		{"Otomatik yedek", onOff(w.autoBackup), u.Pal.Text},
		{"Tünel", onOff(w.wan), u.Pal.Text},
	})
	y += u.M.PadY

	if w.creating {
		u.Spinner(body.Min.X, y, a.Spin(), u.Pal.Accent)
		u.Text(body.Min.X+u.F.CellW+u.M.Gap, y,
			"Oluşturuluyor…", u.Pal.Accent)
		y += u.F.CellH + u.M.PadY
	} else {
		y = a.hint(body, y,
			"Onaydan sonra sunucu yazılımı arka planda indirilip kurulur.",
			"İndirme sürerken paneli kullanmaya devam edebilirsiniz.")
		y += u.M.PadY
	}
	return y
}

// wizardShortcuts are the status-bar hints for the create wizard.
func (a *App) wizardShortcuts(w *Wizard) []fbui.Shortcut {
	if a.ActiveModal() != nil {
		return []fbui.Shortcut{
			{Key: "↑↓", Label: "Gezin"},
			{Key: "Enter", Label: "Seç"},
			{Key: "Esc", Label: "Kapat"},
		}
	}
	label := "Geri"
	if w.step == wizTemplate {
		label = "Vazgeç"
	}
	return []fbui.Shortcut{
		{Key: "↑↓", Label: "Gezin"},
		{Key: "Enter", Label: "Seç"},
		{Key: "Esc", Label: label},
	}
}

func yesNo(b bool) string {
	if b {
		return "evet"
	}
	return "hayır"
}

func okColor(a *App, ok bool) any {
	if ok {
		return a.ui.Pal.OK
	}
	return a.ui.Pal.TextFaint
}

// wizardSummaryLine is used by tests to assert the wizard collected everything.
//
// YALNIZCA TEST: version() a.mu tutulurken çağrılmalıdır; testler tek
// goroutine olduğu için burada kilit alınmaz (kilit alınsaydı, testin
// kilit sırasına da dikkat etmesi gerekirdi).
func (w *Wizard) summaryLine() string {
	return strings.Join([]string{
		"ad=" + w.name,
		"yazilim=" + string(w.software()),
		"surum=" + w.version(),
		"ram=" + strconv.Itoa(wizRAMChoices[w.ramIdx]),
		"port=" + w.port,
		"mod=" + wizGamemodes[w.gamemodeIdx],
		"zorluk=" + wizDifficulties[w.difficultyIdx],
		"paylasim=" + onOff(w.clusterShare),
		"eula=" + onOff(w.eula),
	}, " ")
}

// wizardHasPeerPicker reports whether any wizard page offers to choose a peer.
//
// Kullanıcının bildirdiği mantık hatası tam olarak buydu: "sunucu kurarken
// secilmemli". Test bunu doğruluyor; burada tek bir yerde tanımlı olması,
// ileride biri eş listesi eklemeye kalkarsa testin kırılmasını sağlar.
func (w *Wizard) hasPeerPicker(a *App) bool {
	saved := w.step
	defer func() { w.step = saved }()
	for s := wizStep(0); s < wizStepCount; s++ {
		w.step = s
		for _, r := range w.rows(a) {
			if r.kind != rowPick {
				continue
			}
			l := strings.ToLower(r.label)
			if strings.Contains(l, "cihaz") || strings.Contains(l, "eş") ||
				strings.Contains(l, "peer") {
				return true
			}
		}
	}
	return false
}
