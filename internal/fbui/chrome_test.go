package fbui

import (
	"image"
	"image/color"
	"testing"
	"time"

	"mcos/internal/fbfont"
)

// TestMockupModalOverBlur çizer: parola penceresi, BULANIK arka planın üstünde.
//
// Kullanıcının isteği: "wifi felan secerken parola girerken arkada olan sey
// dursun arkada ne varsa artık blur gelsin veya tamamen siyah olmasın hafif
// karartma olsun".
func TestMockupModalOverBlur(t *testing.T) {
	const W, H = 1280, 800
	img, u, f := newScreen(t, W, H, 18)
	defer f.Close()

	// 1) Önce ARKA EKRANI çiz: Wi-Fi seçim listesi.
	drawWiFiList(u, f, W, H, 0)

	// 2) Perde: bulanıklaştır + hafif karart.
	u.Scrim(img.Bounds())

	// 3) Üstüne parola penceresi.
	in := u.Modal(f.CellW*46, f.CellH*12, "MCOS-Lab ağına bağlan")
	y := in.Min.Y
	u.Text(in.Min.X, y, "Bu ağ korumalı. Parolayı girin.", u.Pal.TextDim)
	y += f.CellH + u.M.PadY*2

	// Parola alanı.
	fieldH := u.M.ButtonH
	u.P.StrokeRoundRect(
		rectF(in.Min.X, y, in.Dx(), fieldH),
		u.M.RadiusSmall, u.M.StrokeFocus, u.Pal.BorderFocus)
	u.Text(in.Min.X+u.M.PadX, y+(fieldH-f.CellH)/2, "••••••••••", u.Pal.Text)
	// İmleç: çizilmiş dikey çubuk, karakter değil.
	cx := in.Min.X + u.M.PadX + u.TextWidth("••••••••••")
	u.P.VLine(float64(y)+float64(fieldH-f.CellH)/2, float64(y+fieldH)-float64(fieldH-f.CellH)/2,
		float64(cx), 2, u.Pal.Accent)
	y += fieldH + u.M.PadY

	u.Check(in.Min.X, y, false)
	u.Text(in.Min.X+f.CellW+u.M.Gap, y, "Parolayı göster", u.Pal.TextDim)

	by := in.Max.Y - u.M.ButtonH
	u.ButtonRow(in.Min.X, by, []Btn{
		{Label: "Bağlan", Key: "Enter", Style: ButtonPrimary},
		{Label: "Vazgeç", Key: "Esc", Style: ButtonSecondary},
	}, 0)

	// 4) Alt çubuk perdenin de üstünde kalır: her zaman okunur.
	u.StatusBar(
		&Event{Kind: EventBusy, Text: "MCOS-Lab ağına bağlanılıyor…", At: time.Now()},
		[]Shortcut{{Key: "Esc", Label: "Vazgeç"}, {Key: "Enter", Label: "Bağlan"}},
		3)

	save(t, img, "05-parola-bulanik.png")
}

// TestMockupStatusBar çizer: ana ekran + canlı olay çubuğu.
//
// Kullanıcının isteği: "altta biraz alan olacak oradan girdiğimiz yerin
// kısayolu ve su an olan şey olacak mesela sunucuyu actıysak altta sunucu
// acılıyor basladı gibi uyarılar olacak".
func TestMockupStatusBar(t *testing.T) {
	const W, H = 1280, 800
	img, u, f := newScreen(t, W, H, 18)
	defer f.Close()

	// İçerik alanı alt çubuğun ÜSTÜNDE biter.
	contentH := H - u.StatusBarH()
	pad := u.M.PadX
	sideW := 260

	side := u.Panel(image.Rect(pad, pad, pad+sideW, contentH-pad), "", false)
	y := side.Min.Y
	u.Text(side.Min.X, y, "MCOS", u.Pal.Accent)
	u.Text(side.Min.X+f.CellW*5, y, "v0.1.0", u.Pal.TextFaint)
	y += f.CellH + u.M.PadY
	u.Divider(side.Min.X, side.Max.X, y)
	y += u.M.PadY * 2

	items := []string{"Sistem Durumu", "Sunucular", "USB Bellek", "Yazılım", "Ekran", "Ağ", "Güç", "Ayarlar"}
	for i, it := range items {
		row := image.Rect(side.Min.X-u.M.PadX/2, y, side.Max.X+u.M.PadX/2, y+u.M.RowH)
		cx := u.Row(row, i == 1)
		ty := y + (u.M.RowH-f.CellH)/2
		col := u.Pal.TextDim
		if i == 1 {
			col = u.Pal.Accent
			u.Chevron(cx-f.CellW, ty, 0, u.Pal.Accent)
		}
		u.Text(cx, ty, it, col)
		y += u.M.RowH
	}

	main := u.Panel(image.Rect(pad*2+sideW, pad, W-pad, contentH-pad), "Sunucular", true)
	my := main.Min.Y

	servers := []struct {
		name, ver, state string
		running          bool
		players          string
	}{
		{"Survival", "Paper 1.21.1", "ÇALIŞIYOR", true, "3 / 20"},
		{"Yaratıcı", "Fabric 1.21.11", "BAŞLIYOR", false, "0 / 10"},
	}
	for i, s := range servers {
		card := image.Rect(main.Min.X, my, main.Max.X, my+f.CellH*4)
		u.P.FillRoundRect(rectF(card.Min.X, card.Min.Y, card.Dx(), card.Dy()), u.M.Radius, u.Pal.Raised)
		if i == 1 {
			u.P.StrokeRoundRect(rectF(card.Min.X, card.Min.Y, card.Dx(), card.Dy()), u.M.Radius, u.M.StrokeFocus, u.Pal.Accent)
		}
		cx := card.Min.X + u.M.PadX
		cy := card.Min.Y + u.M.PadY

		if s.running {
			u.StatusDot(cx, cy, u.Pal.OK)
		} else {
			u.Spinner(cx, cy, 5, u.Pal.Warn)
		}
		nx := u.Text(cx+f.CellW+u.M.Gap, cy, s.name, u.Pal.Text)
		u.Text(nx+u.M.Gap*2, cy, s.ver, u.Pal.TextDim)

		badgeCol := u.Pal.Warn
		if s.running {
			badgeCol = u.Pal.OK
		}
		bw := u.TextWidth(s.state) + f.CellW*2
		u.Badge(card.Max.X-u.M.PadX-bw, cy-f.CellH/6, s.state, badgeCol)

		cy += f.CellH + u.M.PadY
		u.Text(cx+f.CellW+u.M.Gap, cy, "Oyuncular "+s.players, u.Pal.TextDim)
		my = card.Max.Y + u.M.PadY
	}

	u.StatusBar(
		&Event{Kind: EventBusy, Text: "Yaratıcı açılıyor — Fabric yükleniyor…", At: time.Now()},
		[]Shortcut{
			{Key: "N", Label: "Yeni"},
			{Key: "S", Label: "Başlat"},
			{Key: "K", Label: "Konsol"},
			{Key: "F1", Label: "Yardım"},
		}, 7)

	save(t, img, "06-alt-cubuk.png")
}

// TestMockupThemes cizer: alti temanin da ayni ekranda gorunumu.
func TestMockupThemes(t *testing.T) {
	const W, H = 1280, 860
	img, u, f := newScreen(t, W, H, 16)
	defer f.Close()

	cardH := (H - u.M.PadY*2) / len(ThemeOrder)
	for i, name := range ThemeOrder {
		pal := DefaultPalette.WithAccent(name)
		tu := *u
		tu.Pal = pal

		top := u.M.PadY + i*cardH
		card := image.Rect(u.M.PadX, top, W-u.M.PadX, top+cardH-u.M.PadY)
		in := tu.Panel(card, "", i == 0)

		ty := in.Min.Y + (in.Dy()-f.CellH)/2
		x := in.Min.X
		tu.Radio(x, ty, i == 0)
		x += f.CellW + u.M.Gap
		col := pal.Text
		if i == 0 {
			col = pal.Accent
		}
		x = tu.Text(x, ty, ThemeLabel(name), col)
		tu.Text(x+u.M.Gap*2, ty, name, pal.TextFaint)

		// Sagda rol renklerinin ornekleri: tema degisince YALNIZCA vurgu
		// degismeli, tamam/uyari/hata her temada ayni kalmali. Bu gorsel,
		// okunabilirligin temadan bagimsiz oldugunu dogrular.
		bx := in.Max.X - u.M.PadX
		for _, b := range []struct {
			lbl string
			c   color.RGBA
		}{
			{"hata", pal.Error}, {"uyari", pal.Warn},
			{"tamam", pal.OK}, {"vurgu", pal.Accent},
		} {
			w := tu.TextWidth(b.lbl) + f.CellW*2
			bx -= w
			tu.Badge(bx, ty-f.CellH/6, b.lbl, b.c)
			bx -= u.M.Gap
		}
	}

	save(t, img, "07-temalar.png")
}

// drawWiFiList paints the background screen used by the modal mockup.
func drawWiFiList(u *UI, f *fbfont.Face, W, H, sel int) {
	pw, ph := 760, 470
	in := u.Panel(image.Rect((W-pw)/2, (H-ph)/2, (W-pw)/2+pw, (H-ph)/2+ph), "", true)

	y := in.Min.Y
	u.Text(in.Min.X, y, "Kablosuz Ag Sec", u.Pal.Text)
	u.TextRight(in.Max.X, y, "esc", u.Pal.TextFaint)
	y += f.CellH + u.M.PadY

	sh := u.M.ButtonH
	u.P.StrokeRoundRect(rectF(in.Min.X, y, in.Dx(), sh), u.M.RadiusSmall, u.M.Stroke, u.Pal.Border)
	u.Text(in.Min.X+u.M.PadX, y+(sh-f.CellH)/2, "Ara...", u.Pal.TextFaint)
	y += sh + u.M.PadY*2

	u.Text(in.Min.X, y, "ONERILEN", u.Pal.Accent)
	y += f.CellH + u.M.PadY/2

	nets := []struct {
		name   string
		signal int
	}{{"MCOS-Lab", 92}, {"Ofis-5G", 74}, {"Misafir", 61}}
	for i, n := range nets {
		row := image.Rect(in.Min.X, y, in.Max.X, y+u.M.RowH)
		cx := u.Row(row, i == sel)
		ty := y + (u.M.RowH-f.CellH)/2
		u.Radio(cx, ty, i == sel)
		col := u.Pal.Text
		if i == sel {
			col = u.Pal.Accent
		}
		u.Text(cx+f.CellW+u.M.Gap, ty, n.name, col)
		u.Progress(in.Max.X-u.M.PadX-f.CellW*6, ty+f.CellH/3, f.CellW*6, n.signal)
		y += u.M.RowH
	}
}
