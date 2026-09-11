package theme

// TASARIM TOKEN'LARI — MCOS arayüzünün TEK doğruluk kaynağı.
//
// Kural: hiçbir görünüm dosyası ham sayı veya ham glif YAZMAZ. Her ölçü, her
// simge ve her etiket burada bir kez tanımlanır ve kullanım yerinde adıyla
// çağrılır. Eskiden ölçüler dağınıktı ve birbirini tutmuyordu:
//
//	RenderInputField etiket genişliği %-18s
//	kv() etiket genişliği            %-14s
//	RenderHeader ilerleme çubuğu     25
//	USB listesi ad alanı             %-30s
//	RenderCard en küçük genişlik     30 / 28
//
// Bu yüzden hiçbir iki satır hizalanmıyordu. Aşağıdaki tek ızgara bunu çözer.

// ── Izgara ──────────────────────────────────────────────────────────────────
//
// Tüm yatay ölçüler tek bir birim ızgarasına oturur. Panel 80 kolon
// varsayılanına göre tasarlandı; daha geniş ekranlarda içerik ortalanır.

const (
	// GridUnit, tek yatay boşluk birimi (karakter).
	GridUnit = 2

	// ContentWidthMin / ContentWidthMax, ana içerik alanının sınırları.
	// Max sınır önemli: 200 kolonluk bir framebuffer'da satırlar okunamaz
	// hale gelmesin diye içerik genişliği kısıtlanır.
	ContentWidthMin = 48
	ContentWidthMax = 110

	// SidebarWidth, sol gezinme şeridinin toplam genişliği (kenarlık dahil).
	SidebarWidth = 24

	// LabelWidth, "etiket : değer" satırlarında etiket kolonunun genişliği.
	// TEK değer — hem form alanları hem bilgi satırları bunu kullanır.
	LabelWidth = 18

	// ValueWidthMin, değer kolonunun en az genişliği.
	ValueWidthMin = 20

	// ListNameWidth, liste satırlarında ad kolonunun genişliği
	// (sunucu listesi, USB jar listesi, katalog sonuçları — hepsi aynı).
	ListNameWidth = 30

	// BarWidth, ilerleme çubuklarının gövde genişliği (yüzde metni hariç).
	BarWidth = 28

	// CardPadX / CardPadY, kart içi dolgu.
	CardPadX = 2
	CardPadY = 1

	// BorderWidth, lipgloss kenarlığının her iki yanda kapladığı kolon.
	// Genişlik hesaplarında kullanılır: içerik = toplam - 2*BorderWidth.
	BorderWidth = 1
)

// ── Dikey ölçüler ───────────────────────────────────────────────────────────

const (
	// HeaderHeight, üst başlık şeridinin satır sayısı (başlık + ilerleme).
	HeaderHeight = 2
	// FooterHeight, alt tuş ipucu şeridinin satır sayısı.
	FooterHeight = 1
	// ChromeHeight, başlık + altlık + aralarındaki boşlukların toplamı.
	// Görünümler kullanılabilir yüksekliği "h - ChromeHeight" ile bulur.
	ChromeHeight = HeaderHeight + FooterHeight + 2
	// ListHeightMin, bir listenin altına düşmemesi gereken satır sayısı.
	ListHeightMin = 3
)

// ── Simgeler ────────────────────────────────────────────────────────────────
//
// Hepsi FiraCode Nerd Font'ta mevcut. fbterm bu fontu TrueType olarak
// işlediği için box-drawing ve Unicode simgeler doğru çizilir.
//
// NOT: burada emoji KULLANILMAZ. Emoji glifleri çift genişlikte olduğundan
// hizalamayı bozar ve fbterm bunları yalnızca yedek fontla çizebilir. Yerine
// tek genişlikli Nerd Font simgeleri kullanılıyor.

const (
	// Seçim ve odak
	IconCursor   = "▸" // seçili satır göstergesi
	IconSelected = "●" // seçili radyo
	IconUnselect = "○" // seçili olmayan radyo
	IconChecked  = "■" // işaretli onay kutusu
	IconUnchcked = "□" // işaretsiz onay kutusu

	// Durum
	IconOK   = "✓"
	IconFail = "✗"
	// IconWarn: U+26A0 TEK BAŞINA tek kolondur (eaw=N).
	//
	// Arkasına ASLA U+FE0F (variation selector-16) eklenmez. VS16 emoji
	// sunumunu zorlar: terminal glifi iki kolon çizerken lipgloss.Width bir
	// sayar ve her kullanımda 1 kolon kayma olur. Bu hata gerçekten oluştu —
	// OOBE disk ekranındaki uyarı satırı U+26A0 + U+FE0F kullandığı için kutu
	// kenarlığını 1 kolon kaydırıyordu.
	// scripts/check-tui-glyphs.py artık U+FE0F'i hata olarak yakalıyor.
	IconWarn = "⚠"

	// IconBullet: madde imi. IconCursor'dan (seçim işaretçisi) FARKLI bir
	// glif olmalı, yoksa "seçilebilir satır" ile "bilgi maddesi" görsel olarak
	// karışır.
	IconBullet = "▪"
	IconDash   = "–"
	IconDotOn  = "●"
	IconDotOf  = "○"

	// Yön
	IconUp    = "▲"
	IconDown  = "▼"
	IconLeft  = "◂"
	IconRight = "▸"

	// Çubuk parçaları
	BarFill  = "━"
	BarEmpty = "─"

	// Ayırıcılar
	SepVert  = "│"
	SepHoriz = "─"
	SepDot   = "·"
)

// ── Bölüm simgeleri ─────────────────────────────────────────────────────────
//
// EMOJI KULLANILMAZ. Ölçüldü (scripts/check-glyph-width.py): kullanımdaki 16
// emoji'nin tamamı east-asian-width = W, yani İKİ kolon genişliğinde.
// go-runewidth bunları 2 sayarken fbterm onları monospace olmayan Noto Emoji
// yedeğiyle çizdiği için gerçek genişlik öngörülemez — kutu kenarları ve sütun
// hizaları kayar.
//
// Aşağıdaki gliflerin hepsi TEK kolon olarak ölçülmüştür ve DejaVu Sans Mono
// ile FiraCode Nerd Font'ta mevcuttur.
// Bu dosyada da emoji YAZILMAZ — scripts/check-tui-glyphs.py yorumları da
// denetler, böylece kural tek ve istisnasız kalır: panel/ ağacında hiçbir yerde
// çift genişlikli glif bulunmaz.
const (
	// Sunucu paneli sekmeleri (detailTabs ile aynı sırada)
	IconInfo     = "ℹ" // genel / bilgi
	IconConsole  = "⌁" // konsol
	IconSettings = "⚙" // ayarlar
	IconPlayers  = "◍" // oyuncular
	IconSoftware = "◈" // yazılım / eklenti
	IconFiles    = "▤" // dosyalar
	IconWorlds   = "◉" // dünyalar
	IconBackup   = "▣" // yedekler
	IconAccess   = "⚑" // erişim
	IconTunnel   = "◇" // internete aç
	IconPerf     = "▲" // performans
	IconNetwork  = "▪" // ağ / kablolu

	// Genel
	IconServer = "◆" // sunucu
	IconTurbo  = "★" // turbo modu
	IconSearch = "»" // arama
	IconDisk   = "▣" // disk
	IconUSB    = "◈" // USB bellek
	IconLock   = "⚑" // korumalı kablosuz ağ
	IconSignal = "▪" // sinyal gücü
	IconWait   = "⟳" // bekleniyor / taranıyor
	IconLog    = "▫" // son günlük satırı
)

// ── Buton ölçüleri ──────────────────────────────────────────────────────────

const (
	// ButtonMinWidth, bir butonun etiket alanının en az genişliği. Butonlar
	// aynı satırda yan yana dizildiğinde eşit görünsün diye sabit.
	ButtonMinWidth = 16
	// ButtonPadX, buton içi yatay dolgu.
	ButtonPadX = 2
	// KeyCapPadX, tuş kapağı içi yatay dolgu.
	KeyCapPadX = 1
)

// ── Metin kısaltma ──────────────────────────────────────────────────────────

const (
	// Ellipsis, kısaltılan metnin sonuna eklenir (tek genişlikli).
	Ellipsis = "…"
)

// ── Durum etiketleri ────────────────────────────────────────────────────────
//
// Tek yerde tanımlı Türkçe durum metinleri. Genişlikleri eşitlenmiş
// (StateLabelWidth) böylece liste sütunları kaymaz.

const (
	// StateLabelWidth, en uzun durum etiketinin genişliği ("ÇALIŞIYOR" = 9).
	StateLabelWidth = 9

	StateLabelRunning  = "ÇALIŞIYOR"
	StateLabelStarting = "BAŞLIYOR"
	StateLabelStopping = "DURUYOR"
	StateLabelStopped  = "KAPALI"
	StateLabelError    = "HATA"
)

// ContentWidth clamps an available terminal width to the readable content band.
// Görünümler genişliği DOĞRUDAN kullanmaz; her zaman bundan geçirir.
func ContentWidth(available int) int {
	w := available
	if w > ContentWidthMax {
		w = ContentWidthMax
	}
	if w < ContentWidthMin {
		w = ContentWidthMin
	}
	return w
}

// InnerWidth returns the value to pass to lipgloss Style.Width() so that a
// bordered box occupies exactly `total` columns.
//
// lipgloss semantiği: Style.Width(n) DOLGUYU İÇERİR, kenarlık ise dışta kalır.
// Dolayısıyla yalnızca kenarlık payı çıkarılır. Dolguyu da çıkarmak kartları
// her kenardan CardPadX kadar dar bırakıyordu (62 kolonluk alanda 58 kolonluk
// kart → sağda boşluk).
func InnerWidth(total int) int {
	inner := total - 2*BorderWidth
	if inner < 1 {
		inner = 1
	}
	return inner
}

// ListHeight returns how many rows a list may use inside a view of height h.
func ListHeight(h int) int {
	rows := h - ChromeHeight
	if rows < ListHeightMin {
		rows = ListHeightMin
	}
	return rows
}
