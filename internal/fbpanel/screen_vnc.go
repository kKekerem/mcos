package fbpanel

import (
	"fmt"

	"mcos/internal/fbui"
	"mcos/internal/ipcclient"
)

// ════════════════════════════════════════════════════════════════════════════
// EKRAN PAYLAŞIMI (VNC) EKRANI
// ════════════════════════════════════════════════════════════════════════════
//
// ── Kullanıcının isteği ─────────────────────────────────────────────────────
//
//	"realvnc ile bağlanma bu da olsun"
//
// ── Bu pencere neyi anlatmak zorunda ────────────────────────────────────────
//
// VNC'de kullanıcının elinde olması gereken tam olarak ÜÇ şey var: adres,
// port ve parola. Pencere onları büyük ve okunur yazıyor — kullanıcı bunları
// başka bir cihaza ELLE yazacak.
//
// Ayrıca iki dürüst uyarı:
//
//   - Trafik ŞİFRESİZ. RFB'nin kendisi şifreleme taşımaz. Yerel ağ için
//     uygundur; internete açmak için SSH tüneli gerekir ve pencere bunu
//     yazıyor.
//   - Girdi aygıtı yoksa yalnızca İZLEME mümkündür. "Bağlandım ama
//     tıklayamıyorum" sorusunun cevabı burada görünür.

// openVNCSettings shows the screen-sharing dialog.
func (a *App) openVNCSettings() {
	if a.offline() {
		a.Emit(fbui.EventWarn, "mcosd bağlantısı yok")
		return
	}
	// Durumu ARKA PLANDA al: ipc.Client çağrıları sıraya dizer ve bir tarama
	// sürüyorsa bu çağrı saniyelerce bekleyebilir (uzaktan kontrol
	// penceresiyle aynı gerekçe).
	a.Emit(fbui.EventBusy, "ekran paylaşımı durumu alınıyor…")
	go func() {
		st, err := a.cl.VNCGet()
		if err != nil {
			a.Fail("ekran paylaşımı durumu alınamadı", err)
			return
		}
		a.showVNCModal(st)
	}()
}

// showVNCModal builds the dialog from a status snapshot.
func (a *App) showVNCModal(st ipcclient.VNCStatus) {
	var lines []string
	var actions []ListItem

	if !st.Enabled {
		lines = []string{
			"Ekran paylaşımı KAPALI.",
			"",
			"Açtığınızda bu ekranın aynısını başka bir",
			"bilgisayardan görebilir ve kullanabilirsiniz.",
			"",
			"İstemci: RealVNC Viewer, TigerVNC, Remmina…",
			"",
			"DİKKAT: VNC trafiği şifresizdir. Yerel ağ için",
			"uygundur; internete AÇMAYIN.",
		}
		actions = []ListItem{
			{Label: "Ekran paylaşımını aç", Value: "enable"},
		}
	} else {
		addr := "adres yok"
		if len(st.Addresses) > 0 {
			addr = st.Addresses[0]
		}
		// RealVNC Viewer "adres:port" biçimini kabul eder. 5900 varsayılan
		// olduğu için yalnızca IP yazmak da yeter; ikisini de gösteriyoruz.
		target := addr
		if st.Port != 5900 {
			target = fmt.Sprintf("%s:%d", addr, st.Port)
		}

		lines = []string{
			"RealVNC Viewer'a şunu yazın:",
			"",
			"   Adres  :  " + target,
			fmt.Sprintf("   Port   :  %d", st.Port),
			"",
			"   Parola :  " + st.Password,
			"",
		}
		switch {
		case !st.Running:
			lines = append(lines, "UYARI: açık ama dinlemiyor.", "")
		case st.ViewOnly:
			lines = append(lines,
				"Kip: YALNIZCA İZLEME — uzaktaki kullanıcı",
				"ekranı görür ama hiçbir şeye dokunamaz.", "")
		case !st.Input:
			lines = append(lines,
				"Kip: yalnızca izleme (sanal girdi aygıtı yok).",
				"Çekirdekte uinput kapalı olabilir.", "")
		default:
			lines = append(lines,
				"Kip: TAM DENETİM — klavye ve fare çalışır.", "")
		}
		if st.Clients > 0 {
			lines = append(lines,
				fmt.Sprintf("Şu an bağlı: %d izleyici", st.Clients), "")
		}
		// Bilgi satırları KISA tutuluyor: pencere sabit yükseklikte ve uzun
		// bir açıklama, EYLEM satırlarını (aç/kapat/yenile) görünür alanın
		// dışına itiyordu. Kullanıcı kaydırarak ulaşabiliyordu ama bir
		// eylemin görünmemesi, olmadığı anlamına gelir.
		lines = append(lines,
			"Trafik ŞİFRESİZDİR — internete açmayın.",
			"SSH tüneli: ssh -L 5900:localhost:5900 root@"+addr,
			"",
		)

		viewLabel := "Yalnızca izlemeye al"
		if st.ViewOnly {
			viewLabel = "Tam denetime aç"
		}
		actions = []ListItem{
			{Label: viewLabel, Value: "viewonly"},
			{Label: "Parolayı yenile", Value: "rotate"},
			{Label: "Ekran paylaşımını kapat", Value: "disable"},
		}
	}

	a.OpenModal(NewListModal("Ekran Paylaşımı (VNC)", "",
		append(infoRows(lines), actions...),
		func(app *App, _ int, it ListItem) bool {
			key, ok := it.Value.(string)
			if !ok {
				return false // bilgi satırı: pencere kapanmasın
			}
			app.runVNCAction(key, st)
			return true
		}))
}

// runVNCAction performs one of the dialog's buttons.
func (a *App) runVNCAction(what string, prev ipcclient.VNCStatus) {
	a.Emit(fbui.EventBusy, "uygulanıyor…")
	go func() {
		var (
			st  ipcclient.VNCStatus
			err error
		)
		switch what {
		case "enable":
			st, err = a.cl.VNCEnable()
		case "disable":
			st, err = a.cl.VNCDisable()
		case "rotate":
			st, err = a.cl.VNCRotate()
		case "viewonly":
			st, err = a.cl.VNCViewOnly(!prev.ViewOnly)
		default:
			return
		}
		if err != nil {
			a.Fail("ekran paylaşımı", err)
			return
		}

		switch what {
		case "enable":
			a.Emit(fbui.EventOK, fmt.Sprintf(
				"Ekran paylaşımı açık — parola: %s", st.Password))
		case "disable":
			a.Emit(fbui.EventInfo, "Ekran paylaşımı kapatıldı")
		case "rotate":
			a.Emit(fbui.EventOK, "Yeni parola: "+st.Password)
		case "viewonly":
			if st.ViewOnly {
				a.Emit(fbui.EventInfo, "Yalnızca izleme kipine alındı")
			} else {
				a.Emit(fbui.EventOK, "Tam denetim açıldı")
			}
		}
		// Pencereyi GÜNCEL durumla yeniden aç: kullanıcı yeni parolayı
		// görebilmeli ve kapatma sonrası pencere eski bilgiyi göstermemeli.
		if what != "disable" {
			a.showVNCModal(st)
		}
		a.Invalidate()
	}()
}
