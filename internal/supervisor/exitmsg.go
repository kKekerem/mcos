package supervisor

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// exitMessage explains an unexpected exit in Turkish.
//
// Eskiden kullanıcıya "Çıkış kodu: -1, Hata: signal: killed" gidiyordu: Go'nun
// İngilizce hata metni ve anlamsız bir -1. En yaygın sebep (bellek yetmemesi)
// hiç söylenmiyordu; kullanıcı sunucunun "kendi kendine kapandığını" sanıyordu.
func exitMessage(code int, err error, sigkill bool, grupOOM, sistemOOM int) string {
	switch {
	case sigkill && grupOOM > 0:
		return "[MCOS HATA] Sunucu, kendisine ayrılan bellek tavanını aştığı için çekirdek " +
			"tarafından kapatıldı (OOM, SIGKILL). Sunucu ayarlarından RAM'i artırın ya da " +
			"eklenti/mod yükünü azaltın."
	case sigkill && sistemOOM > 0:
		return "[MCOS HATA] Sistemin belleği bittiği için çekirdek sunucuyu kapattı (OOM, " +
			"SIGKILL). MCOS'un kendisi de RAM'de çalışır: sunucunun RAM ayarını düşürün, " +
			"diğer sunucuları kapatın ya da makineye RAM ekleyin."
	case sigkill:
		return "[MCOS HATA] Sunucu zorla sonlandırıldı (SIGKILL). Bellek yetersizliği ya da " +
			"dışarıdan öldürülme olabilir; sistem günlüğüne bakın."
	}
	neden := ""
	if err != nil {
		neden = turkceHata(err.Error())
	}
	if neden == "" {
		return fmt.Sprintf("[MCOS HATA] Süreç beklenmeyen bir şekilde sonlandı (çıkış kodu %d).", code)
	}
	return fmt.Sprintf("[MCOS HATA] Süreç beklenmeyen bir şekilde sonlandı (çıkış kodu %d, %s).", code, neden)
}

// killedBySIGKILL reports whether the finished command died from SIGKILL.
func killedBySIGKILL(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.ProcessState == nil {
		return false
	}
	ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	return ok && ws.Signaled() && ws.Signal() == syscall.SIGKILL
}

// turkceHata, Go'nun "signal: X" / "exit status N" metinlerini Türkçeleştirir.
func turkceHata(s string) string {
	switch {
	case strings.HasPrefix(s, "exit status "):
		return "" // kod zaten yazılıyor
	case strings.HasPrefix(s, "signal: "):
		return "sinyal: " + strings.TrimPrefix(s, "signal: ")
	}
	return s
}
