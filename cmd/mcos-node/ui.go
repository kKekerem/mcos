package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
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
//
// Çizim YALNIZCA nodeStatusFile'dan yapılır: aynı ekran hem düğümü çalıştıran
// pencerede hem de arka plandaki düğümü İZLEYEN pencerede kullanılıyor.

// drawnLines is how many lines the previous paint used, so the next paint
// can move the cursor up exactly that far (eş sayısı değişince satır sayısı
// da değişir; sabit bir sayı kalıntı bırakırdı).
var drawnLines int

// uiFooter is the last line: running vs. watching differ in what closing does.
func draw(st nodeStatusFile, footer string) {
	var b strings.Builder
	if drawnLines > 0 {
		fmt.Fprintf(&b, "\033[%dA", drawnLines)
	}
	n := 0
	line := func(format string, args ...any) {
		// \033[K: satırın kalanını temizle. Olmazsa kısalan bir metnin
		// arkasında eski metnin kuyruğu kalır.
		fmt.Fprintf(&b, "  "+format+"\033[K\n", args...)
		n++
	}

	spin := " "
	if st.Busy {
		spin = spinner()
	}

	line("")
	line("\033[1m  MCOS Düğüm %s\033[0m", st.Version)
	line("  ────────────────────────────────────────────────────────")
	line("")
	line("  Bu makine   : %s", st.Name)
	line("  Adres       : \033[1m%s\033[0m", fmtAddr(st.Addr, st.Port))
	line("  Durum       : %s %s", spin, st.Note)
	line("")
	// Eşleştirme isteği EN ÜSTTE ve göze batan biçimde: MCOS ekranında aynı
	// anda aynı kod görünüyor, kullanıcı ikisini karşılaştıracak.
	for _, o := range st.Offers {
		line("\033[1;36m  ┌─ Eşleştirme isteği: %s (%s)\033[0m", o.Name, o.IP)
		line("\033[1;36m  │\033[0m  Kod: \033[1m%s\033[0m   — MCOS ekranındaki kodla AYNI mı?", o.Code)
		if o.Accepted {
			line("\033[1;36m  │\033[0m  Kabul edildi. MCOS ekranında \"Kodlar aynı, onayla\"ya basın.")
		} else {
			line("\033[1;36m  │\033[0m  Aynıysa: K yazıp Enter   (ya da: mcos-node --kabul %s)", digitsOnly(o.Code))
			line("\033[1;36m  │\033[0m  Farklıysa: R yazıp Enter (ya da: mcos-node --reddet %s)", digitsOnly(o.Code))
		}
		line("\033[1;36m  └─\033[0m  %d sn içinde yanıtlanmazsa düşer", o.LeftSec)
		line("")
	}
	// Hangisi YENİYSE o: "K" ile kabulün yanıtı ("MCOS'ta onaylayın")
	// anahtar geldikten sonra "eşleşildi"nin önünü kesmesin (konsolda
	// sınanırken görüldü: 10 saniye boyunca eski ileti duruyordu).
	msg, at := consoleNote()
	if e := st.PairEvent; e != nil && time.Since(e.At) < time.Minute && e.At.After(at) {
		msg = e.Msg
	}
	if msg != "" {
		line("  » %s", msg)
		line("")
	}
	if st.Paired == 0 {
		line("  MCOS'ta:  Sol menü → MCOS Paylaşım → Ağı tara → bu PC → Eşleştir")
		line("  Burada bir kod çıkar; MCOS'takiyle aynıysa K + Enter.")
		line("  Bulamazsa: \"IP adresi gir…\" → %s", st.Addr)
	} else {
		line("  Eşleşen MCOS: %d (çevrimiçi %d)", st.Paired, st.Online)
		for _, p := range st.Peers {
			line("    • %s", p)
		}
	}
	line("")
	if st.LogPath != "" {
		line("  Günlük: %s", st.LogPath)
	}
	line("  %s", footer)
	// Önceki çizim daha uzunsa artan satırları sil.
	for i := n; i < drawnLines; i++ {
		b.WriteString("\033[K\n")
		n++
	}
	line("")
	drawnLines = n

	os.Stdout.WriteString(b.String())
}

// consoleMsg is the last reply to a console key ("… kabul edildi").
//
// Ekran her saniye yerinde yeniden çiziliyor; araya doğrudan satır basmak
// satır sayımını (drawnLines) bozup ekranı kaydırırdı. İleti çizimin
// parçası olur ve 10 saniye görünür.
var (
	consoleMu  sync.Mutex
	consoleMsg string
	consoleAt  time.Time
)

func setConsoleNote(s string) {
	consoleMu.Lock()
	consoleMsg, consoleAt = s, time.Now()
	consoleMu.Unlock()
}

func consoleNote() (string, time.Time) {
	consoleMu.Lock()
	defer consoleMu.Unlock()
	if time.Since(consoleAt) > 10*time.Second {
		return "", consoleAt
	}
	return consoleMsg, consoleAt
}

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
var spinIdx int

func spinner() string {
	spinIdx = (spinIdx + 1) % len(spinFrames)
	return spinFrames[spinIdx]
}

// watchLoop shows a background node's status until the user closes it.
//
// Bu pencereyi kapatmak düğümü DURDURMAZ — bilinçli: düğüm oturum boyunca
// arka planda çalışsın diye kuruldu. Durdurmak isteyen "--kaldir" kullanır.
func watchLoop(dataRoot string, stop <-chan os.Signal, until <-chan time.Time) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	// Arka plandaki düğümün eşleştirme isteği bu pencereden de kabul
	// edilebilsin (K + Enter): karar dosyası süreçten bağımsızdır.
	go consoleKeys(dataRoot, os.Stdin, setConsoleNote)
	stale := 0
	for {
		st, ok := readStatus(dataRoot)
		switch {
		case ok && time.Since(st.Updated) < 10*time.Second:
			stale = 0
			draw(st, "Düğüm arka planda çalışıyor. Bu pencereyi kapatmak onu DURDURMAZ.")
		default:
			stale++
			if stale == 5 {
				fmt.Println("\n  Arka plandaki düğüm durum bildirmiyor; günlüğe bakın.")
			}
		}
		select {
		case <-stop:
			fmt.Print("\n")
			return
		case <-until:
			return
		case <-t.C:
		}
	}
}
