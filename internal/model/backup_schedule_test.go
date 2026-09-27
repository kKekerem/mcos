package model

import (
	"strings"
	"testing"
	"time"

	// Saat dilimi verisi test ikilisine gömülür: yaz saati testleri, zoneinfo
	// olmayan bir makinede de (CI, Windows) aynı sonucu versin.
	_ "time/tzdata"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	l, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("saat dilimi %s: %v", name, err)
	}
	return l
}

// Eski kayıtlar ("6h") ve Go süre biçimi AYNEN çalışmalı: diskteki her
// manifest bu biçimde ve göç kodu yok.
func TestYedekPlaniEskiBicimCalisir(t *testing.T) {
	for _, c := range []struct {
		in    string
		every time.Duration
		canon string
		label string
	}{
		{"6h", 6 * time.Hour, "6h", "Her 6 saatte bir"},
		{" 6H ", 6 * time.Hour, "6h", "Her 6 saatte bir"},
		{"1h", time.Hour, "1h", "Her saat"},
		{"24h", 24 * time.Hour, "24h", "Her 24 saatte bir"},
		{"30m", 30 * time.Minute, "30m", "Her 30 dakikada bir"},
		{"1h30m", 90 * time.Minute, "90m", "Her 90 dakikada bir"},
	} {
		sc, err := ParseBackupSchedule(c.in)
		if err != nil {
			t.Fatalf("%q reddedildi: %v", c.in, err)
		}
		if sc.Every != c.every || sc.Days != 0 || sc.HasAt {
			t.Errorf("%q → %+v, aralık %v bekleniyordu", c.in, sc, c.every)
		}
		if sc.String() != c.canon || sc.Label() != c.label {
			t.Errorf("%q → %q / %q, %q / %q bekleniyordu", c.in, sc.String(), sc.Label(), c.canon, c.label)
		}
	}
}

func TestYedekPlaniGunVeSaatBicimi(t *testing.T) {
	for _, c := range []struct {
		in    string
		days  int
		hasAt bool
		atMin int
		canon string
		label string
	}{
		{"1d", 1, false, 0, "1d", "Her gün"},
		{"2d", 2, false, 0, "2d", "Her 2 günde bir"},
		{"7d", 7, false, 0, "7d", "Her hafta"},
		{"14d", 14, false, 0, "14d", "Her 2 haftada bir"},
		{"1d@04:00", 1, true, 240, "1d@04:00", "Her gün 04:00"},
		{"1D@4:00", 1, true, 240, "1d@04:00", "Her gün 04:00"},
		{"2d@23:59", 2, true, 23*60 + 59, "2d@23:59", "Her 2 günde bir 23:59"},
		{"7d@03:30", 7, true, 210, "7d@03:30", "Her hafta 03:30"},
		{"3d@00:00", 3, true, 0, "3d@00:00", "Her 3 günde bir 00:00"},
	} {
		sc, err := ParseBackupSchedule(c.in)
		if err != nil {
			t.Fatalf("%q reddedildi: %v", c.in, err)
		}
		if sc.Days != c.days || sc.HasAt != c.hasAt || sc.AtMin != c.atMin ||
			sc.Every != time.Duration(c.days)*24*time.Hour {
			t.Errorf("%q → %+v", c.in, sc)
		}
		if sc.String() != c.canon || sc.Label() != c.label {
			t.Errorf("%q → %q / %q, %q / %q bekleniyordu", c.in, sc.String(), sc.Label(), c.canon, c.label)
		}
	}
}

// Geçersiz değer açık, Türkçe bir hatayla reddedilmeli; sessizce "hiç yedek
// alma"ya dönüşmemeli.
func TestYedekPlaniGecersizDegerler(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", "boş"},
		{"abc", "anlaşılamadı"},
		{"6x", "anlaşılamadı"},
		{"-6h", "en az 15 dakika"},
		{"0h", "en az 15 dakika"},
		{"5m", "en az 15 dakika"},
		{"90s", "en az 15 dakika"},
		{"15m30s", "tam dakika"},
		{"721h", "en fazla 30 gün"},
		{"0d", "1 ile 30"},
		{"31d", "1 ile 30"},
		{"1.5d", "anlaşılamadı"},
		{"d", "anlaşılamadı"},
		{"6h@04:00", "yalnızca gün aralığıyla"},
		{"1d@25:00", "00:00–23:59"},
		{"1d@04:60", "00:00–23:59"},
		{"1d@0400", "00:00–23:59"},
		{"1d@04:5", "00:00–23:59"},
		{"1d@", "00:00–23:59"},
		{"1d@-1:00", "00:00–23:59"},
	} {
		_, err := ParseBackupSchedule(c.in)
		if err == nil {
			t.Errorf("%q kabul edildi", c.in)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q hatası %q, %q içermeliydi", c.in, err, c.want)
		}
	}
}

func TestYedekPolitikasiDogrulamaVeEtiket(t *testing.T) {
	if err := ValidateBackupPolicy(BackupPolicy{Auto: true, Schedule: "6h", Keep: -1}); err == nil ||
		!strings.Contains(err.Error(), "kopya") {
		t.Errorf("eksi kopya sayısı: %v", err)
	}
	if err := ValidateBackupPolicy(BackupPolicy{Auto: true, Schedule: "6h", Keep: BackupMaxKeep + 1}); err == nil {
		t.Error("üst sınırı aşan kopya sayısı kabul edildi")
	}
	if err := ValidateBackupPolicy(BackupPolicy{Auto: true, Schedule: ""}); err == nil {
		t.Error("açık plan boş aralıkla kabul edildi")
	}
	if err := ValidateBackupPolicy(BackupPolicy{}); err != nil {
		t.Errorf("kapalı ve boş plan reddedildi: %v", err)
	}
	for _, c := range []struct {
		p    BackupPolicy
		want string
	}{
		{BackupPolicy{Auto: true, Schedule: "6h", Keep: 5}, "Her 6 saatte bir · son 5 kopya"},
		{BackupPolicy{Auto: true, Schedule: "1d@04:00", Keep: 0}, "Her gün 04:00 · sınırsız kopya"},
		{BackupPolicy{Auto: true, Schedule: "7d@03:30", Keep: 10}, "Her hafta 03:30 · son 10 kopya"},
		{BackupPolicy{Auto: false, Schedule: "6h", Keep: 5}, "Kapalı"},
		{BackupPolicy{Auto: true, Schedule: "bozuk", Keep: 5}, "Plan okunamadı — yeniden ayarlayın"},
	} {
		if got := BackupPolicyLabel(c.p); got != c.want {
			t.Errorf("%+v → %q, %q bekleniyordu", c.p, got, c.want)
		}
	}
}

// Saat aralığı: son yedekten tam Every sonra (eski davranış).
func TestYedekSonrakiSaatAraligi(t *testing.T) {
	sc, _ := ParseBackupSchedule("6h")
	last := time.Date(2026, 9, 27, 10, 3, 0, 0, time.UTC)
	if got := sc.Next(last, time.UTC); !got.Equal(last.Add(6 * time.Hour)) {
		t.Errorf("sonraki %v", got)
	}
}

// Günün saati verilmiş plan: normal gün, kaçırılan yedek, geç alınan yedekten
// sonra plana dönüş, elle alınmış akşam yedeği, haftalık ve iki günlük plan.
func TestYedekSonrakiGununSaati(t *testing.T) {
	ist := mustLoc(t, "Europe/Istanbul")
	at := func(d, h, m int) time.Time { return time.Date(2026, 9, d, h, m, 0, 0, ist) }
	daily, _ := ParseBackupSchedule("1d@04:00")
	two, _ := ParseBackupSchedule("2d@04:00")
	weekly, _ := ParseBackupSchedule("7d@03:30")
	for _, c := range []struct {
		name string
		sc   BackupSchedule
		last time.Time
		want time.Time
	}{
		{"planlı yedekten sonra ertesi gün", daily, at(20, 4, 3), at(21, 4, 0)},
		// Cihaz 21'inde 04:00'da kapalıydı: sonraki yedek 21 04:00, yani
		// 09:00'da açılınca GEÇMİŞTE → hemen alınır.
		{"kaçırılan yedek geçmişte kalır", daily, at(20, 4, 3), at(21, 4, 0)},
		{"geç alınan yedekten sonra plana dönüş", daily, at(21, 9, 0), at(22, 4, 0)},
		// 03:00'te alınan telafi yedeği 04:00'ü karşılar: bir saat sonra
		// ikinci kopya alınmaz.
		{"planlı saatten az önce alınan yedek", daily, at(21, 3, 0), at(22, 4, 0)},
		{"akşam elle alınan yedek sabahı yutmaz", daily, at(21, 17, 0), at(22, 4, 0)},
		{"iki günde bir", two, at(20, 4, 1), at(22, 4, 0)},
		{"iki günde bir, geç yedekten sonra", two, at(21, 11, 0), at(23, 4, 0)},
		{"haftalık", weekly, at(7, 3, 31), at(14, 3, 30)},
	} {
		if got := c.sc.Next(c.last, ist); !got.Equal(c.want) {
			t.Errorf("%s: sonraki %v, %v bekleniyordu", c.name, got.In(ist), c.want)
		}
	}
}

// Yaz saati: yerel 04:00 (ve geçişin tam içindeki 02:30) her gün TEK kez
// gelmeli — 23 saatlik günde atlanmamalı, 25 saatlik günde yinelenmemeli.
// Bir yıl dakika dakika taranır, her yedek Next'e göre "alınır".
func TestYedekYazSaatindeGunAtlamazYinelemez(t *testing.T) {
	for _, zone := range []string{"Europe/Berlin", "America/New_York", "Europe/Istanbul"} {
		loc := mustLoc(t, zone)
		for _, spec := range []string{"1d@04:00", "1d@02:30", "1d@01:30"} {
			sc, _ := ParseBackupSchedule(spec)
			last := time.Date(2025, 12, 31, sc.AtMin/60, sc.AtMin%60, 20, 0, loc)
			perDay := map[string]int{}
			end := time.Date(2027, 1, 1, 0, 0, 0, 0, loc)
			next := sc.Next(last, loc)
			for now := last; now.Before(end); now = now.Add(time.Minute) {
				if now.Before(next) {
					continue
				}
				lt := now.In(loc)
				perDay[lt.Format("2006-01-02")]++
				// Yedek YEREL saatte planlanan dakikada alınmalı (24 saat
				// eklemek yaz saatinden sonra 04:00'ü 05:00'e kaydırırdı).
				// Yalnızca o gün var olmayan saat (ileri geçiş) time.Date'in
				// verdiği ana kayar.
				want := time.Date(lt.Year(), lt.Month(), lt.Day(), sc.AtMin/60, sc.AtMin%60, 0, 0, loc)
				if lt.Format("15:04") != want.Format("15:04") {
					t.Errorf("%s %s: %s yedeği %s'te alındı, %s bekleniyordu",
						zone, spec, lt.Format("2006-01-02"), lt.Format("15:04"), want.Format("15:04"))
				}
				next = sc.Next(now.Add(20*time.Second), loc)
			}
			days := 0
			for d := time.Date(2026, 1, 1, 12, 0, 0, 0, loc); d.Year() == 2026; d = d.AddDate(0, 0, 1) {
				days++
				key := d.Format("2006-01-02")
				if perDay[key] != 1 {
					t.Errorf("%s %s: %s günü %d yedek", zone, spec, key, perDay[key])
				}
			}
			if days != 365 {
				t.Fatalf("gün sayısı %d", days)
			}
		}
	}
}
