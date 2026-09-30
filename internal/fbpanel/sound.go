package fbpanel

import (
	"fmt"
	"strings"
	"sync"

	"mcos/internal/fbui"
	"mcos/internal/sound"
)

// Panelin ses bağlantısı.
//
// ── Neden burada, tek bir yerde ─────────────────────────────────────────────
//
// Ses, arayüzün HER yerinden tetiklenebilir (pencere açılışı, sayfa geçişi,
// hata, kapanış). Her çağrı yerinde "ayar açık mı, çalar var mı" diye
// denetlemek, bir gün birinin unutacağı bir kural olurdu. Buradaki tek geçit
// hem ayarı hem çaları biliyor; çağıran yalnızca "şu oldu" diyor.
//
// ── Neden ayar her seferinde okunuyor ───────────────────────────────────────
//
// Kullanıcı sesi Ayarlar'dan kapattığında etkisi ANINDA olmalı. Çalar
// nesnesini yapılandırma değiştiğinde yeniden kurmak yerine, ayarın kendisi
// her çalmada okunuyor: model.UIConfig zaten kilit altında tutulan küçük bir
// yapı ve okuma maliyeti ihmal edilebilir.

// soundOnce, çaları ilk ihtiyaçta kurar.
//
// TEMBEL: ses hiç çalınmayacaksa (ayar kapalı, ekran görüntüsü kipi, test)
// hiçbir goroutine başlamaz ve hiçbir aygıt aranmaz.
var (
	soundOnce   sync.Once
	soundPlayer *sound.Player
)

// player returns the process-wide sound player, honouring the current config.
func (a *App) player() *sound.Player {
	ui := a.UIPrefs()
	soundOnce.Do(func() { soundPlayer = sound.New(ui.Sounds) })
	soundPlayer.SetEnabled(ui.Sounds)
	// Bipçi izni de her çalmada okunuyor: Ayarlar'dan kapatıldığı anda bir
	// sonraki ses artık bipçiden çıkmıyor.
	soundPlayer.SetBeeper(ui.Beeper)
	return soundPlayer
}

// playSound queues an interface effect. Never blocks, never fails.
func (a *App) playSound(e sound.Effect) {
	if a == nil || a.ui == nil {
		return
	}
	// Ekran görüntüsü kipinde ses çalmak anlamsız: o kip tek kare çizip
	// çıkıyor ve ses goroutine'i başlatmak süreci uzatırdı.
	a.mu.Lock()
	headless := a.headless
	a.mu.Unlock()
	if headless {
		return
	}
	a.player().Play(e)
}

// SoundBackend reports which output the player picked ("alsa", "pcspkr",
// "yok"). Ayarlar ekranı bunu gösterir.
func (a *App) SoundBackend() string {
	if soundPlayer == nil {
		return "denenmedi"
	}
	return soundPlayer.Backend()
}

// SoundOutput, çalan çıkışın okunur adı ("ALC892 Analog · analog"); yoksa "".
func (a *App) SoundOutput() string {
	if soundPlayer == nil {
		return ""
	}
	return soundPlayer.Output()
}

// SetHeadless marks the app as a one-shot screenshot run (no sound, no
// background goroutines).
func (a *App) SetHeadless(on bool) {
	a.mu.Lock()
	a.headless = on
	a.mu.Unlock()
}

// showVolume flashes the new level in the status bar.
//
// ── Neden durum çubuğu, neden ayrı bir kaplama değil ────────────────────────
//
// Ayrı bir "ses seviyesi" kaplaması çizmek çekici görünüyor ama iki sorun
// getiriyor: (1) panelin her katmanının üstünde durması gerekir ve çizim
// sırasını karmaşıklaştırır, (2) kaybolması için bir zamanlayıcı ister.
//
// Durum çubuğu ZATEN bunun için var ve zaten kayboluyor. Ses seviyesi de
// öteki geri bildirimlerle aynı yerde görünüyor — kullanıcı nereye bakacağını
// öğrenmek zorunda kalmıyor.
func (a *App) showVolume(v int, sessiz bool) {
	if sessiz || v == 0 {
		a.Emit(fbui.EventInfo, "Ses: kapalı")
		return
	}
	// Çubuk GÖRSEL: yüzde sayısı tek başına "ne kadar yüksek" sorusunu
	// yanıtlamıyor, dolu/boş oran anında okunuyor.
	const genislik = 20
	dolu := v * genislik / 100
	bar := strings.Repeat("█", dolu) + strings.Repeat("░", genislik-dolu)
	a.Emit(fbui.EventInfo, fmt.Sprintf("Ses  %s  %%%d", bar, v))
}
