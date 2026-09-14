package fbpanel

import (
	"image"
	"math"
	"time"

	"mcos/internal/fbdraw"
	"mcos/internal/fbui"
)

// Bu dosya İSTEĞE BAĞLI panel kilidini uygular.
//
// ── Neden isteğe bağlı? ─────────────────────────────────────────────────────
// MCOS çoğu zaman evdeki bir makinede, klavyesiz bir köşede durur. Zorunlu
// bir parola, kullanıcıyı kendi sunucusundan kilitler ve "parolayı unuttum"
// durumunda tek çıkış yolu yeniden kurulum olur. Bu yüzden parola yoksa
// kilit ekranı HİÇ devreye girmez.
//
// ── Ne korur, ne korumaz? ───────────────────────────────────────────────────
// KORUR: yanından geçen birinin sunucuyu durdurması, ayarları değiştirmesi,
// diski silmesi.
// KORUMAZ: makineyi açıp diski çıkaran birini. Disk şifreli değildir ve
// MCOS bunu iddia etmez — kullanıcıya da böyle söylüyoruz (ayarlar ekranı).
//
// Bunu açıkça yazmak önemli: "parola koydum, verim güvende" sanan bir
// kullanıcıya yalan söylemiş oluruz.

// ── Kilit ekranı parola alanının animasyonu ─────────────────────────────────
//
// Kullanıcının isteği: "yazma animasyonu şifre girerken falan, silerken".
//
// Kilit ekranı TextModal KULLANMAZ — tam ekrandır, pencere değil — bu yüzden
// aynı hareket burada ayrıca kuruldu. Süreler password.go ile AYNI sabitlerden
// geliyor: iki parola alanının farklı hızda hareket etmesi, aynı sistemde iki
// ayrı arayüz gibi hissettirirdi.
//
// Tek gerçek fark HİZALAMA: pencere alanı sola dayalıdır, kilit ekranı
// ORTALIDIR. Yani işaret sayısı değiştiğinde bütün satır kayar. Silme
// animasyonu bunu hesaba katmak zorunda: hayalet, işaretin ESKİ düzendeki
// yerinde çizilir ve kalan işaretler eski yerlerinden yeni yerlerine kayar.
// Aksi hâlde silinen işaret, kalanların altından fırlamış gibi görünürdü.
type lockAnim struct {
	// typedAt, son karakterin eklendiği an.
	typedAt time.Time
	// delAt / delIdx, silinen son karakterin hayaleti.
	delAt  time.Time
	delIdx int
	// clearAt / clearN, Ctrl+U ile topluca silinenler.
	clearAt time.Time
	clearN  int
	// shakeAt, yanlış parolada alanın sarsıldığı an.
	shakeAt time.Time
}

// animating reports whether any lock-field animation is still running.
func (l *lockAnim) animating() bool {
	now := time.Now()
	for _, s := range []struct {
		at  time.Time
		dur time.Duration
	}{
		{l.typedAt, typePopDur},
		{l.delAt, delGhostDur},
		{l.clearAt, clearDur + time.Duration(l.clearN)*clearStagger},
		{l.shakeAt, shakeDur},
	} {
		if !s.at.IsZero() && now.Sub(s.at) < s.dur {
			return true
		}
	}
	return false
}

// reset clears every animation stamp (used when the screen is re-armed).
func (l *lockAnim) reset() { *l = lockAnim{} }

// lockMaxTries is how many wrong attempts trigger the slow-down.
const lockMaxTries = 5

// lockPenalty is how long the screen refuses input after too many tries.
//
// Kilidi kalıcı olarak kapatmıyoruz (kullanıcı kendi makinesinden tamamen
// kilitlenmemeli), yalnızca yavaşlatıyoruz: 20 saniyelik bir bekleme, elle
// deneme yanılmayı anlamsız kılar ama sahibini fazla cezalandırmaz.
const lockPenalty = 20 * time.Second

// Locked reports whether the panel is showing the lock screen.
func (a *App) Locked() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.locked
}

// PasswordRequired reports whether a panel password is configured.
func (a *App) PasswordRequired() bool {
	_, _, cfg := a.Snapshot()
	return cfg != nil && cfg.Security.PasswordSet()
}

// Lock shows the lock screen. No-op when no password is set.
func (a *App) Lock() {
	if !a.PasswordRequired() {
		return
	}
	a.mu.Lock()
	a.locked = true
	a.lockInput = ""
	a.lockErr = ""
	a.dirty = true
	a.mu.Unlock()
}

// lockOnSleepWanted reports whether waking should ask for the password.
func (a *App) lockOnSleepWanted() bool {
	_, _, cfg := a.Snapshot()
	return cfg != nil && cfg.Security.PasswordSet() && cfg.Security.LockOnSleep
}

// unlock clears the lock screen.
func (a *App) unlock() {
	a.mu.Lock()
	a.locked = false
	a.lockInput = ""
	a.lockErr = ""
	a.lockTries = 0
	a.mu.Unlock()
	a.Emit(fbui.EventOK, "Kilit açıldı")
	a.beginTransition(transFade)
}

// lockPenaltyLeft returns how long input is still refused.
func (a *App) lockPenaltyLeft() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lockTries < lockMaxTries || a.lockUntil.IsZero() {
		return 0
	}
	d := time.Until(a.lockUntil)
	if d < 0 {
		return 0
	}
	return d
}

// lockKey handles a keystroke on the lock screen.
func (a *App) lockKey(key string) Action {
	defer a.Invalidate()

	if left := a.lockPenaltyLeft(); left > 0 {
		a.mu.Lock()
		a.lockErr = "Çok fazla deneme — " +
			itoa(int(left.Seconds())+1) + " saniye bekleyin"
		a.mu.Unlock()
		return ActNone
	}

	switch key {
	case "enter":
		a.mu.Lock()
		attempt := a.lockInput
		a.lockInput = ""
		a.mu.Unlock()

		_, _, cfg := a.Snapshot()
		if cfg != nil && cfg.Security.VerifyPassword(attempt) {
			a.unlock()
			return ActNone
		}
		a.mu.Lock()
		a.lockTries++
		a.lockErr = "Parola yanlış"
		// Sarsılma, hata METNİNDEN önce algılanır: kullanıcı yazıyı okumadan
		// parolanın yanlış olduğunu anlar. Aynı jest metin alanında da var.
		a.lockAn.reset()
		a.lockAn.shakeAt = time.Now()
		if a.lockTries >= lockMaxTries {
			a.lockUntil = time.Now().Add(lockPenalty)
			a.lockErr = "Çok fazla deneme — " +
				itoa(int(lockPenalty.Seconds())) + " saniye bekleyin"
		}
		a.mu.Unlock()
		return ActNone

	case "backspace":
		a.mu.Lock()
		if n := len([]rune(a.lockInput)); n > 0 {
			a.lockAn.delIdx = n - 1
			a.lockAn.delAt = time.Now()
			// Süren bir yazma animasyonu iptal: ikisi aynı anda çalışırsa
			// silinen işaretin yerinde hem hayalet hem "yeni giren" işaret
			// çizilirdi.
			a.lockAn.typedAt = time.Time{}
			a.lockInput = string([]rune(a.lockInput)[:n-1])
		}
		a.lockErr = ""
		a.mu.Unlock()
		return ActNone

	case "ctrl+u":
		a.mu.Lock()
		if n := len([]rune(a.lockInput)); n > 0 {
			a.lockAn.clearN = n
			a.lockAn.clearAt = time.Now()
			a.lockAn.typedAt = time.Time{}
			a.lockAn.delAt = time.Time{}
		}
		a.lockInput = ""
		a.lockErr = ""
		a.mu.Unlock()
		return ActNone

	case "space":
		a.appendLock(" ")
		return ActNone

	case "ctrl+c":
		// Kilitliyken çıkış YOK: paneli kapatmak kilidi anlamsız kılardı
		// (arkada kabuk yok, ama yine de doğru davranış bu).
		return ActNone
	}

	rs := []rune(key)
	if len(rs) == 1 {
		a.appendLock(string(rs))
	}
	return ActNone
}

func (a *App) appendLock(s string) {
	a.mu.Lock()
	if len([]rune(a.lockInput)) < 64 {
		a.lockInput += s
		a.lockAn.typedAt = time.Now()
		a.lockAn.delAt = time.Time{}
	} else {
		// Sınıra dayanan karakter SESSİZCE yutulmaz: alan sarsılır. Maskeli
		// bir alanda kullanıcı yazdığını okuyup sayamaz; sessiz yutma ona
		// "klavyem çalışmıyor" dedirtir.
		a.lockAn.shakeAt = time.Now()
	}
	a.lockErr = ""
	a.mu.Unlock()
}

// drawLock paints the full-screen lock prompt.
//
// Arka planda sistem durumu GÖSTERİLMEZ: kaç sunucu çalıştığı, hangi ağa
// bağlı olduğu bile bilgi sızıntısıdır. Yalnızca logo, saat ve alan.
func (a *App) drawLock(b image.Rectangle) {
	u := a.ui
	u.P.Fill(b, u.Pal.Bg)

	// Hafif bir arka plan dokusu: düz siyah bir ekran "çöktü" gibi görünür.
	cx := float64(b.Min.X+b.Max.X) / 2
	cy := float64(b.Min.Y) + float64(b.Dy())*0.34
	frame := a.Spin()

	size := math.Min(float64(b.Dx()), float64(b.Dy())) * 0.12
	u.LogoPulse(cx, cy, size, frame, u.Pal.Accent)

	y := int(cy+size) + u.M.PadY*4
	u.TextCenter(b.Min.X, b.Max.X, y, "MCOS kilitli", u.Pal.Text)
	y += u.F.CellH + u.M.PadY*2

	// Saat: kullanıcı makinenin canlı olduğunu görmeli.
	u.TextCenter(b.Min.X, b.Max.X, y, time.Now().Format("15:04"), u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY*4

	// Parola alanı: ekranın ortasında, sabit genişlikte.
	fw := b.Dx() / 3
	if fw < u.F.CellW*24 {
		fw = u.F.CellW * 24
	}
	fx := b.Min.X + (b.Dx()-fw)/2
	fh := u.M.ButtonH

	a.mu.Lock()
	n := len([]rune(a.lockInput))
	errText := a.lockErr
	an := a.lockAn
	a.mu.Unlock()

	// Yanlış parolada alan yatayda gider gelir: üç tam salınım, genliği
	// doğrusal sönen. Metin alanındaki (password.go) jestle AYNI.
	shakeDX := 0
	if p := progress(an.shakeAt, shakeDur); p >= 0 {
		amp := float64(u.F.CellW) * 0.55 * (1 - p)
		shakeDX = int(math.Round(amp * math.Sin(p*3*2*math.Pi)))
	}
	fx += shakeDX

	border := u.Pal.BorderFocus
	if errText != "" {
		border = u.Pal.Error
	}
	u.P.FillRoundRect(fbdraw.R(float64(fx), float64(y), float64(fw), float64(fh)),
		u.M.RadiusSmall, u.Pal.Surface)
	u.P.StrokeRoundRect(fbdraw.R(float64(fx), float64(y), float64(fw), float64(fh)),
		u.M.RadiusSmall, u.M.StrokeFocus, border)

	ty := y + (fh-u.F.CellH)/2
	if n == 0 && !an.removing() {
		u.TextCenter(fx, fx+fw, ty, "parola", u.Pal.TextFaint)
	} else {
		a.drawLockDots(fx, fw, ty, n, an)
	}
	y += fh + u.M.PadY*2

	if errText != "" {
		u.TextCenter(b.Min.X, b.Max.X, y, errText, u.Pal.Error)
		y += u.F.CellH + u.M.PadY
	}

	u.TextCenter(b.Min.X, b.Max.X, y, "Parolayı yazıp Enter'a basın", u.Pal.TextFaint)

	// Alt çubuk kilitliyken de var: kullanıcı hangi tuşların çalıştığını
	// görmeli, ama hiçbir SİSTEM bilgisi göstermez.
	u.StatusBar(nil, []fbui.Shortcut{
		{Key: "Enter", Label: "Aç"},
		{Key: "^U", Label: "Temizle"},
	}, frame)
}

// removing reports whether a delete/clear ghost is still on screen.
//
// Alan BOŞ ama hayalet hâlâ çiziliyorsa parola örnek metni YAZILMAZ: yoksa
// son işaret silinirken örnek metin onun üstüne biner ve iki yazı üst üste
// görünür.
func (l *lockAnim) removing() bool {
	return progress(l.delAt, delGhostDur) >= 0 ||
		progress(l.clearAt, clearDur+time.Duration(l.clearN)*clearStagger) >= 0
}
