package fbpanel

import (
	"fmt"
	"strings"

	"mcos/internal/fbui"
	"mcos/internal/ipcclient"
	"mcos/internal/remote"
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
			// QR EN ÜSTTE: elle yazmak artık istisna, kolay yol değil.
			{Label: "QR ile bağlan (telefonla okut)", Value: "qr"},
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
			if key == "qr" {
				// QR yerel olarak üretiliyor: RPC GEREKMEZ, çünkü
				// gereken her şey elimizdeki durum anlık görüntüsünde.
				app.showPairQR(st)
				return true
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

	a.OpenModal(NewListModal("SSH", "",
		append(infoRows(sshModalLines(st)), sshActions(st)...),
		func(app *App, _ int, it ListItem) bool {
			key, ok := it.Value.(string)
			if !ok {
				return false
			}
			app.runSSHAction(key)
			return true
		}))
}

// sshNoteWidth is the widest note line that fits the SSH window.
const sshNoteWidth = 46

// sshModalLines builds the explanatory rows of the SSH window.
//
// Ayrı bir işlev, çünkü sınanabilir olmalı: bu pencere kullanıcının SSH'ın
// NEDEN çalışmadığını öğrendiği TEK yer. Eskiden daemon'un "Note" alanı hiç
// gösterilmiyordu; sunucu çöktüğünde kullanıcı yalnızca "açık ama
// çalışmıyor" görüyordu, nedeni (ör. port dolu) mcosd günlüğünde kalıyordu.
func sshModalLines(st ipcclient.SSHStatus) []string {
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

	switch {
	case st.PasswordSet:
		lines = append(lines, "Parola: kurulu (kullanıcı adı: root)")
	case st.Keys > 0:
		// Daemon, kalıcı bir parola yokken parola girişini KAPATIR (imajın
		// varsayılan root parolasıyla girilmesin diye). Kullanıcı parolayla
		// denerse "Permission denied" alır; nedenini burada görmeli.
		lines = append(lines,
			"Parola: yok — yalnızca açık anahtarla girilir.",
			"(Parola girişi, parola koyunca açılır.)")
	default:
		lines = append(lines, "Parola: YOK — önce bir parola koyun.")
	}
	if st.Keys > 0 {
		lines = append(lines, fmt.Sprintf("Açık anahtar: %d adet", st.Keys))
	}

	if note := strings.TrimSpace(st.Note); note != "" {
		lines = append(lines, "")
		for _, part := range strings.Split(note, "; ") {
			lines = append(lines, sshWrap("! "+part, sshNoteWidth)...)
		}
	}
	return lines
}

// sshActions lists the selectable actions of the SSH window.
func sshActions(st ipcclient.SSHStatus) []ListItem {
	actions := []ListItem{
		{Label: "Parola koy / değiştir", Value: "password"},
	}
	switch {
	case st.Enabled:
		actions = append(actions, ListItem{Label: "SSH'ı kapat", Value: "disable"})
	case !st.PasswordSet && st.Keys == 0:
		// Parolasız ve anahtarsız açmayı daemon reddeder ("önce bir parola
		// koyun"). Kullanıcıyı hataya yürütüp geri göndermek yerine iki adımı
		// tek eylemde birleştiriyoruz.
		actions = append(actions, ListItem{Label: "Parola koy ve SSH'ı aç", Value: "password+enable"})
	default:
		actions = append(actions, ListItem{Label: "SSH'ı aç", Value: "enable"})
	}
	return actions
}

// sshWrap breaks s into lines of at most n runes, at spaces when possible.
func sshWrap(s string, n int) []string {
	var out []string
	r := []rune(s)
	indent := ""
	for len(r) > n-len([]rune(indent)) {
		w := n - len([]rune(indent))
		cut := w
		for i := w; i > w/2; i-- {
			if r[i] == ' ' {
				cut = i
				break
			}
		}
		out = append(out, indent+strings.TrimRight(string(r[:cut]), " "))
		r = []rune(strings.TrimLeft(string(r[cut:]), " "))
		// Devam satırları içeriden başlar: hangi notun devamı olduğu
		// okunabilsin.
		indent = "  "
	}
	return append(out, indent+string(r))
}

func (a *App) runSSHAction(what string) {
	switch what {
	case "password":
		a.openSSHPasswordPrompt(false)
		return
	case "password+enable":
		a.openSSHPasswordPrompt(true)
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

// openSSHPasswordPrompt asks for the shell password; enableAfter also turns
// SSH on once the password is stored.
func (a *App) openSSHPasswordPrompt(enableAfter bool) {
	a.OpenModal(NewTextModal("SSH parolası", "Yeni parola",
		func(app *App, pw string) {
			app.Emit(fbui.EventBusy, "parola ayarlanıyor…")
			// ARKA PLANDA: modal geri çağrıları ana döngüde çalışır ve
			// burada doğrudan RPC yapmak paneli dondururdu.
			go func() {
				st, err := app.cl.SSHSetPassword(pw)
				if err != nil {
					// Bağlam "SSH parolası": daemon'un iletisi zaten ne olduğunu
					// söylüyor ("parola kaydedildi ama SSH yeniden
					// başlatılamadı: ..." gibi); "ayarlanamadı" öneki bu
					// durumda yanlış olurdu.
					app.Fail("SSH parolası", err)
					return
				}
				app.Emit(fbui.EventOK, "SSH parolası ayarlandı (kalıcı)")
				if enableAfter && !st.Enabled {
					st, err = app.cl.SSHEnable(0)
					if err != nil {
						app.Fail("SSH açılamadı", err)
						return
					}
					app.Emit(fbui.EventOK, "SSH açıldı")
					app.refreshAsync()
				}
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

// showPairQR opens the QR pairing dialog.
//
// ── Neden adres BURADA seçiliyor ────────────────────────────────────────────
//
// QR'a hangi adresin gireceği, telefonun NEREDEN bağlanacağına bağlı:
//
//   - Aynı ağdaysa yerel adres en hızlısı ve en güvenilirisi.
//   - Başka bir ağdaysa yalnızca tünel adresi işe yarar.
//
// Tünel adresi VARSA o seçiliyor, çünkü kullanıcının isteği açıkça "aynı
// internette bile bağlı olmadan bağlanabilsin"di. Tünel yoksa yerel adrese
// düşülüyor ve pencere bunu AÇIKÇA yazıyor — sessizce yerel adres verip
// "neden dışarıdan bağlanamıyorum" sorusuna yol açmıyor.
func (a *App) showPairQR(st ipcclient.RemoteStatus) {
	hosts := pairHosts(st.Addresses)
	if len(hosts) == 0 {
		a.OpenModal(NewInfoModal("QR ile bağlan", []string{
			"Bu makinenin ağ adresi yok.",
			"",
			"Önce Ağ bölümünden bir bağlantı kurun.",
		}))
		return
	}

	uri := remote.PairURIHosts(hosts, st.Port, st.Token, st.Fingerprint)
	a.OpenModal(newQRModal("QR ile bağlan", uri, pairQRLines(st, hosts)))
}

// maxPairHosts bounds how many addresses go into the QR.
//
// Her adres QR'ı ~18 bayt büyütüyor; büyüyen QR daha küçük modül demek ve
// 800x600'de sığmamaya başlıyor. Üç adres, gerçek makinelerde görülen
// (kablo + Wi-Fi + bir sanal bağdaştırıcı) durumu karşılıyor.
const maxPairHosts = 3

// pairHosts orders the machine's addresses by how likely a phone reaches them.
//
// ── Neden sıralama gerekiyor ────────────────────────────────────────────────
//
// hostAddresses arayüz sırasıyla gelir. VirtualBox'ta NAT bağdaştırıcısı
// (10.0.2.15) çoğunlukla İLK sıradadır ve telefon ona HİÇBİR ZAMAN ulaşamaz:
// NAT'ın arkasındaki sanal makineye dışarıdan bağlantı gitmez. Eskiden QR
// yalnızca ilk adresi taşıdığı için kullanıcı "QR'ı okuttum, bağlanmıyor"
// yaşıyordu. Şimdi yerel ağ adresleri öne, NAT ve kendinden atanmış
// (169.254) adresler sona gidiyor; telefon sırayla deniyor.
func pairHosts(addrs []string) []string {
	var iyi, zayif []string
	for _, ad := range addrs {
		ad = strings.TrimSpace(ad)
		if ad == "" {
			continue
		}
		if isNATOnlyAddr(ad) || strings.HasPrefix(ad, "169.254.") {
			zayif = append(zayif, ad)
		} else {
			iyi = append(iyi, ad)
		}
	}
	out := append(iyi, zayif...)
	if len(out) > maxPairHosts {
		out = out[:maxPairHosts]
	}
	return out
}

// isNATOnlyAddr reports the fixed guest address of VirtualBox/QEMU user NAT.
//
// 10.0.2.0/24 her iki öykünücünün de varsayılan NAT ağı; misafir her zaman
// 10.0.2.15 alır. Bu ağdaki bir adres, telefonun erişemeyeceği bir adrestir.
func isNATOnlyAddr(ad string) bool {
	return strings.HasPrefix(ad, "10.0.2.")
}

// pairQRLines is the text shown beside the pairing QR.
//
// Değerler METİN olarak da yazılıyor: kamerası olmayan ya da QR okuyamayan
// telefon aynı pencereden elle girebilmeli.
func pairQRLines(st ipcclient.RemoteStatus, hosts []string) []string {
	satir := []string{
		"Telefonda MCOS uygulamasını açıp",
		"\"QR ile bağlan\"a dokunun.",
		"",
		fmt.Sprintf("Adres: %s   Port: %d", hosts[0], st.Port),
	}
	if len(hosts) > 1 {
		satir = append(satir, "Diğer adresler: "+strings.Join(hosts[1:], ", "))
	}
	if isNATOnlyAddr(hosts[0]) {
		// Tek adres NAT ise QR okunur ama bağlantı ASLA kurulmaz; bunu
		// şimdi söylemek, kullanıcının telefonda saatlerce aramasını önler.
		satir = append(satir, "",
			"UYARI: bu adres sanal makinenin NAT ağı —",
			"telefon ulaşamaz. VirtualBox'ta ağ",
			"bağdaştırıcısını \"Köprü\" (Bridged) yapın.")
	} else {
		satir = append(satir, "Dışarıdan bağlanmak için tünel gerekir.")
	}
	if st.Fingerprint == "" {
		// Bu DURUST olmak zorunda: parmak izi olmadan telefon karşı tarafı
		// doğrulayamaz ve jetonu körlemesine gönderir.
		satir = append(satir, "",
			"UYARI: sertifika parmak izi yok —",
			"telefon karşı tarafı doğrulayamaz.")
	}
	return satir
}
