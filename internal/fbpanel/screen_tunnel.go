package fbpanel

import (
	"image"
	"time"

	"mcos/internal/fbui"
)

// ════════════════════════════════════════════════════════════════════════════
// TÜNEL EKRANI (playit.gg)
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının isteği:
//
//	"playit servisini adam akıllı yaptın mı? hesaba giris yapmak felan lazım;
//	 ilk kurulumda kursun servisi, sonra giris yapalım agente, sonra siteden
//	 tünel acıp bağlayabilelim"
//
// Ekran ÜÇ ADIMDIR ve sırayla ilerler. Her adım kendi durumunu gösterir,
// böylece kullanıcı "neredeyim?" diye sormaz:
//
//	1. Ajan    — imajda kurulu mu?
//	2. Hesap   — playit.gg hesabına bağlı mı?  (telefonla tek seferlik onay)
//	3. Tünel   — ajan çalışıyor mu, adres ne?
//
// ── Neden telefon adımı atlanamıyor ─────────────────────────────────────────
// playit, ajanı bir hesaba bağlamak için hesap sahibinin tarayıcıdan onay
// vermesini ister. Bu bir eksiklik değil, hesap sahipliği doğrulamasıdır ve
// atlanamaz. Biz bunu gizlemek yerine EKRANDA AÇIKÇA söylüyoruz: kod ve
// adres büyük harflerle yazılı, yanında dönen bir bekleme göstergesi var.
//
// Ondan SONRA her şey otomatiktir: ajan kendiliğinden başlar, tünelleri
// buluttan çeker, adres ekranda belirir.

// tunnelStep identifies a row on the tunnel screen.
type tunnelStep int

const (
	stepAgent tunnelStep = iota
	stepAccount
	stepTunnel
	tunnelStepCount
)

// tunnelStepLabels are the row titles.
var tunnelStepLabels = [tunnelStepCount]string{
	stepAgent:   "1. playit ajanı",
	stepAccount: "2. playit.gg hesabı",
	stepTunnel:  "3. Tünel",
}

func (a *App) drawTunnel(r image.Rectangle) {
	u := a.ui
	in := a.contentPanel(r, "Tünel (playit)")

	a.mu.Lock()
	pl := a.playit
	a.mu.Unlock()

	y := in.Min.Y
	u.Text(in.Min.X, y,
		"Sunucunuzu port yönlendirmeden internete açar.", u.Pal.TextDim)
	y += u.F.CellH + u.M.PadY*2

	cur := a.Cursor()
	for i := tunnelStep(0); i < tunnelStepCount; i++ {
		rowH := u.F.CellH*2 + u.M.PadY
		row := image.Rect(in.Min.X, y, in.Max.X, y+rowH)
		cx, col := a.contentRow(row, int(i))
		ty := row.Min.Y + u.M.PadY/2

		if int(i) == cur && a.contentFocused() {
			u.Chevron(cx-u.F.CellW, ty, 0, u.Pal.Accent)
		}

		state, sub, dot := a.tunnelStepState(i)
		if dot == u.Pal.Accent && a.tunnelBusy(i) {
			u.Spinner(cx, ty, a.Spin(), dot)
		} else {
			u.StatusDot(cx, ty, dot)
		}
		u.Text(cx+u.F.CellW+u.M.Gap, ty, tunnelStepLabels[i], col)
		u.TextRight(in.Max.X-u.M.PadX, ty, state, dot)
		u.Text(cx+u.F.CellW+u.M.Gap, ty+u.F.CellH, sub, u.Pal.TextFaint)

		y = row.Max.Y + u.M.PadY/2
	}

	y += u.M.PadY
	u.Divider(in.Min.X, in.Max.X, y)
	y += u.M.PadY * 2

	// ── Bekleyen onay: en görünür şey bu olmalı ─────────────────────────
	if pl.ClaimCode != "" {
		// Blok tıklanabilir: kullanıcı pencereyi kapattıysa adresi yeniden
		// büyük puntoyla görmek isteyebilir.
		a.addActionZone(
			image.Rect(in.Min.X, y, in.Max.X, y+u.F.CellH*3+u.M.PadY*2),
			"playit-claim-info")
		u.Spinner(in.Min.X, y, a.Spin(), u.Pal.Accent)
		u.Text(in.Min.X+u.F.CellW+u.M.Gap, y,
			"Onay bekleniyor — telefonunuzdan açın:", u.Pal.Warn)
		y += u.F.CellH + u.M.PadY

		// Adresi BÜYÜK ve vurgulu yaz: kullanıcı bunu başka bir cihazdan
		// elle yazacak; küçük ve soluk bir satır okunmaz.
		u.Text(in.Min.X+u.F.CellW*2, y, pl.ClaimURL, u.Pal.Accent)
		y += u.F.CellH + u.M.PadY
		u.Text(in.Min.X+u.F.CellW*2, y, "Kod: "+pl.ClaimCode, u.Pal.Text)
		y += u.F.CellH + u.M.PadY*2
	} else if pl.Address != "" {
		u.Text(in.Min.X, y, "GENEL ADRES", u.Pal.TextFaint)
		y += u.F.CellH + u.M.PadY/2
		u.Text(in.Min.X+u.F.CellW, y, pl.Address, u.Pal.Accent)
		y += u.F.CellH + u.M.PadY
		a.hint(in, y, "Arkadaşlarınız bu adresle bağlanabilir.")
		y += u.F.CellH + u.M.PadY
	}

	if pl.Note != "" && y+u.F.CellH < in.Max.Y {
		u.Text(in.Min.X, y, pl.Note, u.Pal.TextDim)
		y += u.F.CellH + u.M.PadY
	}

	// ── Ajan günlüğü ────────────────────────────────────────────────────
	if len(pl.Log) > 0 && y+u.F.CellH*2 < in.Max.Y {
		u.Divider(in.Min.X, in.Max.X, y)
		y += u.M.PadY * 2
		u.Text(in.Min.X, y, "AJAN GÜNLÜĞÜ", u.Pal.TextFaint)
		y += u.F.CellH + u.M.PadY/2
		for i := len(pl.Log) - 1; i >= 0 && y+u.F.CellH <= in.Max.Y; i-- {
			u.Text(in.Min.X, y, trimLine(pl.Log[i], in.Dx()/u.F.CellW),
				u.Pal.TextFaint)
			y += u.F.CellH
		}
	}
}

// trimLine cuts a log line to the available columns.
//
// Taşan satır panelin kenarından dışarı çizilir ve komşu sütunu bozar; kesmek
// tek doğru davranıştır.
func trimLine(s string, cols int) string {
	if cols <= 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= cols {
		return s
	}
	return string(r[:cols-1]) + "…"
}

// tunnelStepState returns the right-hand status, the sub-line and the dot colour.
func (a *App) tunnelStepState(s tunnelStep) (string, string, colorRGBA) {
	u := a.ui
	a.mu.Lock()
	pl := a.playit
	a.mu.Unlock()

	switch s {
	case stepAgent:
		if pl.Installed {
			return "kurulu", "playitd + playit-cli imaja gömülü.", u.Pal.OK
		}
		return "yok", "İmaj 'make offline-bundle' ile derlenmemiş.", u.Pal.Error

	case stepAccount:
		switch {
		case pl.Claimed:
			return "bağlı", "Gizli anahtar kayıtlı; yeniden giriş gerekmez.",
				u.Pal.OK
		case pl.ClaimCode != "":
			return "onay bekleniyor", "Telefonunuzdan adresi açıp onaylayın.",
				u.Pal.Accent
		default:
			return "bağlı değil", "Enter: hesabı bağlamayı başlat.", u.Pal.Warn
		}

	default: // stepTunnel
		switch {
		case pl.Running && pl.Address != "":
			return "açık", pl.Address, u.Pal.OK
		case pl.Running:
			return "çalışıyor", "playit.gg sitesinden tünel oluşturun.", u.Pal.Accent
		case pl.Claimed:
			return "kapalı", "Enter: ajanı başlat.", u.Pal.TextFaint
		default:
			return "—", "Önce hesabı bağlayın.", u.Pal.TextFaint
		}
	}
}

// tunnelBusy reports whether a step is waiting on something.
func (a *App) tunnelBusy(s tunnelStep) bool {
	a.mu.Lock()
	pl := a.playit
	a.mu.Unlock()
	switch s {
	case stepAccount:
		return pl.ClaimCode != ""
	case stepTunnel:
		return pl.Running && pl.Address == ""
	}
	return false
}

// ── Eylemler ────────────────────────────────────────────────────────────────

// tunnelActivate performs the selected step.
func (a *App) tunnelActivate(idx int) {
	if a.offline() {
		return
	}
	a.mu.Lock()
	pl := a.playit
	a.mu.Unlock()

	switch tunnelStep(idx) {
	case stepAgent:
		go func() {
			msg, err := a.cl.PlayitInstall()
			if err != nil {
				a.Fail("playit", err)
				return
			}
			a.Emit(fbui.EventOK, msg)
			a.loadTunnelSection()
		}()

	case stepAccount:
		if pl.Claimed {
			a.OpenModal(NewInfoModal("Hesap zaten bağlı", []string{
				"Bu cihaz bir playit.gg hesabına bağlı.",
				"",
				"Tünel eklemek için playit.gg sitesindeki panelden",
				"yeni bir tünel oluşturun; ajan onu kendiliğinden görür —",
				"burada bir şey yapmanız gerekmez.",
			}))
			return
		}
		a.startPlayitClaim()

	case stepTunnel:
		if pl.Running {
			go func() {
				msg, err := a.cl.PlayitStop()
				if err != nil {
					a.Fail("ajan durdurulamadı", err)
					return
				}
				a.Emit(fbui.EventInfo, msg)
				a.loadTunnelSection()
			}()
			return
		}
		a.Emit(fbui.EventBusy, "playit ajanı başlatılıyor…")
		go func() {
			msg, err := a.cl.PlayitStart()
			if err != nil {
				a.Fail("ajan başlatılamadı", err)
				return
			}
			a.Emit(fbui.EventOK, msg)
			a.loadTunnelSection()
		}()
	}
}

// startPlayitClaim begins account binding and polls until the user approves.
//
// Yoklama ARKA PLANDA yapılır ve panel bu sırada tamamen kullanılabilir
// kalır: kullanıcı telefonuyla uğraşırken sunucusunu başlatabilmeli.
func (a *App) startPlayitClaim() {
	a.Emit(fbui.EventBusy, "playit onay kodu alınıyor…")
	go func() {
		code, url, err := a.cl.PlayitClaim()
		if err != nil {
			a.Fail("onay kodu alınamadı", err)
			return
		}
		a.mu.Lock()
		a.playit.ClaimCode = code
		a.playit.ClaimURL = url
		a.dirty = true
		a.mu.Unlock()
		a.Emit(fbui.EventWarn, "Telefonunuzdan açın: "+url)

		a.OpenModal(NewInfoModal("playit.gg hesabına bağlan", []string{
			"Telefonunuzdan veya başka bir cihazdan şu adresi açın:",
			"",
			"   " + url,
			"",
			"Kod: " + code,
			"",
			"Onayladıktan sonra bu ekran kendiliğinden ilerler;",
			"burada beklemenize gerek yok.",
		}))
		a.pollPlayitClaim()
	}()
}

// playitPollInterval is how often the pending claim is checked.
//
// 2 saniye: kullanıcının telefonda onaylaması ile ekranın değişmesi arası
// gecikme fark edilmez, ama saniyede bir RPC yapmaktan ucuzdur.
const playitPollInterval = 2 * time.Second

// playitPollLimit bounds the polling loop.
//
// 10 dakika: kullanıcı telefonunu bulamadıysa, uygulamayı sonsuza dek
// yoklatmanın anlamı yok. Süre dolunca ekranda yeniden başlatılabilir.
const playitPollLimit = 10 * time.Minute

func (a *App) pollPlayitClaim() {
	deadline := time.Now().Add(playitPollLimit)
	for time.Now().Before(deadline) {
		time.Sleep(playitPollInterval)
		if a.offline() {
			return
		}
		res, err := a.cl.PlayitPoll()
		if err != nil {
			a.Fail("onay durumu alınamadı", err)
			return
		}
		if res.Pending {
			continue
		}

		a.mu.Lock()
		a.playit.ClaimCode = ""
		a.playit.ClaimURL = ""
		a.dirty = true
		a.mu.Unlock()

		switch {
		case res.Error != "":
			a.Emit(fbui.EventError, "playit: "+res.Error)
		case res.Claimed:
			a.Emit(fbui.EventOK, "playit hesabı bağlandı")
			if res.Message != "" {
				a.Emit(fbui.EventInfo, res.Message)
			}
		}
		a.loadTunnelSection()
		return
	}
	a.mu.Lock()
	a.playit.ClaimCode = ""
	a.playit.ClaimURL = ""
	a.dirty = true
	a.mu.Unlock()
	a.Emit(fbui.EventWarn, "playit onayı zaman aşımına uğradı — tekrar deneyin")
}

// loadTunnelSection refreshes the playit status.
func (a *App) loadTunnelSection() {
	if a.offline() {
		return
	}
	st, err := a.cl.Playit()
	if err != nil {
		a.Fail("playit durumu alınamadı", err)
		return
	}
	a.mu.Lock()
	// Bekleyen onay kodunu KORU: durum çağrısı onu boş döndürebilir
	// (daemon yeniden başlatıldıysa) ama ekranda hâlâ gösteriyorsak
	// kullanıcı adresi okuyor olabilir.
	if st.ClaimCode == "" && a.playit.ClaimCode != "" {
		st.ClaimCode = a.playit.ClaimCode
		st.ClaimURL = a.playit.ClaimURL
	}
	a.playit = st
	a.dirty = true
	a.mu.Unlock()
}

// showClaimInstructions re-opens the pending account-binding instructions.
//
// Kullanıcı pencereyi kapatmış olabilir ama kodu hâlâ telefonuna yazması
// gerekiyordur. Adresi yeniden büyük puntoyla göstermek, ekrandaki küçük
// satırı okumaya çalışmaktan iyidir.
func (a *App) showClaimInstructions() {
	a.mu.Lock()
	pl := a.playit
	a.mu.Unlock()

	if pl.ClaimCode == "" {
		a.Emit(fbui.EventInfo, "Bekleyen bir onay yok")
		return
	}
	a.OpenModal(NewInfoModal("playit.gg hesabına bağlan", []string{
		"Telefonunuzdan veya başka bir cihazdan şu adresi açın:",
		"",
		"   " + pl.ClaimURL,
		"",
		"Kod: " + pl.ClaimCode,
		"",
		"Onayladıktan sonra bu ekran kendiliğinden ilerler;",
		"burada beklemenize gerek yok.",
	}))
}
