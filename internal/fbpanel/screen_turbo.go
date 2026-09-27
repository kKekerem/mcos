package fbpanel

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

	"mcos/internal/fbui"
	"mcos/internal/model"
)

// ── Turbo: GERÇEKTE ne yapıyor ──────────────────────────────────────────────
//
// Eskiden bu satır yalnızca "Turbo AÇIK" yazıyordu; altındaki açıklama da
// "kaynak sınırları yok sayılır" diyordu. Gerçek PC'de işlemci 400 MHz'de,
// fanlar sessizde kalırken ekran "AÇIK" gösteriyordu. Artık daemon'un madde
// madde bildirdiği sonuç (frekans yöneticisi, EPP, turbo boost, fanlar,
// P-çekirdekleri) ve çekirdek başına ANLIK frekans gösteriliyor; bir kol bu
// makinede yoksa "desteklenmiyor" diye görünür.

// turboItemColor madde durumunu renge çevirir.
func (a *App) turboItemColor(state string) color.RGBA {
	u := a.ui
	switch state {
	case model.TurboOK:
		return u.Pal.OK
	case model.TurboPartial:
		return u.Pal.Warn
	case model.TurboFailed:
		return u.Pal.Error
	case model.TurboInfo:
		return u.Pal.TextDim
	default:
		return u.Pal.TextFaint
	}
}

// drawTurboBlock Performans ekranındaki turbo anahtarını ve ayrıntısını
// çizer; bir sonraki boş y'yi döner. bottom'un altına taşmaz.
func (a *App) drawTurboBlock(in image.Rectangle, y, bottom int, st *model.SystemStatus, cfg *model.Config) int {
	u := a.ui
	on := cfg != nil && cfg.Turbo
	ts := st.Turbo

	row := image.Rect(in.Min.X, y, in.Max.X, y+u.M.RowH)
	cx, _ := a.contentRow(row, 0)
	ty := y + (u.M.RowH-u.F.CellH)/2
	u.Check(cx, ty, on)
	txt, col := "Turbo kapalı", u.Pal.TextDim
	if on {
		txt, col = "Turbo AÇIK", u.Pal.Warn
	}
	x := u.Text(cx+u.F.CellW+u.M.Gap, ty, txt, col)
	if on && ts != nil && ts.PinnedThreads > 0 {
		u.Text(x+u.M.Gap*2, ty, fmt.Sprintf("%d iş parçacığı P-çekirdeklerinde", ts.PinnedThreads), u.Pal.TextFaint)
	}
	y += u.M.RowH + u.M.PadY/2

	cols := (in.Dx() - u.M.PadX) / u.F.CellW
	line := func(s string, c color.RGBA) bool {
		if y+u.F.CellH > bottom {
			return false
		}
		u.Text(in.Min.X, y, fitText(u.F, s, cols), c)
		y += u.F.CellH
		return true
	}

	switch {
	case ts == nil:
		// Eski daemon: ayrıntı yok, en azından ne yapılamadığını söyle.
		line("Turbo ayrıntısı alınamadı (daemon eski sürüm). t veya Enter ile değiştirin.", u.Pal.TextFaint)
		return y
	case !on:
		if ts.Supported {
			line("Açınca: işlemci tam frekans, fanlar son güç, sunucular "+pcoreWord(ts)+".", u.Pal.TextFaint)
		} else {
			line("Bu makinede frekans/fan denetimi yok (sanal makine?) — turbo yalnızca", u.Pal.TextFaint)
			line("öncelik ve çekirdek sabitlemesi yapabilir.", u.Pal.TextFaint)
		}
		line("t tuşu veya Enter ile değiştirin · d: turbo tanısı (neden tam hızda değil?)", u.Pal.TextFaint)
		return y
	}

	// Açık: tek satır özet (kullanıcının istediği biçim), sonra maddeler.
	for _, l := range u.WrapLines(ts.Summary, in.Dx()-u.M.PadX) {
		if !line(l, u.Pal.Warn) {
			return y
		}
	}
	y += u.M.PadY / 2
	nameCols := 22
	for _, it := range ts.Items {
		if y+u.F.CellH > bottom {
			break
		}
		u.StatusDot(in.Min.X, y, a.turboItemColor(it.State))
		nx := in.Min.X + u.F.CellW + u.M.Gap
		u.Text(nx, y, fitText(u.F, it.Name, nameCols-1), u.Pal.TextDim)
		dx := nx + u.F.CellW*nameCols
		detail := it.Detail
		if it.State == model.TurboUnsupported || it.State == model.TurboFailed {
			detail = it.State + ": " + detail
		}
		u.Text(dx, y, fitText(u.F, detail, (in.Max.X-u.M.PadX-dx)/u.F.CellW), a.turboItemColor(it.State))
		y += u.F.CellH
	}
	line("d: turbo tanısı — ölçülen MHz, PL1/PL2, sıcaklık, fan (ekran görüntüsü için)", u.Pal.TextFaint)
	return y
}

// ── Turbo tanısı penceresi ──────────────────────────────────────────────────
//
// Gerçek PC'de turbo "4,4 GHz olması lazımken 2,4 GHz" verdi, fan dönmedi;
// Performans ekranı her kolu "tamam" gösteriyordu ve SEBEP hiçbir yerde
// yoktu. Bu pencere daemon'un donanımdan geri okuduğu tanıyı (bkz.
// internal/turbo/diag.go) ekranın tamamına yayar: kullanıcı tek bir ekran
// görüntüsüyle sürücüyü, ölçülen frekansı, güç sınırlarını, kısıtlama
// sayaçlarını, sıcaklığı ve fan geri okumalarını gönderebilsin. İçerik her
// durum yenilemesinde canlı güncellenir (daemon 15 sn'de bir ölçer).
type turboDiagModal struct {
	scroll int
	rows   int // son çizimde sığan satır (sayfa kaydırma için)
}

func (a *App) openTurboDiag() { a.OpenModal(&turboDiagModal{}) }

func (m *turboDiagModal) Title() string { return "Turbo tanısı" }

// Size büyük ister; fbui.Modal pencereyi ekrana sığdırır.
func (m *turboDiagModal) Size() (int, int) { return 170, 70 }

type diagLine struct {
	s string
	c color.RGBA
}

func (a *App) turboDiagLines(width int) []diagLine {
	u := a.ui
	st, _, _ := a.Snapshot()
	var out []diagLine
	put := func(s string, c color.RGBA) {
		for _, l := range u.WrapLines(s, width) {
			out = append(out, diagLine{l, c})
		}
	}
	if st == nil || st.Turbo == nil {
		put("Tanı alınamadı: daemon bağlı değil ya da eski sürüm.", u.Pal.Warn)
		return out
	}
	ts := st.Turbo
	put("Özet: "+ts.Summary, u.Pal.Warn)
	if ts.DiagAt > 0 {
		put("Ölçüm: "+time.Unix(ts.DiagAt, 0).Format("15:04:05")+" · günlük: /data/log/turbo.log", u.Pal.TextFaint)
	}
	if ts.Active {
		for _, it := range ts.Items {
			put("● "+it.Name+": "+it.Detail, a.turboItemColor(it.State))
		}
	}
	put("", u.Pal.TextFaint)
	if len(ts.Diag) == 0 {
		put("Tanı henüz ölçülmedi (daemon 15 sn içinde ölçer).", u.Pal.TextFaint)
	}
	for _, it := range ts.Diag {
		pre := "  "
		if it.State == model.TurboPartial || it.State == model.TurboFailed || it.State == model.TurboUnsupported {
			pre = "! "
		}
		put(pre+it.Name+": "+it.Detail, a.turboItemColor(it.State))
	}
	return out
}

// Draw implements Modal.
func (m *turboDiagModal) Draw(a *App, r image.Rectangle) {
	u := a.ui
	lines := a.turboDiagLines(r.Dx())
	rows := (r.Dy() - u.M.ButtonH - u.M.PadY) / u.F.CellH
	if rows < 1 {
		rows = 1
	}
	m.rows = rows
	if max := len(lines) - rows; m.scroll > max {
		m.scroll = max
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	y := r.Min.Y
	for i := m.scroll; i < len(lines) && i < m.scroll+rows; i++ {
		u.Text(r.Min.X, y, lines[i].s, lines[i].c)
		y += u.F.CellH
	}
	by := r.Max.Y - u.M.ButtonH
	btns := u.ButtonRow(r.Min.X, by, []fbui.Btn{
		{Label: "Kapat", Key: "Esc", Style: fbui.ButtonSecondary},
	}, 0)
	if len(btns) > 0 {
		a.addZone(btns[0], zoneModalCancel, 0)
	}
	if len(lines) > rows {
		u.TextRight(r.Max.X, by+(u.M.ButtonH-u.F.CellH)/2,
			fmt.Sprintf("↑↓ kaydır · %d-%d / %d", m.scroll+1, min(m.scroll+rows, len(lines)), len(lines)), u.Pal.TextFaint)
	}
}

// Key implements Modal.
func (m *turboDiagModal) Key(a *App, key string) bool {
	page := m.rows - 1
	if page < 1 {
		page = 1
	}
	switch key {
	case "esc", "enter", "d", "q":
		return true
	case "up", "k":
		m.scroll--
	case "down", "j":
		m.scroll++
	case "pgup":
		m.scroll -= page
	case "pgdown", "space", " ":
		m.scroll += page
	case "home":
		m.scroll = 0
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	return false
}

func pcoreWord(ts *model.TurboStatus) string {
	if ts.Hybrid {
		return "P-çekirdeklerinde (" + ts.PCores + ")"
	}
	return "tüm çekirdeklerde"
}

// drawCoreMHz çekirdek başına anlık frekansı ızgara olarak çizer. Hibrit
// işlemcide P ve E çekirdekleri harfle ayrılır ki turbonun sunucuyu nereye
// koyduğu ve o çekirdeklerin gerçekten yükseldiği görülsün.
func (a *App) drawCoreMHz(in image.Rectangle, y, bottom int, st *model.SystemStatus) int {
	u := a.ui
	mhz := st.CPU.CoreMHz
	if len(mhz) == 0 || y+u.F.CellH*2 > bottom {
		return y
	}
	eSet := map[int]bool{}
	if st.Turbo != nil && st.Turbo.Hybrid {
		for _, c := range parseCPUList(st.Turbo.ECores) {
			eSet[c] = true
		}
	}
	u.Text(in.Min.X, y, "ÇEKİRDEK FREKANSI (MHz, anlık)", u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY/3

	const cellCols = 10 // "P12 4500" + boşluk
	perRow := (in.Dx() - u.M.PadX) / (u.F.CellW * cellCols)
	if perRow < 1 {
		perRow = 1
	}
	top := 0
	for _, v := range mhz {
		if v > top {
			top = v
		}
	}
	i := 0
	for cpu, v := range mhz {
		if v <= 0 {
			continue
		}
		if i%perRow == 0 && i > 0 {
			y += u.F.CellH
		}
		if y+u.F.CellH > bottom {
			break
		}
		tag := "P"
		if eSet[cpu] {
			tag = "E"
		} else if st.Turbo == nil || !st.Turbo.Hybrid {
			tag = "#"
		}
		c := u.Pal.Text
		if top > 0 && v*100 < top*60 { // en hızlıdan belirgin yavaş: soluk
			c = u.Pal.TextDim
		}
		cx := in.Min.X + (i%perRow)*u.F.CellW*cellCols
		x := u.Text(cx, y, fmt.Sprintf("%s%d", tag, cpu), u.Pal.TextFaint)
		u.Text(x+u.F.CellW, y, fmt.Sprintf("%d", v), c)
		i++
	}
	return y + u.F.CellH
}

// parseCPUList "0-3,8" biçimini çözer (turbo paketine bağımlılık eklememek
// için küçük bir kopya; panel yalnızca model türlerini kullanır).
func parseCPUList(s string) []int {
	var out []int
	for _, part := range strings.Split(s, ",") {
		var lo, hi int
		if n, _ := fmt.Sscanf(part, "%d-%d", &lo, &hi); n == 2 {
			for i := lo; i <= hi && i-lo < 4096; i++ {
				out = append(out, i)
			}
		} else if n, _ := fmt.Sscanf(part, "%d", &lo); n == 1 {
			out = append(out, lo)
		}
	}
	return out
}

// turboEventText toggle sonrası olay satırı: "Turbo açıldı" yerine GERÇEK
// sonuç.
func turboEventText(on bool, d *model.TurboStatus) string {
	switch {
	case d == nil && on:
		return "Turbo açıldı"
	case d == nil:
		return "Turbo kapatıldı"
	case on:
		return "Turbo: " + d.Summary
	default:
		return "Turbo kapatıldı — frekans ve fanlar otomatiğe döndü"
	}
}
