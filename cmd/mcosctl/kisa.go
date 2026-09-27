package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"mcos/internal/ipc"
	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// SSH KISA KOMUTLARI
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının isteği: "SSH ile bağlanınca komutlar olsun: restart sw_adı,
// stop sw_adı gibi." mcosctl kimlik (3f9c…) ister; SSH'ta çalışan biri
// sunucuyu ADIYLA bilir. Bu komutlar /usr/bin altında mcosctl'e bağdır
// (baslat, durdur, yeniden, liste…) ve argv[0]'dan tanınır; sunucu adı
// büyük/küçük harf duyarsız, tek anlamlıysa önekle de bulunur.

// kisaKomutlar: bağ adı -> iç komut.
var kisaKomutlar = map[string]string{
	"liste": "liste", "list": "liste", "sunucular": "liste",
	"baslat": "baslat", "start": "baslat",
	"durdur": "durdur", "stop": "durdur",
	"yeniden": "yeniden", "restart": "yeniden",
	"durum":  "durum",
	"konsol": "konsol", "console": "konsol",
	"komut":    "komut",
	"ekle":     "ekle",
	"guncelle": "guncelle",
	"yardim":   "yardim", "mcos": "yardim",
}

// kisaKomutMu reports whether the program was invoked under a short name.
func kisaKomutMu() (string, bool) {
	c, ok := kisaKomutlar[filepath.Base(os.Args[0])]
	return c, ok
}

func kisaYardim() {
	fmt.Print(`MCOS komutları (sunucu ADIYLA; "hepsi" = bütün sunucular):
  liste                     sunucuları ve durumlarını göster
  baslat  <ad|hepsi>        başlat          (start)
  durdur  <ad|hepsi>        durdur          (stop)
  yeniden <ad|hepsi>        yeniden başlat  (restart)
  durum   [ad]              ayrıntılı durum
  konsol  <ad>              canlı konsol (çıkış: Ctrl-C)
  komut   <ad> <komut...>   sunucu konsoluna komut gönder (ör. komut Survival say merhaba)
  ekle    <klasör>          /data/sunucular'a yüklenen klasörü hemen sunucu yap
  guncelle [no]             USB'deki MCOS ISO'su ile sistemi güncelle (veriler korunur)
  yardim                    bu liste

WinSCP: sunucular /data/sunucular altında adlarıyla görünür. Yeni bir sunucu
klasörü oraya atılınca yükleme bitince kendiliğinden eklenir.
`)
}

func runKisa(cli *ipc.Client, cmd string, args []string) {
	switch cmd {
	case "yardim":
		kisaYardim()
	case "liste":
		kisaListe(cli)
	case "baslat", "durdur", "yeniden":
		method := map[string]string{"baslat": ipc.MethodServerStart,
			"durdur": ipc.MethodServerStop, "yeniden": ipc.MethodServerRestart}[cmd]
		if len(args) == 0 {
			fail("kullanım: %s <sunucu adı|hepsi>", cmd)
		}
		for _, s := range sunucuSec(cli, strings.Join(args, " ")) {
			var res ipc.OKResult
			if err := cli.Call(method, ipc.IDParams{ID: s.ID}, &res); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", s.Name, err)
				continue
			}
			fmt.Printf("%s: %s\n", s.Name, map[string]string{"baslat": "başlatılıyor",
				"durdur": "durduruluyor", "yeniden": "yeniden başlatılıyor"}[cmd])
		}
	case "durum":
		if len(args) == 0 {
			kisaListe(cli)
			return
		}
		for _, s := range sunucuSec(cli, strings.Join(args, " ")) {
			fmt.Printf("%s\n  yazılım : %s %s\n  durum   : %s\n  port    : %d\n  oyuncu  : %d\n  RAM     : %d MB\n",
				s.Name, s.Software, s.MCVersion, durumAdi(s.State), s.Port, s.Players, s.RAMMB)
		}
	case "konsol":
		if len(args) == 0 {
			fail("kullanım: konsol <sunucu adı>")
		}
		s := tekSunucu(cli, strings.Join(args, " "))
		cmdConsole(cli, s.ID)
	case "komut":
		if len(args) < 2 {
			fail("kullanım: komut <sunucu adı> <minecraft komutu...>")
		}
		s, rest := adVeKalan(cli, args)
		cmdSend(cli, s.ID, rest)
	case "ekle":
		if len(args) == 0 {
			fail("kullanım: ekle <klasör adı>   (/data/sunucular altındaki)")
		}
		var res ipc.ServerResult
		if err := cli.Call(ipc.MethodServerImportFolder, map[string]string{"name": strings.Join(args, " ")}, &res); err != nil {
			fail("ekle: %v", err)
		}
		if res.Server != nil {
			fmt.Printf("%s eklendi (%s %s, port %d). Başlatmak için: baslat %s\n",
				res.Server.Name, res.Server.Software, res.Server.MCVersion, res.Server.Port, res.Server.Name)
		}
	case "guncelle":
		kisaGuncelle(cli, args)
	default:
		kisaYardim()
	}
}

func sunucuListesi(cli *ipc.Client) []*model.Server {
	var res ipc.ServerListResult
	if err := cli.Call(ipc.MethodServerList, nil, &res); err != nil {
		fail("sunucular okunamadı: %v", err)
	}
	return res.Servers
}

func kisaListe(cli *ipc.Client) {
	list := sunucuListesi(cli)
	if len(list) == 0 {
		fmt.Println("(sunucu yok — WinSCP ile /data/sunucular'a bir sunucu klasörü atabilir ya da panelden kurabilirsiniz)")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AD\tYAZILIM\tSÜRÜM\tDURUM\tPORT\tOYUNCU")
	for _, s := range list {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%d\n", s.Name, s.Software, s.MCVersion, durumAdi(s.State), s.Port, s.Players)
	}
	w.Flush()
}

func durumAdi(st model.ServerState) string {
	switch st {
	case model.StateRunning:
		return "çalışıyor"
	case model.StateStarting:
		return "açılıyor"
	case model.StateStopping:
		return "kapanıyor"
	case model.StateError:
		return "HATA"
	default:
		return "kapalı"
	}
}

// eslesen: kimlik, tam ad (harf duyarsız), sonra tek anlamlı önek.
func eslesen(list []*model.Server, q string) []*model.Server {
	q = strings.TrimSpace(q)
	lq := strings.ToLower(q)
	var tam, onek []*model.Server
	for _, s := range list {
		if s.ID == q || strings.ToLower(s.Name) == lq {
			tam = append(tam, s)
		} else if strings.HasPrefix(strings.ToLower(s.Name), lq) {
			onek = append(onek, s)
		}
	}
	if len(tam) > 0 {
		return tam
	}
	return onek
}

// sunucuSec: "hepsi" bütün sunucular; değilse tek bir eşleşme.
func sunucuSec(cli *ipc.Client, q string) []*model.Server {
	list := sunucuListesi(cli)
	if l := strings.ToLower(strings.TrimSpace(q)); l == "hepsi" || l == "all" {
		return list
	}
	return []*model.Server{tekBul(list, q)}
}

func tekSunucu(cli *ipc.Client, q string) *model.Server { return tekBul(sunucuListesi(cli), q) }

func tekBul(list []*model.Server, q string) *model.Server {
	m := eslesen(list, q)
	switch len(m) {
	case 1:
		return m[0]
	case 0:
		var adlar []string
		for _, s := range list {
			adlar = append(adlar, s.Name)
		}
		fail("%q adında sunucu yok. Sunucular: %s", q, strings.Join(adlar, ", "))
	default:
		var adlar []string
		for _, s := range m {
			adlar = append(adlar, s.Name)
		}
		fail("%q birden çok sunucuya uyuyor: %s — adı tam yazın", q, strings.Join(adlar, ", "))
	}
	return nil
}

// adVeKalan: "komut Benim Sunucum say merhaba" — en UZUN eşleşen adı alır,
// kalanı Minecraft komutudur (adlar boşluk içerebilir).
func adVeKalan(cli *ipc.Client, args []string) (*model.Server, []string) {
	list := sunucuListesi(cli)
	for n := len(args) - 1; n >= 1; n-- {
		ad := strings.Join(args[:n], " ")
		if m := eslesen(list, ad); len(m) == 1 && (strings.EqualFold(m[0].Name, ad) || m[0].ID == ad) {
			return m[0], args[n:]
		}
	}
	return tekBul(list, args[0]), args[1:]
}
