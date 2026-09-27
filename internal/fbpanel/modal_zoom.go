package fbpanel

import (
	"image"

	"mcos/internal/fbdraw"
)

// ════════════════════════════════════════════════════════════════════════════
// AÇILIR PENCERE: ÖLÇEKLENEREK GELİR
// ════════════════════════════════════════════════════════════════════════════
//
// ── Kullanıcının isteği ─────────────────────────────────────────────────────
//
//	"tasarım güzel olsun fade in fade out zoomlaeı kullan her yerde"
//
// ── Eskiden ne vardı ────────────────────────────────────────────────────────
//
// Pencere açılırken TÜM EKRAN çapraz soluklaşıyordu (transFade). Sonuç
// doğruydu ama hareketin yönü yoktu: pencere "belirmiyor", ekran "değişiyordu".
// Arkadaki bulanıklık zaten var olduğu için değişimin çoğu göze de çarpmıyordu.
//
// ── Şimdi ───────────────────────────────────────────────────────────────────
//
// Pencere KENDİ MERKEZİNDEN büyüyerek geliyor (%94 → %100) ve aynı anda
// soluktan tama çıkıyor. Kapanırken tersi. Arka plan perdesi eskisi gibi
// çapraz soluklaşıyor; yani iki hareket birlikte "pencere öne çıktı" diyor.
//
// ── Neden yalnızca PENCERE ölçekleniyor ─────────────────────────────────────
//
// Tüm ekranı ölçeklemek 1080p'de kare başına 2 milyon piksel demek. Pencere
// tipik olarak ekranın %20'si; onu ölçeklemek beşte bir maliyet. Üstelik
// doğru olan da bu: hareket eden şey penceredir, arka plan değil.

// modalZoomDur is how long the dialog takes to settle.
//
// transDuration (180 ms) ile AYNI: perde soluklaşması ve pencere büyümesi
// aynı anda bitmeli, yoksa biri ötekini bekliyormuş gibi görünür.
const modalZoomStart = 0.94

// drawModalZoomed draws the dialog, scaled by the active transition.
//
// Geçiş yoksa (ya da animasyonlar kapalıysa) hiçbir ek iş yapılmaz: pencere
// doğrudan çizilir.
func (a *App) drawModalZoomed(m Modal, in image.Rectangle) {
	u := a.ui

	scale, active := a.modalZoomScale()
	if !active {
		m.Draw(a, in)
		return
	}

	// Pencere gövdesi ZATEN çizildi (u.Modal çerçeveyi çizdi); içeriği de
	// çizip sonra TÜM pencere dikdörtgenini ölçekliyoruz. Ölçeklenecek alan
	// çerçeveyi de kapsamalı, yoksa içerik büyürken çerçeve yerinde kalır.
	m.Draw(a, in)

	// Çerçeve, içerik dikdörtgeninin dışına taşıyor; cömert bir pay
	// bırakıyoruz. Ekran dışına taşan kısım Intersect ile kırpılıyor.
	pad := u.M.PadX * 2
	rect := image.Rect(in.Min.X-pad, in.Min.Y-pad*2, in.Max.X+pad, in.Max.Y+pad*2).
		Intersect(u.Bounds())
	if rect.Empty() {
		return
	}

	a.mu.Lock()
	if a.scratch == nil || a.scratch.Bounds() != u.Bounds() {
		a.scratch = image.NewRGBA(u.Bounds())
	}
	scratch := a.scratch
	a.mu.Unlock()

	// Zoom KAYNAK ve HEDEF farklı tampon ister: aynı tampondan okuyup yazmak,
	// okunan pikselin çoktan ezilmiş olmasına yol açar.
	copyRectRGBA(scratch, u.Canvas(), rect)
	fbdraw.Zoom(u.Canvas(), scratch, rect, scale)
}

// modalZoomScale returns the current dialog scale and whether it applies.
func (a *App) modalZoomScale() (float64, bool) {
	if !a.animationsOn() {
		return 1, false
	}
	a.mu.Lock()
	tr := a.trans
	a.mu.Unlock()
	if tr == nil || tr.kind != transFade {
		return 1, false
	}
	t, _ := tr.progress()
	if t >= 1 {
		return 1, false
	}
	// EaseOutBack: sonunda hafifçe aşıp yerine oturur. Bir pencerenin
	// "yerine oturduğu" hissini veren şey budur; doğrusal bir büyüme
	// mekanik görünür.
	e := fbdraw.EaseOutBack(t)
	return modalZoomStart + (1-modalZoomStart)*e, true
}

// copyRectRGBA copies one rectangle between same-sized buffers.
func copyRectRGBA(dst, src *image.RGBA, r image.Rectangle) {
	r = r.Intersect(dst.Bounds()).Intersect(src.Bounds())
	if r.Empty() {
		return
	}
	w := r.Dx() * 4
	for y := r.Min.Y; y < r.Max.Y; y++ {
		o := dst.PixOffset(r.Min.X, y)
		s := src.PixOffset(r.Min.X, y)
		copy(dst.Pix[o:o+w], src.Pix[s:s+w])
	}
}
