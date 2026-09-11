package panel

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"mcos/internal/model"
	"mcos/panel/theme"
)

// MCOS BİLEŞEN KÜTÜPHANESİ
//
// Kurallar:
//
//  1. Hiçbir ölçü burada ham sayı olarak yazılmaz — hepsi panel/theme/tokens.go
//     içindeki token'lardan gelir (theme.LabelWidth, theme.BarWidth …).
//  2. Hiçbir renk burada seçilmez — hepsi hazır tema stillerinden gelir
//     (th.Body, th.Muted, th.Selected …).
//  3. Hiçbir glif burada ham yazılmaz — theme.Icon* sabitleri kullanılır.
//
// Böylece "bir yerde 18, başka yerde 14 kolon" tutarsızlığı yapısal olarak
// imkânsız hale gelir.

// ── Metin yardımcıları ──────────────────────────────────────────────────────

// Truncate shortens s to at most n display cells, appending an ellipsis.
// Rune tabanlı çalışır: Türkçe karakterler yarıdan kesilmez.
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return theme.Ellipsis
	}
	return string(r[:n-1]) + theme.Ellipsis
}

// pad right-pads s to exactly w display cells (truncating if longer).
// lipgloss.Width kullanır, böylece renk kaçış dizileri sayılmaz.
func pad(s string, w int) string {
	cur := lipgloss.Width(s)
	if cur > w {
		return Truncate(s, w)
	}
	return s + strings.Repeat(" ", w-cur)
}

// ── Kart ────────────────────────────────────────────────────────────────────

// Card renders content inside a rounded box of exactly `width` total columns.
//
// Genişlik hesabı TEK yerde: theme.InnerWidth(). Eskiden her çağıran kendi
// "width-2" aritmetiğini yapıyordu ve kenarlık/dolgu payları tutmuyordu.
func Card(th *theme.Theme, title, body string, width int, focused bool) string {
	inner := theme.InnerWidth(width)
	style := th.Card
	if focused {
		style = th.CardFocused
	}
	content := body
	if title != "" {
		content = th.Heading.Render(Truncate(title, inner)) + "\n\n" + body
	}
	return style.Width(inner).Render(content)
}

// RenderCard is the previous name for Card, kept for the existing views.
func RenderCard(th *theme.Theme, title string, body string, width int, focused bool) string {
	return Card(th, title, body, width, focused)
}

// card is the compact internal helper used by dashboard-style views.
func card(t *theme.Theme, title, body string, w int) string {
	return Card(t, title, body, w, false)
}

// ── Butonlar ────────────────────────────────────────────────────────────────

// Button renders a REAL bordered button with an optional key hint.
//
// Eskiden butonlar yalnızca renkli metindi: renderPill() aldığı arka plan
// rengini hiç kullanmıyor, sadece Foreground + Bold uyguluyordu. Bu yüzden
// "buton" ile normal metin görsel olarak ayırt edilemiyordu. Artık gerçek
// yuvarlak çerçeve var ve odaklı buton çerçevesi vurgu rengine döner.
//
// Etiket alanı theme.ButtonMinWidth'e doldurulur, böylece yan yana dizilen
// butonlar eşit genişlikte görünür.
func Button(th *theme.Theme, label, key string, focused bool) string {
	// Odak, renkten BAĞIMSIZ olarak da anlaşılmalı (renk körlüğü, düşük
	// kontrastlı ekran): odaklı butona bir işaretçi eklenir. İşaretçi ve
	// boşluğu, etiketin yerini YEMEZ — buton büyür.
	prefix := "  "
	if focused {
		prefix = theme.IconCursor + " "
	}

	text := prefix + label
	if key != "" {
		text += "  " + key
	}

	// Buton etikete göre BÜYÜR; asla kesmez. Eski sürüm metni
	// theme.ButtonMinWidth'e pad() ile zorluyordu ve pad() uzun metni
	// kısaltıyordu, bu yüzden "Taramayı Başlat" → "Taramayı Başl…" oluyordu:
	// butonun ne yaptığı okunamaz hale geliyordu.
	if w := lipgloss.Width(text); w < theme.ButtonMinWidth {
		text += strings.Repeat(" ", theme.ButtonMinWidth-w)
	}

	if focused {
		return th.ButtonFocused.Render(text)
	}
	return th.Button.Render(text)
}

// RenderButton is the previous name for Button.
func RenderButton(th *theme.Theme, label string, shortcut string, focused bool) string {
	return Button(th, label, shortcut, focused)
}

// Chip renders a SINGLE-LINE selectable label, for tab bars and inline
// affordances.
//
// Button() çerçeveli olduğu için ÜÇ satır yükseklik kaplar. Sekme çubuğu gibi
// tek satırlık bir şeride konulduğunda satır sayıları uyuşmaz ve düzen bozulur
// (renderTabBar bunu yapıyordu: çerçeveli butonu düz metinlerle strings.Join
// ile aynı satıra koyuyordu). Tek satır gereken her yerde Chip kullanılmalı.
func Chip(th *theme.Theme, label string, active bool) string {
	if active {
		return th.Selected.Render(theme.IconCursor + label)
	}
	return th.Muted.Render(" " + label)
}

// ChipBar joins chips on one line with a thin vertical separator.
func ChipBar(th *theme.Theme, chips []string) string {
	sep := th.Divider.Render(" " + theme.SepVert + " ")
	return strings.Join(chips, sep)
}

// ButtonRow lays buttons out horizontally with one grid unit between them.
func ButtonRow(buttons ...string) string {
	if len(buttons) == 0 {
		return ""
	}
	gap := strings.Repeat(" ", theme.GridUnit)
	parts := make([]string, 0, len(buttons)*2-1)
	for i, b := range buttons {
		if i > 0 {
			parts = append(parts, gap)
		}
		parts = append(parts, b)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// Action describes one entry in an action bar.
type Action struct {
	Label   string // ne yaptığı ("İleri", "Geri", "İptal")
	Key     string // hangi tuş ("Enter", "Shift+Tab", "Esc")
	Primary bool   // birincil eylem: çerçevesi vurgu renginde ve işaretçili
}

// ActionBar renders the screen's available actions as REAL buttons.
//
// Neden: kullanıcı "arayüzde yazı ile buton anlaşılmıyor" dedi. Eskiden her
// ekranın altında yalnızca tek satırlık bir tuş ipucu şeridi vardı
// ("Enter ilerle · Esc vazgeç") — düz metin, eylem olduğu belli değil.
// Burada her eylem yuvarlak çerçeveli bir butondur ve birincil eylem
// (genelde "İleri") vurgu rengiyle + işaretçiyle öne çıkar.
//
// Ekran daralırsa butonlar sığmaz; o durumda tek satırlık ipucu şeridine
// düşer, böylece düzen asla taşmaz.
func ActionBar(th *theme.Theme, maxWidth int, actions ...Action) string {
	if len(actions) == 0 {
		return ""
	}
	btns := make([]string, 0, len(actions))
	hints := make([]KeyHint, 0, len(actions))
	for _, a := range actions {
		btns = append(btns, Button(th, a.Label, a.Key, a.Primary))
		hints = append(hints, KeyHint{Key: a.Key, Label: a.Label})
	}
	row := ButtonRow(btns...)
	if maxWidth > 0 && lipgloss.Width(row) > maxWidth {
		return RenderKeyHints(th, hints, maxWidth)
	}
	return row
}

// ── Tuş kapağı ve ipuçları ──────────────────────────────────────────────────

// KeyCap renders one keyboard key as a bordered cap so it reads as a key.
func KeyCap(th *theme.Theme, key string) string {
	return th.KeyCap.Render(key)
}

// KeyHint pairs a key with a short action label for the footer bar.
type KeyHint struct {
	Key   string
	Label string
}

// RenderKeyHints renders the footer hint bar on ONE line.
//
// Tuş kapakları çerçeveli olduğu için 3 satır yükseklik kaplar; alt şerit tek
// satır olmalı (theme.FooterHeight). Bu yüzden burada kapak KULLANILMAZ:
// tuş vurgu renginde, eylem ikincil renkte yazılır.
func RenderKeyHints(th *theme.Theme, hints []KeyHint, maxWidth int) string {
	sep := th.Muted.Render("  " + theme.SepDot + "  ")
	parts := make([]string, 0, len(hints))
	for _, h := range hints {
		if h.Key == "" || h.Label == "" {
			continue
		}
		part := th.Selected.Render(h.Key) + th.Muted.Render(" "+h.Label)
		candidate := strings.Join(append(append([]string{}, parts...), part), sep)
		if maxWidth > 0 && lipgloss.Width(candidate) > maxWidth {
			break
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, sep)
}

// ── Bilgi satırları ─────────────────────────────────────────────────────────

// InfoRow renders a "label   value" row on the shared label grid.
func InfoRow(th *theme.Theme, label, value string) string {
	return th.Label.Render(Truncate(label, theme.LabelWidth)) + " " + th.Value.Render(value)
}

// kv is the previous name for InfoRow and is used in 76 places.
func kv(t *theme.Theme, key, val string) string {
	return InfoRow(t, key, val)
}

// ── Seçim satırları ────────────────────────────────────────────────────────

// Radio renders a single-choice option row.
func Radio(th *theme.Theme, label, note string, selected bool) string {
	icon, style := theme.IconUnselect, th.Body
	if selected {
		icon, style = theme.IconSelected, th.Selected
	}
	row := style.Render(icon + " " + label)
	if note != "" {
		row += "  " + th.Muted.Render(note)
	}
	return row
}

// RenderOptionRow is the previous name for Radio.
func RenderOptionRow(th *theme.Theme, label string, note string, selected bool) string {
	return Radio(th, label, note, selected)
}

// Checkbox renders a multi-choice option row.
func Checkbox(th *theme.Theme, label string, checked, focused bool) string {
	icon := theme.IconUnchcked
	if checked {
		icon = theme.IconChecked
	}
	prefix := "  "
	style := th.Body
	if focused {
		prefix = theme.IconCursor + " "
		style = th.Selected
	}
	return style.Render(prefix + icon + " " + label)
}

// NavItem renders one sidebar entry within exactly `width` columns.
//
// Odak renkten BAĞIMSIZ olarak da anlaşılır: seçili satır işaretçi taşır.
// Pasif bölümler sonuna "kapalı" eklenir ve etiket buna göre KISALTILIR —
// eskiden "(pasif)" eklenip taşan satır alt satıra sarıyor ve kenar çubuğu
// hizasını bozuyordu.
func NavItem(th *theme.Theme, icon, label string, width int, selected, disabled bool) string {
	const marker = 2 // "▸ " veya "  "
	avail := width - marker - lipgloss.Width(icon) - 1
	if avail < 4 {
		avail = 4
	}

	if disabled {
		const suffix = " kapalı"
		return th.Disabled.Render("  " + icon + " " +
			Truncate(label, avail-len(suffix)) + suffix)
	}
	text := icon + " " + Truncate(label, avail)
	if selected {
		return th.Selected.Render(theme.IconCursor + " " + text)
	}
	return th.Body.Render("  " + text)
}

// RenderNavItem is the previous name for NavItem.
func RenderNavItem(th *theme.Theme, icon string, label string, width int, selected bool, disabled bool) string {
	return NavItem(th, icon, label, width, selected, disabled)
}

// ── Form alanı ──────────────────────────────────────────────────────────────

// Field renders a labelled text input.
//
// Kutu çerçeveli olduğu için ÜÇ satırdır. Eskiden
//
//	fmt.Sprintf("%s : %s", label, box)
//
// biçimi kullanılıyordu; bu, kutunun 1. satırını etiketin yanına, 2. ve 3.
// satırlarını 0. kolona koyuyordu ve formlar tamamen kayıyordu. Doğru araç
// lipgloss.JoinHorizontal: etiketi kutunun dikey ortasına hizalar.
func Field(th *theme.Theme, label, inputView, help string, focused bool) string {
	labelStyle := th.Label
	if focused {
		labelStyle = th.Label.Foreground(th.P.Accent).Bold(true)
	}
	box := th.Input
	if focused {
		box = th.InputFocused
	}

	row := lipgloss.JoinHorizontal(
		lipgloss.Center, // ← etiket, çerçeveli kutunun ortasına hizalanır
		labelStyle.Render(Truncate(label, theme.LabelWidth)),
		" ",
		box.Render(inputView),
	)
	if help != "" && focused {
		indent := strings.Repeat(" ", theme.LabelWidth+1)
		row += "\n" + indent + th.Muted.Render(help)
	}
	return row
}

// RenderInputField is the previous name for Field.
func RenderInputField(th *theme.Theme, label string, inputView string, help string, focused bool) string {
	return Field(th, label, inputView, help, focused)
}

// ── İlerleme ────────────────────────────────────────────────────────────────

// ProgressBar renders a bar of exactly `width` body cells plus a percentage.
func ProgressBar(th *theme.Theme, pct, width int) string {
	if width <= 0 {
		width = theme.BarWidth
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := (pct * width) / 100
	return th.Selected.Render(strings.Repeat(theme.BarFill, filled)) +
		th.Divider.Render(strings.Repeat(theme.BarEmpty, width-filled)) +
		th.Muted.Render(fmt.Sprintf(" %3d%%", pct))
}

// RenderProgressBar is the previous name for ProgressBar.
func RenderProgressBar(th *theme.Theme, pct int, width int) string {
	return ProgressBar(th, pct, width)
}

// bar renders a float-percentage gauge (CPU / RAM meters).
func bar(t *theme.Theme, pct float64, width int) string {
	return ProgressBar(t, int(pct+0.5), width)
}

// ── Başlık ──────────────────────────────────────────────────────────────────

// StepHeader renders the wizard header: title on one line, progress on the
// next. Toplam yükseklik theme.HeaderHeight ile eşleşir.
func StepHeader(th *theme.Theme, step, total int, title string) string {
	if total < 1 {
		total = 1
	}
	pct := (step * 100) / total
	if pct > 100 {
		pct = 100
	}
	head := th.Title.Render(title) + th.Muted.Render(fmt.Sprintf("   adım %d/%d", step, total))
	return head + "\n" + ProgressBar(th, pct, theme.BarWidth)
}

// RenderHeader is the previous name for StepHeader.
func RenderHeader(th *theme.Theme, currentStep int, totalSteps int, title string) string {
	return StepHeader(th, currentStep, totalSteps, title)
}

// Divider renders a horizontal rule of the given width.
func Divider(th *theme.Theme, width int) string {
	if width < 1 {
		return ""
	}
	return th.Divider.Render(strings.Repeat(theme.SepHoriz, width))
}

// ── Liste ───────────────────────────────────────────────────────────────────

// List windows already-styled rows around cursor into exactly h lines,
// appending a position indicator when the list does not fit.
func List(th *theme.Theme, rows []string, cursor, h int) string {
	n := len(rows)
	if h < 1 {
		h = 1
	}
	if n <= h {
		return strings.Join(rows, "\n")
	}
	visible := h - 1
	if visible < 1 {
		visible = 1
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor > n-1 {
		cursor = n - 1
	}
	top := cursor - visible/2
	if top < 0 {
		top = 0
	}
	if top > n-visible {
		top = n - visible
	}
	status := th.Muted.Render(fmt.Sprintf("  %s%s  %d-%d / %d",
		theme.IconUp, theme.IconDown, top+1, top+visible, n))
	return strings.Join(append(append([]string{}, rows[top:top+visible]...), status), "\n")
}

// scrollList is the previous name for List.
func scrollList(th *theme.Theme, rows []string, cursor, h int) string {
	return List(th, rows, cursor, h)
}

// ── Durum göstergeleri ──────────────────────────────────────────────────────

// StateBadge renders a server state with a fixed-width label so list columns
// never shift as state changes.
func StateBadge(t *theme.Theme, st model.ServerState) string {
	var (
		icon  string
		label string
		style lipgloss.Style
	)
	switch st {
	case model.StateRunning:
		icon, label, style = theme.IconDotOn, theme.StateLabelRunning, t.OK
	case model.StateStarting:
		icon, label, style = theme.IconDotOn, theme.StateLabelStarting, t.Warn
	case model.StateStopping:
		icon, label, style = theme.IconDotOn, theme.StateLabelStopping, t.Warn
	case model.StateError:
		icon, label, style = theme.IconFail, theme.StateLabelError, t.Error
	default:
		icon, label, style = theme.IconDotOf, theme.StateLabelStopped, t.Muted
	}
	return style.Render(icon + " " + pad(label, theme.StateLabelWidth))
}

// stateBadge is the previous name for StateBadge.
func stateBadge(t *theme.Theme, st model.ServerState) string { return StateBadge(t, st) }

// BoolBadge renders a yes/no marker with optional custom labels.
func BoolBadge(t *theme.Theme, val bool, labels ...string) string {
	on, off := "EVET", "HAYIR"
	if len(labels) > 0 && labels[0] != "" {
		on = labels[0]
	}
	if len(labels) > 1 && labels[1] != "" {
		off = labels[1]
	}
	if val {
		return t.OK.Render(theme.IconOK + " " + on)
	}
	return t.Muted.Render(theme.IconDash + " " + off)
}

// boolBadge is the previous name for BoolBadge.
func boolBadge(t *theme.Theme, val bool, labels ...string) string {
	return BoolBadge(t, val, labels...)
}

// ── Biçimlendirme ───────────────────────────────────────────────────────────

func fmtBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func fmtUptime(sec int64) string {
	if sec <= 0 {
		return "0s"
	}
	d := sec / 86400
	h := (sec % 86400) / 3600
	m := (sec % 3600) / 60
	s := sec % 60

	switch {
	case d > 0:
		return fmt.Sprintf("%dg %dsa", d, h)
	case h > 0:
		return fmt.Sprintf("%dsa %ddk", h, m)
	case m > 0:
		return fmt.Sprintf("%ddk %dsn", m, s)
	default:
		return fmt.Sprintf("%dsn", s)
	}
}

// joinH joins two blocks side by side with an explicit gap.
func joinH(gap int, left, right string) string {
	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right)
}
