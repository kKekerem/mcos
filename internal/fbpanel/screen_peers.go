package fbpanel

import (
	"fmt"
	"image"
	"strings"

	"mcos/internal/fbdraw"
	"mcos/internal/fbui"
	"mcos/internal/ipcclient"
	"mcos/internal/model"
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
			u.StatusDot(cx, ty, dot)
			x := u.Text(cx+u.F.CellW+u.M.Gap, ty, p.Name, col)
			u.Text(x+u.M.Gap*2, ty,
				fmt.Sprintf("%s · %d çekirdek · %d MB", p.IP, p.Cores, p.RAMMB),
				u.Pal.TextFaint)

			label, lc := "eşleşmemiş", u.Pal.TextFaint
			if p.Paired {
				label, lc = "EŞLEŞTİ", u.Pal.OK
			}
			bw := u.TextWidth(label) + u.F.CellW*2
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
		}
		y += u.M.RowH
	}

	// ── 3. Ortak dünya özeti ────────────────────────────────────────────
	if link.Mode != model.LinkSharedWorld || len(link.Territories) == 0 {
		if y+u.F.CellH*3 < in.Max.Y {
			y += u.M.PadY
			u.Divider(in.Min.X, in.Max.X, y)
			y += u.M.PadY * 2
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
	y = a.kvList(in, y, 18, [][3]any{
		{"Sunucu", link.ServerName, u.Pal.Text},
		{"Zorluk", model.DifficultyLabel(link.Difficulty), u.Pal.Text},
		{"Aktarım", fmt.Sprintf("%d oyuncu geçişi", link.Handoffs), u.Pal.Text},
	})
	if !link.ModInstalled {
		u.WarnTriangle(in.Min.X, y, u.Pal.Warn)
		u.Text(in.Min.X+u.F.CellW+u.M.Gap, y,
			"mcos-link modu kurulu değil — ortak dünya çalışmaz", u.Pal.Warn)
		y += u.F.CellH + u.M.PadY/2
	}
	if y+u.F.CellH*2 < in.Max.Y {
		a.drawTerritories(in, y, link.Territories)
	}
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
	}
}

// startPeerScan runs an active LAN scan with the radar animation.
func (a *App) startPeerScan() {
	if a.offline() {
		return
	}
	if a.scanning() {
		return // zaten sürüyor; ikinci tarama ağı boşuna yorar
	}
	a.setScanning(true)
	a.setScanNote("Ağdaki MCOS cihazları aranıyor…")
	a.Emit(fbui.EventBusy, "Ağ taranıyor…")

	go func() {
		peers, err := a.cl.ClusterScan()
		a.setScanning(false)
		a.setScanNote("")
		if err != nil {
			a.Fail("tarama başarısız", err)
			// Tarama çalışmadıysa elle girişi ÖNER: kullanıcı çıkmaz
			// sokakta kalmamalı.
			a.Emit(fbui.EventInfo, "IP adresi girerek elle eşleştirebilirsiniz")
			return
		}
		a.mu.Lock()
		a.peers = peers
		a.dirty = true
		a.mu.Unlock()
		// Kullanıcının isteği: "listeye seçenek gelince animasyon".
		// Radar ekranından listeye geçiş soluklaşarak olur.
		a.beginTransition(transFade)
		if len(peers) == 0 {
			a.Emit(fbui.EventWarn,
				"Ağda MCOS cihazı bulunamadı — IP adresi girerek deneyin")
			return
		}
		a.Emit(fbui.EventOK, fmt.Sprintf("%d cihaz bulundu", len(peers)))
	}()
}

// openManualPair asks for an address and pairs with it.
func (a *App) openManualPair() {
	if a.offline() {
		return
	}
	a.OpenModal(NewTextModal("Elle eşleştir",
		"Öbür MCOS cihazının IP adresini girin.",
		func(app *App, addr string) { app.doManualPair(addr) }).
		WithPlaceholder("192.168.1.50").
		WithOK("Eşleştir").
		WithHint("Adresi öbür cihazın MCOS Paylaşım ekranında görebilirsiniz.").
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
		_, msg, err := a.cl.ClusterPairManual(addr)
		if err != nil {
			a.Fail("eşleştirilemedi", err)
			return
		}
		a.Emit(fbui.EventOK, msg)
		a.loadSection()
	}()
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
		"Bu anahtar İKİ makinede de AYNI olmalıdır.",
		"",
		"  " + key,
		"",
		"Adres: " + id.Address,
		"Düğüm: " + id.NodeName,
		"",
		"Anahtar eşleşmezse eşleştirme kurulur ama görevler ve",
		"ortak dünya kurulumu karşı tarafça reddedilir.",
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
				"Eşlerdeki kopyalar durdurulmaz; dünya verisi silinmez.",
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
	// YALNIZCA mod yükleyen sürümler: ortak dünya bir moda dayanır.
	items := make([]ListItem, 0, len(servers))
	for _, s := range servers {
		it := ListItem{
			Label:  s.Name,
			Detail: string(s.Software) + " " + s.MCVersion,
			Value:  s.ID,
		}
		if !s.Software.SupportsMods() {
			it.Disabled = true
			it.Badge = "mod yok"
			it.BadgeKind = fbui.EventWarn
		}
		items = append(items, it)
	}
	a.OpenModal(NewListModal("Ortak dünya — sunucu seç",
		fmt.Sprintf("%d eşleşmiş cihazla paylaşılacak.", paired), items,
		func(app *App, _ int, it ListItem) bool {
			app.chooseDifficulty(it.Value.(string))
			return true
		}).WithEmpty("Önce bir Fabric sunucusu oluşturun."))
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
			"Her cihazda aynı sunucu kurulacak ve mcos-link modu yüklenecek.",
		},
		"Aç", false,
		func(app *App) {
			app.Emit(fbui.EventBusy, "Ortak dünya kuruluyor…")
			go func() {
				msg, seed, err := app.cl.LinkEnable(serverID, diff, 0, "")
				if err != nil {
					app.Fail("ortak dünya açılamadı", err)
					return
				}
				app.Emit(fbui.EventOK, msg)
				app.Emit(fbui.EventInfo, "Dünya tohumu: "+seed)
				app.loadSection()
			}()
		}))
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
