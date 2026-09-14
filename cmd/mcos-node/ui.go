package main

import (
	"fmt"
	"os"
	"strings"

	"mcos/internal/cluster"
	"mcos/internal/model"
	"mcos/internal/version"
)

// Bu dosya konsol durum ekranını çizer.
//
// ── Neden grafik arayüz yok ─────────────────────────────────────────────────
// Bu program açılıp bırakılacak: kullanıcı ona bakmayacak, yalnızca "çalışıyor
// mu?" diye göz atacak. Bir pencere kütüphanesi eklemek, ikiliyi onlarca
// megabayt büyütür, Windows'ta imzasız çalıştırma uyarıları getirir ve
// Linux'ta masaüstü bağımlılıkları ister. Konsol her yerde çalışır.
//
// Ekran yerinde güncellenir (imleci yukarı taşıyıp üzerine yazarak), böylece
// kayan bir günlük seli yerine sabit bir tablo görünür.

const uiLines = 14

// drawn tracks whether we have already painted once, so the next paint can
// move the cursor up instead of scrolling.
var drawn bool

func draw(h *nodeHost, mgr *cluster.Manager, s nodeSettings, port int) {
	var b strings.Builder

	if drawn {
		// İmleci çizdiğimiz satır sayısı kadar yukarı al ve üzerine yaz.
		fmt.Fprintf(&b, "\033[%dA", uiLines)
	}
	drawn = true

	line := func(format string, args ...any) {
		// \033[K: satırın kalanını temizle. Olmazsa kısalan bir metnin
		// arkasında eski metnin kuyruğu kalır.
		fmt.Fprintf(&b, "  "+format+"\033[K\n", args...)
	}

	addr := cluster.PrimaryIPv4()
	if addr == "" {
		addr = "adres yok"
	}

	note, busy := h.status()
	peers := mgr.Peers()
	paired := 0
	online := 0
	for _, p := range peers {
		if p.Paired {
			paired++
			if p.State == model.PeerAvailable {
				online++
			}
		}
	}

	spin := " "
	if busy {
		spin = spinner()
	}

	line("")
	line("\033[1m  MCOS Düğüm %s\033[0m", version.Display())
	line("  ────────────────────────────────────────────────────────")
	line("")
	line("  Bu makine   : %s", s.Name)
	line("  Adres       : \033[1m%s:%d\033[0m", addr, port)
	line("  Durum       : %s %s", spin, note)
	line("")
	if paired == 0 {
		line("  MCOS'ta:  Sol menü → PC Eşleştirme → Ağı tara")
		line("  Bulamazsa: \"IP gir\" → %s", addr)
	} else {
		line("  Eşleşen MCOS: %d (çevrimiçi %d)", paired, online)
		line("")
	}
	line("")
	line("  Kapatmak için Ctrl+C. Kapatınca bu makinedeki sunucu durur.")
	line("")

	os.Stdout.WriteString(b.String())
}

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
var spinIdx int

func spinner() string {
	spinIdx = (spinIdx + 1) % len(spinFrames)
	return spinFrames[spinIdx]
}
