package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"mcos/internal/ipc"
)

// ════════════════════════════════════════════════════════════════════════════
// SSH: "guncelle" — USB'deki ISO ile sistemi güncelle
// ════════════════════════════════════════════════════════════════════════════
//
// Panelin Ayarlar > Sistemi güncelle akışının kabuk karşılığı: ekran başında
// olmayan (SSH ile bağlanan) kullanıcı da güncelleyebilsin. Aynı daemon
// çağrıları kullanılır; iş yine mcos-update'tir, /data'ya dokunulmaz.
//
//	guncelle        listele, sor, kur
//	guncelle 2      listedeki 2. ISO'yu (yine onay sorarak) kur

var girdi = bufio.NewReader(os.Stdin)

func sor(soru string) string {
	fmt.Print(soru)
	s, _ := girdi.ReadString('\n')
	return strings.TrimSpace(s)
}

func evetMi(s string) bool {
	switch strings.ToLower(s) {
	case "e", "evet", "y", "yes":
		return true
	}
	return false
}

// guncelleDurumAdi: karşılaştırma sonucunun Türkçe adı.
func guncelleDurumAdi(it ipc.UpdateISO) string {
	switch it.Compare {
	case ipc.UpdateNewer:
		return "daha yeni"
	case ipc.UpdateSame:
		return "aynı"
	case ipc.UpdateOlder:
		return "eski"
	}
	return "uygun değil: " + it.Error
}

func kisaGuncelle(cli *ipc.Client, args []string) {
	fmt.Println("USB'de MCOS ISO'su aranıyor…")
	var res ipc.UpdateScanResult
	if err := cli.Call(ipc.MethodSystemUpdateScan, struct{}{}, &res); err != nil {
		fail("USB taranamadı: %v", err)
	}
	if res.Live {
		fail("Canlı sistemde güncelleme yok: yeni ISO ile açın ya da Ayarlar'dan diske kurun")
	}
	cur := res.CurrentBuild
	if cur == "" {
		cur = "derleme kimliği yok"
	}
	fmt.Printf("Çalışan sistem: %s · %s\n\n", res.CurrentVersion, cur)
	if len(res.Items) == 0 {
		fail("USB'de MCOS ISO'su bulunamadı (ISO dosyasını USB belleğe kopyalayıp takın)")
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NO\tDOSYA\tSÜRÜM\tDERLEME\tDURUM")
	for i, it := range res.Items {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", i+1, it.Name, it.Version, it.BuildID, guncelleDurumAdi(it))
	}
	tw.Flush()
	fmt.Println()

	secim := ""
	if len(args) > 0 {
		secim = args[0]
	} else {
		secim = sor(fmt.Sprintf("Hangisi kurulsun? [1-%d, boş = vazgeç]: ", len(res.Items)))
	}
	if secim == "" {
		fmt.Println("Vazgeçildi.")
		return
	}
	n, err := strconv.Atoi(secim)
	if err != nil || n < 1 || n > len(res.Items) {
		fail("geçersiz seçim: %s", secim)
	}
	it := res.Items[n-1]
	if it.Compare == ipc.UpdateInvalid {
		fail("%s kurulamaz: %s", it.Name, it.Error)
	}
	if it.Compare == ipc.UpdateOlder {
		fmt.Println("DİKKAT: bu ISO çalışan sistemden ESKİ (sürüm düşürülür).")
	}
	fmt.Println("Sunucular, dünyalar ve ayarlar KORUNUR. Güncelleme yeniden başlatınca uygulanır.")
	if !evetMi(sor("Devam edilsin mi? [e/H]: ")) {
		fmt.Println("Vazgeçildi.")
		return
	}

	var ok ipc.OKResult
	if err := cli.Call(ipc.MethodSystemUpdate, ipc.UpdateParams{Device: it.Device, Path: it.Path}, &ok); err != nil {
		fail("güncelleme başlatılamadı: %v", err)
	}
	son := ""
	for {
		time.Sleep(time.Second)
		var st ipc.UpdateStatusResult
		if err := cli.Call(ipc.MethodSystemUpdateStatus, struct{}{}, &st); err != nil {
			fail("durum alınamadı: %v", err)
		}
		if st.Running {
			if st.Message != "" && st.Message != son {
				son = st.Message
				fmt.Println("  " + st.Message)
			}
			continue
		}
		if st.Failed {
			for _, l := range st.Lines {
				fmt.Fprintln(os.Stderr, l)
			}
			fmt.Fprintln(os.Stderr, "Eski sistem yerinde duruyor; bilgisayar olduğu gibi açılır.")
			os.Exit(1)
		}
		if !st.Done {
			fail("daemon güncellemenin sonucunu bildirmedi")
		}
		fmt.Println(st.Message)
		break
	}
	if !evetMi(sor("Şimdi yeniden başlatılsın mı? Açık sunucular önce düzgünce durdurulur. [e/H]: ")) {
		fmt.Println("Güncelleme bir sonraki yeniden başlatmada kurulacak.")
		return
	}
	if err := cli.Call(ipc.MethodSystemPower, ipc.PowerParams{Action: "reboot"}, &ok); err != nil {
		fail("yeniden başlatılamadı: %v", err)
	}
	fmt.Println("Sunucular durduruluyor, ardından yeniden başlatılıyor…")
}
