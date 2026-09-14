package fbpanel

import (
	"image"
	"time"

	"mcos/internal/fbdraw"
)

// Bu dosya EKRANLAR ARASI GEÇİŞLERİ yönetir.
//
// ── Neden geçiş? ────────────────────────────────────────────────────────────
// Sert kesme (bir kare eski ekran, sonraki kare yeni ekran) beynin
// "ne değişti?" sorusunu yanıtlamasını zorlaştırır. 180 ms'lik bir kayma,
// yeni içeriğin NEREDEN geldiğini gösterir ve arayüzü tek parça hissettirir.
//
// ── Neden yalnızca içerik alanı? ────────────────────────────────────────────
// Kenar çubuğu YERİNDE KALIR. Değişen yalnızca sağdaki içeriktir; onu
// kaydırmak "menü sabit, sayfa değişti" der. Tüm ekranı kaydırmak, menünün
// de değiştiği yanılsaması yaratırdı — ve 1080p'de iki kat pahalıdır.
//
// ── Bellek ──────────────────────────────────────────────────────────────────
// Bir geçiş, önceki karenin KOPYASINI ister: 1920x1080x4 = 8.3 MB. Bu tampon
// TEMBEL ayrılır, animasyonlar kapalıysa HİÇ ayrılmaz ve kullanıcı ayarı
// kapatırsa ilk karede GERİ VERİLİR. MCOS zayıf makineleri hedefler;
// kullanmayan kullanıcı bedelini ödememeli.
//
// Animasyonlar AÇIKKEN prevFrame elde tutulur: geçiş isteği her an (üstelik
// arka plandan) gelebildiği için "önceki kare" sürekli hazır olmalıdır.
// Açılışa özgü ikinci tampon (scratch) ise açılış bitince serbest bırakılır.
//
// ── Kopya NE ZAMAN alınır? ──────────────────────────────────────────────────
// Tuvale yalnızca ÇİZİM GOROUTINE'İ dokunur ve Draw() bunu hiçbir kilit
// tutmadan yapar. Bu yüzden "önceki kare" kopyası geçiş İSTENDİĞİNDE değil,
// her karenin SONUNDA (applyTransition → captureLastFrame) alınır: istek
// USB taraması, eş taraması, kurulum kaydı ve sunucu oluşturma
// goroutine'lerinden de gelir; oradan tuvali okumak hem yarış hem de yarım
// boyanmış bir kare demektir.

// transKind selects the visual form of a transition.
type transKind int

const (
	// transNone: geçiş yok.
	transNone transKind = iota
	// transSlideDown: yeni içerik aşağıdan gelir (listede aşağı inildi).
	transSlideDown
	// transSlideUp: yeni içerik yukarıdan gelir.
	transSlideUp
	// transFade: yerinde soluklaşarak değişir (odak değişimi gibi küçük
	// değişiklikler için).
	transFade
	// transIntro: açılış — yakınlaşarak ve bulanıklığı azalarak gelir.
	transIntro
)

// transDuration is how long a normal screen transition lasts.
//
// 180 ms: 100 ms'nin altı fark edilmez (boşa harcanan iş), 300 ms'nin üstü
// arayüzü yavaş hissettirir. Materyal tasarım kılavuzu da 150-250 ms önerir.
const transDuration = 180 * time.Millisecond

// introDuration is the first-boot zoom-in. Bilerek daha uzun: bu bir
// karşılama jestidir, bir gezinme değil.
const introDuration = 900 * time.Millisecond

// transition is an in-progress screen change.
type transition struct {
	kind  transKind
	start time.Time
	dur   time.Duration
	// pending ise SAAT HENÜZ BAŞLAMAMIŞTIR: progress() sürekli 0 döner ve
	// geçiş ilk karesinde donmuş gibi görünür. armIntro() saati başlatır.
	//
	// ── Düzeltilen gerçek hata ──────────────────────────────────────────
	// Açılış geçişi HİÇ GÖRÜNMÜYORDU. QEMU'da 100 ms aralıklarla kare
	// alındığında 900 ms'lik yakınlaşmanın tek bir ara karesi bile yoktu:
	// ekran açılış ekranından panele TEK KAREDE atlıyordu.
	//
	// Sebep, geçişin kendisi değil SAATİYDİ. Sıra şuydu:
	//
	//   BeginIntro(kare)      <- saat burada başlıyordu
	//   app.Draw(); Flip()    <- bir kare (t≈0, yani açılış ekranının aynısı)
	//   Run(): a.refresh()    <- mcosd'ye BLOKLAYAN RPC
	//          a.refresh()    <- ikincisi
	//   ... ancak bundan sonra döngü çizmeye başlıyordu
	//
	// İki refresh yeni açılmış bir daemon'dan sunucu, durum, Java ve küme
	// bilgisini toplar; yavaş bir makinede toplam süre 900 ms'yi rahatça
	// aşar. Yani animasyon, HİÇBİR kare çizilmeden bitiyordu. Ekranda
	// görünen tek şey t=0 karesi (açılış ekranı) ve ardından sert kesme
	// oluyordu — kullanıcının "geçiş efekti yok" dediği şey tam olarak bu.
	//
	// Saati duvar saatinden alıp İLK GERÇEK KAREYE bağlamak doğrusu:
	// açılış ne kadar sürerse sürsün yakınlaşma tam olarak oynar.
	pending bool
}

// progress returns 0..1 (eased) and whether the transition is finished.
func (t *transition) progress() (float64, bool) {
	if t == nil || t.dur <= 0 {
		return 1, true
	}
	if t.pending {
		// Saat henüz başlamadı: ilk kareyi çiz ve bekle.
		return 0, false
	}
	el := time.Since(t.start)
	if el >= t.dur {
		return 1, true
	}
	raw := float64(el) / float64(t.dur)
	if t.kind == transIntro {
		// Açılışta hızlanıp yavaşlayan bir eğri: "içeri dalma" hissi
		// başta yumuşak olmalı.
		return fbdraw.EaseInOutCubic(raw), false
	}
	return fbdraw.EaseOutCubic(raw), false
}

// animationsOn reports whether transitions are enabled.
func (a *App) animationsOn() bool { return a.UIPrefs().Animations }

// beginTransition requests an animation. It NEVER touches the canvas.
//
// Animasyonlar kapalıysa HİÇBİR ŞEY yapmaz — ne kopya alınır ne bellek
// ayrılır.
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
// Bu fonksiyon eskiden o anki TUVALİ prevFrame'e kopyalıyordu. Oysa çağıran
// çoğu zaman ana döngü değil, bir arka plan goroutine'idir: USB taraması
// (keys.go), eş taraması (screen_peers.go), kurulum kaydı (setup.go), sunucu
// oluşturma (wizard.go) ve Wi-Fi taramasından açılan pencereler. Tuvali ise
// ana döngü Draw() içinde hiçbir kilit tutmadan boyar; a.mu 8 MB'lık piksel
// arabelleğini KORUMUYORDU.
//
// Kullanıcının gördüğü: "MCOS Paylaşım"da tarama bitip liste gelirken ekran
// yumuşak geçmiyor, bir an YARIM çizilmiş bir kareye (kenar çubuğu yenilenmiş,
// içerik hâlâ eski) atlayıp sıçrıyordu. Süren bir geçiş varken ikinci bir
// tarama biterse prevFrame ortasından değiştiği için soluklaşma yırtılıyordu.
// `go test -race` aynı arabellekte veri yarışı raporluyordu.
//
// Artık burada YALNIZCA istek kaydedilir (kilit altında, iki alan); önceki
// karenin kopyası çizim goroutine'inde, bir önceki karenin sonunda çoktan
// alınmıştır (bkz. captureLastFrame).
func (a *App) beginTransition(kind transKind) {
	// animationsOn kendi kilidini alır: a.mu TUTULURKEN çağrılamaz
	// (sync.Mutex yeniden girilebilir değildir, kendi kendine kilitlenirdi).
	if !a.animationsOn() {
		return
	}
	a.mu.Lock()
	// Bekleyen bir AÇILIŞ geçişi EZİLMEZ. O geçiş, panel daha ilk karesini
	// çizmeden kurulur ve saati döngüye girilirken başlar (armIntro); tam o
	// aralıkta bir arka plan goroutine'i (kurulum sihirbazı, eş taraması)
	// 180 ms'lik bir soluklaşma isteyebilir ve açılış yakınlaşmasını
	// sessizce yok ederdi. Açılış jesti, o kısa geçişten daha önemli.
	if a.trans == nil || !a.trans.pending {
		a.trans = &transition{kind: kind, start: time.Now(), dur: transDuration}
		a.dirty = true
	}
	a.mu.Unlock()
}

// BeginIntro starts the first-boot zoom-in from a captured splash frame.
//
// from, açılış ekranının son karesidir. nil ise o anki tuval kullanılır
// (açılış ekranı çalışmadıysa yine de yumuşak bir giriş olur).
//
// YALNIZCA çalışma döngüsü başlamadan önce çağrılır (cmd/mcos-panel-fb, ilk
// Draw'dan önce). Tuvali okuyan tek diğer yer burasıdır ve o anda ortada
// başka goroutine yoktur; döngü başladıktan sonra çağrılırsa beginTransition
// öncesindeki yarışın aynısı geri gelir.
func (a *App) BeginIntro(from *image.RGBA) {
	if !a.animationsOn() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.ui.Bounds()
	if from != nil && from.Bounds() == b {
		if a.prevFrame == nil || a.prevFrame.Bounds() != b {
			a.prevFrame = image.NewRGBA(b)
		}
		copy(a.prevFrame.Pix, from.Pix)
	} else {
		a.snapshotFrameLocked()
	}
	// pending: saat burada DEĞİL, armIntro()'da başlar. Gerekçe transition
	// türünün yanında yazılı — açılışın bloklayan RPC'leri arada duruyor.
	a.trans = &transition{kind: transIntro, dur: introDuration, pending: true}
	a.dirty = true
}

// armIntro starts the clock of a pending boot transition.
//
// Run(), açılıştaki bloklayan işleri (iki refresh, kurulum sihirbazının
// kurulması, kilit ekranı) BİTİRDİKTEN sonra, tam çizim döngüsüne girmeden
// önce çağırır. O ana kadar ekranda açılış ekranının son karesi durur —
// yani hiçbir kırpışma olmaz, çünkü zaten ekranda olan görüntünün aynısıdır.
//
// Bekleyen bir açılış geçişi yoksa hiçbir şey yapmaz.
func (a *App) armIntro() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.trans == nil || !a.trans.pending {
		return
	}
	a.trans.pending = false
	a.trans.start = time.Now()
	a.dirty = true
}

// snapshotFrameLocked copies the canvas into prevFrame. Caller holds a.mu.
//
// Tuvali okur: yalnızca BeginIntro'dan, yani döngü başlamadan çağrılabilir.
func (a *App) snapshotFrameLocked() {
	b := a.ui.Bounds()
	if a.prevFrame == nil || a.prevFrame.Bounds() != b {
		a.prevFrame = image.NewRGBA(b)
	}
	copy(a.prevFrame.Pix, a.canvasPix())
}

// canvasPix returns the raw pixels the UI is drawing into.
func (a *App) canvasPix() []uint8 { return a.ui.Pix() }

// transitionActive reports whether an animation is running.
func (a *App) transitionActive() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.trans != nil
}

// applyTransition composes the freshly drawn frame with the previous one and
// then remembers this frame as the source of the next transition.
//
// Draw()'un SONUNDA, ÇİZİM GOROUTINE'İNDE çağrılır: o noktada tuvalde YENİ
// kare vardır ve prevFrame'de bir önceki kare durur. Tuval piksellerine
// dokunan tek yol budur (BeginIntro hariç, o da döngüden önce çalışır);
// beginTransition artık dokunmadığı için arabellek bir kilide muhtaç değil:
// ona erişen tek goroutine var.
func (a *App) applyTransition(content image.Rectangle) {
	// Kilidi ALMADAN önce: animationsOn a.mu'yu kendisi kilitler.
	on := a.animationsOn()

	a.mu.Lock()
	tr := a.trans
	prev := a.prevFrame
	a.mu.Unlock()

	if !on {
		// Animasyonlar kapalı: 8.3 MB'lık tamponu elde tutma. Kullanıcı
		// ayarı kapattığı anda bellek geri verilir; sürmekte olan bir
		// geçiş de sert kesmeye döner (istenen zaten budur).
		if tr != nil || prev != nil {
			a.mu.Lock()
			a.trans = nil
			a.prevFrame = nil
			a.scratch = nil
			a.mu.Unlock()
		}
		return
	}

	if tr == nil {
		a.captureLastFrame()
		return
	}
	if prev == nil {
		// Geçiş istendi ama karşılaştıracak kare yok: ilk kare, ya da
		// animasyonlar az önce açıldı. Sert kesme yap ama BU kareyi
		// yakala ki bir sonraki geçiş yumuşak olsun.
		//
		// Temizlemek şart: Tick() a.trans != nil iken her turda "yeniden
		// çiz" der; bırakırsak boştaki makine sonsuza dek kare basar.
		a.finishTransition(tr)
		a.captureLastFrame()
		return
	}

	t, done := tr.progress()
	b := a.ui.Bounds()

	switch tr.kind {
	case transIntro:
		// Açılış: eski kare (açılış ekranı) yakınlaşır ve bulanıklaşırken
		// yeni ekran netleşerek belirir.
		//
		// Kullanıcının isteği: "boot animasyonu bitince içeri zoomlanarak
		// blur felan ile OOBE'nin ilk ekranı gelsin".
		a.mu.Lock()
		if a.scratch == nil || a.scratch.Bounds() != b {
			a.scratch = image.NewRGBA(b)
		}
		scratch := a.scratch
		a.mu.Unlock()

		// Ölçek 1.00 → 1.35: içeri dalma. Bulanıklık 0 → hücre yüksekliği:
		// eski ekran odaktan çıkar.
		scale := 1 + 0.35*t
		radius := int(float64(a.ui.F.CellH) * 0.9 * t)
		fbdraw.ZoomBlurFade(a.ui.Canvas(), prev, scratch, b, scale, radius, t)

	case transFade:
		fbdraw.CrossFade(a.ui.Canvas(), prev, content, t)

	case transSlideDown, transSlideUp:
		// Kayma mesafesi içerik genişliğinin %8'i: fark edilir ama
		// "fırlatılmış" görünmez.
		dist := content.Dx() * 8 / 100
		dx := int(float64(dist) * (1 - t))
		if tr.kind == transSlideUp {
			dx = -dx
		}
		fbdraw.SlideBlend(a.ui.Canvas(), prev, content, dx, t)
	}

	if done {
		// Biten geçişin son karesi (t=1) ARTIK ekranda duran karedir.
		// Bir sonraki geçişin kaynağı olarak sakla: burada yakalamazsak
		// panel geçişten sonra boşa düşerse prevFrame geçiş ÖNCESİNDEKİ
		// kareyle kalır ve sonraki soluklaşma iki adım eski bir görüntüden
		// başlardı.
		//
		// ── Yakalanan gerçek hata ───────────────────────────────────────
		// Koşul eskiden "done && a.finishTransition(tr)" idi: kare ancak
		// geçiş HÂLÂ bizimken saklanıyordu. Oysa bu karenin çizimi birkaç
		// milisaniye sürer ve arka plan goroutine'i (eş taraması bitişi,
		// sunucu oluşturma, kurulum kaydı) tam o aralıkta yeni bir geçiş
		// kurabilir; o zaman finishTransition false döner ve kare HİÇ
		// saklanmazdı.
		//
		// Kullanıcının gördüğü: A ekranından B'ye geçerken tarama bitip
		// C'ye gidilince, C'ye soluklaşma B'den değil A'dan başlıyordu —
		// çoktan terk edilmiş bir ekran bir an geri gelip yanıp sönüyordu.
		//
		// t=1'de tuvalde zaten YENİ kare (B) durur, yani onu saklamak her
		// iki durumda da doğrudur; koşula bağlamanın bir faydası yoktu.
		a.finishTransition(tr)
		a.captureLastFrame()
	}
}

// finishTransition clears tr, but only if it is still the active transition.
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
// Eskiden koşulsuz "a.trans = nil" yazılırdı. Bir arka plan goroutine'i (eş
// taraması bitişi, sunucu oluşturma, kurulum kaydı) tam bu kare çizilirken
// YENİ bir geçiş kurmuşsa, o geçiş daha ilk karesi çizilmeden sessizce iptal
// oluyordu. Kullanıcının gördüğü: aynı iş bazen yumuşak geçiyor, bazen hiç
// animasyonsuz sıçrıyordu — ve hangisi olacağı zamanlamaya bağlıydı.
//
// Artık KARŞILAŞTIR-VE-TEMİZLE: yalnızca hâlâ bizim geçişimizse silinir.
func (a *App) finishTransition(tr *transition) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.trans != tr {
		return
	}
	a.trans = nil
	// Açılış çalışma arabelleği 8.3 MB'dır ve YALNIZCA açılışta kullanılır.
	// Açılış bir kez olur; bittiğinde belleği geri ver. (prevFrame kalır:
	// bir sonraki geçişin kaynağı odur.)
	if tr.kind == transIntro {
		a.scratch = nil
	}
}

// captureLastFrame stores the frame now on the canvas as the source of the
// NEXT transition.
//
// YALNIZCA çizim goroutine'inden (applyTransition) çağrılır; tuvali okuyan
// tek yer orasıdır, dolayısıyla Draw ile asla çakışmaz.
//
// ── Neden her karede? ───────────────────────────────────────────────────────
// Geçiş isteği çizimle ilgisiz bir anda, arka plandan gelir. Kopyayı karenin
// SONUNDA almak, istek geldiğinde elimizde TAM ve o an ekranda duran bir kare
// olmasını garanti eder — yarım boyanmış bir kare değil.
//
// Maliyeti bitişik TEK bir memmove'dur (1080p'de 8.3 MB); tüm ekranı yazı ve
// şekil olarak yeniden boyamış bir karenin yanında küçüktür, animasyonlar
// kapalıyken hiç yapılmaz ve ekran kirli değilken Draw zaten çalışmaz.
//
// Süren bir geçiş varken ATLANIR: yoksa geçiş kendi çıktısını kaynak alır ve
// soluklaşma "iz bırakarak" sürüklenirdi.
func (a *App) captureLastFrame() {
	b := a.ui.Bounds()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.prevFrame == nil || a.prevFrame.Bounds() != b {
		a.prevFrame = image.NewRGBA(b)
	}
	copy(a.prevFrame.Pix, a.canvasPix())
}

// Spin returns the animation frame counter (used by animated widgets).
func (a *App) Spin() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.spin
}

// scanning reports whether a background scan is in progress, so screens can
// draw the radar animation instead of an empty list.
func (a *App) scanning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.scanBusy
}

// setScanning marks a scan as running or finished.
func (a *App) setScanning(on bool) {
	a.mu.Lock()
	a.scanBusy = on
	a.dirty = true
	a.mu.Unlock()
}
