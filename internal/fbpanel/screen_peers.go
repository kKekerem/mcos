package fbpanel

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"mcos/internal/fbdraw"
	"mcos/internal/fbfont"
	"mcos/internal/fbui"
	"mcos/internal/ipcclient"
	"mcos/internal/model"
	"time"
)

// ════════════════════════════════════════════════════════════════════════════
// PC EŞLEŞTİRME EKRANI
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının isteği aynen şuydu:
//
//	"soldaki menüden pc eslestirmeye gidince oradan pc eslestirebilelim,
//	 otomatik ağda tarasın, bulamazsak ipyi girelim, ekranda tararken dönen
//	 animasyon, listelenecek listeye secenek gelince animasyon"
//
// Bu ekran tam olarak o akıştır ve ÜÇ bölümden oluşur:
//
//	1. BU MAKİNE   — kendi adresimiz ve eşleştirme anahtarımız. Öbür
//	                 makinede elle eşleştirme yapacak kullanıcı bunu görmeli.
//	2. CİHAZLAR    — bulunanların listesi. Tararken radar animasyonu döner,
//	                 sonuçlar geldiğinde liste belirir.
//	3. ORTAK DÜNYA — eşleşmiş cihazlarla aynı dünyayı çalıştırma.
//
// ── Neden eşleştirme OOBE'de değil? ─────────────────────────────────────────
// Kullanıcı bunu açıkça söyledi: "mantık hatası var, oobe de eklenmemeli,
// sunucu kurarken secilmemeli". Haklı: ilk kurulumda öbür makine HENÜZ
// KURULMAMIŞTIR — taranacak bir şey yoktur. Sunucu kurarken de seçilmemeli,
// çünkü eşleştirme sunucuya değil MAKİNEYE aittir. OOBE'de yalnızca
// "bu özellik açılsın mı?" sorusu var; eşleştirmenin kendisi burada.

// peersRowKind identifies what a row in the peers screen does.
type peersRowKind int

const (
	peerRowDevice peersRowKind = iota
	peerRowScan
	peerRowManual
	peerRowSharedWorld
	peerRowShowKey
	// peerRowEnable: PC paylaşımı KAPALIYKEN tek satır. Eskiden kapalıyken
	// de "Ağı tara" görünüyordu; basınca "kapalı — Ayarlar'dan açın" hatası
	// geliyor ve kullanıcı ekranı terk etmek zorunda kalıyordu.
	peerRowEnable
)

// peersRow is one selectable line.
type peersRow struct {
	kind peersRowKind
	idx  int // peerRowDevice: index into a.peers
}

// peersRows builds the row list: actions first, then the devices found.
//
// EYLEMLER ÜSTTE: ekrana ilk girildiğinde henüz hiç cihaz yoktur ve imleç
// "Ağı tara"nın üzerinde olmalıdır — kullanıcı Enter'a basıp işe
// başlayabilsin diye.
func (a *App) peersRows() []peersRow {
	if !a.sharingEnabled() {
		return []peersRow{{kind: peerRowEnable}}
	}
	rows := []peersRow{
		{kind: peerRowScan},
		{kind: peerRowManual},
	}
	a.mu.Lock()
	n := len(a.peers)
	a.mu.Unlock()
	for i := 0; i < n; i++ {
		rows = append(rows, peersRow{kind: peerRowDevice, idx: i})
	}
	rows = append(rows,
		peersRow{kind: peerRowSharedWorld},
		peersRow{kind: peerRowShowKey})
	return rows
}

func (a *App) drawPeers(r image.Rectangle) {
	u := a.ui
	in := a.contentPanel(r, "MCOS Paylaşım")

	a.mu.Lock()
	peers := a.peers
	ident := a.clusterID
	link := a.link
	scanNote := a.scanNote
	a.mu.Unlock()

	rows := a.peersRows()
	cur := a.Cursor()
	y := in.Min.Y

	// ── 1. Bu makine ────────────────────────────────────────────────────
	u.Text(in.Min.X, y, "BU MAKİNE", u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY/2

	self := ident.NodeName
	if self == "" {
		self = "—"
	}
	addr := ident.Address
	if addr == "" {
		addr = "adres alınamadı (ağ bağlı mı?)"
	}
	y = a.kvList(in, y, 18, [][3]any{
		{"Ad", self, u.Pal.Text},
		{"Adres", addr, u.Pal.Accent},
	})

	y += u.M.PadY
	u.Divider(in.Min.X, in.Max.X, y)
	y += u.M.PadY * 2

	// ── 2. Tarama / cihazlar ────────────────────────────────────────────
	if a.scanning() {
		// Kullanıcının istediği dönen animasyon: liste dolana kadar radar.
		note := scanNote
		if note == "" {
			note = "Ağdaki MCOS cihazları aranıyor…"
		}
		y = u.ScanBanner(in, y, a.Spin(), note,
			"Bu birkaç saniye sürebilir.")
		// Tarama sürerken bile eylem satırları çizilir ki kullanıcı
		// "IP gir"e geçebilsin.
	}

	for i, row := range rows {
		if y+u.M.RowH > in.Max.Y-u.F.CellH {
			break
		}

		// Grup başlıkları satır SIRASINI değiştirmez; yalnızca araya
		// boşluk ve etiket koyar. İmleç numaralandırması bozulmadığı için
		// klavye ile fare aynı satırı gösterir.
		if i > 0 && rows[i-1].kind != row.kind {
			switch row.kind {
			case peerRowDevice:
				y += u.M.PadY
				u.Text(in.Min.X, y, "BULUNAN CİHAZLAR", u.Pal.TextFaint)
				y += u.F.CellH + u.M.PadY/2
			case peerRowSharedWorld:
				y = a.drawPeerProblem(in, y, rows, cur, peers)
				y += u.M.PadY
				u.Divider(in.Min.X, in.Max.X, y)
				y += u.M.PadY * 2
			}
		}

		rect := image.Rect(in.Min.X, y, in.Max.X, y+u.M.RowH)
		cx, col := a.contentRow(rect, i)
		ty := y + (u.M.RowH-u.F.CellH)/2
		if i == cur && a.contentFocused() {
			u.Chevron(cx-u.F.CellW, ty, 0, u.Pal.Accent)
		}

		switch row.kind {
		case peerRowScan:
			u.Text(cx, ty, "Ağı tara", col)
			u.TextRight(in.Max.X-u.M.PadX, ty,
				fmt.Sprintf("%d cihaz", len(peers)), u.Pal.TextFaint)

		case peerRowManual:
			u.Text(cx, ty, "IP adresi gir…", col)
			u.TextRight(in.Max.X-u.M.PadX, ty, "bulunamazsa", u.Pal.TextFaint)

		case peerRowDevice:
			if row.idx >= len(peers) {
				break
			}
			p := peers[row.idx]
			dot := u.Pal.TextFaint
			if p.Paired {
				dot = u.Pal.OK
			} else if p.State == model.PeerAvailable {
				dot = u.Pal.Warn
			}
			label, lc := peerBadge(p, u.Pal.TextFaint, u.Pal.OK, u.Pal.Warn)
			if p.Problem != "" {
				dot = u.Pal.Warn
			}
			u.StatusDot(cx, ty, dot)
			x := u.Text(cx+u.F.CellW+u.M.Gap, ty, p.Name, col)
			bw := u.TextWidth(label) + u.F.CellW*2
			// Sorun varsa AYRINTININ YERİNE nedeni yaz: "EŞLEŞTİ" deyip
			// hiçbir şey olmayan bir satır, kullanıcıya hiçbir şey söylemez.
			detail, dc := peerDetail(p), u.Pal.TextFaint
			if p.Problem != "" {
				detail, dc = p.Problem, u.Pal.Warn
			}
			room := (in.Max.X - u.M.PadX - bw - u.M.Gap*3 - x) / max(u.F.CellW, 1)
			u.Text(x+u.M.Gap*2, ty, fitText(u.F, detail, room), dc)
			u.Badge(in.Max.X-u.M.PadX-bw, ty-u.F.CellH/6, label, lc)

		case peerRowSharedWorld:
			u.Text(cx, ty, "Ortak dünya…", col)
			state, sc := "kapalı", u.Pal.TextFaint
			if link.Mode == model.LinkSharedWorld {
				state, sc = "AÇIK", u.Pal.OK
			}
			bw := u.TextWidth(state) + u.F.CellW*2
			u.Badge(in.Max.X-u.M.PadX-bw, ty-u.F.CellH/6, state, sc)

		case peerRowShowKey:
			u.Text(cx, ty, "Eşleştirme anahtarını göster", col)
			u.TextRight(in.Max.X-u.M.PadX, ty, "mcos-node için", u.Pal.TextFaint)

		case peerRowEnable:
			u.Text(cx, ty, "PC paylaşımını aç", col)
			bw := u.TextWidth("KAPALI") + u.F.CellW*2
			u.Badge(in.Max.X-u.M.PadX-bw, ty-u.F.CellH/6, "KAPALI", u.Pal.Warn)
		}
		y += u.M.RowH
	}

	if !a.sharingEnabled() {
		y += u.M.PadY
		a.hint(in, y,
			"PC paylaşımı kapalı: bu MCOS eşleştirme isteklerini (port 2222) dinlemiyor.",
			"Açınca bir eşleştirme anahtarı üretilir. İkinci PC'de mcos-node'u açıp",
			"bu anahtarı girin; sonra burada 'Ağı tara' ya da 'IP adresi gir…'.")
		return
	}

	// ── 3. Ortak dünya özeti ────────────────────────────────────────────
	if link.Mode != model.LinkSharedWorld || len(link.Territories) == 0 {
		if y+u.F.CellH*3 < in.Max.Y {
			y += u.M.PadY
			u.Divider(in.Min.X, in.Max.X, y)
			y += u.M.PadY * 2
			// Ortak dünya AÇIK ama dilim yok (eş çevrimdışı: "en az iki
			// eşleşmiş cihaz gerekir"): NEDEN burada da söylenmeli. Eskiden bu
			// dal yalnızca genel ipucunu çiziyordu; sürümü 1.20.1'e çevrilip
			// modu kaldırılan bir ortak dünyada satır "AÇIK" diyor, ekranın
			// hiçbir yerinde mod sorunu görünmüyordu (ölçüldü: neden verilince
			// uyarı rengi piksel sayısı hiç değişmiyordu; bkz.
			// TestPeersModProblemShownWithoutTerritories).
			if link.Mode == model.LinkSharedWorld && a.drawLinkWarning(in, y, link) != y {
				return
			}
			a.hint(in, y,
				"Eşleşmiş cihazlar ağır işleri (yedekleme, günlük analizi) paylaşır.",
				"Ortak dünya açılırsa dünyanın bir yarısı bu cihazda, diğer yarısı",
				"eşte çalışır; oyuncu sınırı geçince otomatik aktarılır.")
		}
		return
	}

	if y+u.F.CellH*5 > in.Max.Y {
		return
	}
	y += u.M.PadY
	u.Divider(in.Min.X, in.Max.X, y)
	y += u.M.PadY * 2

	u.Text(in.Min.X, y, "ORTAK DÜNYA", u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY/2
	// Uyarı: mod yoksa ya da koordinatör bir not düştüyse ("en az iki
	// eşleşmiş cihaz gerekir"). Not eskiden hiç gösterilmiyordu: eş
	// çevrimdışıyken ortak dünya kendini kapatıyor, ekran ise "AÇIK"
	// demeye devam ediyordu.
	//
	// Uyarı ÖZETİN ÜSTÜNDE ve en fazla iki satır: 800x600'de özetin altına
	// konan iki satırlık neden panelin alt kenarından taşıyordu (PNG'de
	// görüldü). Ortak dünyanın neden çalışmadığı, aktarım sayısından önemli.
	y = a.drawLinkWarning(in, y, link)
	summary := [][3]any{
		{"Sunucu", link.ServerName, u.Pal.Text},
		{"Zorluk", model.DifficultyLabel(link.Difficulty), u.Pal.Text},
		{"Aktarım", fmt.Sprintf("%d oyuncu geçişi", link.Handoffs), u.Pal.Text},
	}
	// Tek adres: oyuncular yalnızca proxy'ye bağlanır, geçişler kopmadan olur.
	if link.ProxyAddr != "" {
		summary = append(summary, [3]any{"Tek adres", link.ProxyAddr + " (proxy)", u.Pal.OK})
	}
	// Sığmayan özet satırı ÇİZİLMEZ (kvList sınır denetlemiyor).
	fit := 0
	for ry := y; fit < len(summary) && ry+u.F.CellH <= in.Max.Y; ry += u.F.CellH + u.M.PadY/2 {
		fit++
	}
	y = a.kvList(in, y, 18, summary[:fit])
	// Katılımcılar: hangi makine açık, kaç oyuncu. Dilim çubuğu KİMİN
	// neresi olduğunu söyler; bu satır o makinenin ŞU AN çalışıp
	// çalışmadığını söyler.
	if len(link.Nodes) > 0 && y+u.F.CellH*3 < in.Max.Y {
		x := in.Min.X
		for _, n := range link.Nodes {
			c := u.Pal.OK
			if !n.Online {
				c = u.Pal.Warn
			}
			label := n.Name
			if n.Self {
				label += " (bu MCOS)"
			}
			switch {
			case !n.Online:
				label += " — çevrimdışı"
			case n.ModReady != nil && !*n.ModReady:
				// Eşleştirme portu yanıt veriyor ama sunucudaki mod vermiyor:
				// aktarım çalışmaz (bkz. model.LinkNode.ModReady).
				c = u.Pal.Warn
				label += " — mod yanıt vermiyor"
			default:
				label += fmt.Sprintf(" — %d oyuncu", n.Players)
			}
			u.StatusDot(x, y, c)
			x = u.Text(x+u.F.CellW+u.M.Gap, y, label, u.Pal.Text) + u.F.CellW*2
		}
		y += u.F.CellH + u.M.PadY
	}
	if y+u.F.CellH*2 < in.Max.Y {
		a.drawTerritories(in, y, link.Territories)
	}
}

// linkModWarning is the line shown when the shared-world mod is missing.
//
// Kullanıcının gerçek raporu: "ortak dünyayı açınca 'mcos link kurulu değil'
// diyor." Ekran yalnızca "mcos-link modu kurulu değil" yazıyordu; asıl neden
// (sunucu 26.3'tü, mod yalnızca 1.21.11 için vardı) yalnızca günlükteydi.
// Artık daemon'un verdiği NEDEN (link.status ModProblem) gösterilir.
func linkModWarning(link model.LinkStatus) string {
	if p := strings.TrimSpace(link.ModProblem); p != "" {
		return p + " — ortak dünya çalışmaz"
	}
	return "ortak dünya modu kurulu değil — ortak dünya çalışmaz"
}

// drawLinkWarning writes why the shared world does not work: the mod reason
// (ModProblem) or the coordinator's note. En fazla iki satır, sığdırılarak;
// dönüş yeni y (söylenecek bir şey yoksa aynı y).
func (a *App) drawLinkWarning(in image.Rectangle, y int, link model.LinkStatus) int {
	u := a.ui
	warn := link.Note
	if !link.ModInstalled {
		warn = linkModWarning(link)
	}
	if warn == "" {
		return y
	}
	room := (in.Dx() - u.F.CellW*2) / max(u.F.CellW, 1)
	lines := fitLines(u.F, warn, room, min(2, (in.Max.Y-y)/max(u.F.CellH, 1)))
	if len(lines) == 0 {
		return y
	}
	u.WarnTriangle(in.Min.X, y, u.Pal.Warn)
	for _, l := range lines {
		u.Text(in.Min.X+u.F.CellW+u.M.Gap, y, l, u.Pal.Warn)
		y += u.F.CellH
	}
	return y + u.M.PadY/2
}

// fitLines wraps s into at most maxLines lines of cols cells each.
//
// Neden uzun metin: "Fabric 1.20.1 için ortak dünya modu yok (desteklenen:
// 1.20.5–26.3) — ortak dünya çalışmaz" 800 piksellik ekranda tek satıra
// sığmıyor ve kırpılınca tam da DESTEKLENEN aralık kayboluyordu. Son satır
// yine taşarsa "…" ile kırpılır: metin asla ekranın dışına çıkmaz.
func fitLines(f *fbfont.Face, s string, cols, maxLines int) []string {
	if cols <= 0 || maxLines <= 0 {
		return nil
	}
	lines := wrapWords(s, cols)
	if len(lines) > maxLines {
		lines = append(lines[:maxLines-1], strings.Join(lines[maxLines-1:], " "))
	}
	for i, l := range lines {
		lines[i] = fitText(f, l, cols)
	}
	return lines
}

// sharedWorldFailureLines explains why link.enable refused.
//
// Bildirim çubuğu uzun nedeni kırpıyor (bkz. pairFailureLines); neden ve
// YAPILACAK ŞEY ayrı pencerede tam gösterilir.
func sharedWorldFailureLines(err error) []string {
	msg := err.Error()
	lines := wrapWords(msg, 58)
	low := strings.ToLower(msg)
	var todo []string
	switch {
	case strings.Contains(low, "desteklenen"):
		todo = []string{"Sunucuyu desteklenen bir Minecraft sürümüyle kurun,",
			"sonra ortak dünyayı yeniden açın."}
	case strings.Contains(low, "desteklemiyor"):
		todo = []string{"Ortak dünya için bir Fabric ya da Paper/Purpur sunucusu seçin."}
	case strings.Contains(low, "fabric-api"):
		todo = []string{"İnternete bağlanıp yeniden deneyin: fabric-api o zaman",
			"Modrinth'ten indirilir."}
	case strings.Contains(low, "bulunamadı") || strings.Contains(low, "kayıtlı ama"):
		todo = []string{"Bu MCOS'ta ortak dünya dosyaları eksik; MCOS'u güncelleyin."}
	}
	if len(todo) > 0 {
		lines = append(lines, "")
		lines = append(lines, todo...)
	}
	return lines
}

// drawPeerProblem writes the FULL reason under the list for the selected row.
//
// Satırda neden kırpılıyor: "ortak dünya kurulamadı: anahtar yanlış — o
// cihaza bu MCOS'un eşleştirme…" — asıl YAPILACAK ŞEY tam da kesilen
// kısımda. İmleç sorunlu bir cihazın üzerindeyken cümlenin tamamı burada,
// satırlara bölünmüş olarak görünür.
func (a *App) drawPeerProblem(in image.Rectangle, y int, rows []peersRow, cur int,
	peers []model.Peer) int {
	u := a.ui
	if cur < 0 || cur >= len(rows) || rows[cur].kind != peerRowDevice ||
		rows[cur].idx >= len(peers) {
		return y
	}
	p := peers[rows[cur].idx]
	if p.Problem == "" {
		return y
	}
	room := (in.Dx() - u.F.CellW*3) / max(u.F.CellW, 1)
	lines := wrapWords(p.Name+": "+p.Problem, room)
	if len(lines) > 3 {
		lines = lines[:3]
	}
	y += u.M.PadY / 2
	u.WarnTriangle(in.Min.X, y, u.Pal.Warn)
	for _, l := range lines {
		u.Text(in.Min.X+u.F.CellW+u.M.Gap, y, l, u.Pal.Warn)
		y += u.F.CellH
	}
	return y
}

// drawTerritories renders the world split as a labelled bar.
//
// Metinle ("mcos-1: x<0, mcos-2: x>=0") anlatmak, üç düğümden sonra
// okunmaz hale gelir. Çubuk, hangi makinenin dünyanın neresini tuttuğunu
// bir bakışta gösterir.
func (a *App) drawTerritories(in image.Rectangle, y int, ts []model.Territory) {
	u := a.ui
	if len(ts) == 0 {
		return
	}
	barH := u.F.CellH
	w := in.Dx()
	seg := w / len(ts)

	// Haritanın tamamı tıklanabilir: kullanıcı dilimlere bakıp "bunu
	// değiştirmek istiyorum" dediğinde en doğal hareket üstüne tıklamaktır.
	a.addActionZone(image.Rect(in.Min.X, y, in.Max.X, y+barH),
		"peers-shared-world")

	for i, t := range ts {
		x := in.Min.X + i*seg
		sw := seg
		if i == len(ts)-1 {
			sw = in.Max.X - x
		}
		// Dönüşümlü ton: bitişik iki dilim ayırt edilebilsin.
		c := u.Pal.Accent
		if i%2 == 1 {
			c = u.Pal.OK
		}
		u.P.FillRoundRect(
			rect(image.Rect(x+1, y, x+sw-1, y+barH)),
			float64(barH)/3, fbdraw.Alpha(c, 0.35))
		u.TextCenter(x, x+sw, y, t.Node, u.Pal.Text)
	}
	y += barH + u.M.PadY/2

	// Sınır etiketleri: "x < 0" / "x ≥ 0" gibi.
	for i, t := range ts {
		x := in.Min.X + i*seg
		sw := seg
		if i == len(ts)-1 {
			sw = in.Max.X - x
		}
		u.TextCenter(x, x+sw, y, territoryLabel(t), u.Pal.TextFaint)
	}
}

// territoryLabel formats a territory's chunk range in blocks.
//
// CHUNK DEĞİL BLOK gösteriyoruz: oyuncu F3 ekranında blok koordinatı görür,
// chunk değil. "x < -512" anlamlıdır; "chunkX < -32" değildir.
func territoryLabel(t model.Territory) string {
	switch {
	case t.UnboundedMin && t.UnboundedMax:
		return "tüm dünya"
	case t.UnboundedMin:
		return fmt.Sprintf("x < %d", t.MaxChunkX*16)
	case t.UnboundedMax:
		return fmt.Sprintf("x ≥ %d", t.MinChunkX*16)
	default:
		return fmt.Sprintf("%d … %d", t.MinChunkX*16, t.MaxChunkX*16)
	}
}

// ── Eylemler ────────────────────────────────────────────────────────────────

// peersActivate performs the action for the selected row.
func (a *App) peersActivate(idx int) {
	rows := a.peersRows()
	if idx < 0 || idx >= len(rows) {
		return
	}
	switch rows[idx].kind {
	case peerRowScan:
		a.startPeerScan()
	case peerRowManual:
		a.openManualPair()
	case peerRowDevice:
		a.pairPeer(rows[idx].idx)
	case peerRowSharedWorld:
		a.openSharedWorld()
	case peerRowShowKey:
		a.showPairingKey()
	case peerRowEnable:
		a.toggleSharing()
		// Anahtar ve adres, daemon eşleştirme sunucusunu açtıktan SONRA
		// dolar; ekranı kısa bir gecikmeyle tazele.
		go func() {
			time.Sleep(1500 * time.Millisecond)
			a.loadSection()
		}()
	}
}

// sharingEnabled reports whether PC pairing is switched on.
func (a *App) sharingEnabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg != nil && a.cfg.Cluster.Enabled
}

// peerBadge picks the right-hand badge for a device row.
func peerBadge(p model.Peer, faint, ok, warn color.RGBA) (string, color.RGBA) {
	switch {
	case !p.Paired:
		return "eşleşmemiş", faint
	case p.Problem != "":
		return "SORUN", warn
	case p.State != model.PeerAvailable:
		return "ÇEVRİMDIŞI", warn
	}
	return "EŞLEŞTİ", ok
}

// peerDetail is the grey detail text for a healthy device row.
func peerDetail(p model.Peer) string {
	d := fmt.Sprintf("%s · %d çekirdek · %d MB", p.IP, p.Cores, p.RAMMB)
	if p.Version != "" {
		d += " · " + p.Version
	}
	return d
}

// startPeerScan opens the live scan dialog and probes the LAN behind it.
//
// ── Neden kablosuz taramasıyla AYNI pencere ─────────────────────────────────
//
// Kullanıcının isteği iki liste için de aynıydı: "ağda tararken dönen
// animasyon listelenecek, listeye seçenek gelince animasyon; aynıları wifi
// veya bir şey listelenirken de olacak."
//
// Eskiden burada farklı bir davranış vardı: arka plandaki ekranda bir radar
// dönüyor, cihazlar ancak tarama BİTİNCE (25 saniyeye kadar) listeye
// düşüyordu. Artık pencere hemen açılıyor ve her bulunan cihaz satır satır
// geliyor — kablosuz taramasının aynısı (bkz. modal_scan.go).
func (a *App) startPeerScan() {
	if a.offline() {
		return
	}

	m := NewScanModal("MCOS Cihazları", "Ağdaki MCOS cihazları aranıyor…",
		func(app *App, _ int, it ListItem) bool {
			p, ok := it.Value.(model.Peer)
			if !ok {
				return false
			}
			// Tarama bir GÜVEN işlemi değildir: bulunan cihaz otomatik
			// eşleşmez, kullanıcı onaylar.
			app.confirmPairPeer(p)
			return true
		}).
		WithEmpty("Ağda MCOS cihazı bulunamadı — 'i' ile IP girin.").
		WithRescan(func(app *App, sm *ScanModal) { app.runPeerScan(sm) })

	a.OpenModal(m)
	a.runPeerScan(m)
}

// runPeerScan drives one scan session into the dialog.
func (a *App) runPeerScan(m *ScanModal) {
	m.SetScanning(true)
	a.setScanning(true)
	a.setScanNote("Ağdaki MCOS cihazları aranıyor…")
	a.Emit(fbui.EventBusy, "Ağ taranıyor…")
	a.Invalidate()

	go func() {
		defer func() {
			a.setScanning(false)
			a.setScanNote("")
			a.Invalidate()
		}()

		st, err := a.cl.ClusterScanStart()
		if err != nil {
			m.SetError("Tarama başlatılamadı: " + err.Error())
			a.Fail("tarama başarısız", err)
			// Tarama çalışmadıysa elle girişi ÖNER: kullanıcı çıkmaz
			// sokakta kalmamalı.
			a.Emit(fbui.EventInfo, "IP adresi girerek elle eşleştirebilirsiniz")
			return
		}
		gen := st.Gen
		m.Replace(peerItems(st.Peers))

		for {
			if m.Closed() {
				return
			}
			time.Sleep(wifiPollInterval)

			st, err = a.cl.ClusterScanStatus()
			if err != nil {
				m.SetError("Tarama durumu alınamadı: " + err.Error())
				return
			}
			if st.Gen != gen {
				m.SetScanning(false)
				return
			}
			m.Replace(peerItems(st.Peers))
			// Bulunanları ana ekrana da yaz: pencere kapanınca liste orada
			// durmalı.
			a.mu.Lock()
			a.peers = st.Peers
			a.dirty = true
			a.mu.Unlock()

			if !st.Scanning {
				m.SetScanning(false)
				if st.Error != "" {
					m.SetError(st.Error)
					a.Emit(fbui.EventWarn, "Tarama: "+st.Error)
					return
				}
				if len(st.Peers) == 0 {
					a.Emit(fbui.EventWarn,
						"Ağda MCOS cihazı bulunamadı — IP adresi girerek deneyin")
				} else {
					a.Emit(fbui.EventOK,
						fmt.Sprintf("%d cihaz bulundu", len(st.Peers)))
				}
				return
			}
		}
	}()
}

// peerItems converts peers to dialog rows.
func peerItems(peers []model.Peer) []ListItem {
	items := make([]ListItem, 0, len(peers))
	for _, p := range peers {
		badge, kind := "eşleşmemiş", fbui.EventInfo
		if p.Paired {
			badge, kind = "EŞLEŞTİ", fbui.EventOK
		}
		detail := peerDetail(p)
		if p.Problem != "" {
			badge, kind, detail = "SORUN", fbui.EventWarn, p.Problem
		}
		items = append(items, ListItem{
			Label:     p.Name,
			Detail:    detail,
			Badge:     badge,
			BadgeKind: kind,
			Current:   p.Paired,
			Value:     p,
		})
	}
	return items
}

// openManualPair asks for an address and pairs with it.
func (a *App) openManualPair() {
	if a.offline() {
		return
	}
	a.OpenModal(NewTextModal("Elle eşleştir",
		"Öbür MCOS cihazının ya da mcos-node çalışan PC'nin IP adresini girin.",
		func(app *App, addr string) { app.doManualPair(addr) }).
		WithPlaceholder("192.168.1.50").
		WithOK("Eşleştir").
		WithHint("Adres, mcos-node penceresinde ya da öbür MCOS'un bu ekranında yazar.").
		WithValidate(validatePeerAddress))
}

// validatePeerAddress rejects obviously wrong input before the RPC.
//
// Doğrulamayı BURADA yapmak, kullanıcının yazdığı metni kaybetmeden hatayı
// görmesini sağlar; RPC'ye gönderip pencereyi kapatmak, her hatada baştan
// yazdırırdı.
func validatePeerAddress(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Adres boş olamaz"
	}
	host := s
	if i := strings.LastIndex(s, ":"); i > 0 {
		host = s[:i]
		port := s[i+1:]
		if port == "" {
			return "Port eksik"
		}
		for _, r := range port {
			if r < '0' || r > '9' {
				return "Port yalnızca rakam olmalı"
			}
		}
	}
	if strings.ContainsAny(host, " \t") {
		return "Adreste boşluk olamaz"
	}
	if host == "" {
		return "Adres eksik"
	}
	return ""
}

func (a *App) doManualPair(addr string) {
	a.Emit(fbui.EventBusy, addr+" adresine bağlanılıyor…")
	go func() {
		peer, msg, needsCode, err := a.cl.ClusterPairManualCode(addr)
		if err == nil && needsCode {
			// Anahtarsız düğüm: anahtarı elle kopyalatmak yerine iki
			// ekranda aynı kodla eşleştir (bkz. startCodePairing).
			a.mu.Lock()
			a.clearBusyLocked()
			a.mu.Unlock()
			a.startCodePairing(peer)
			return
		}
		if err != nil {
			a.Fail("eşleştirilemedi", err)
			// Bildirim çubuğu uzun nedeni kırpar; NEDENİ ve ne yapılacağını
			// ayrı bir pencerede tam göster. "Eşleşme başarısız" tek başına
			// kullanıcıya hiçbir şey söylemez.
			a.OpenModal(NewInfoModal("Eşleşme kurulamadı", pairFailureLines(addr, err)))
			return
		}
		a.Emit(fbui.EventOK, msg)
		a.loadSection()
	}()
}

// pairFailureLines explains a failed manual pairing.
//
// Daemon zaten nedeni Türkçe veriyor (cluster.describeNetErr / helloProblem);
// burada o nedene göre YAPILACAK ŞEYİ ekliyoruz.
func pairFailureLines(addr string, err error) []string {
	msg := err.Error()
	// ipc hatası "kod: mesaj" biçiminde gelebilir; kullanıcı kodu görmesin.
	if i := strings.Index(msg, ": "); i > 0 && i < 12 && !strings.ContainsAny(msg[:i], " ") {
		msg = msg[i+2:]
	}
	lines := []string{addr, ""}
	lines = append(lines, wrapWords(msg, 58)...)
	lines = append(lines, "")
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "güvenlik duvarı"):
		// "kur.bat" yazıyordu: böyle bir dosya HİÇ YOKTU. Gerçek yol:
		// mcos-node ilk açılışta izni kendisi ister; atlandıysa --kur.
		lines = append(lines,
			"Windows'ta: mcos-node.exe'ye bir kez çift tıklayın — ilk açılışta",
			"güvenlik duvarı izni ister. İzin verilmediyse: mcos-node.exe --kur",
			"Linux'ta: güvenlik duvarında TCP 2222 ve 25565-25600'ü açın.")
	case strings.Contains(low, "anahtar"):
		lines = append(lines,
			"'Eşleştirme anahtarını göster' ile anahtarı görün ve o PC'de",
			"çalıştırın:  mcos-node --key <anahtar>  (çalışan düğüm yeniden başlar)")
	case strings.Contains(low, "sürüm"):
		lines = append(lines, "İki tarafı da aynı MCOS sürümüne güncelleyin.")
	case strings.Contains(low, "reddedildi"):
		lines = append(lines,
			"O bilgisayarda mcos-node açık mı? Açıksa penceresindeki",
			"adresi ve portu (ör. 192.168.1.50:2222) aynen yazın.")
	default:
		lines = append(lines, "İki cihazın aynı ağa bağlı olduğunu denetleyin.")
	}
	return lines
}

// wrapWords breaks s into lines of at most n runes, on spaces.
func wrapWords(s string, n int) []string {
	var out []string
	line := ""
	for _, w := range strings.Fields(s) {
		if line != "" && len([]rune(line))+1+len([]rune(w)) > n {
			out = append(out, line)
			line = w
			continue
		}
		if line != "" {
			line += " "
		}
		line += w
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

// showPairingKey displays the shared secret so it can be copied by hand.
func (a *App) showPairingKey() {
	a.mu.Lock()
	id := a.clusterID
	a.mu.Unlock()

	key := id.Secret
	if key == "" {
		key = "(anahtar henüz üretilmedi — PC paylaşımını açın)"
	}
	a.OpenModal(NewInfoModal("Eşleştirme anahtarı", []string{
		"İkinci PC'de mcos-node ilk açılışta bu anahtarı sorar:",
		"",
		"  " + key,
		"",
		"Adres: " + id.Address,
		"Düğüm: " + id.NodeName,
		"",
		"Anahtar yanlışsa eşleşme ANINDA reddedilir ve bu ekranda",
		"cihazın yanında 'anahtar yanlış' yazar.",
	}))
}

// ── Ortak dünya kurulumu ────────────────────────────────────────────────────

// openSharedWorld starts (or stops) the shared-world flow.
//
// AKIŞ: sunucu seç → zorluk seç → onayla. Üç adım, üç pencere. Tek bir
// devasa formda toplamak, framebuffer arayüzünde okunmaz olurdu.
func (a *App) openSharedWorld() {
	if a.offline() {
		return
	}
	a.mu.Lock()
	link := a.link
	peers := a.peers
	a.mu.Unlock()

	if link.Mode == model.LinkSharedWorld {
		a.OpenModal(NewConfirmModal("Ortak dünyayı kapat?",
			[]string{
				link.ServerName + " artık tek makinede çalışacak.",
				"Eşlerdeki yarılar durdurulur; hiçbir dünya verisi silinmez.",
				"Oyuncular yalnızca bu makinenin dilimini görür.",
			},
			"Kapat", false,
			func(app *App) {
				go func() {
					msg, err := app.cl.LinkDisable()
					if err != nil {
						app.Fail("kapatılamadı", err)
						return
					}
					app.Emit(fbui.EventOK, msg)
					app.loadSection()
				}()
			}))
		return
	}

	paired := 0
	for _, p := range peers {
		if p.Paired {
			paired++
		}
	}
	if paired == 0 {
		a.OpenModal(NewInfoModal("Önce bir cihaz eşleştirin", []string{
			"Ortak dünya için en az bir eşleşmiş MCOS cihazı gerekir.",
			"",
			"1. 'Ağı tara' ile cihazları bulun,",
			"2. bulunamazsa 'IP adresi gir…' ile elle ekleyin,",
			"3. cihaz satırında Enter'a basıp eşleştirin.",
		}))
		return
	}

	_, servers, _ := a.Snapshot()
	// Fabric (mcos-link modu) ya da Paper/Purpur (mcos-link-paper
	// eklentisi). Eskiden burada SupportsMods vardı: Paper'ı kapatıyor,
	// Fabric modunu yükleyemeyen Forge'u ise açık gösteriyordu.
	items := make([]ListItem, 0, len(servers))
	for _, s := range servers {
		it := ListItem{
			Label:  s.Name,
			Detail: string(s.Software) + " " + s.MCVersion,
			Value:  s.ID,
		}
		if s.Software != model.SoftwareFabric && !s.Software.SupportsPlugins() {
			it.Disabled = true
			it.Badge = "desteklenmiyor"
			it.BadgeKind = fbui.EventWarn
		}
		items = append(items, it)
	}
	a.OpenModal(NewListModal("Ortak dünya — sunucu seç",
		fmt.Sprintf("%d eşleşmiş cihazla paylaşılacak.", paired), items,
		func(app *App, _ int, it ListItem) bool {
			app.chooseDifficulty(it.Value.(string))
			return true
		}).WithEmpty("Önce bir Fabric ya da Paper sunucusu oluşturun."))
}

// chooseDifficulty asks for the world difficulty before enabling.
//
// Kullanıcının isteği: "hatta zorluğu bile secebilecez". Zorluk BÜTÜN
// düğümlere aynı gider — bir dilimde peaceful, ötekinde hard olsaydı
// oyuncu sınırı geçtiğinde canavarların kaybolduğunu görürdü.
func (a *App) chooseDifficulty(serverID string) {
	items := make([]ListItem, 0, len(model.AllDifficulties))
	for _, d := range model.AllDifficulties {
		items = append(items, ListItem{
			Label:   model.DifficultyLabel(d),
			Detail:  string(d),
			Current: d == model.DifficultyNormal,
			Value:   d,
		})
	}
	a.OpenModal(NewListModal("Zorluk",
		"Bütün cihazlarda aynı zorluk uygulanır.", items,
		func(app *App, _ int, it ListItem) bool {
			app.confirmSharedWorld(serverID, it.Value.(model.LinkDifficulty))
			return true
		}))
}

func (a *App) confirmSharedWorld(serverID string, diff model.LinkDifficulty) {
	a.mu.Lock()
	peers := a.peers
	a.mu.Unlock()
	names := []string{}
	for _, p := range peers {
		if p.Paired {
			names = append(names, p.Name)
		}
	}

	a.OpenModal(NewConfirmModal("Ortak dünyayı aç?",
		[]string{
			"Dünya, eşleşmiş cihazlar arasında bölünecek.",
			"Cihazlar: " + strings.Join(names, ", "),
			"Zorluk: " + model.DifficultyLabel(diff),
			"Her cihazda aynı sunucu kurulur ve başlatılır; bu sunucudaki",
			"mod/eklentiler de kopyalanır (sonradan eklenenler dahil).",
		},
		"Aç", false,
		func(app *App) {
			app.Emit(fbui.EventBusy, "Ortak dünya kuruluyor…")
			go func() {
				msg, seed, err := app.cl.LinkEnable(serverID, diff, 0, "")
				if err != nil {
					app.Fail("ortak dünya açılamadı", err)
					// Neden (ör. "Fabric 1.20.1 için ortak dünya modu yok
					// (desteklenen: 1.20.5–26.3)") bildirim çubuğunda
					// kırpılıyor: tamamı ve yapılacak şey pencerede.
					app.OpenModal(NewInfoModal("Ortak dünya açılamadı",
						sharedWorldFailureLines(err)))
					return
				}
				app.Emit(fbui.EventOK, msg)
				app.Emit(fbui.EventInfo, "Dünya tohumu: "+seed)
				app.loadSection()
			}()
		}))
}

// scanNoteText returns what the scan banner says (empty = no scan running).
func (a *App) scanNoteText() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.scanNote
}

// setScanNote records what the scan banner should say.
func (a *App) setScanNote(s string) {
	a.mu.Lock()
	a.scanNote = s
	a.dirty = true
	a.mu.Unlock()
}

// loadPeersSection fetches peers, identity and link status together.
func (a *App) loadPeersSection() {
	if a.offline() {
		return
	}
	if p, err := a.cl.ClusterPeers(); err == nil {
		a.mu.Lock()
		a.peers = p
		a.dirty = true
		a.mu.Unlock()
	}
	if id, err := a.cl.ClusterSecret(); err == nil {
		a.mu.Lock()
		a.clusterID = id
		a.dirty = true
		a.mu.Unlock()
	}
	if st, err := a.cl.LinkStatus(); err == nil {
		a.mu.Lock()
		a.link = st
		a.dirty = true
		a.mu.Unlock()
	}
}

// peersIdentity is the type stored on App (kept here so the field's meaning
// is documented next to its only user).
type peersIdentity = ipcclient.ClusterIdentity
