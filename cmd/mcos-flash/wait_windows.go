//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"
)

// waitOnWindows keeps the console open when the program was double-clicked.
//
// ── Neden gerekli? ──────────────────────────────────────────────────────────
// Windows'ta bir .exe'ye çift tıklandığında konsol penceresi, program
// bitince ANINDA kapanır. Kullanıcı ne hata mesajını ne de "tamamlandı"
// satırını görebilir — programın hiç çalışmadığını sanır.
//
// ── Neden her zaman değil? ──────────────────────────────────────────────────
// Komut isteminden çalıştırıldığında beklemek can sıkıcıdır ve betiklerde
// (CI, toplu iş) programı sonsuza dek askıda bırakır. Bu yüzden yalnızca
// mcos-flash.bat bunu İSTEDİĞİNDE bekliyoruz: .bat, konsolu kendisi açık
// tuttuğu için değişkeni ayarlamaz ve burada beklemeyiz.
func waitOnWindows() {
	if strings.EqualFold(os.Getenv("MCOS_FLASH_NO_PAUSE"), "1") {
		return
	}
	// Girdi bir boru hattıysa (betik) bekleme: okuma anında EOF döner ve
	// sonsuz döngüye girmeyiz, ama yine de anlamsız bir satır basardık.
	if st, err := os.Stdin.Stat(); err == nil && st.Mode()&os.ModeCharDevice == 0 {
		return
	}
	fmt.Print("\n  Kapatmak için Enter'a basın… ")
	_, _ = stdin.ReadString('\n')
}
