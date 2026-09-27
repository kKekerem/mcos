// Tüm arayüz seslerini tek bir WAV dosyasına yazar (aralarında 400 ms sessizlik).
// Yalnızca gözden geçirme içindir; imaja girmez.
package main

import (
	"encoding/binary"
	"fmt"
	"os"

	"mcos/internal/sound"
)

func main() {
	out := "efektler.wav"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}

	names := []struct {
		e sound.Effect
		n string
	}{
		{sound.Boot, "Boot (açılış)"},
		{sound.Nav, "Nav (sayfa geçişi)"},
		{sound.Open, "Open (pencere açılışı)"},
		{sound.Close, "Close (pencere kapanışı)"},
		{sound.Confirm, "Confirm (onay)"},
		{sound.Error, "Error (hata)"},
		{sound.Shutdown, "Shutdown (kapanış)"},
	}

	const rate = 44100
	var all []int16
	gap := make([]int16, rate*4/10) // 400 ms sessizlik

	for _, it := range names {
		pcm := sound.Render(it.e)
		fmt.Printf("%-28s %5d örnek (%.0f ms)\n", it.n, len(pcm),
			float64(len(pcm))*1000/rate)
		all = append(all, pcm...)
		all = append(all, gap...)
	}

	f, err := os.Create(out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	dataLen := len(all) * 2
	w := func(v any) { _ = binary.Write(f, binary.LittleEndian, v) }
	f.WriteString("RIFF")
	w(uint32(36 + dataLen))
	f.WriteString("WAVEfmt ")
	w(uint32(16))       // fmt chunk size
	w(uint16(1))        // PCM
	w(uint16(1))        // mono
	w(uint32(rate))     // sample rate
	w(uint32(rate * 2)) // byte rate
	w(uint16(2))        // block align
	w(uint16(16))       // bits
	f.WriteString("data")
	w(uint32(dataLen))
	for _, s := range all {
		w(s)
	}
	fmt.Printf("\nyazıldı: %s (%.1f sn)\n", out, float64(len(all))/rate)
}
