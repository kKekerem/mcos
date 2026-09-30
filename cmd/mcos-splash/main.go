// Command mcos-splash draws the MCOS boot animation on the framebuffer.
//
// ════════════════════════════════════════════════════════════════════════════
// NE İŞE YARIYOR
// ════════════════════════════════════════════════════════════════════════════
//
// Açılışta ekranda çekirdek mesajları akar, sonra bir süre siyah kalır, sonra
// panel belirir. Kullanıcı bu arada makinenin çalışıp çalışmadığını bilemez.
//
// Bu program o boşluğu doldurur: logo, nefes alan bir hâle, ilerleme halkası
// ve "ne yapılıyor" satırı. Panel hazır olduğunda kendiliğinden çıkar ve
// SON KARESİNİ kaydeder — panel oradan yakınlaşarak açılır ("içeri zoomlanarak
// blur ile OOBE'nin ilk ekranı gelsin").
//
// ════════════════════════════════════════════════════════════════════════════
// NASIL DURUR
// ════════════════════════════════════════════════════════════════════════════
//
// Üç yoldan biriyle, hangisi önce olursa:
//
//  1. --until ile verilen dosya oluşur (panel "hazırım" der),
//  2. --timeout dolar (bir şey takıldıysa kullanıcı sonsuza kadar
//     logoya bakmasın),
//  3. SIGTERM gelir.
//
// Her durumda konsol GERİ VERİLİR. Bu dosyadaki en önemli satır o defer'dır:
// grafik kipinde ölen bir program, kullanılamaz bir konsol bırakır.
package main

import (
	"flag"
	"fmt"
	"image"
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mcos/internal/fbdev"
	"mcos/internal/fbdraw"
	"mcos/internal/fbfont"
	"mcos/internal/fbui"
	"mcos/internal/fbvt"
	"mcos/internal/version"
)

// frameInterval is the animation tick.
//
// 33 ms ≈ 30 kare/sn. Açılışta CPU zaten meşguldür (servisler başlıyor);
// 60 kare/sn hedeflemek açılışı YAVAŞLATIRDI. 30 kare akıcı görünür.
const frameInterval = 33 * time.Millisecond

// stageFile lets the init script tell the splash what is happening.
//
// Tek satırlık düz metin. Yoksa varsayılan mesaj kullanılır — yani init
// betiği bunu hiç yazmazsa açılış ekranı yine de düzgün çalışır.
// ── Neden /run DEĞİL /dev altında ───────────────────────────────────────────
//
// Açılış animasyonu artık initramfs'in /init'inden, yani busybox init'ten
// ÖNCE başlıyor (kullanıcının isteği: "direkt acılırken ilk animasyon
// baslasın"). O anda /run sıradan bir dizindir; inittab'in "mount -a" satırı
// birkaç saniye sonra ÜSTÜNE tmpfs bağlar ve o ana kadar yazılmış her şey
// görünmez olur — durum dosyası, PID dosyası, "hazırım" bayrağı, hepsi.
//
// Sonuç sessiz ve tuhaf olurdu: S04splash animasyonun çalıştığını göremeyip
// İKİNCİ bir animasyon başlatır, ilki ise artık kimsenin yazmadığı bir durum
// dosyasını okumaya devam ederdi.
//
// /dev devtmpfs'tir ve TEK BİR çekirdek örneğidir: ikinci kez bağlansa bile
// aynı ağacı gösterir, içeriği kaybolmaz. Bu yüzden açılış koordinasyonu
// oraya taşındı.
const mcosRunDir = "/dev/.mcos"

const defaultStageFile = mcosRunDir + "/stage"

func main() {
	var (
		fbPath  = flag.String("fb", "/dev/fb0", "framebuffer aygıtı")
		ttyPath = flag.String("tty", "/dev/tty", "konsol aygıtı")
		until   = flag.String("until", mcosRunDir+"/ready",
			"bu dosya oluşunca çık")
		stage = flag.String("stage", defaultStageFile, "durum metni dosyası")
		save  = flag.String("save", mcosRunDir+"/frame.rgba",
			"son kareyi buraya yaz (panel geçişi için)")
		timeout = flag.Duration("timeout", 45*time.Second, "en fazla bekleme")
		fontPx  = flag.Float64("font", 0, "yazı boyutu (0 = otomatik)")
		shot    = flag.String("screenshot", "", "bir kare çizip PNG yaz ve çık")
		brand   = flag.Bool("brand", false,
			"ekran görüntüsünde YALNIZCA marka karesi (ilerleme ve donanım yok)")
		minShow = flag.Duration("min", 1200*time.Millisecond,
			"en az bu kadar göster (yanıp sönmeyi engeller)")
		keep = flag.Bool("keep", false,
			"çıkarken konsolu metin kipine DÖNDÜRME (panel devralacak)")
		restore = flag.Bool("restore", false,
			"yalnızca konsolu metin kipine döndür ve çık")
	)
	flag.Parse()

	// ── Yalnızca geri yükleme ───────────────────────────────────────────
	// --keep ile çalışan bir açılış ekranından sonra panel açılmazsa konsol
	// grafik kipinde kalır ve kullanıcı kara ekranla baş başa kalır.
	// Başlatıcı betiği (mcos-launch → restore_console) bu bayrakla konsolu
	// geri alır.
	//
	// ── Yakalanan gerçek hata ───────────────────────────────────────────
	// Bu yol ETKİSİZDİ. fbvt.Open() o zamanlar mevcut kipi "önceki durum"
	// diye kaydediyor, Restore() de aynı kipi geri yazıyordu. Oysa --restore
	// yalnızca konsol ZATEN KD_GRAPHICS iken çağrılır; yani okunan değer
	// KD_GRAPHICS, geri yazılan değer de KD_GRAPHICS oluyordu ve hiçbir şey
	// değişmiyordu. Termios için de aynısı geçerliydi: splash'ın bıraktığı
	// ham ayarlar okunup aynen geri konuyordu.
	// Kullanıcı bunu şöyle görüyordu: panel açılamadığında (ör. mcosd
	// soketi 20 sn içinde gelmezse) mcos-launch kurtarma ekranını —
	// "MCOS paneli guvenli bekleme modunda", hata nedeni, daemon log kuyruğu
	// ve 5 saniyelik menü — grafik kipindeki konsola basıyordu. Ekranda
	// donmuş siyah bir kare vardı, tek satır yazı görünmüyordu.
	//
	// Artık fbvt.Restore() anlık görüntüyü körü körüne tekrarlamıyor:
	// devralma zincirinin BAŞINDAKİ duruma (metin kipi, kullanılabilir
	// klavye kipi, pişmiş termios) döner ve zinciri kapatır. Bu yüzden
	// buradaki Open()+Restore() çifti gerçekten kullanılabilir bir konsol
	// bırakır. Konsol hâlihazırda grafik kipinde olduğu için Open()'ın
	// kısa süreli devralması ekranda ek bir kırpışma yaratmaz.
	if *restore {
		con, err := fbvt.Open(*ttyPath, *fbPath)
		if err != nil {
			// Devralınamayan bir konsol (seri konsol, VT olmayan aygıt) geri
			// yüklenemez de. Sessizce başarılı görünmek yerine söylüyoruz:
			// kurtarma ekranının neden görünmediği aksi hâlde anlaşılmaz.
			fmt.Fprintln(os.Stderr, "mcos-splash: konsol geri yüklenemedi:", err)
			os.Exit(1)
		}
		if err := con.Restore(); err != nil {
			fmt.Fprintln(os.Stderr, "mcos-splash: konsol geri yüklenemedi:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(*fbPath, *ttyPath, *until, *stage, *save, *shot, *brand,
		*timeout, *minShow, *fontPx, *keep); err != nil {
		fmt.Fprintln(os.Stderr, "mcos-splash:", err)
		os.Exit(1)
	}
}

func run(fbPath, ttyPath, until, stage, save, shot string, brandOnly bool,
	timeout, minShow time.Duration, fontPx float64, keep bool) error {

	// ── Ekran görüntüsü kipi: donanım gerekmez ──────────────────────────
	if shot != "" {
		w, h := 1920, 1080
		canvas := image.NewRGBA(image.Rect(0, 0, w, h))
		ui, err := newUI(canvas, fontPx, h)
		if err != nil {
			return err
		}
		if brandOnly {
			// ── Neden AYRI bir kare ─────────────────────────────────────
			//
			// Bu kare GRUB'un arka planı olarak kullanılıyor, yani ekranda
			// çekirdek daha yüklenmeden görünüyor. O anda ilerleme diye bir
			// şey YOK ve donanım da OKUNMADI.
			//
			// Kullanıcı "ekrandaki açıklamalar gerçek olsun" dedi. Bir
			// ilerleme halkasını %62'de göstermek ya da işlemci adı yazmak
			// tam olarak o kuralı çiğnerdi: ikisi de uydurma olurdu.
			// Geriye doğru olan tek şey kalıyor — bu makine MCOS.
			drawBrand(ui)
		} else {
			drawSplash(ui, 18, 62, "Sunucular hazırlanıyor…", hardwareLine())
		}
		return fbdev.SavePNG(canvas, shot)
	}

	dev, err := fbdev.Open(fbPath)
	if err != nil {
		return fmt.Errorf("framebuffer açılamadı (%s): %w", fbPath, err)
	}
	defer dev.Close()

	w, h := dev.Size()
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	ui, err := newUI(canvas, fontPx, h)
	if err != nil {
		return err
	}

	// ── Konsolu devral ──────────────────────────────────────────────────
	// Bunu YAPMAZSAK çekirdek mesajları logonun üstüne yazmaya devam eder.
	con, err := fbvt.Open(ttyPath, fbPath)
	if err != nil {
		// Konsol devralınamıyorsa (ör. seri konsoldan çalışıyorsa) yine de
		// çiz: kötü bir açılış ekranı, hiç açılış ekranı olmamasından iyidir.
		con = nil
	}
	// handOver, konsolun panele devredileceğini söyler.
	//
	// ── Neden gerekli? ──────────────────────────────────────────────────
	// Çıkarken konsolu METİN kipine döndürmek normalde doğrudur. Ama hemen
	// ardından panel açılacaksa, metin kipine dönmek ekranda bir an konsol
	// metni gösterir ve "içeri zoomlanarak geçiş" etkisini kırar. --keep
	// ile geri yükleme atlanır; panel zaten grafik kipini kendi kurar.
	//
	// Panik ve sinyal yollarında YİNE DE geri yüklenir: orada panel
	// açılmayacaktır ve kullanıcıyı kara ekranda bırakmak kabul edilemez.
	//
	// Devralmadan ÖNCEKİ konsol durumu bu süreçle birlikte kaybolmaz:
	// fbvt.Open() onu /run altına yazar, panel (veya --restore) oradan okur.
	// Bu olmadan panel, splash'ın bıraktığı grafik kipini "önceki durum"
	// sanıp çıkışta geri yazardı ve konsol bir daha metne dönmezdi.
	handOver := false
	restore := func() {
		if con != nil && !handOver {
			_ = con.Restore()
		}
	}
	defer restore()
	defer func() {
		if r := recover(); r != nil {
			handOver = false
			if con != nil {
				_ = con.Restore()
			}
			panic(r)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// Durum/hazır/kare dosyalarını dizine BAĞLA: diskten açılışta switch_root
	// sonrası mutlak yollar bu süreç için çözülmüyor (bkz. anchor.go).
	until, stage, save = anchorRunDir(until, stage, save)

	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()

	start := time.Now()
	deadline := start.Add(timeout)
	frame := 0
	pct := 0
	msg := "Sistem başlatılıyor…"
	// ── Donanım satırı: BAŞARILI OLANA KADAR yeniden denenir ───────────
	//
	// Bir kez okumak YETMİYOR ve sebebi ölçüldü. Animasyon artık initramfs'in
	// /init'inden başlıyor; /init hemen ardından canlı sisteme devrederken
	// /proc ve /sys'i SÖKÜYOR (live_boot) ve busybox init onları birkaç yüz
	// milisaniye sonra yeniden bağlıyor. Animasyon tam o pencerede açılırsa
	// /proc/cpuinfo yoktur ve satır sonsuza kadar boş kalırdı — QEMU'da
	// gözlendi: ekranda donanım yerine sabit slogan yazıyordu.
	//
	// Saniyede bir yeniden denemek bunu kapatıyor. Okuma başarılı olduğunda
	// bir daha denenmez; yani maliyeti yalnızca ilk saniyelerde birkaç
	// /proc okumasıdır.
	hw := hardwareLine()

	for {
		// ── İlerleme: GERÇEK aşamalardan ───────────────────────────────
		//
		// Yüzde artık süreden değil, açılışın BİTEN ADIMLARINDAN geliyor
		// (bkz. stage.go ve /usr/bin/mcos-stage). Bir adım bittiğinde onu
		// bitiren betik yüzdeyi yazıyor.
		//
		// İki koruma var:
		//   - Yüzde GERİ GİTMEZ. Sıra dışı bir yazım (ör. eski bir betik)
		//     halkayı geri saramaz; ilerleme çubuğunun geri gitmesi,
		//     kullanıcıya "bir şeyler bozuldu" dedirtir.
		//   - Aşama bildirimi hiç gelmezse (tüm betikler eski) halka
		//     kıpırdamaz ama yavaşça soluklaşan bir bekleme göstergesi
		//     zaten dönüyor; "donmuş" görünmez.
		if hw == "" && frame%30 == 0 {
			hw = hardwareLine()
		}

		st := readStageInfo(stage)
		if st.pct > pct {
			pct = st.pct
		}
		if st.text != "" {
			msg = st.text
		}

		drawSplash(ui, frame, pct, msg, hw)
		if err := dev.Flip(canvas); err != nil {
			return err
		}
		frame++

		// ── Çıkış koşulları ─────────────────────────────────────────────
		ready := fileExists(until)
		if ready && time.Since(start) >= minShow {
			// Son kareyi %100'e tamamla: yarım kalmış bir halka, geçişte
			// göze batar.
			drawSplash(ui, frame, 100, "Hazır", hw)
			_ = dev.Flip(canvas)
			if save != "" {
				// Geçiş karesi: panel buradan yakınlaşarak açılacak.
				if err := fbdev.SaveFrame(canvas, save); err != nil {
					// Kaydedilemezse panel yalnızca geçiş animasyonunu
					// atlar; açılış yine de çalışır.
					fmt.Fprintln(os.Stderr, "mcos-splash: kare kaydedilemedi:", err)
				}
			}
			handOver = keep
			return nil
		}
		if time.Now().After(deadline) {
			return nil
		}

		select {
		case <-ticker.C:
		case <-sig:
			return nil
		}
	}
}

// newUI builds the drawing context with an auto-sized font.
//
// Yazı tipi GÖMÜLÜDÜR; yüklenememesi ancak ikilinin bozulması demektir.
// Yine de sessizce çökmek yerine anlamlı bir hata döndürüyoruz: açılışta
// siyah bir ekran, nedeni asla anlaşılmayan bir arızadır.
func newUI(canvas *image.RGBA, fontPx float64, screenH int) (*fbui.UI, error) {
	if fontPx <= 0 {
		// Yaklaşık 45 satır hedefle; panelle AYNI kural, böylece açılış
		// ekranı ile panel arasında yazı boyu zıplamaz.
		fontPx = float64(screenH) / 45 * 0.8
		if fontPx < 12 {
			fontPx = 12
		}
		if fontPx > 40 {
			fontPx = 40
		}
	}
	face, err := fbfont.Load(fontPx)
	if err != nil {
		return nil, fmt.Errorf("gömülü yazı tipi yüklenemedi: %w", err)
	}
	return fbui.NewUI(canvas, face, fbui.DefaultPalette), nil
}

// drawSplash paints one frame of the boot animation.
// drawBrand paints the still frame used as GRUB's background.
//
// Açılış animasyonunun İLK karesiyle aynı kompozisyon, ama hareketli ve
// bilinmeyen her şey çıkarılmış: ilerleme halkası yok (henüz ilerleme yok),
// donanım satırı yok (henüz okunmadı), durum metni yok (henüz bir şey
// yapılmıyor). Ekranda kalan her şey o anda DOĞRU.
//
// Böylece kullanıcı güç düğmesine bastıktan ~1 saniye sonra MCOS logosunu
// görüyor ve çekirdek yüklenene kadar o logo duruyor; animasyon devraldığında
// aynı yerde aynı logo olduğu için geçiş görünmüyor.
func drawBrand(u *fbui.UI) {
	b := u.Bounds()
	u.P.Fill(b, u.Pal.Bg)

	cx := float64(b.Min.X+b.Max.X) / 2
	cy := float64(b.Min.Y) + float64(b.Dy())*0.42
	size := math.Min(float64(b.Dx()), float64(b.Dy())) * 0.13

	u.Logo(cx, cy, size, u.Pal.Accent)

	y := int(cy+size*1.35) + u.M.PadY*4
	u.TextCenter(b.Min.X, b.Max.X, y, "MCOS", u.Pal.Text)
	y += u.F.CellH + u.M.PadY/2
	u.TextCenter(b.Min.X, b.Max.X, y, version.Display(), u.Pal.TextFaint)
}

func drawSplash(u *fbui.UI, frame, pct int, msg, hw string) {
	b := u.Bounds()
	u.P.Fill(b, u.Pal.Bg)

	cx := float64(b.Min.X+b.Max.X) / 2
	cy := float64(b.Min.Y) + float64(b.Dy())*0.42

	// Arka planda çok soluk, yavaşça genişleyen halkalar: ekranın "canlı"
	// olduğunu, donmadığını anlatır. Düz bir logo, donmuş bir kare gibi
	// görünür ve kullanıcı makineyi kapatmaya kalkar.
	maxR := math.Min(float64(b.Dx()), float64(b.Dy())) * 0.42
	for i := 0; i < 3; i++ {
		ph := math.Mod(float64(frame)/90+float64(i)/3, 1)
		r := maxR * fbdraw.EaseOutCubic(ph)
		u.P.StrokeCircle(cx, cy, r, 1.5,
			fbdraw.Alpha(u.Pal.Accent, 0.07*(1-ph)))
	}

	size := math.Min(float64(b.Dx()), float64(b.Dy())) * 0.13
	u.LogoPulse(cx, cy, size, frame, u.Pal.Accent)

	// İlerleme halkası logoyu ÇEVRELER: ayrı bir çubuk, ekranı ikiye böler
	// ve gözü logodan uzaklaştırır.
	u.ProgressRing(cx, cy, size*1.35, pct, u.Pal.Accent)

	y := int(cy+size*1.35) + u.M.PadY*4
	u.TextCenter(b.Min.X, b.Max.X, y, "MCOS", u.Pal.Text)
	y += u.F.CellH + u.M.PadY/2
	u.TextCenter(b.Min.X, b.Max.X, y, version.Display(), u.Pal.TextFaint)
	y += u.F.CellH + u.M.PadY*3

	u.TextCenter(b.Min.X, b.Max.X, y, msg, u.Pal.TextDim)

	// ── Altta GERÇEK donanım ───────────────────────────────────────────
	//
	// Burada eskiden sabit bir slogan vardı ("Minecraft Sunucu İşletim
	// Sistemi"). Artık makinenin kendisi yazıyor: işlemci, çekirdek sayısı,
	// bellek — hepsi /proc'tan, hiçbiri tahmin değil.
	//
	// Okunamazsa slogan geri geliyor: boş bir satır bırakmak, ekranın
	// yarım çizildiği izlenimi verirdi.
	bottom := b.Max.Y - u.F.CellH - u.M.PadY*2
	if hw != "" {
		u.TextCenter(b.Min.X, b.Max.X, bottom, hw, u.Pal.TextFaint)
	} else {
		u.TextCenter(b.Min.X, b.Max.X, bottom,
			"Minecraft Sunucu İşletim Sistemi", u.Pal.TextFaint)
	}
}

func fileExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}
