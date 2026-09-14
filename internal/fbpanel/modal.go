package fbpanel

import (
	"image"
	"image/color"

	"mcos/internal/fbui"
)

// Bu dosya TÜM seçim pencerelerinin ortak altyapısıdır.
//
// Kullanıcının isteği: "sadece wifi secme değil herhangi bisi secme ekranı
// gelince arkası blurlanacak". Bu yüzden hiçbir ekran kendi listesini elle
// çizmez — hepsi buradaki ListModal'ı açar. Tek bir yerde tanımlı olması,
// bir ekranın yanlışlıkla bulanıklık olmadan liste göstermesini imkânsız
// kılar.

// ListItem is one selectable row in a picker dialog.
type ListItem struct {
	// Label sol tarafta, ana metin.
	Label string
	// Detail sağ tarafta, ikincil metin (boyut, sürüm, adres…).
	Detail string
	// Badge varsa sağda renkli rozet olarak çizilir.
	Badge string
	// BadgeKind rozetin rengini belirler.
	BadgeKind fbui.EventKind
	// Current = "şu an uygulanan" — radyo düğmesi dolu çizilir.
	Current bool
	// Disabled satır seçilemez (gri çizilir, Enter yoksayılır).
	Disabled bool
	// Value çağıranın kendi verisini taşıması için.
	Value any
}

// ListModal is the one and only selection dialog.
//
// Wi-Fi ağı, disk, çözünürlük, tema, Java sürümü, sunucu türü… hepsi bunu
// kullanır. Arka planın bulanıklaşması App.Draw tarafından otomatik yapılır.
type ListModal struct {
	title  string
	intro  string // liste üstünde tek satır açıklama (boş olabilir)
	items  []ListItem
	cursor int
	// onPick seçim yapıldığında çağrılır. true dönerse pencere kapanır.
	onPick func(a *App, idx int, it ListItem) bool
	// onCancel Esc ile kapatılınca çağrılır (nil olabilir).
	onCancel func(a *App)
	// empty liste boşken gösterilecek metin.
	empty string
}

// NewListModal builds a picker.
func NewListModal(title, intro string, items []ListItem,
	onPick func(a *App, idx int, it ListItem) bool) *ListModal {
	m := &ListModal{title: title, intro: intro, items: items, onPick: onPick,
		empty: "Seçenek yok."}
	// İmleci "şu an uygulanan" satıra koy: kullanıcı listeyi açtığında
	// nerede olduğunu görsün, en baştan aramasın.
	for i, it := range items {
		if it.Current {
			m.cursor = i
			break
		}
	}
	return m
}

// WithEmpty sets the text shown when the list has no rows.
func (m *ListModal) WithEmpty(s string) *ListModal { m.empty = s; return m }

// rowModal is a modal whose rows can be selected with the mouse.
//
// -- Yakalanan gercek hata -------------------------------------------------
// Tiklama yolu SOMUT tipe bakiyordu: m.(*ListModal). "Fare ve touchpad"
// penceresi ise *pointerModal'dir, yani tip donusumu basarisiz oluyor ve
// tiklama SESSIZCE dusuyordu. Pencere satirlari imlecin altinda
// isiklaniyordu (yani tiklanabilir GORUNUYORDU) ama fareyle hicbir ayar
// degistirilemiyordu -- tam da farenin ayarlandigi ekranda.
//
// Arayuze baglamak, bundan sonra eklenecek her satirli pencerenin fareyle
// calismasini KENDILIGINDEN saglar.
type rowModal interface {
	Modal
	Cursor() int
	SetCursor(int)
}

// Cursor returns the selected row index.
//
// Fare desteği için gerekli: tıklanan satır ZATEN seçiliyse ikinci tık
// onu çalıştırır (bkz. pointer.go, dispatchClick).
func (m *ListModal) Cursor() int { return m.cursor }

// SetCursor moves the selection (used when the mouse clicks a row).
func (m *ListModal) SetCursor(i int) {
	if i >= 0 && i < len(m.items) {
		m.cursor = i
	}
}

// Items returns the row count.
func (m *ListModal) Items() int { return len(m.items) }

// WithCancel sets a callback for Esc.
func (m *ListModal) WithCancel(f func(a *App)) *ListModal { m.onCancel = f; return m }

// Title implements Modal.
func (m *ListModal) Title() string { return m.title }

// Size implements Modal: size in character cells.
//
// Yükseklik satır sayısına göre büyür ama ekranı taşmaz — Modal() zaten
// kırpıyor, burada makul bir üst sınır koyuyoruz ki çok uzun listelerde
// pencere ekranı kaplamasın.
func (m *ListModal) Size() (int, int) {
	rows := len(m.items)
	if rows == 0 {
		rows = 1
	}
	if rows > 12 {
		rows = 12
	}
	// başlık + varsa açıklama + liste + alt boşluk
	h := rows*2 + 6
	if m.intro != "" {
		h += 2
	}

	w := 44
	for _, it := range m.items {
		n := len([]rune(it.Label)) + len([]rune(it.Detail)) + len([]rune(it.Badge)) + 14
		if n > w {
			w = n
		}
	}
	if n := len([]rune(m.intro)) + 6; n > w {
		w = n
	}
	if w > 90 {
		w = 90
	}
	return w, h
}

// visibleRows returns how many rows fit and the scroll offset.
//
// İmleç her zaman görünür pencerenin içinde tutulur. Eski panelde Wi-Fi
// listesi 8 satır çiziyordu ama imleç daha aşağı inebiliyordu — kullanıcı
// seçtiğini göremiyordu. Burada kaydırma ile bu imkânsız.
func (m *ListModal) visibleRows(maxRows int) (start, end int) {
	n := len(m.items)
	if n <= maxRows {
		return 0, n
	}
	start = m.cursor - maxRows/2
	if start < 0 {
		start = 0
	}
	if start+maxRows > n {
		start = n - maxRows
	}
	return start, start + maxRows
}

// Draw implements Modal.
func (m *ListModal) Draw(a *App, r image.Rectangle) {
	u := a.ui
	y := r.Min.Y

	if m.intro != "" {
		u.Text(r.Min.X, y, m.intro, u.Pal.TextDim)
		y += u.F.CellH + u.M.PadY
	}

	if len(m.items) == 0 {
		u.Text(r.Min.X, y, m.empty, u.Pal.TextFaint)
		return
	}

	// Alt buton satırı için yer ayır.
	listBottom := r.Max.Y - u.M.ButtonH - u.M.PadY
	maxRows := (listBottom - y) / u.M.RowH
	if maxRows < 1 {
		maxRows = 1
	}
	start, end := m.visibleRows(maxRows)

	for i := start; i < end; i++ {
		it := m.items[i]
		row := image.Rect(r.Min.X, y, r.Max.X, y+u.M.RowH)
		if !it.Disabled {
			a.addZone(row, zoneModalRow, i)
		}
		if i != m.cursor && a.hoverModalRow(i) {
			u.HoverRow(row)
		}
		cx := u.Row(row, i == m.cursor)
		ty := y + (u.M.RowH-u.F.CellH)/2

		u.Radio(cx, ty, it.Current)
		cx += u.F.CellW + u.M.Gap

		col := u.Pal.Text
		switch {
		case it.Disabled:
			col = u.Pal.TextFaint
		case i == m.cursor:
			col = u.Pal.Accent
		}
		u.Text(cx, ty, it.Label, col)

		// Sağdan sola: önce detay, sonra rozet.
		bx := r.Max.X - u.M.PadX
		if it.Detail != "" {
			u.TextRight(bx, ty, it.Detail, u.Pal.TextDim)
			bx -= u.TextWidth(it.Detail) + u.M.Gap*2
		}
		if it.Badge != "" {
			_, bc := u.EventColors(it.BadgeKind)
			bw := u.TextWidth(it.Badge) + u.F.CellW*2
			u.Badge(bx-bw, ty-u.F.CellH/6, it.Badge, bc)
		}
		y += u.M.RowH
	}

	// Kaydırma göstergesi: liste sığmıyorsa kullanıcı bunu bilmeli.
	if end-start < len(m.items) {
		u.TextRight(r.Max.X, y,
			itoa(m.cursor+1)+" / "+itoa(len(m.items)), u.Pal.TextFaint)
	}

	by := r.Max.Y - u.M.ButtonH
	btns := u.ButtonRow(r.Min.X, by, []fbui.Btn{
		{Label: "Seç", Key: "Enter", Style: fbui.ButtonPrimary},
		{Label: "Vazgeç", Key: "Esc", Style: fbui.ButtonSecondary},
	}, 0)
	a.addModalButtons(btns)
}

// Key implements Modal.
func (m *ListModal) Key(a *App, key string) bool {
	n := len(m.items)
	switch key {
	case "esc", "left", "h":
		if m.onCancel != nil {
			m.onCancel(a)
		}
		return true
	case "up", "k":
		if n > 0 {
			m.cursor = (m.cursor - 1 + n) % n
		}
	case "down", "j":
		if n > 0 {
			m.cursor = (m.cursor + 1) % n
		}
	case "enter", "right", "l":
		if n == 0 {
			return true
		}
		it := m.items[m.cursor]
		if it.Disabled {
			return false // seçilemez satır: pencere açık kalsın
		}
		if m.onPick == nil {
			return true
		}
		return m.onPick(a, m.cursor, it)
	}
	return false
}

// ── Onay penceresi ──────────────────────────────────────────────────────────

// ConfirmModal asks a yes/no question before a destructive action.
//
// Yıkıcı işlemler (disk silme, sunucu silme, kapatma) için. Enter'ın her yerde
// "devam" anlamına geldiği bir arayüzde, geri alınamaz bir işlemi tek Enter'a
// bağlamak tehlikelidir.
type ConfirmModal struct {
	title   string
	lines   []string
	danger  bool
	yes     string
	onYes   func(a *App)
	focused int // 0 = onayla, 1 = vazgeç
}

// NewConfirmModal builds a confirmation dialog.
//
// İmleç VAZGEÇ üzerinde başlar: yıkıcı bir işlemde varsayılan seçenek
// güvenli olan olmalıdır.
func NewConfirmModal(title string, lines []string, yesLabel string,
	danger bool, onYes func(a *App)) *ConfirmModal {
	return &ConfirmModal{title: title, lines: lines, yes: yesLabel,
		danger: danger, onYes: onYes, focused: 1}
}

// Title implements Modal.
func (m *ConfirmModal) Title() string { return m.title }

// Size implements Modal.
func (m *ConfirmModal) Size() (int, int) {
	w := 40
	for _, l := range m.lines {
		if n := len([]rune(l)) + 6; n > w {
			w = n
		}
	}
	if w > 80 {
		w = 80
	}
	return w, len(m.lines)*2 + 7
}

// Draw implements Modal.
func (m *ConfirmModal) Draw(a *App, r image.Rectangle) {
	u := a.ui
	y := r.Min.Y
	for i, l := range m.lines {
		col := u.Pal.Text
		if i == 0 && m.danger {
			col = u.Pal.Error
			// Çizilmiş uyarı üçgeni — font glifi değil.
			u.WarnTriangle(r.Min.X, y, u.Pal.Error)
			u.Text(r.Min.X+u.F.CellW+u.M.Gap, y, l, col)
			y += u.F.CellH + u.M.PadY
			continue
		}
		if i > 0 {
			col = u.Pal.TextDim
		}
		u.Text(r.Min.X, y, l, col)
		y += u.F.CellH + u.M.PadY
	}

	style := fbui.ButtonPrimary
	if m.danger {
		style = fbui.ButtonDanger
	}
	by := r.Max.Y - u.M.ButtonH
	btns := u.ButtonRow(r.Min.X, by, []fbui.Btn{
		{Label: m.yes, Key: "Enter", Style: style},
		{Label: "Vazgeç", Key: "Esc", Style: fbui.ButtonSecondary},
	}, m.focused)
	a.addModalButtons(btns)
}

// Key implements Modal.
func (m *ConfirmModal) Key(a *App, key string) bool {
	switch key {
	case "esc":
		return true
	case "confirm":
		// Fare onay düğmesine tıkladı: odağın nerede olduğuna bakma.
		if m.onYes != nil {
			m.onYes(a)
		}
		return true
	case "left", "h", "right", "l", "tab":
		m.focused = 1 - m.focused
	case "enter":
		if m.focused == 0 && m.onYes != nil {
			m.onYes(a)
		}
		return true
	}
	return false
}

// ── Bilgi penceresi ─────────────────────────────────────────────────────────

// InfoModal shows read-only text (logs, details, errors).
type InfoModal struct {
	title  string
	lines  []string
	colors []color.RGBA // satır başına renk; nil ise varsayılan
	scroll int
}

// NewInfoModal builds a read-only dialog.
func NewInfoModal(title string, lines []string) *InfoModal {
	return &InfoModal{title: title, lines: lines}
}

// Title implements Modal.
func (m *InfoModal) Title() string { return m.title }

// Size implements Modal.
func (m *InfoModal) Size() (int, int) {
	w := 46
	for _, l := range m.lines {
		if n := len([]rune(l)) + 6; n > w {
			w = n
		}
	}
	if w > 100 {
		w = 100
	}
	h := len(m.lines) + 6
	if h > 26 {
		h = 26
	}
	return w, h
}

// Draw implements Modal.
func (m *InfoModal) Draw(a *App, r image.Rectangle) {
	u := a.ui
	y := r.Min.Y
	rows := (r.Dy() - u.M.ButtonH - u.M.PadY) / u.F.CellH
	for i := m.scroll; i < len(m.lines) && i < m.scroll+rows; i++ {
		c := u.Pal.TextDim
		if m.colors != nil && i < len(m.colors) {
			c = m.colors[i]
		}
		u.Text(r.Min.X, y, m.lines[i], c)
		y += u.F.CellH
	}
	by := r.Max.Y - u.M.ButtonH
	btns := u.ButtonRow(r.Min.X, by, []fbui.Btn{
		{Label: "Kapat", Key: "Esc", Style: fbui.ButtonSecondary},
	}, 0)
	// Tek düğme var ve o da KAPAT: iptal bölgesi olarak kaydedilir.
	if len(btns) > 0 {
		a.addZone(btns[0], zoneModalCancel, 0)
	}
}

// Key implements Modal.
func (m *InfoModal) Key(a *App, key string) bool {
	switch key {
	case "esc", "enter":
		return true
	case "up", "k":
		if m.scroll > 0 {
			m.scroll--
		}
	case "down", "j":
		if m.scroll < len(m.lines)-1 {
			m.scroll++
		}
	}
	return false
}

// itoa is a tiny int-to-string without importing strconv everywhere.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// ── Fare desteği ────────────────────────────────────────────────────────────

// addModalButtons registers the standard [onayla] [vazgeç] pair.
//
// Sıra SABİT: ilk düğme birincil eylem, ikincisi iptaldir. Bütün
// pencereler ButtonRow'u bu sırayla çağırdığı için burada varsayabiliyoruz;
// bir pencere sırayı değiştirirse tıklama tersine döner, o yüzden sıra
// modal.go dışında DEĞİŞTİRİLMEMELİDİR.
func (a *App) addModalButtons(btns []image.Rectangle) {
	if len(btns) > 0 {
		a.addZone(btns[0], zoneModalPrimary, 0)
	}
	if len(btns) > 1 {
		a.addZone(btns[1], zoneModalCancel, 0)
	}
}

// hoverModalRow reports whether the mouse is over dialog row idx.
func (a *App) hoverModalRow(idx int) bool {
	z, ok := a.hoverZone()
	return ok && z.kind == zoneModalRow && z.idx == idx
}
