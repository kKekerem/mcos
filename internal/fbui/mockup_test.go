package fbui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"mcos/internal/fbdraw"
	"mcos/internal/fbfont"
)

// rectF, tamsayi piksel dikdortgenini fbdraw.Rect e cevirir.
func rectF(x, y, w, h int) fbdraw.Rect {
	return fbdraw.R(float64(x), float64(y), float64(w), float64(h))
}

type ptAlias = fbdraw.Pt

// outDir, üretilen PNG'lerin yazılacağı yer. MCOS_UI_OUT ile değiştirilebilir.
func outDir(t *testing.T) string {
	d := os.Getenv("MCOS_UI_OUT")
	if d == "" {
		d = filepath.Join(os.TempDir(), "mcos-ui")
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatalf("çıktı dizini: %v", err)
	}
	return d
}

func save(t *testing.T, img *image.RGBA, name string) {
	t.Helper()
	p := filepath.Join(outDir(t), name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("%s kodlanamadı: %v", p, err)
	}
	t.Logf("yazıldı: %s", p)
}

func newScreen(t *testing.T, w, h int, px float64) (*image.RGBA, *UI, *fbfont.Face) {
	t.Helper()
	f, err := fbfont.Load(px)
	if err != nil {
		t.Fatalf("font: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	u := NewUI(img, f, DefaultPalette)
	u.Clear()
	return img, u, f
}

// TestMockupSelectionScreen çizer: "bir şey seç" ekranı — disk seçimi,
// WiFi seçimi, sunucu türü seçimi hepsi bu düzeni paylaşır.
//
// Kullanıcının verdiği örnek görsellerdeki gibi: tek parça yuvarlak pencere,
// içinde arama satırı, gruplanmış seçenek listesi, sağda kısayol tuşları.
func TestMockupSelectionScreen(t *testing.T) {
	const W, H = 1280, 800
	img, u, f := newScreen(t, W, H, 18)
	defer f.Close()

	// Ortalanmış pencere.
	pw, ph := 760, 470
	px, py := (W-pw)/2, (H-ph)/2
	win := image.Rect(px, py, px+pw, py+ph)
	in := u.Panel(win, "", true)

	y := in.Min.Y

	// Başlık satırı: solda başlık, sağda kapatma ipucu.
	u.Text(in.Min.X, y, "Kablosuz Ağ Seç", u.Pal.Text)
	u.TextRight(in.Max.X, y, "esc", u.Pal.TextFaint)
	y += f.CellH + u.M.PadY

	// Arama alanı — tek parça yuvarlak çerçeve.
	searchH := u.M.ButtonH
	u.P.StrokeRoundRect(
		rectF(in.Min.X, y, in.Dx(), searchH),
		u.M.RadiusSmall, u.M.Stroke, u.Pal.Border)
	u.Text(in.Min.X+u.M.PadX, y+(searchH-f.CellH)/2, "Ara…", u.Pal.TextFaint)
	y += searchH + u.M.PadY*2

	// Grup başlığı.
	u.Text(in.Min.X, y, "ÖNERİLEN", u.Pal.Accent)
	y += f.CellH + u.M.PadY/2

	type net struct {
		name   string
		signal int
		locked bool
	}
	nets := []net{
		{"MCOS-Lab", 92, true},
		{"Ofis-5G", 74, true},
		{"Misafir", 61, false},
	}

	for i, n := range nets {
		row := image.Rect(in.Min.X, y, in.Max.X, y+u.M.RowH)
		cx := u.Row(row, i == 0)
		ty := y + (u.M.RowH-f.CellH)/2

		u.Radio(cx, ty, i == 0)
		cx += f.CellW + u.M.Gap

		col := u.Pal.Text
		if i == 0 {
			col = u.Pal.Accent
		}
		u.Text(cx, ty, n.name, col)

		// Sağ taraf: kilit rozeti + sinyal çubuğu.
		bx := in.Max.X - u.M.PadX
		sigW := f.CellW * 6
		bx -= sigW
		u.Progress(bx, ty+f.CellH/3, sigW, n.signal)
		bx -= u.M.Gap * 2
		if n.locked {
			u.TextRight(bx, ty, "korumalı", u.Pal.TextFaint)
		} else {
			u.TextRight(bx, ty, "açık", u.Pal.TextFaint)
		}
		y += u.M.RowH
	}

	y += u.M.PadY
	u.Divider(in.Min.X, in.Max.X, y)
	y += u.M.PadY * 2

	u.Text(in.Min.X, y, "DİĞER", u.Pal.TextDim)
	y += f.CellH + u.M.PadY/2
	for _, label := range []string{"Kablolu bağlantı kullan", "Ağı elle gir"} {
		row := image.Rect(in.Min.X, y, in.Max.X, y+u.M.RowH)
		cx := u.Row(row, false)
		ty := y + (u.M.RowH-f.CellH)/2
		u.Radio(cx, ty, false)
		u.Text(cx+f.CellW+u.M.Gap, ty, label, u.Pal.Text)
		y += u.M.RowH
	}

	// Alt buton çubuğu.
	by := in.Max.Y - u.M.ButtonH
	u.ButtonRow(in.Min.X, by, []Btn{
		{Label: "Bağlan", Key: "Enter", Style: ButtonPrimary},
		{Label: "Geri", Key: "Esc", Style: ButtonSecondary},
	}, 0)
	u.TextRight(in.Max.X, by+(u.M.ButtonH-f.CellH)/2, "ok tuslari ile gezin", u.Pal.TextFaint)

	save(t, img, "01-secim-ekrani.png")
}

// TestMockupInstallScreen çizer: OOBE disk kurulum adımı.
func TestMockupInstallScreen(t *testing.T) {
	const W, H = 1280, 800
	img, u, f := newScreen(t, W, H, 18)
	defer f.Close()

	pw, ph := 880, 560
	px, py := (W-pw)/2, (H-ph)/2
	in := u.Panel(image.Rect(px, py, px+pw, py+ph), "", true)

	y := in.Min.Y
	u.Text(in.Min.X, y, "MCOS Kurulumu", u.Pal.Text)
	u.TextRight(in.Max.X, y, "adım 5 / 10", u.Pal.TextDim)
	y += f.CellH + u.M.PadY

	u.Progress(in.Min.X, y, in.Dx(), 50)
	y += f.CellH
	u.Divider(in.Min.X, in.Max.X, y)
	y += u.M.PadY * 2

	u.Text(in.Min.X, y, "MCOS'u kalıcı olarak kuracağınız diski seçin", u.Pal.Text)
	y += f.CellH + u.M.PadY*2

	disks := []struct {
		dev, model, size string
		removable        bool
	}{
		{"/dev/sda", "Kingston DataTraveler", "32 GB", true},
		{"/dev/sdb", "Samsung Portable SSD", "500 GB", true},
		{"/dev/nvme0n1", "WD Black SN750 (sistem diski)", "1 TB", false},
	}
	for i, d := range disks {
		row := image.Rect(in.Min.X, y, in.Max.X, y+u.M.RowH+f.CellH/2)
		cx := u.Row(row, i == 0)
		ty := row.Min.Y + (row.Dy()-f.CellH)/2

		u.Radio(cx, ty, i == 0)
		cx += f.CellW + u.M.Gap

		col := u.Pal.Text
		if i == 0 {
			col = u.Pal.Accent
		}
		nx := u.Text(cx, ty, d.dev, col)
		u.Text(nx+u.M.Gap*2, ty, d.model, u.Pal.TextDim)

		// Sağdan sola yerleşim: önce boyut, sonra varsa rozet. Sabit sütun
		// yerine ölçülen genişlik kullanılır, yoksa uzun metinler çakışır.
		bx := in.Max.X - u.M.PadX
		u.TextRight(bx, ty, d.size, u.Pal.Text)
		bx -= u.TextWidth(d.size) + u.M.Gap*2
		if !d.removable {
			bw := u.TextWidth("DAHİLİ DİSK") + f.CellW*2
			u.Badge(bx-bw, ty-f.CellH/6, "DAHİLİ DİSK", u.Pal.Warn)
		}
		y += row.Dy()
	}

	y += u.M.PadY * 2
	u.Check(in.Min.X, y, true)
	u.Text(in.Min.X+f.CellW+u.M.Gap, y, "Java 17 ve 21'i şimdi indir", u.Pal.Text)
	y += u.M.RowH
	u.Check(in.Min.X, y, true)
	u.Text(in.Min.X+f.CellW+u.M.Gap, y, "Çevrimdışı sunucu paketini kur (Fabric 1.21.11 + ViaVersion)", u.Pal.Text)
	y += u.M.RowH + u.M.PadY

	// Tehlike uyarısı — çizilmiş üçgen, font glifi değil.
	warnY := y
	u.P.FillPolygon(warnTriangle(float64(in.Min.X)+float64(f.CellW)/2, float64(warnY)+float64(f.CellH)/2, float64(f.CellH)*0.42), u.Pal.Error)
	u.Text(in.Min.X+f.CellW+u.M.Gap, warnY, "Seçilen disk tamamen silinecek.", u.Pal.Error)

	by := in.Max.Y - u.M.ButtonH
	u.ButtonRow(in.Min.X, by, []Btn{
		{Label: "Diski Sil ve Kur", Key: "Enter", Style: ButtonDanger},
		{Label: "Geri", Key: "Shift+Tab", Style: ButtonSecondary},
		{Label: "Atla", Key: "S", Style: ButtonSecondary},
	}, 0)

	save(t, img, "02-kurulum-disk.png")
}

// TestMockupDashboard çizer: ana ekran (kenar çubuğu + içerik).
func TestMockupDashboard(t *testing.T) {
	const W, H = 1280, 800
	img, u, f := newScreen(t, W, H, 18)
	defer f.Close()

	pad := u.M.PadX
	sideW := 260

	// Kenar çubuğu paneli.
	side := u.Panel(image.Rect(pad, pad, pad+sideW, H-pad), "", false)
	y := side.Min.Y
	u.Text(side.Min.X, y, "MCOS", u.Pal.Accent)
	u.Text(side.Min.X+f.CellW*5, y, "v0.1.0", u.Pal.TextFaint)
	y += f.CellH + u.M.PadY
	u.Divider(side.Min.X, side.Max.X, y)
	y += u.M.PadY * 2

	items := []string{"Sistem Durumu", "Sunucular", "USB Bellek", "Yazılım", "Ekran", "Ağ", "Ayarlar"}
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

	// İçerik paneli.
	main := u.Panel(image.Rect(pad*2+sideW, pad, W-pad, H-pad), "Sunucular", true)
	my := main.Min.Y

	servers := []struct {
		name, ver, state string
		running          bool
		players          string
	}{
		{"Survival", "Paper 1.21.1", "ÇALIŞIYOR", true, "3 / 20"},
		{"Yaratıcı", "Fabric 1.21.11", "KAPALI", false, "0 / 10"},
	}
	for i, s := range servers {
		card := image.Rect(main.Min.X, my, main.Max.X, my+f.CellH*4)
		u.P.FillRoundRect(rectF(card.Min.X, card.Min.Y, card.Dx(), card.Dy()), u.M.Radius, u.Pal.Raised)
		if i == 0 {
			u.P.StrokeRoundRect(rectF(card.Min.X, card.Min.Y, card.Dx(), card.Dy()), u.M.Radius, u.M.StrokeFocus, u.Pal.Accent)
		}
		cx := card.Min.X + u.M.PadX
		cy := card.Min.Y + u.M.PadY

		dot := u.Pal.TextFaint
		if s.running {
			dot = u.Pal.OK
		}
		u.StatusDot(cx, cy, dot)
		nx := u.Text(cx+f.CellW+u.M.Gap, cy, s.name, u.Pal.Text)
		u.Text(nx+u.M.Gap*2, cy, s.ver, u.Pal.TextDim)

		badgeCol := u.Pal.TextFaint
		if s.running {
			badgeCol = u.Pal.OK
		}
		bw := u.TextWidth(s.state) + f.CellW*2
		u.Badge(card.Max.X-u.M.PadX-bw, cy-f.CellH/6, s.state, badgeCol)

		cy += f.CellH + u.M.PadY
		u.Text(cx+f.CellW+u.M.Gap, cy, "Oyuncular "+s.players, u.Pal.TextDim)

		my = card.Max.Y + u.M.PadY
	}

	my += u.M.PadY
	u.ButtonRow(main.Min.X, my, []Btn{
		{Label: "Yeni Sunucu", Key: "N", Style: ButtonPrimary},
		{Label: "Başlat", Key: "S", Style: ButtonSecondary},
	}, 0)

	save(t, img, "03-ana-ekran.png")
}

// TestMockupDisplaySettings çizer: yeni ekran ayarları sayfası.
func TestMockupDisplaySettings(t *testing.T) {
	const W, H = 1280, 800
	img, u, f := newScreen(t, W, H, 18)
	defer f.Close()

	pw, ph := 720, 460
	px, py := (W-pw)/2, (H-ph)/2
	in := u.Panel(image.Rect(px, py, px+pw, py+ph), "Ekran Ayarları", true)

	y := in.Min.Y
	u.Text(in.Min.X, y, "Çözünürlük", u.Pal.TextDim)
	y += f.CellH + u.M.PadY

	modes := []string{"1920 x 1080", "1600 x 900", "1366 x 768", "1280 x 720", "1024 x 768"}
	for i, m := range modes {
		row := image.Rect(in.Min.X, y, in.Max.X, y+u.M.RowH)
		cx := u.Row(row, i == 0)
		ty := y + (u.M.RowH-f.CellH)/2
		u.Radio(cx, ty, i == 0)
		col := u.Pal.Text
		if i == 0 {
			col = u.Pal.Accent
		}
		u.Text(cx+f.CellW+u.M.Gap, ty, m, col)
		if i == 0 {
			u.TextRight(in.Max.X-u.M.PadX, ty, "şu an", u.Pal.TextFaint)
		}
		y += u.M.RowH
	}

	y += u.M.PadY
	u.Divider(in.Min.X, in.Max.X, y)
	y += u.M.PadY * 2

	u.Text(in.Min.X, y, "Yazı boyutu", u.Pal.TextDim)
	y += f.CellH + u.M.PadY
	sizes := []string{"Küçük", "Normal", "Büyük"}
	bx := in.Min.X
	for i, s := range sizes {
		st := ButtonSecondary
		if i == 1 {
			st = ButtonPrimary
		}
		r := u.Button(bx, y, s, "", st, false)
		bx = r.Max.X + u.M.Gap
	}

	by := in.Max.Y - u.M.ButtonH
	u.ButtonRow(in.Min.X, by, []Btn{
		{Label: "Uygula", Key: "Enter", Style: ButtonPrimary},
		{Label: "Vazgeç", Key: "Esc", Style: ButtonSecondary},
	}, 0)

	save(t, img, "04-ekran-ayarlari.png")
}

// warnTriangle builds an equilateral warning triangle centred on (cx,cy).
func warnTriangle(cx, cy, r float64) []ptAlias {
	return []ptAlias{
		{X: cx, Y: cy - r},
		{X: cx + r*0.92, Y: cy + r*0.72},
		{X: cx - r*0.92, Y: cy + r*0.72},
	}
}
