package model

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ── Otomatik yedek planı ────────────────────────────────────────────────────
//
// Kullanıcının isteği: "yedek alma saat/gün aralığını da ayarlayalım".
// BackupPolicy.Schedule TEK bir dizgi olarak kalıyor ve genişletiliyor:
//
//	"6h", "12h", "30m"      son yedekten bu kadar sonra (ESKİ biçim, Go süresi)
//	"1d", "2d", "7d"        son yedekten N gün sonra
//	"1d@04:00", "7d@03:30"  N günde bir, sistem saat dilimine göre HH:MM'de
//
// ── Neden yeni alanlar değil, genişletilmiş dizgi ──────────────────────────
//
//  1. Eski kayıtlar DOKUNULMADAN çalışır: şimdiye dek yazılan tek değer
//     backupPolicyFor'un "6h"'ı ve bu, yeni biçimin saat kolunun kendisi.
//     Göç (migration) kodu yok, yarım kalmış bir göç de yok.
//  2. Tek doğruluk kaynağı: ayrı "Days"/"At" alanları olsaydı Schedule="6h"
//     ile At="04:00" gibi ÇELİŞEN birleşimler diske yazılabilirdi ve hangisinin
//     geçerli olduğunu her okuyucu (daemon, panel, uygulama) ayrı ayrı
//     çözmek zorunda kalırdı.
//  3. Geri dönüşte (eski mcosd yeni bir manifest okursa) güvenli: eski kod
//     "1d@04:00"'ı time.ParseDuration ile okuyamaz ve o sunucuyu ATLAR; ayrı
//     alanlarda ise yeni alanları sessizce yok sayıp eski Schedule'la,
//     kullanıcının seçmediği bir aralıkla yedek almayı sürdürürdü.

const (
	// DefaultBackupSchedule / DefaultBackupKeep: sihirbazın "otomatik yedek"
	// seçeneğinin yazdığı plan (daemon backupPolicyFor ile aynı).
	DefaultBackupSchedule = "6h"
	DefaultBackupKeep     = 5

	// BackupMinInterval: bundan sık yedek bir Minecraft dünyasının TAMAMINI
	// sürekli sıkıştırmak demek; disk ve işlemci oyunla yarışır. Zamanlayıcı
	// zaten dakikada bir döndüğü için saniyeli değerlerin anlamı da yok.
	BackupMinInterval = 15 * time.Minute
	// BackupMaxDays: daha seyrek bir "otomatik" yedek, kullanıcının haberi
	// olmadan haftalarca yedeksiz kalmak demek.
	BackupMaxDays = 30
	// BackupMaxKeep: saklanacak kopya üst sınırı (0 = sınırsız ayrıca geçerli).
	BackupMaxKeep = 100

	// backupSlotCover: günün saati verilmiş planlarda bir yedek, kendisinden
	// sonraki 6 saat içindeki planlı saati "karşılamış" sayılır.
	//
	// Neden: cihaz 04:00'ı kapalı geçirip 03:00'te açılırsa kaçırılan yedek
	// hemen alınır; 6 saatlik pay olmasa bir saat sonra, 04:00'te İKİNCİ bir
	// kopya alınır ve kısıtlı kopya sayısında gerçek bir eski yedeği siler.
	// Pay 6 saatten büyük olsaydı akşam elle alınmış bir yedek ertesi sabahın
	// planlı yedeğini yutar, iki yedek arası 30 saati aşardı.
	backupSlotCover = 6 * time.Hour
)

// Panelin ve uygulamanın sunduğu seçenekler. Elle (IPC/CLI) bunların dışında
// geçerli değerler de verilebilir; listeler yalnızca arayüz içindir.
var (
	BackupHourChoices = []int{1, 2, 3, 4, 6, 8, 12}
	BackupDayChoices  = []int{1, 2, 3, 7}
	BackupKeepChoices = []int{3, 5, 10, 20, 0}
)

// BackupSchedule is the parsed form of BackupPolicy.Schedule.
type BackupSchedule struct {
	// Every: iki yedek arasındaki süre. Gün planlarında Days*24h.
	Every time.Duration
	// Days > 0: gün tabanlı plan ("2d", "1d@04:00").
	Days int
	// HasAt: günün sabit bir saatinde (yalnızca Days > 0 iken).
	HasAt bool
	// AtMin: gece yarısından beri dakika (0..1439).
	AtMin int
}

// ParseBackupSchedule parses "6h", "90m", "2d" or "1d@04:00".
//
// Hata metinleri Türkçe ve kullanıcıya gösterilir (panel, uygulama, CLI).
func ParseBackupSchedule(s string) (BackupSchedule, error) {
	raw := s
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return BackupSchedule{}, fmt.Errorf("yedek aralığı boş — örnek: 6h, 1d ya da 1d@04:00")
	}
	left, at, hasAt := strings.Cut(s, "@")
	left = strings.TrimSpace(left)

	if n, ok := strings.CutSuffix(left, "d"); ok {
		days, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil {
			return BackupSchedule{}, fmt.Errorf("yedek aralığı anlaşılamadı: %q — gün için örnek: 1d, 2d, 7d", raw)
		}
		if days < 1 || days > BackupMaxDays {
			return BackupSchedule{}, fmt.Errorf("gün aralığı 1 ile %d arasında olmalı (verilen: %d)", BackupMaxDays, days)
		}
		sc := BackupSchedule{Every: time.Duration(days) * 24 * time.Hour, Days: days}
		if hasAt {
			m, err := parseClock(at)
			if err != nil {
				return BackupSchedule{}, err
			}
			sc.HasAt, sc.AtMin = true, m
		}
		return sc, nil
	}

	if hasAt {
		return BackupSchedule{}, fmt.Errorf("günün saati yalnızca gün aralığıyla verilebilir (örnek: 1d@04:00), %q değil", raw)
	}
	d, err := time.ParseDuration(left)
	if err != nil {
		return BackupSchedule{}, fmt.Errorf("yedek aralığı anlaşılamadı: %q — örnek: 6h, 1d ya da 1d@04:00", raw)
	}
	if d < BackupMinInterval {
		return BackupSchedule{}, fmt.Errorf("yedek aralığı en az 15 dakika olmalı (verilen: %s)", strings.TrimSpace(raw))
	}
	if d > time.Duration(BackupMaxDays)*24*time.Hour {
		return BackupSchedule{}, fmt.Errorf("yedek aralığı en fazla %d gün olabilir (verilen: %s)", BackupMaxDays, strings.TrimSpace(raw))
	}
	if d%time.Minute != 0 {
		return BackupSchedule{}, fmt.Errorf("yedek aralığı tam dakika olmalı (verilen: %s)", strings.TrimSpace(raw))
	}
	return BackupSchedule{Every: d}, nil
}

// parseClock parses "04:00" / "4:00" into minutes after midnight.
func parseClock(s string) (int, error) {
	s = strings.TrimSpace(s)
	hs, ms, ok := strings.Cut(s, ":")
	h, herr := strconv.Atoi(hs)
	m, merr := strconv.Atoi(ms)
	if !ok || herr != nil || merr != nil || len(hs) == 0 || len(hs) > 2 || len(ms) != 2 ||
		h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("saat SS:DD biçiminde ve 00:00–23:59 arasında olmalı (verilen: %q)", s)
	}
	return h*60 + m, nil
}

// String returns the canonical stored form ("6h", "90m", "2d", "1d@04:00").
func (s BackupSchedule) String() string {
	if s.Days > 0 {
		out := strconv.Itoa(s.Days) + "d"
		if s.HasAt {
			out += "@" + s.Clock()
		}
		return out
	}
	if s.Every%time.Hour == 0 {
		return strconv.Itoa(int(s.Every/time.Hour)) + "h"
	}
	return strconv.Itoa(int(s.Every/time.Minute)) + "m"
}

// Clock returns the time of day as "HH:MM" (empty when there is none).
func (s BackupSchedule) Clock() string {
	if !s.HasAt {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", s.AtMin/60, s.AtMin%60)
}

// Label is the Turkish description shown to the user ("Her 6 saatte bir",
// "Her gün 04:00", "Her hafta").
func (s BackupSchedule) Label() string {
	if s.Days > 0 {
		var l string
		switch {
		case s.Days == 1:
			l = "Her gün"
		case s.Days == 7:
			l = "Her hafta"
		case s.Days%7 == 0:
			l = fmt.Sprintf("Her %d haftada bir", s.Days/7)
		default:
			l = fmt.Sprintf("Her %d günde bir", s.Days)
		}
		if s.HasAt {
			l += " " + s.Clock()
		}
		return l
	}
	if s.Every%time.Hour == 0 {
		h := int(s.Every / time.Hour)
		if h == 1 {
			return "Her saat"
		}
		return fmt.Sprintf("Her %d saatte bir", h)
	}
	return fmt.Sprintf("Her %d dakikada bir", int(s.Every/time.Minute))
}

// Next returns when the backup after one taken at last is due.
//
// Saat verilmemiş planlarda son yedekten Every sonra (eski davranış). Saat
// verilmişse: son yedekten en az (Days-1) gün + 6 saat sonraki İLK HH:MM.
// Böylece kaçırılan bir yedek geç alındığında (09:00) plan ertesi günün
// 04:00'üne DÖNER; kayıp hiçbir gün yoktur.
//
// loc, sistemin saat dilimidir. HH:MM her gün time.Date ile YENİDEN
// hesaplanır, 24 saat eklenerek değil: yaz saatine geçilen 23 saatlik ya da
// geri dönülen 25 saatlik günde de yedek aynı yerel saatte ve günde TEK kez
// alınır (geri dönüşte 02:30 iki kez yaşanır ama time.Date tek bir an verir;
// ileri geçişte var olmayan 02:30 03:30'a kayar, gün atlanmaz).
func (s BackupSchedule) Next(last time.Time, loc *time.Location) time.Time {
	if !s.HasAt || s.Days <= 0 {
		return last.Add(s.Every)
	}
	if loc == nil {
		loc = time.Local
	}
	after := last.Add(time.Duration(s.Days-1)*24*time.Hour + backupSlotCover)
	return s.slotAtOrAfter(after, loc)
}

// slotAtOrAfter returns the first HH:MM in loc that is not before t.
func (s BackupSchedule) slotAtOrAfter(t time.Time, loc *time.Location) time.Time {
	tl := t.In(loc)
	h, m := s.AtMin/60, s.AtMin%60
	c := time.Date(tl.Year(), tl.Month(), tl.Day(), h, m, 0, 0, loc)
	if c.Before(t) {
		c = time.Date(tl.Year(), tl.Month(), tl.Day()+1, h, m, 0, 0, loc)
	}
	return c
}

// Plan parses the policy's schedule. Otomatik yedek kapalıysa da ayrıştırır:
// kapatılan plan saklanır ki yeniden açan kullanıcı eski seçimini bulsun.
func (p BackupPolicy) Plan() (BackupSchedule, error) {
	return ParseBackupSchedule(p.Schedule)
}

// ValidateBackupPolicy checks a policy before it is stored.
func ValidateBackupPolicy(p BackupPolicy) error {
	if p.Keep < 0 || p.Keep > BackupMaxKeep {
		return fmt.Errorf("saklanacak kopya sayısı 0 (sınırsız) ile %d arasında olmalı (verilen: %d)", BackupMaxKeep, p.Keep)
	}
	if !p.Auto && strings.TrimSpace(p.Schedule) == "" {
		return nil
	}
	_, err := p.Plan()
	return err
}

// BackupKeepLabel describes the retention count ("son 5 kopya").
func BackupKeepLabel(keep int) string {
	if keep <= 0 {
		return "sınırsız kopya"
	}
	return fmt.Sprintf("son %d kopya", keep)
}

// BackupPolicyLabel is the one-line summary shown in the panel and the app:
// "Her 6 saatte bir · son 5 kopya" ya da "Kapalı".
func BackupPolicyLabel(p BackupPolicy) string {
	if !p.Auto {
		return "Kapalı"
	}
	sc, err := p.Plan()
	if err != nil {
		return "Plan okunamadı — yeniden ayarlayın"
	}
	return sc.Label() + " · " + BackupKeepLabel(p.Keep)
}
