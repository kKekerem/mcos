package fbpanel

import (
	"time"

	"mcos/internal/fbdraw"
)

// drawLockDots paints the masked lock field as animated circles.
//
// ── Neden metin değil daire? ────────────────────────────────────────────────
// Eskiden burada strings.Repeat("•", n) tek parça metin olarak çiziliyordu.
// Metin ÖLÇEKLENEMEZ ve tek tek saydamlaştırılamaz (font hücresi sabittir),
// yani "yeni giren işaret yerine otursun, silinen sönerek kaybolsun" istendiği
// anda o yol çalışmaz. Daire, yarıçapı ve saydamlığı serbest olan tek çizim
// ilkesidir ve hücre ızgarasında aynı yeri kaplar — hizalama bozulmaz.
//
// ── Ortalama ve kayma ───────────────────────────────────────────────────────
// Satır ORTALI olduğu için işaret sayısı değişince bütün satır kayar. Yazma ve
// silme sırasında bu kayma ANİDEN değil animasyon boyunca yumuşak yapılır;
// aksi hâlde her tuşta bütün işaretler zıplardı. Pencere içindeki alan
// (password.go) sola dayalı olduğu için orada bu soruna hiç düşülmüyor —
// iki alanın tek farkı budur.
func (a *App) drawLockDots(fx, fw, ty, n int, an lockAnim) {
	u := a.ui
	cellW := float64(u.F.CellW)
	baseR := dotRadius(u)
	cy := float64(ty) + float64(u.F.CellH)/2

	// rowStart, verilen işaret sayısı için satırın sol kenarı.
	rowStart := func(count int) float64 {
		return float64(fx) + (float64(fw)-float64(count)*cellW)/2
	}

	start := rowStart(n)
	if p := progress(an.typedAt, typePopDur); p >= 0 && n > 0 {
		// Yazarken satır bir hücre genişler: kalanlar eski yerlerinden yeni
		// yerlerine kayar.
		e := fbdraw.EaseOutCubic(p)
		start = rowStart(n-1) + (rowStart(n)-rowStart(n-1))*e
	} else if p := progress(an.delAt, delGhostDur); p >= 0 {
		// Silerken bir hücre daralır.
		e := fbdraw.EaseOutCubic(p)
		start = rowStart(n+1) + (rowStart(n)-rowStart(n+1))*e
	}

	pop := progress(an.typedAt, typePopDur)
	for i := 0; i < n; i++ {
		cx := start + (float64(i)+0.5)*cellW
		if i == n-1 && pop >= 0 {
			// Son işaret: büyük ve soluk başlar, yerine oturur.
			e := fbdraw.EaseOutCubic(pop)
			rad := baseR * (1.85 - 0.85*e)
			c := fbdraw.Blend(u.Pal.Accent, u.Pal.Text, e)
			u.P.FillCircle(cx, cy, rad, fbdraw.Alpha(c, 0.35+0.65*e))
			continue
		}
		u.P.FillCircle(cx, cy, baseR, u.Pal.Text)
	}

	// Silinen işaretin hayaleti: ESKİ düzendeki yerinde küçülerek söner ve
	// dışa açılan ince bir halka bırakır. Halka olmadan "silindi" ile
	// "uzaklaştı" ayırt edilemezdi.
	if p := progress(an.delAt, delGhostDur); p >= 0 {
		e := fbdraw.EaseOutCubic(p)
		gx := rowStart(n+1) + (float64(an.delIdx)+0.5)*cellW
		u.P.FillCircle(gx, cy, baseR*(1-e), fbdraw.Alpha(u.Pal.Accent, 1-e))
		u.P.StrokeCircle(gx, cy, baseR*(1+2.2*e), 1,
			fbdraw.Alpha(u.Pal.Accent, 0.5*(1-e)))
	}

	// Ctrl+U: soldan sağa bir dalga hâlinde hepsi yukarı süzülerek kaybolur.
	// Soldan sağa, çünkü göz satırı zaten o yönde okur.
	total := clearDur + time.Duration(an.clearN)*clearStagger
	if p := progress(an.clearAt, total); p >= 0 {
		el := time.Since(an.clearAt)
		cstart := rowStart(an.clearN)
		for i := 0; i < an.clearN; i++ {
			q := float64(el-time.Duration(i)*clearStagger) / float64(clearDur)
			if q <= 0 {
				q = 0
			}
			if q >= 1 {
				continue
			}
			e := fbdraw.EaseOutCubic(q)
			cx := cstart + (float64(i)+0.5)*cellW
			u.P.FillCircle(cx, cy-6*e, baseR*(1-e),
				fbdraw.Alpha(u.Pal.Accent, 1-e))
		}
	}
}
