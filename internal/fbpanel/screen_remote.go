package fbpanel

import (
	"fmt"
	"strings"

	"mcos/internal/fbui"
	"mcos/internal/ipcclient"
)

// Bu dosya UZAKTAN KONTROL ve SSH pencerelerini çizer.
//
// ════════════════════════════════════════════════════════════════════════════
// NEDEN PANELDE OLMAK ZORUNDA
// ════════════════════════════════════════════════════════════════════════════
//
// Telefon uygulaması sunucuya bağlanmak için iki şeye ihtiyaç duyar: ADRES ve
// JETON. Jeton 128 bitlik rastgele bir değer; kullanıcı onu bir yerden okumak
// zorunda. Panelde göstermeyen bir uzaktan erişim özelliği, kurulamaz bir
// özelliktir.
//
// Aynı şey SSH için de geçerli: kullanıcı parolayı buradan koyar, yoksa
// makineye hiç giremez.

// sshDefaultPort is the standard SSH port.
const sshDefaultPort = 22

// openRemoteSettings shows the phone-app connection details.
func (a *App) openRemoteSettings() {
	if a.offline() {
		a.Emit(fbui.EventWarn, "mcosd bağlantısı yok")
		return
	}

	// Durumu ARKA PLANDA al: ipc.Client tüm çağrıları sıraya dizer ve bir
	// tarama sürüyorsa bu çağrı saniyelerce bekleyebilir. Ana döngüde
	// beklemek paneli dondururdu.
	a.Emit(fbui.EventBusy, "uzaktan kontrol durumu alınıyor…")
	go func() {
		st, err := a.cl.RemoteStatus()
		if err != nil {
			a.Fail("uzaktan kontrol durumu alınamadı", err)
			return
		}
		a.showRemoteModal(st)
	}()
}

// showRemoteModal builds the dialog from a status snapshot.
func (a *App) showRemoteModal(st ipcclient.RemoteStatus) {
	var lines []string
	var actions []ListItem

	if !st.Enabled {
		lines = []string{
			"Uzaktan kontrol KAPALI.",
			"",
			"Açtığınızda telefon uygulaması bu makineye",
			"bağlanıp sunucuları yönetebilir.",
			"",
			"Bağlantı şifrelidir (TLS) ve her istek bir",
			"jeton taşır.",
		}
		actions = []ListItem{
			{Label: "Uzaktan kontrolü aç", Value: "enable"},
		}
	} else {
		addr := "adres yok"
		if len(st.Addresses) > 0 {
			addr = st.Addresses[0]
		}
		lines = []string{
			"Telefonda MCOS uygulamasını açın ve girin:",
			"",
			"   Adres :  " + addr,
			fmt.Sprintf("   Port  :  %d", st.Port),
			"",
			"   Jeton :",
			"   " + chunkToken(st.Token, 16),
			"",
		}
		if st.Fingerprint != "" {
			// Parmak izinin İLK bölümü yeter: kullanıcı telefondakiyle
			// karşılaştıracak, tamamı pencereyi doldururdu.
			lines = append(lines,
				"Sertifika parmak izi (ilk bölüm):",
				"   "+shortText(st.Fingerprint, 29),
				"",
			)
		}
		if !st.Running {
			lines = append(lines, "UYARI: açık ama dinlemiyor.", "")
		}
		lines = append(lines,
			"Jetonu yenilerseniz bağlı telefonların",
			"erişimi kesilir.",
		)
		actions = []ListItem{
			{Label: "Jetonu yenile", Value: "rotate"},
			{Label: "Uzaktan kontrolü kapat", Value: "disable"},
		}
	}

	a.OpenModal(NewListModal("Uzaktan Kontrol", "",
		append(infoRows(lines), actions...),
		func(app *App, _ int, it ListItem) bool {
			key, ok := it.Value.(string)
			if !ok {
				return false // bilgi satırı: pencere kapanmasın
			}
			app.runRemoteAction(key)
			return true
		}))
}

// runRemoteAction performs one of the dialog's buttons.
func (a *App) runRemoteAction(what string) {
	a.Emit(fbui.EventBusy, "uygulanıyor…")
	go func() {
		var (
			st  ipcclient.RemoteStatus
			err error
		)
		switch what {
		case "enable":
			st, err = a.cl.RemoteEnable(0)
		case "disable":
			st, err = a.cl.RemoteDisable()
		case "rotate":
			st, err = a.cl.RemoteRotate()
		default:
			return
		}
		if err != nil {
			a.Fail("uzaktan kontrol", err)
			return
		}

		switch what {
		case "enable":
			a.Emit(fbui.EventOK, "Uzaktan kontrol açıldı")
		case "disable":
			a.Emit(fbui.EventInfo, "Uzaktan kontrol kapatıldı")
		case "rotate":
			a.Emit(fbui.EventOK, "Yeni jeton üretildi — telefonu yeniden bağlayın")
		}

		// Yapılandırma değişti; ayar satırının sağındaki "açık/kapalı"
		// değeri tazelensin.
		a.refreshAsync()
		if what != "disable" {
			// Yeni jetonu hemen göster: kullanıcı onu telefona yazacak.
			a.showRemoteModal(st)
		}
	}()
}

// ── SSH ─────────────────────────────────────────────────────────────────────

// openSSHSettings shows shell-access setup.
func (a *App) openSSHSettings() {
	if a.offline() {
		a.Emit(fbui.EventWarn, "mcosd bağlantısı yok")
		return
	}
	a.Emit(fbui.EventBusy, "SSH durumu alınıyor…")
	go func() {
		st, err := a.cl.SSHStatus()
		if err != nil {
			a.Fail("SSH durumu alınamadı", err)
			return
		}
		a.showSSHModal(st)
	}()
}

func (a *App) showSSHModal(st ipcclient.SSHStatus) {
	if !st.Available {
		a.OpenModal(NewListModal("SSH", "", infoRows([]string{
			"Bu MCOS imajında SSH sunucusu yok.",
			"",
			"İmajı openssh ya da dropbear ile yeniden",
			"derlemek gerekir.",
		}), nil))
		return
	}

	addr := "adres yok"
	if len(st.Addresses) > 0 {
		addr = st.Addresses[0]
	}

	var lines []string
	switch {
	case st.Running:
		lines = []string{
			"SSH AÇIK. Bilgisayarınızdan bağlanmak için:",
			"",
			fmt.Sprintf("   ssh %s@%s", st.User, addr),
		}
		if st.Port != sshDefaultPort {
			lines = append(lines,
				fmt.Sprintf("   (port %d için: -p %d ekleyin)", st.Port, st.Port))
		}
		lines = append(lines, "")
	case st.Enabled:
		lines = []string{"SSH açık ama çalışmıyor.", ""}
	default:
		lines = []string{
			"SSH KAPALI.",
			"",
			"Açarsanız bilgisayarınızdan kabuk erişimi",
			"kurabilirsiniz — panelin yapamadığı işler için.",
			"",
		}
	}

	if st.PasswordSet {
		lines = append(lines, "Parola: kurulu")
	} else {
		lines = append(lines, "Parola: YOK — parolasız giriş yapılamaz.")
	}
	if st.Keys > 0 {
		lines = append(lines, fmt.Sprintf("Açık anahtar: %d adet", st.Keys))
	}

	actions := []ListItem{
		{Label: "Parola koy / değiştir", Value: "password"},
	}
	if st.Enabled {
		actions = append(actions, ListItem{Label: "SSH'ı kapat", Value: "disable"})
	} else {
		actions = append(actions, ListItem{Label: "SSH'ı aç", Value: "enable"})
	}

	a.OpenModal(NewListModal("SSH", "",
		append(infoRows(lines), actions...),
		func(app *App, _ int, it ListItem) bool {
			key, ok := it.Value.(string)
			if !ok {
				return false
			}
			app.runSSHAction(key)
			return true
		}))
}

func (a *App) runSSHAction(what string) {
	if what == "password" {
		a.openSSHPasswordPrompt()
		return
	}
	a.Emit(fbui.EventBusy, "uygulanıyor…")
	go func() {
		var (
			st  ipcclient.SSHStatus
			err error
		)
		if what == "enable" {
			st, err = a.cl.SSHEnable(0)
		} else {
			st, err = a.cl.SSHDisable()
		}
		if err != nil {
			a.Fail("SSH", err)
			return
		}
		if what == "enable" {
			a.Emit(fbui.EventOK, "SSH açıldı")
			a.showSSHModal(st)
		} else {
			a.Emit(fbui.EventInfo, "SSH kapatıldı")
		}
		a.refreshAsync()
	}()
}

// openSSHPasswordPrompt asks for the shell password.
func (a *App) openSSHPasswordPrompt() {
	a.OpenModal(NewTextModal("SSH parolası", "Yeni parola",
		func(app *App, pw string) {
			app.Emit(fbui.EventBusy, "parola ayarlanıyor…")
			// ARKA PLANDA: modal geri çağrıları ana döngüde çalışır ve
			// burada doğrudan RPC yapmak paneli dondururdu.
			go func() {
				st, err := app.cl.SSHSetPassword(pw)
				if err != nil {
					app.Fail("parola ayarlanamadı", err)
					return
				}
				app.Emit(fbui.EventOK, "SSH parolası ayarlandı")
				app.showSSHModal(st)
			}()
		}).
		Masked().
		WithHint("En az 8 karakter. Kullanıcı adı: root").
		WithValidate(func(s string) string {
			// Daemon'un alt sınırıyla AYNI kural: arayüz kabul edip daemon
			// reddetseydi kullanıcı nedenini anlamazdı.
			if len([]rune(strings.TrimSpace(s))) < 8 {
				return "En az 8 karakter olmalı"
			}
			return ""
		}))
}

// ── Biçimlendirme ───────────────────────────────────────────────────────────

// infoRows turns explanation lines into non-selectable list rows.
//
// ── Neden liste öğesi ───────────────────────────────────────────────────────
// ListModal'ın "intro" alanı TEK satırdır; jeton, adres ve parmak izi ise
// birkaç satır tutuyor. Yeni bir pencere türü yazmak yerine açıklamayı
// seçilemez satırlar olarak çiziyoruz: aynı pencerede hem bilgi hem eylem
// olur ve imleç yalnızca gerçek eylemlerde durur.
func infoRows(lines []string) []ListItem {
	out := make([]ListItem, 0, len(lines))
	for _, l := range lines {
		out = append(out, ListItem{Label: l, Disabled: true})
	}
	return out
}

// chunkToken breaks a long secret into readable groups.
//
// 32 karakterlik bir jetonu tek parça hâlinde okuyup telefona doğru yazmak
// neredeyse imkânsız. Boşlukla ayırmak hata oranını belirgin düşürür.
func chunkToken(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i += n {
		if i > 0 {
			b.WriteByte(' ')
		}
		end := i + n
		if end > len(s) {
			end = len(s)
		}
		b.WriteString(s[i:end])
	}
	return b.String()
}

// shortText truncates with an ellipsis.
func shortText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
