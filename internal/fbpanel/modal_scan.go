package fbpanel

import (
	"image"
	"math"
	"sync"
	"time"

	"mcos/internal/fbdraw"
	"mcos/internal/fbui"
)

// ScanModal is a picker that opens EMPTY and fills itself while a scan runs.
//
// ── Çözdüğü şikâyet ─────────────────────────────────────────────────────────
//
// Kullanıcının kelimeleriyle: "kablosuz tarama öyle olmamalı. kablosuz tara
// deyince üste bir menü gelecek ama animasyon o tarama animasyonu orada olacak
// ve canlı listelenecek bulduğunda."
//
// Eski akış: "w" → arka plandaki ekranda radar döner → tarama BİTİNCE
// (ölçülen: 4-12 sn) pencere açılır ve liste tam gelir. Yani kullanıcı
// saniyelerce hiçbir pencere görmüyor, sonra ekran birden değişiyordu.
//
// Yeni akış: "w" → pencere ANINDA açılır, içinde dönen gösterge + radar
// dalgası vardır, ağlar bulundukça satır satır düşer, tarama bitince gösterge
// yerini "n ağ bulundu" satırına bırakır.
//
// ── Neden ListModal'a bir bayrak eklenmedi ──────────────────────────────────
//
// ListModal'ın satırları KURULURKEN veriliyor ve tek bir goroutine (çizim)
// tarafından okunuyor; alanlarında kilit yok. Canlı liste, arka plandaki
// yoklama goroutine'inin aynı dilime yazması demek — yani ListModal'a
// dokunmak, fare tıklamasından tema seçimine kadar onu kullanan on kadar
// yerin hepsini kilit sözleşmesine ortak etmek olurdu. Ayrı tip, kilidi
// yalnızca gerçekten paylaşılan veride tutuyor.
type ScanModal struct {
	title string
	// hint, gösterge yanında yazan "ne arıyoruz" metni.
	hint string
	// emptyBusy tarama sürerken, emptyDone tarama bitince boş liste metni.
	emptyBusy string
	emptyDone string

	// onPick bir satır seçilince çağrılır; true dönerse pencere kapanır.
	onPick func(a *App, idx int, it ListItem) bool
	// onCancel Esc/Vazgeç ile kapatılınca çağrılır (nil olabilir).
	onCancel func(a *App)
	// onRescan "Yeniden tara" düğmesine basılınca çağrılır (nil ise düğme
	// gösterilmez).
	onRescan func(a *App, m *ScanModal)

	// ── Paylaşılan durum: arka plan goroutine'i YAZAR, çizim OKUR ──────────
	mu       sync.Mutex
	items    []ListItem
	seenAt   []time.Time // satırın listeye düştüğü an (belirme animasyonu)
	scanning bool
	errText  string
	cursor   int
	// closed, pencerenin kapandığını yoklama goroutine'ine bildirir.
	closed bool
	// peakRows, pencerenin ulaştığı en büyük satır sayısı (bkz. Size).
	peakRows int
}

// rowAppearDur, yeni satırın yerine oturma süresi.
//
// 220 ms: göz bir satırın "geldiğini" fark edecek kadar uzun, art arda üç ağ
// bulunduğunda liste sıçrıyor gibi görünmeyecek kadar kısa.
const rowAppearDur = 220 * time.Millisecond

// NewScanModal builds a live picker. It starts in the scanning state: the
// caller is expected to launch the scan right after opening it.
func NewScanModal(title, hint string,
	onPick func(a *App, idx int, it ListItem) bool) *ScanModal {
	return &ScanModal{
		title:     title,
		hint:      hint,
		emptyBusy: "Aranıyor…",
		emptyDone: "Hiçbir şey bulunamadı.",
		scanning:  true,
		onPick:    onPick,
	}
}

// WithEmpty sets the text shown when the finished scan found nothing.
func (m *ScanModal) WithEmpty(s string) *ScanModal { m.emptyDone = s; return m }

// WithCancel sets the Esc callback.
func (m *ScanModal) WithCancel(f func(a *App)) *ScanModal { m.onCancel = f; return m }

// WithRescan adds a "Yeniden tara" button.
func (m *ScanModal) WithRescan(f func(a *App, m *ScanModal)) *ScanModal {
	m.onRescan = f
	return m
}

// ── Arka plandan güncelleme ─────────────────────────────────────────────────

// Replace swaps the row set, keeping the cursor on the same item when possible
// and stamping newly-arrived rows so they can animate in.
//
// TAM LİSTE alır, fark değil: tarama kaynağı (netcfg.ScanLive) da birikimli
// küme veriyor. Fark hesabını burada yapmak, iki tarafın "yeni ne var"
// tanımının ayrışması demekti.
func (m *ScanModal) Replace(items []ListItem) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// İmleç, ADA göre korunur. İndise göre korumak, araya alfabetik olarak
	// önce gelen bir ağ düştüğünde imlecin başka bir satıra kaymasına yol
	// açardı — kullanıcı seçtiğini sanıp başka ağa bağlanırdı.
	var curLabel string
	if m.cursor >= 0 && m.cursor < len(m.items) {
		curLabel = m.items[m.cursor].Label
	}

	old := make(map[string]time.Time, len(m.items))
	for i, it := range m.items {
		if i < len(m.seenAt) {
			old[it.Label] = m.seenAt[i]
		}
	}

	now := time.Now()
	m.items = items
	m.seenAt = make([]time.Time, len(items))
	for i, it := range items {
		if t, ok := old[it.Label]; ok {
			m.seenAt[i] = t // eski satır: animasyonu baştan oynatma
		} else {
			m.seenAt[i] = now
		}
	}

	m.cursor = 0
	if curLabel != "" {
		for i, it := range items {
			if it.Label == curLabel {
				m.cursor = i
				break
			}
		}
	}
}

// SetScanning reports whether the scan is still running.
func (m *ScanModal) SetScanning(on bool) {
	m.mu.Lock()
	m.scanning = on
	m.mu.Unlock()
}

// SetError shows an error line instead of the spinner.
func (m *ScanModal) SetError(s string) {
	m.mu.Lock()
	m.errText = s
	m.scanning = false
	m.mu.Unlock()
}

// Scanning reports whether the scan is still running.
func (m *ScanModal) Scanning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.scanning
}

// Closed reports whether the dialog was dismissed; the polling goroutine uses
// this to stop touching a dialog nobody can see.
func (m *ScanModal) Closed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

func (m *ScanModal) markClosed() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
}

// ── Modal arayüzü ───────────────────────────────────────────────────────────

// Title implements Modal.
func (m *ScanModal) Title() string { return m.title }

// Size implements Modal.
//
// ── Neden "büyür ama KÜÇÜLMEZ" ──────────────────────────────────────────────
//
// İki uç da kötü:
//   - Satır sayısına göre serbestçe değişen bir pencere, her yeni ağda
//     boyut değiştirir; liste dolarken pencere zıplar.
//   - Sabit büyük bir pencere ise iki ağ bulunmuşken yarısı boş durur —
//     kullanıcı "daha çok şey bulunacak" sanır, tarama bitince de boşluk
//     öylece kalır.
//
// Ara yol: pencere en az 3, en çok 8 satırlık yer tutar ve BİR KEZ büyüdüğü
// boyun altına inmez. Yani dolarken büyür (doğal), dolduktan sonra sabit
// kalır (zıplamaz), ağlar listeden düşse bile küçülmez.
func (m *ScanModal) Size() (int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	w := 52
	for _, it := range m.items {
		n := len([]rune(it.Label)) + len([]rune(it.Detail)) + len([]rune(it.Badge)) + 16
		if n > w {
			w = n
		}
	}
	if w > 90 {
		w = 90
	}

	rows := len(m.items)
	if rows < 3 {
		rows = 3
	}
	if rows > 8 {
		rows = 8
	}
	if rows > m.peakRows {
		m.peakRows = rows
	}
	// başlık + gösterge satırı + liste + buton satırı
	return w, m.peakRows*2 + 7
}

func (m *ScanModal) visibleRows(maxRows int) (start, end int) {
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
func (m *ScanModal) Draw(a *App, r image.Rectangle) {
	u := a.ui

	m.mu.Lock()
	items := m.items
	seenAt := m.seenAt
	scanning := m.scanning
	errText := m.errText
	cursor := m.cursor
	m.mu.Unlock()

	y := r.Min.Y

	// ── Durum satırı: dönen gösterge + radar dalgası + metin ───────────────
	statusH := u.M.RowH
	cy := float64(y) + float64(statusH)/2
	cx := float64(r.Min.X) + float64(u.F.CellW)

	switch {
	case errText != "":
		u.P.FillCircle(cx, cy, float64(u.F.CellH)*0.22, u.Pal.Error)
		u.Text(r.Min.X+u.F.CellW*3, y+(statusH-u.F.CellH)/2, errText, u.Pal.Error)
	case scanning:
		// Radar dalgası göstergenin ARKASINDA: ikisi aynı merkezde, biri
		// yayılan halka biri dönen yay. Tek başına gösterge "meşgul" der,
		// dalga "arıyorum" der; kullanıcı ikincisini istedi.
		u.ScanPulse(cx, cy, float64(u.F.CellH)*0.9, a.spinFrame(), u.Pal.Accent)
		u.SpinnerAt(cx, cy, float64(u.F.CellH)*0.42, a.spinFrame(), u.Pal.Accent)
		txt := m.hint
		if n := len(items); n > 0 {
			txt += "  (" + itoa(n) + " bulundu)"
		}
		u.Text(r.Min.X+u.F.CellW*3, y+(statusH-u.F.CellH)/2, txt, u.Pal.TextDim)
	default:
		u.P.FillCircle(cx, cy, float64(u.F.CellH)*0.22, u.Pal.OK)
		u.Text(r.Min.X+u.F.CellW*3, y+(statusH-u.F.CellH)/2,
			"Tarama bitti — "+itoa(len(items))+" sonuç", u.Pal.TextDim)
	}
	y += statusH + u.M.PadY

	listTop := y
	listBottom := r.Max.Y - u.M.ButtonH - u.M.PadY
	maxRows := (listBottom - y) / u.M.RowH
	if maxRows < 1 {
		maxRows = 1
	}

	if len(items) == 0 {
		msg := m.emptyDone
		if scanning {
			msg = m.emptyBusy
			// Boş liste + iskelet satırlar: "bir şey gelecek" hissi.
			// Sabit "Aranıyor…" metni donmuş bir pencere gibi duruyordu.
			for i := 0; i < 3 && i < maxRows; i++ {
				row := image.Rect(r.Min.X, listTop+i*u.M.RowH,
					r.Max.X, listTop+(i+1)*u.M.RowH)
				u.SkeletonRow(row, a.spinFrame(), i)
			}
		} else {
			u.Text(r.Min.X, y, msg, u.Pal.TextFaint)
		}
	}

	start, end := m.visibleRows(maxRows)
	for i := start; i < end; i++ {
		it := items[i]
		row := image.Rect(r.Min.X, y, r.Max.X, y+u.M.RowH)

		// Belirme animasyonu: yeni satır aşağıdan kayarak ve soluktan
		// belirerek gelir. Hazır listede hiçbir şey oynamaz.
		var slide int
		alpha := 1.0
		if i < len(seenAt) {
			if p := float64(time.Since(seenAt[i])) / float64(rowAppearDur); p < 1 {
				e := fbdraw.EaseOutCubic(math.Max(0, p))
				slide = int(float64(u.M.RowH) * 0.45 * (1 - e))
				alpha = 0.15 + 0.85*e
			}
		}
		drawRow := row.Add(image.Pt(0, slide))

		if !it.Disabled {
			a.addZone(row, zoneModalRow, i)
		}
		if i != cursor && a.hoverModalRow(i) {
			u.HoverRow(drawRow)
		}
		cxr := u.Row(drawRow, i == cursor)
		ty := drawRow.Min.Y + (u.M.RowH-u.F.CellH)/2

		// "Şu an bağlı olan" işareti: ListModal ile aynı dil.
		u.Radio(cxr, ty, it.Current)
		cxr += u.F.CellW + u.M.Gap

		col := u.Pal.Text
		switch {
		case it.Disabled:
			col = u.Pal.TextFaint
		case i == cursor:
			col = u.Pal.Accent
		}
		u.Text(cxr, ty, it.Label, fbdraw.Alpha(col, alpha))

		bx := r.Max.X - u.M.PadX
		if it.Detail != "" {
			u.TextRight(bx, ty, it.Detail, fbdraw.Alpha(u.Pal.TextDim, alpha))
			bx -= u.TextWidth(it.Detail) + u.M.Gap*2
		}
		if it.Badge != "" {
			_, bc := u.EventColors(it.BadgeKind)
			bw := u.TextWidth(it.Badge) + u.F.CellW*2
			u.Badge(bx-bw, ty-u.F.CellH/6, it.Badge, fbdraw.Alpha(bc, alpha))
		}
		y += u.M.RowH
	}

	if end-start < len(items) {
		u.TextRight(r.Max.X, y, itoa(cursor+1)+" / "+itoa(len(items)), u.Pal.TextFaint)
	}

	by := r.Max.Y - u.M.ButtonH
	btns := []fbui.Btn{
		{Label: "Seç", Key: "Enter", Style: fbui.ButtonPrimary},
		{Label: "Vazgeç", Key: "Esc", Style: fbui.ButtonSecondary},
	}
	if m.onRescan != nil && !scanning {
		btns = append(btns, fbui.Btn{Label: "Yeniden tara", Key: "r",
			Style: fbui.ButtonSecondary})
	}
	rects := u.ButtonRow(r.Min.X, by, btns, 0)
	a.addModalButtons(rects)
	if len(rects) > 2 {
		a.addActionZone(rects[2], "scan-rescan")
	}
}

// Key implements Modal.
func (m *ScanModal) Key(a *App, key string) bool {
	m.mu.Lock()
	n := len(m.items)
	switch key {
	case "esc", "left", "h":
		m.closed = true
		m.mu.Unlock()
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
	case "r":
		scanning := m.scanning
		m.mu.Unlock()
		if m.onRescan != nil && !scanning {
			m.onRescan(a, m)
		}
		return false
	case "enter", "right", "l":
		if n == 0 {
			// Tarama sürerken Enter pencereyi KAPATMAZ: kullanıcı "seç"
			// demek istiyor, henüz seçecek bir şey yok.
			m.mu.Unlock()
			return false
		}
		it := m.items[m.cursor]
		idx := m.cursor
		if it.Disabled {
			m.mu.Unlock()
			return false
		}
		if m.onPick == nil {
			m.closed = true
			m.mu.Unlock()
			return true
		}
		m.mu.Unlock()
		done := m.onPick(a, idx, it)
		if done {
			m.markClosed()
		}
		return done
	}
	m.mu.Unlock()
	return false
}

// Cursor implements rowModal (mouse support).
func (m *ScanModal) Cursor() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cursor
}

// SetCursor implements rowModal.
func (m *ScanModal) SetCursor(i int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i >= 0 && i < len(m.items) {
		m.cursor = i
	}
}

// Items returns the row count.
func (m *ScanModal) Items() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.items)
}

// Animating implements Animated.
//
// Tarama sürerken 60 kare/sn ister (dönen gösterge ve radar dalgası yavaş
// kare hızında tökezler). Tarama bittikten sonra bile kısa bir süre "hızlı"
// kalır: son gelen satırların belirme animasyonu oynarken durmamalı.
func (m *ScanModal) Animating(fast bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.scanning {
		return true
	}
	for _, t := range m.seenAt {
		if time.Since(t) < rowAppearDur {
			return true
		}
	}
	return false
}
