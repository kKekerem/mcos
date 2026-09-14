package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"mcos/internal/flash"
)

// Terminalde ilerleme çubuğu.
//
// ── Neden elle yazıyoruz? ───────────────────────────────────────────────────
// Hazır bir ilerleme kütüphanesi eklemek, masaüstü aracına bir bağımlılık
// daha demek. Çubuğun tamamı 80 satır ve tek bir şey yapıyor: aynı satırı
// üzerine yazmak (\r). Bunun için kütüphane gerekmez.
//
// ── Neden \r, neden ANSI imleç kodları değil? ───────────────────────────────
// Windows'un eski konsolu ANSI kaçış dizilerini varsayılan olarak
// yorumlamaz (Windows 10 1511 öncesi, ve bazı yapılandırmalarda hâlâ).
// Satır başı (\r) HER terminalde çalışır. Çubuk basit kalsın ama her yerde
// görünsün.

// barWidth is how many characters the bar body occupies.
const barWidth = 34

// refreshInterval throttles redraws.
//
// 100 ms: gözle akıcı görünür, ama saniyede 1000 kez satır basmak
// (özellikle Windows konsolunda) yazmanın kendisinden yavaş olurdu.
const refreshInterval = 100 * time.Millisecond

type progress struct {
	lastDraw  time.Time
	lastStage string
	started   time.Time
	lastBytes uint64
	lastRate  float64
	printed   bool
}

func newProgress() *progress {
	return &progress{started: time.Now()}
}

// update is the callback handed to flash.Writer.
func (p *progress) update(pr flash.Progress) {
	if pr.Err != nil {
		// Hata ana akışta bildirilecek; burada yalnızca satırı temizle ki
		// hata mesajı çubuğun üstüne binmesin.
		p.clear()
		return
	}

	now := time.Now()
	stageChanged := pr.Stage != p.lastStage
	if !stageChanged && !pr.Done && now.Sub(p.lastDraw) < refreshInterval {
		return
	}
	p.lastDraw = now

	if stageChanged {
		// Yeni aşama: hızı sıfırla, yoksa "yazılıyor" hızı "doğrulanıyor"
		// aşamasına taşınır ve yanlış süre tahmini verir.
		p.started = now
		p.lastBytes = 0
		p.lastRate = 0
		p.lastStage = pr.Stage
	}

	pct := 0
	if pr.BytesTotal > 0 {
		pct = int(pr.BytesDone * 100 / pr.BytesTotal)
	}

	// Hız: aşama başından beri ortalama. Anlık hız USB'de çok oynar ve
	// kalan süre tahmini saniyede bir zıplar.
	el := now.Sub(p.started).Seconds()
	if el > 0.5 && pr.BytesDone > 0 {
		p.lastRate = float64(pr.BytesDone) / el
	}

	filled := pct * barWidth / 100
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("#", filled) + strings.Repeat(".", barWidth-filled)

	line := fmt.Sprintf("  %-22s [%s] %3d%%  %s",
		pr.Stage, bar, pct, flash.HumanBytes(pr.BytesDone))
	if p.lastRate > 0 {
		line += fmt.Sprintf("  %s/s", flash.HumanBytes(uint64(p.lastRate)))
		if remain := remaining(pr, p.lastRate); remain != "" {
			line += "  kalan " + remain
		}
	}

	// Satırı temizlemek için sonuna boşluk: kısalan bir satır, önceki
	// satırın kuyruğunu ekranda bırakırdı.
	fmt.Printf("\r%-100s", line)
	p.printed = true
}

// remaining estimates the time left at the current rate.
func remaining(pr flash.Progress, rate float64) string {
	if rate <= 0 || pr.BytesTotal <= pr.BytesDone {
		return ""
	}
	secs := float64(pr.BytesTotal-pr.BytesDone) / rate
	if secs < 1 {
		return ""
	}
	if secs < 60 {
		return fmt.Sprintf("%d sn", int(secs))
	}
	return fmt.Sprintf("%d dk", int(secs/60)+1)
}

// finish moves off the progress line.
func (p *progress) finish() {
	if p.printed {
		fmt.Println()
	}
}

func (p *progress) clear() {
	if p.printed {
		fmt.Printf("\r%-100s\r", "")
		p.printed = false
	}
	_ = os.Stdout.Sync()
}
