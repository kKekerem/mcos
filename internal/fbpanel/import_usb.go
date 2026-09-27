package fbpanel

import (
	"fmt"
	"strings"

	"mcos/internal/fbui"
	"mcos/internal/model"
)

// importUSBServer imports a server folder from a USB drive as a new server.
//
// Kullanıcı: "Sunucu klasörü aktarma ekle: flaşı seçip bir klasör seçip onu
// aktarabilelim; içindeki sunucu, dünya, her şey, modlar." Önce USB'lerde
// sunucuya benzeyen klasörler (server.properties, dünya, sunucu jar'ı) aranır;
// hiç yoksa üst düzey klasörler elle seçilebilir. Seçim, aktarmayı arka
// plan işi olarak başlatır: dünya yüzlerce MB olabilir, kullanıcı beklerken
// başka ekranlara geçebilmeli (bkz. jobs.go).
func (a *App) importUSBServer() {
	if a.offline() {
		return
	}
	a.Emit(fbui.EventBusy, "USB bellekler taranıyor…")
	go func() {
		items, err := a.cl.ScanUSBServerFolders()
		a.mu.Lock()
		a.clearBusyLocked()
		a.mu.Unlock()
		if err != nil {
			a.Fail("USB taranamadı", err)
			return
		}
		if len(items) == 0 {
			a.Emit(fbui.EventWarn, "Takılı bir USB bellekte klasör bulunamadı — belleği takıp yeniden deneyin")
			return
		}
		list := make([]ListItem, 0, len(items))
		for _, it := range items {
			li := ListItem{Label: it.Name, Detail: usbFolderDetail(it), Value: it}
			if it.Recognized {
				li.Badge = "sunucu"
			}
			list = append(list, li)
		}
		intro := "Aktarılacak sunucuyu seçin. Dünya, modlar, eklentiler ve ayarlar kopyalanır; USB'deki dosyalara dokunulmaz."
		if !items[0].Recognized {
			intro = "USB'de sunucu olarak tanınan bir klasör bulunamadı. Aktarılacak klasörü elle seçin."
		}
		a.OpenModal(NewListModal("USB'den sunucu aktar", intro, list,
			func(app *App, _ int, li ListItem) bool {
				it := li.Value.(model.USBServerFolder)
				app.runJob("aktar-"+it.Device+":"+it.RelPath, it.Name+" USB'den aktarılıyor",
					func() (string, error) {
						srv, err := app.cl.ImportUSBServer(it.Device, it.RelPath, "")
						if err != nil {
							return it.Name + " aktarılamadı", err
						}
						return fmt.Sprintf("%s aktarıldı (%s %s) — yazılım hazırlanınca başlatabilirsiniz",
							srv.Name, srv.Software, srv.MCVersion), nil
					}, app.loadSection)
				return true
			}))
	}()
}

// usbFolderDetail: "paper 1.21.1 · dünya · 12 eklenti · 1,2 GB · sdb1".
func usbFolderDetail(it model.USBServerFolder) string {
	s := ""
	add := func(p string) {
		if p == "" {
			return
		}
		if s != "" {
			s += " · "
		}
		s += p
	}
	if it.Software != "" {
		add(it.Software + " " + it.MCVersion)
	}
	if it.HasWorld {
		add("dünya")
	}
	if it.Mods > 0 {
		add(fmt.Sprintf("%d mod", it.Mods))
	}
	if it.Plugins > 0 {
		add(fmt.Sprintf("%d eklenti", it.Plugins))
	}
	if it.SizeBytes > 0 {
		add(humanBytes(it.SizeBytes))
	}
	dev := it.Device
	if i := len(dev) - 1; i >= 0 {
		for i >= 0 && dev[i] != '/' {
			i--
		}
		dev = dev[i+1:]
	}
	add(dev)
	return s
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return strings.Replace(fmt.Sprintf("%.1f GB", float64(n)/(1<<30)), ".", ",", 1)
	case n >= 1<<20:
		return fmt.Sprintf("%d MB", n>>20)
	}
	return fmt.Sprintf("%d KB", n>>10)
}
