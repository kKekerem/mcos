package server

import (
	"testing"

	"mcos/internal/model"
)

// "The server has not responded for 10 seconds": otomatik bellek tavanı
// (ve onun memory.high eşiği) dünya dosyalarının sayfa önbelleğiyle doluyor,
// çekirdek oyun döngüsünü uyutuyordu. Tavan artık hiç konmamalı.
func TestSunucuyaOtomatikBellekTavaniKonmaz(t *testing.T) {
	for _, ram := range []int{1024, 4096, 16384} {
		lim := limitsForServer(&model.Server{RAMMB: ram, CPUQuota: 50})
		if lim.MemoryMB != 0 {
			t.Fatalf("RAM %d MB için bellek tavanı %d MB konmuş", ram, lim.MemoryMB)
		}
		if lim.CPUPercent != 50 {
			t.Fatalf("kullanıcının CPU payı kayboldu: %+v", lim)
		}
	}
}
