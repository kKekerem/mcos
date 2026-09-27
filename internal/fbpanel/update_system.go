package fbpanel

import (
	"errors"
	"strings"
	"time"

	"mcos/internal/fbui"
	"mcos/internal/ipc"
)

// ════════════════════════════════════════════════════════════════════════════
// AYARLAR > SİSTEMİ GÜNCELLE (USB'deki ISO)
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı: "USB'yi takıp içinde yeni bir ISO bulunursa hiçbir veri kaybı
// olmadan sistemi güncelleyebileyim."
//
// Akış: USB'deki ISO'lar listelenir (daha yeni / aynı / eski) → onay →
// mcos-update arka plan işi olarak koşar (jobs.go; kullanıcı bu sırada
// paneli kullanabilir) → bitince "Şimdi yeniden başlat?" sorulur. Yeni sistem
// açılışta kök bölümüne kopyalanır; /data'ya (sunucular, dünyalar, ayarlar)
// hiç dokunulmaz. Ayrıntı: rootfs-overlay/usr/bin/mcos-update.

// updateJobID: güncellemenin arka plan işi; aynı anda ikincisi başlamaz.
const updateJobID = "guncelleme"

// updatePollEvery: ilerleme yoklama aralığı (testte kısaltılır).
var updatePollEvery = time.Second

// updateLiveLines: canlı sistemde (RAM'den açılış) güncellenecek disk yok.
func updateLiveLines() []string {
	return []string{
		"Canlı sistemde güncelleme yok: yeni ISO ile açın ya da Ayarlar'dan diske kurun.",
		"",
		"Sistem şu anda USB/ISO'dan, RAM'de çalışıyor. Yeni sürümü kullanmak için",
		"yeni ISO'yu USB'ye yazıp ondan açmanız yeter.",
		"MCOS'u kalıcı kullanmak için: Ayarlar > Diske / USB'ye kur.",
	}
}

// openUpdatePicker scans USB drives for MCOS ISOs and lists them.
//
// Tarama arka planda (IPC ana döngüyü bloklamamalı; bkz. run.go). Liste
// gelince kullanıcı başka bir pencere açmışsa ya da bölümden çıkmışsa pencere
// açılmaz: görmediği bir pencerenin tuşları yutması "klavye çalışmıyor" demek
// (openInstallPicker ile aynı gerekçe).
func (a *App) openUpdatePicker() {
	if a.offline() {
		a.Emit(fbui.EventError, "daemon bağlantısı yok — güncelleme aranamaz")
		return
	}
	if a.runningJobs()[updateJobID] {
		a.Emit(fbui.EventInfo, "Güncelleme zaten sürüyor — ilerleme alt çubukta")
		return
	}
	a.Emit(fbui.EventBusy, "USB'de MCOS ISO'su aranıyor…")
	go func() {
		res, err := a.cl.UpdateScan()
		a.mu.Lock()
		a.clearBusyLocked()
		a.mu.Unlock()
		if err != nil {
			a.Fail("USB taranamadı", err)
			return
		}
		if a.ActiveModal() != nil || a.Section() != SecSettings ||
			a.setupState() != nil || a.Locked() {
			a.Emit(fbui.EventInfo, "ISO listesi hazır — Ayarlar > Sistemi güncelle ile yeniden açın")
			return
		}
		if res.Live {
			a.OpenModal(NewInfoModal("Sistemi güncelle", updateLiveLines()))
			return
		}
		a.OpenModal(newUpdatePicker(res))
	}()
}

// updateBadge: karşılaştırmanın rozeti.
func updateBadge(c string) (string, fbui.EventKind) {
	switch c {
	case ipc.UpdateNewer:
		return "daha yeni", fbui.EventOK
	case ipc.UpdateSame:
		return "aynı", fbui.EventInfo
	case ipc.UpdateOlder:
		return "eski", fbui.EventWarn
	}
	return "uygun değil", fbui.EventError
}

// buildLabel: "1.0.2 · 20260927-1715" (özet kısmı kullanıcıya bir şey söylemez).
func buildLabel(ver, id string) string {
	if len(id) > 13 {
		id = id[:13]
	}
	switch {
	case ver != "" && id != "":
		return ver + " · " + id
	case id != "":
		return id
	}
	return ver
}

// newUpdatePicker builds the ISO list; seçim onay penceresini açar.
func newUpdatePicker(res ipc.UpdateScanResult) *ListModal {
	items := make([]ListItem, 0, len(res.Items))
	for _, it := range res.Items {
		li := ListItem{Label: it.Name, Value: it}
		li.Badge, li.BadgeKind = updateBadge(it.Compare)
		if it.Compare == ipc.UpdateInvalid {
			li.Disabled = true
			li.Detail = it.Error
		} else {
			li.Detail = buildLabel(it.Version, it.BuildID) + " · " + devBase(it.Device)
		}
		items = append(items, li)
	}
	cur := buildLabel(res.CurrentVersion, res.CurrentBuild)
	if res.CurrentBuild == "" {
		cur = res.CurrentVersion + " (derleme kimliği yok)"
	}
	return NewListModal("Sistemi güncelle (USB'deki ISO)",
		"Çalışan sistem: "+cur+". Kurulacak ISO'yu seçin; sunucular, dünyalar ve ayarlar korunur.",
		items,
		func(app *App, _ int, li ListItem) bool {
			it := li.Value.(ipc.UpdateISO)
			app.OpenModal(NewConfirmModal("Sistemi güncelle?", updateConfirmLines(it),
				"Güncelle", it.Compare == ipc.UpdateOlder,
				func(app2 *App) { app2.startUpdate(it) }))
			return true
		}).
		WithEmpty("USB'de MCOS ISO'su bulunamadı. ISO dosyasını USB belleğe kopyalayıp takın ve yeniden deneyin.")
}

func updateConfirmLines(it ipc.UpdateISO) []string {
	lines := []string{it.Name + " (" + buildLabel(it.Version, it.BuildID) + ")"}
	switch it.Compare {
	case ipc.UpdateOlder:
		lines = append(lines, "DİKKAT: bu ISO çalışan sistemden ESKİ (sürüm düşürülür).")
	case ipc.UpdateSame:
		lines = append(lines, "Bu ISO çalışan sistemle aynı derleme; yeniden yazılır.")
	}
	return append(lines,
		"Sunucular, dünyalar ve ayarlar KORUNUR. Güncelleme yeniden başlatınca uygulanır.",
		"Hazırlık arka planda sürer; bitene kadar USB'yi çıkarmayın.")
}

// devBase: "/dev/sdb1" → "sdb1".
func devBase(dev string) string {
	if i := strings.LastIndexByte(dev, '/'); i >= 0 {
		return dev[i+1:]
	}
	return dev
}

// startUpdate runs the update as a background job and polls its progress.
func (a *App) startUpdate(it ipc.UpdateISO) bool {
	baslik := "Sistem güncelleniyor"
	return a.runJobKind(updateJobID, baslik, func() (fbui.EventKind, string, error) {
		if err := a.cl.UpdateStart(it.Device, it.Path); err != nil {
			return fbui.EventError, "güncelleme başlatılamadı", err
		}
		last, hata := "", 0
		for {
			time.Sleep(updatePollEvery)
			st, err := a.cl.UpdateStatus()
			if err != nil {
				// Tek bir kopuk yoklama işi bitirmesin; uzun süren kopukluk
				// ise "sürüyor" diye sonsuza dek beklemesin.
				if hata++; hata >= 30 {
					return fbui.EventError, "güncelleme durumu alınamadı", err
				}
				continue
			}
			hata = 0
			switch {
			case st.Running:
				if st.Message != "" && st.Message != last {
					last = st.Message
					a.setJobTitle(updateJobID, baslik+" · "+st.Message)
				}
			case st.Failed:
				a.showUpdateFailure(st.Lines)
				msg := st.Message
				if msg == "" {
					msg = "bilinmeyen hata"
				}
				return fbui.EventError, "güncelleme başarısız", errors.New(msg)
			case st.Done:
				msg := st.Message
				if msg == "" {
					msg = "Güncelleme hazır — yeniden başlatınca kurulacak"
				}
				return fbui.EventOK, msg, nil
			default:
				return fbui.EventError, "güncelleme başarısız",
					errors.New("daemon güncellemenin sonucunu bildirmedi")
			}
		}
	}, a.askRebootAfterUpdate)
}

// showUpdateFailure opens the script's reason; kullanıcı başka bir pencerede
// yazıyorsa sonuç yalnızca alt çubuğa düşer (showInstallResult gerekçesi).
func (a *App) showUpdateFailure(lines []string) {
	if a.ActiveModal() != nil || len(lines) == 0 {
		return
	}
	var sarili []string
	for _, l := range lines {
		sarili = append(sarili, wrapWords(l, 72)...)
	}
	sarili = append(sarili, "", "Eski sistem yerinde duruyor; bilgisayar olduğu gibi açılır.")
	a.OpenModal(NewInfoModal("Güncelleme başarısız", sarili))
}

// askRebootAfterUpdate: güncelleme hazır, "Şimdi yeniden başlat?".
//
// Yeniden başlatma mevcut güç yolundan geçer (ActReboot → system.power);
// daemon kapatmadan önce sunucuları düzgünce durdurur (stopServersForPower),
// dünyalar kaydedilir.
func (a *App) askRebootAfterUpdate() {
	if a.ActiveModal() != nil {
		a.Emit(fbui.EventInfo, "Güncelleme hazır — yeniden başlatınca kurulacak (Güç menüsü)")
		return
	}
	a.OpenModal(NewConfirmModal("Şimdi yeniden başlat?",
		[]string{
			"Güncelleme hazır; yeni sistem açılışta kurulur.",
			"Açık sunucular önce düzgünce durdurulur, dünyalar kaydedilir.",
			"İlk açılış birkaç dakika uzun sürebilir; bu sırada kapatmayın.",
		},
		"Yeniden başlat", false,
		func(app *App) {
			app.mu.Lock()
			app.pending = ActReboot
			app.mu.Unlock()
		}))
}
