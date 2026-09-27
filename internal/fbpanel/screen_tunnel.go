package fbpanel

import (
	"image"
	"sync"
	"time"

	"mcos/internal/fbfont"
	"mcos/internal/fbui"
	"mcos/internal/ipc"
	"mcos/internal/ipcclient"
	"mcos/internal/model"
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
		sub = trimLine(sub, (in.Max.X-u.M.PadX-(cx+u.F.CellW+u.M.Gap))/u.F.CellW)
		u.Text(cx+u.F.CellW+u.M.Gap, ty+u.F.CellH, sub, u.Pal.TextFaint)

		y = row.Max.Y + u.M.PadY/2
	}

	y += u.M.PadY
	u.Divider(in.Min.X, in.Max.X, y)
	y += u.M.PadY * 2

	// ── Bekleyen onay: en görünür şey bu olmalı ─────────────────────────
	progress := false // dönen "tünel açılıyor" satırı çizildi mi
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
	} else if tunnelShowAddress(pl) {
		// Arkadaşlara verilecek adres EKRANIN EN BÜYÜK YAZISI: kullanıcı bunu
		// telefonuna/sohbete elle yazacak. Normal puntoda, günlük satırlarının
		// arasında kayboluyordu.
		u.Text(in.Min.X, y, "ARKADAŞLARINIZA VERECEĞİNİZ ADRES", u.Pal.TextFaint)
		y += u.F.CellH + u.M.PadY/2
		y = a.bigText(in.Min.X+u.F.CellW, in.Max.X-u.M.PadX, y, pl.Address, u.Pal.Accent)
		y += u.M.PadY
		y = a.hint(in, y, "Minecraft → Çok Oyunculu → Sunucu Ekle → bu adresi yazın.")
		y += u.M.PadY
	} else if pl.Running && pl.Syncing && pl.TunnelError == "" {
		progress = true
		u.Spinner(in.Min.X, y, a.Spin(), u.Pal.Accent)
		msg := "Tünel açılıyor — playit adres atıyor…"
		if d := tunnelPendingDetail(pl); d != "" {
			msg += " (" + d + ")"
		}
		u.Text(in.Min.X+u.F.CellW+u.M.Gap, y,
			trimLine(msg, in.Dx()/u.F.CellW-2), u.Pal.Accent)
		y += u.F.CellH + u.M.PadY*2
	}

	// ── Hatalar ve hesap uyarıları ──────────────────────────────────────
	// Kullanıcının yapması gereken bir şey varsa (e-postayı doğrula, hesabı
	// yeniden bağla) not satırından ÖNCE ve renkli: bu metin tünelin neden
	// açılmadığının tek açıklaması.
	if pl.TunnelError != "" && y+u.F.CellH < in.Max.Y {
		u.WarnTriangle(in.Min.X, y, u.Pal.Error)
		y = u.TextWrap(in.Min.X+u.F.CellW+u.M.Gap, y, in.Dx()-u.F.CellW*2,
			pl.TunnelError, u.Pal.Error)
		y += u.M.PadY
	}
	for _, n := range pl.Notices {
		if y+u.F.CellH >= in.Max.Y || n == pl.TunnelError {
			continue
		}
		u.WarnTriangle(in.Min.X, y, u.Pal.Warn)
		y = u.TextWrap(in.Min.X+u.F.CellW+u.M.Gap, y, in.Dx()-u.F.CellW*2, n, u.Pal.Warn)
		y += u.M.PadY
	}

	// Not satırı hata/adres yokken anlamlı; hata ya da ilerleme satırı
	// varken onların kopyasıdır.
	if pl.Note != "" && !tunnelShowAddress(pl) && pl.TunnelError == "" && !progress &&
		y+u.F.CellH < in.Max.Y {
		u.Text(in.Min.X, y, trimLine(pl.Note, in.Dx()/u.F.CellW), u.Pal.TextDim)
		y += u.F.CellH + u.M.PadY
	}

	// ── Sunucu başına tünel satırları ───────────────────────────────────
	if pl.Claimed && pl.ClaimCode == "" && y+u.F.CellH*3 < in.Max.Y {
		y = a.drawTunnelServers(in, y)
	}

	// ── Ajan günlüğü ────────────────────────────────────────────────────
	if len(pl.Log) > 0 && y+u.F.CellH*3 < in.Max.Y {
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

// drawTunnelServers draws one selectable row per server plus the account's
// other tunnels, and returns the next y.
//
// Satır dizini adımlardan SONRA başlar (tunnelStepCount + i): imleç ve
// fare tıklaması aynı sayıyı kullanır, contentRows da tunnelRowCount'tan
// okur — çizim ile imleç ayrışmasın.
func (a *App) drawTunnelServers(in image.Rectangle, y int) int {
	u := a.ui
	a.mu.Lock()
	pl := a.playit
	a.mu.Unlock()
	rows := a.tunnelServerRows()

	u.Divider(in.Min.X, in.Max.X, y)
	y += u.M.PadY * 2
	u.Text(in.Min.X, y, "SUNUCULAR", u.Pal.TextFaint)
	u.TextRight(in.Max.X-u.M.PadX, y, "Enter: tünel oluştur / yenile", u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY/2

	// Sığmayan satırlar için KAYDIRMA: imleç görünmeyen bir satıra inerse
	// liste, o satır en altta görünecek kadar kayar. Eskiden sığmayan satır
	// hiç çizilmiyordu; 1024×600'de büyük adresin altında üç satır sığıyor ve
	// dördüncü sunucuda imleç kayboluyor, Enter görünmeyen bir sunucuda tünel
	// açıyordu (TestTunnelManyServersScrollToCursor).
	cur := a.Cursor()
	rowH := u.F.CellH*2 + u.M.PadY
	step := rowH + u.M.PadY/2
	fits := func(avail int) int {
		if avail < rowH {
			return 0
		}
		return (avail-rowH)/step + 1
	}
	fit := fits(in.Max.Y - y)
	if fit < len(rows) {
		fit = fits(in.Max.Y - y - u.F.CellH) // "↑↓ … sunucu daha" satırına yer
	}
	first := 0
	if ci := cur - int(tunnelStepCount); fit > 0 && ci >= fit && ci < len(rows) {
		first = ci - fit + 1
	}
	for i, sr := range rows {
		if i < first {
			continue
		}
		if y+rowH > in.Max.Y || i >= first+fit {
			break
		}
		idx := int(tunnelStepCount) + i
		row := image.Rect(in.Min.X, y, in.Max.X, y+rowH)
		cx, col := a.contentRow(row, idx)
		ty := row.Min.Y + u.M.PadY/2
		if idx == cur && a.contentFocused() {
			u.Chevron(cx-u.F.CellW, ty, 0, u.Pal.Accent)
		}

		state, sub, dot := a.tunnelRowState(sr)
		if dot == u.Pal.Accent {
			u.Spinner(cx, ty, a.Spin(), dot)
		} else {
			u.StatusDot(cx, ty, dot)
		}
		// Ad, sağdaki durum yazısına iki hücre kalana dek kesilir: sınırsız
		// uzun bir sunucu adı "açık" yazısının üstüne ve panelin dışına
		// taşıyordu (PNG'de görüldü; TestTunnelRowLabelFits).
		labelX := cx + u.F.CellW + u.M.Gap
		cols := (in.Max.X - u.M.PadX - u.TextWidth(state) - u.F.CellW*2 - labelX) / u.F.CellW
		u.Text(labelX, ty, tunnelRowLabel(sr.Name, sr.Port, cols), col)
		u.TextRight(in.Max.X-u.M.PadX, ty, state, dot)
		subCol := u.Pal.TextFaint
		if sr.Tunnel != nil && sr.Tunnel.State == ipc.PlayitTunnelOpen && pl.Running {
			subCol = u.Pal.Accent
		} else if sr.Tunnel != nil && sr.Tunnel.State == ipc.PlayitTunnelError {
			subCol = u.Pal.Error
		}
		sub = trimLine(sub, (in.Max.X-u.M.PadX-(cx+u.F.CellW+u.M.Gap))/u.F.CellW)
		u.Text(cx+u.F.CellW+u.M.Gap, ty+u.F.CellH, sub, subCol)
		y = row.Max.Y + u.M.PadY/2
	}
	if above, below := first, len(rows)-first-fit; fit > 0 && (above > 0 || below > 0) {
		msg := ""
		if above > 0 {
			msg = "↑ " + itoa(above)
		}
		if below > 0 {
			if msg != "" {
				msg += " · "
			}
			msg += "↓ " + itoa(below)
		}
		u.Text(in.Min.X+u.F.CellW, y, trimLine(msg+" sunucu daha — ok tuşlarıyla kaydırın",
			in.Dx()/u.F.CellW-2), u.Pal.TextFaint)
		y += u.F.CellH
	}

	// Hesaptaki, hiçbir MCOS sunucusuna gitmeyen tüneller (sitede başka bir
	// iş için açılmış olabilir): seçilemez, yalnızca bilgi.
	var others []ipc.PlayitTunnel
	for _, t := range pl.Tunnels {
		if !tunnelMatchesRows(t, rows) {
			others = append(others, t)
		}
	}
	if len(others) > 0 && y+u.F.CellH*2 < in.Max.Y {
		y += u.M.PadY
		u.Text(in.Min.X, y, "HESAPTAKİ DİĞER TÜNELLER", u.Pal.TextFaint)
		y += u.F.CellH + u.M.PadY/2
		for _, t := range others {
			if y+u.F.CellH > in.Max.Y {
				break
			}
			line := orDash(t.Name) + "  →  " + orDash(t.Address)
			if t.LocalPort > 0 {
				line += "  (yerel :" + itoa(t.LocalPort) + ")"
			}
			if t.State != ipc.PlayitTunnelOpen {
				line += "  " + tunnelStateLabel(t.State)
			}
			u.Text(in.Min.X+u.F.CellW, y, trimLine(line, in.Dx()/u.F.CellW-2), u.Pal.TextDim)
			y += u.F.CellH
		}
	}
	return y + u.M.PadY
}

// tunnelServerRow is one server line on the tunnel screen.
type tunnelServerRow struct {
	ServerID string
	Name     string
	Port     int
	Live     bool
	Tunnel   *ipc.PlayitTunnel
}

// tunnelServerRows lists the servers with their matching tunnel.
//
// Hiç sunucu yoksa TEK bir "varsayılan (25565)" satırı: hesabı yeni bağlayan
// kullanıcı da tüneli açıp adresi görebilsin (daemon aynı portu açar).
func (a *App) tunnelServerRows() []tunnelServerRow {
	_, servers, _ := a.Snapshot()
	a.mu.Lock()
	tunnels := append([]ipc.PlayitTunnel(nil), a.playit.Tunnels...)
	a.mu.Unlock()

	match := func(id string, port int) *ipc.PlayitTunnel {
		for i := range tunnels {
			if id != "" && tunnels[i].ServerID == id {
				return &tunnels[i]
			}
		}
		for i := range tunnels {
			if tunnels[i].LocalPort == port && tunnels[i].ServerID == "" {
				return &tunnels[i]
			}
		}
		return nil
	}
	var out []tunnelServerRow
	for _, s := range servers {
		port := s.Port
		if port <= 0 {
			port = 25565
		}
		out = append(out, tunnelServerRow{ServerID: s.ID, Name: s.Name, Port: port,
			Live:   s.State == model.StateRunning || s.State == model.StateStarting,
			Tunnel: match(s.ID, port)})
	}
	if len(out) == 0 {
		out = append(out, tunnelServerRow{Name: "Varsayılan", Port: 25565,
			Tunnel: match("", 25565)})
	}
	return out
}

func tunnelMatchesRows(t ipc.PlayitTunnel, rows []tunnelServerRow) bool {
	for _, r := range rows {
		if r.Tunnel != nil && r.Tunnel.ID == t.ID && r.Tunnel.LocalPort == t.LocalPort &&
			r.Tunnel.Name == t.Name {
			return true
		}
	}
	return false
}

// tunnelRowCount is how many selectable rows the tunnel screen has.
func (a *App) tunnelRowCount() int {
	a.mu.Lock()
	claimed := a.playit.Claimed && a.playit.ClaimCode == ""
	a.mu.Unlock()
	if !claimed {
		return int(tunnelStepCount)
	}
	return int(tunnelStepCount) + len(a.tunnelServerRows())
}

// tunnelRowState is the right-hand label, the sub-line and the dot colour.
func (a *App) tunnelRowState(sr tunnelServerRow) (string, string, colorRGBA) {
	u := a.ui
	t := sr.Tunnel
	a.mu.Lock()
	running := a.playit.Running
	a.mu.Unlock()
	switch {
	case t != nil && t.State == ipc.PlayitTunnelOpen && !running:
		// Tünel playit'te duruyor ama trafiği taşıyacak ajan kapalı.
		return "ajan kapalı", t.Address + "  (3. adımda Enter: ajanı başlat)", u.Pal.Warn
	case t == nil:
		if sr.Live {
			return "tünel yok", "Enter: bu sunucu için tünel aç.", u.Pal.TextFaint
		}
		return "tünel yok", "Enter: tünel aç (sunucu kapalıyken de açılır).", u.Pal.TextFaint
	case t.State == ipc.PlayitTunnelOpen:
		return "açık", t.Address, u.Pal.OK
	case t.State == ipc.PlayitTunnelPending:
		sub := "playit port ayırıyor…"
		if t.Detail != "" {
			sub += " (" + t.Detail + ")"
		}
		return "bekliyor", sub, u.Pal.Accent
	case t.State == ipc.PlayitTunnelCreating:
		return "açılıyor", "İstek gönderildi; playit listesine düşmesi bekleniyor…", u.Pal.Accent
	case t.State == ipc.PlayitTunnelDisabled:
		return "devre dışı", "playit kapattı: " + t.Detail, u.Pal.Warn
	default: // error
		return "açılamadı", t.Detail, u.Pal.Error
	}
}

func tunnelStateLabel(s string) string {
	switch s {
	case ipc.PlayitTunnelOpen:
		return "açık"
	case ipc.PlayitTunnelPending:
		return "(bekliyor)"
	case ipc.PlayitTunnelCreating:
		return "(açılıyor)"
	case ipc.PlayitTunnelDisabled:
		return "(devre dışı)"
	}
	return "(hata)"
}

// tunnelRowLabel is "name  :port" cut to cols runes; the port always stays.
func tunnelRowLabel(name string, port, cols int) string {
	suffix := "  :" + itoa(port)
	return trimLine(name, cols-len(suffix)) + suffix
}

// tunnelShowAddress reports whether the public address is shown large.
//
// Yalnızca ajan ÇALIŞIRKEN: durdurulan ajanın son adresi durumda kalıyor
// (tünel playit'te duruyor) ama o adrese bağlanan arkadaş sunucuya ulaşamaz.
// Eskiden 3. adım "kapalı" derken altında aynı anda "ARKADAŞLARINIZA
// VERECEĞİNİZ ADRES" büyük yazılıyordu (TestTunnelStoppedAgentNoBigAddress).
func tunnelShowAddress(pl ipcclient.PlayitStatus) bool {
	return pl.Running && pl.Address != ""
}

// tunnelPendingDetail returns the first pending tunnel's status message.
func tunnelPendingDetail(pl ipcclient.PlayitStatus) string {
	for _, t := range pl.Tunnels {
		if t.State == ipc.PlayitTunnelPending && t.Detail != "" {
			return t.Detail
		}
	}
	return ""
}

// ── Büyük yazı ──────────────────────────────────────────────────────────────

// bigFaces caches the enlarged fonts by pixel size.
//
// Font yüklemek (TTF ayrıştırma) pahalı; her karede yapmak çizimi
// yavaşlatırdı. Yüz eşzamanlı kullanıma güvenli (kendi glif önbelleği var).
var (
	bigFacesMu sync.Mutex
	bigFaces   = map[float64]*fbfont.Face{}
)

func bigFace(px float64) *fbfont.Face {
	if px > 96 {
		px = 96
	}
	bigFacesMu.Lock()
	defer bigFacesMu.Unlock()
	if f, ok := bigFaces[px]; ok {
		return f
	}
	f, err := fbfont.Load(px)
	if err != nil {
		f = nil
	}
	bigFaces[px] = f
	return f
}

// bigText draws s as large as fits between x0 and x1 (2×, 1.5×, then normal)
// and returns the y below it.
//
// Adres sığmazsa küçültülür, KESİLMEZ: yarım bir adres arkadaşa verilemez.
func (a *App) bigText(x0, x1, y int, s string, c colorRGBA) int {
	u := a.ui
	for _, k := range []float64{2, 1.5} {
		f := bigFace(u.F.SizePx * k)
		if f == nil {
			continue
		}
		big := fbui.NewUI(u.Canvas(), f, u.Pal)
		if big.TextWidth(s) <= x1-x0 {
			big.Text(x0, y, s, c)
			return y + f.CellH
		}
	}
	u.Text(x0, y, trimLine(s, (x1-x0)/u.F.CellW), c)
	return y + u.F.CellH
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
		case pl.Claimed && pl.KeyInvalid:
			return "yeniden bağlayın", "Anahtar geçersiz — Enter: hesabı yeniden bağla.",
				u.Pal.Error
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
		case pl.Running && pl.KeyInvalid:
			return "anahtar geçersiz", "Önce hesabı yeniden bağlayın (2. adım).", u.Pal.Error
		case pl.Running && pl.TunnelError != "":
			// Nedenin kendisi aşağıda kırmızı satırda; burada tekrarlamak
			// aynı cümleyi ekranda üç kez gösteriyordu (PNG'de görüldü).
			return "açılamadı", "Nedeni aşağıda; düzeltip sunucu satırında Enter'a basın.",
				u.Pal.Error
		case pl.Running:
			return "hazırlanıyor", "Tünel kendiliğinden açılıyor; adres birkaç saniyede gelir.",
				u.Pal.Accent
		case pl.Claimed:
			return "kapalı", "Enter: ajanı başlat — tünel kendiliğinden açılır.", u.Pal.TextFaint
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
		return pl.Running && pl.Address == "" && !pl.KeyInvalid && pl.TunnelError == ""
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

	if idx >= int(tunnelStepCount) {
		a.tunnelServerActivate(idx - int(tunnelStepCount))
		return
	}

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
		// Anahtar reddedildiyse "zaten bağlı" demek kullanıcıyı çıkmaza
		// sokar: tek çıkış hesabı yeniden bağlamak.
		if pl.Claimed && !pl.KeyInvalid {
			a.OpenModal(NewInfoModal("Hesap zaten bağlı", []string{
				"Bu cihaz bir playit.gg hesabına bağlı.",
				"",
				"Minecraft tüneli KENDİLİĞİNDEN açılır: ajan çalışırken",
				"MCOS, WAN'ı açık (ya da ilk) sunucunuz için tüneli",
				"playit'te oluşturur ve adresi bu ekranda gösterir.",
				"",
				"Başka bir sunucu için: aşağıdaki SUNUCULAR listesinden",
				"seçip Enter'a basın.",
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

// tunnelServerActivate creates/refreshes the tunnel of the i-th server row.
//
// RPC anında döner (daemon işi arka planda yapar); ekran ilerlemeyi
// watchTunnel ile yoklar.
func (a *App) tunnelServerActivate(i int) {
	rows := a.tunnelServerRows()
	if i < 0 || i >= len(rows) {
		return
	}
	sr := rows[i]
	a.Emit(fbui.EventBusy, sr.Name+": tünel hazırlanıyor…")
	go func() {
		msg, err := a.cl.PlayitTunnel(sr.ServerID)
		if err != nil {
			a.Fail("tünel açılamadı", err)
			return
		}
		a.Emit(fbui.EventInfo, msg)
		a.loadTunnelSection()
		a.watchTunnel()
	}()
}

// tunnelWatchers guards one status poller per App.
//
// App yapısına alan eklemek yerine paket düzeyinde: aynı ekranda art arda
// Enter'a basmak ikinci bir yoklayıcı açmasın.
var tunnelWatchers sync.Map // *App → struct{}

// tunnelWatchInterval / tunnelWatchLimit bound the progress poll.
//
// 2 sn: playit adresi saniyeler içinde gelebilir, kullanıcı ekrana bakıyor.
// Daemon API'yi kendi hızında (429'dan kaçınarak) okur; buradaki yoklama
// yalnızca yerel IPC'dir, playit'e istek atmaz.
const (
	tunnelWatchInterval = 2 * time.Second
	tunnelWatchLimit    = 5 * time.Minute
)

// watchTunnel refreshes the status while a tunnel is being prepared and the
// user is on the tunnel screen.
func (a *App) watchTunnel() {
	if a.offline() {
		return
	}
	if _, busy := tunnelWatchers.LoadOrStore(a, struct{}{}); busy {
		return
	}
	go func() {
		defer tunnelWatchers.Delete(a)
		deadline := time.Now().Add(tunnelWatchLimit)
		for time.Now().Before(deadline) {
			time.Sleep(tunnelWatchInterval)
			if a.offline() || a.Section() != SecTunnel {
				return
			}
			st, err := a.cl.Playit()
			if err != nil {
				return
			}
			prev := a.tunnelSnapshot()
			a.setPlayit(st)
			if st.Address != "" && prev.Address == "" {
				a.Emit(fbui.EventOK, "Tünel açık: "+st.Address)
			}
			if !tunnelInProgress(st) {
				return
			}
		}
	}()
}

func (a *App) tunnelSnapshot() ipcclient.PlayitStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.playit
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
	a.setPlayit(st)
	if tunnelInProgress(st) {
		a.watchTunnel()
	}
}

// tunnelInProgress reports whether the screen should keep refreshing.
//
// Yalnızca Syncing'e bakmak yetmiyordu: ajan başladığı AN gelen durum
// yanıtı eşitleyicinin ilk turundan önce olabilir (Syncing=false, adres
// yok) ve ekran "hazırlanıyor" deyip hiç yenilenmezdi.
func tunnelInProgress(st ipcclient.PlayitStatus) bool {
	return st.Running && !st.KeyInvalid && (st.Syncing || st.Address == "")
}

// setPlayit stores a fresh status.
func (a *App) setPlayit(st ipcclient.PlayitStatus) {
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
