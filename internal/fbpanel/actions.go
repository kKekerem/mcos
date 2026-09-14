package fbpanel

import (
	"image/color"

	"mcos/internal/fbui"
)

// colorRGBA is the palette colour type, aliased so screen files do not each
// import image/color for a single type name.
type colorRGBA = color.RGBA

// runAction performs a named, screen-specific action.
//
// ── Neden adla? ─────────────────────────────────────────────────────────────
// Fare tıklaması bir DİKDÖRTGENE gelir; o dikdörtgenin ne yapacağını çizim
// kodu bilir, tıklama kodu bilmez. Dikdörtgene bir AD iliştirmek (zoneAction)
// ikisini birbirine bağlar ve tıklama yolunun her ekranı tek tek tanımasına
// gerek kalmaz.
//
// Adlar burada TEK YERDE listelenir: bir ekran var olmayan bir ad kaydederse
// hiçbir şey olmaz (sessiz başarısızlık) — bu yüzden yeni bir eylem eklerken
// buraya da eklemek gerekir. Test bunu doğrular (panel_test.go).
func (a *App) runAction(key string) Action {
	switch key {
	case "peers-scan":
		a.startPeerScan()
	case "peers-manual":
		a.openManualPair()
	case "peers-key":
		a.showPairingKey()
	case "peers-shared-world":
		a.openSharedWorld()

	case "playit-claim":
		a.startPlayitClaim()
	case "playit-claim-info":
		a.showClaimInstructions()
	case "playit-start":
		a.tunnelActivate(int(stepTunnel))

	case "settings-theme":
		a.openThemePicker()
	case "settings-pointer":
		a.openPointerSettings()
	case "settings-password":
		a.openPasswordSettings()

	case "sleep":
		return ActSleep
	case "reboot":
		a.confirmPower("Yeniden Başlat", ActReboot)
	case "poweroff":
		a.confirmPower("Kapat", ActPoweroff)

	default:
		// Bilinmeyen bir ad, çizim ile eylem listesinin ayrıştığını gösterir.
		// Sessizce yutmak yerine görünür kılıyoruz: geliştirme sırasında
		// hemen fark edilir, kullanıcıda ise yalnızca bir bilgi satırı olur.
		a.Emit(fbui.EventWarn, "Tanımsız eylem: "+key)
	}
	return ActNone
}
