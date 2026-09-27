package sound

import (
	"sort"
	"strconv"
	"strings"
)

// ════════════════════════════════════════════════════════════════════════════
// ÇIKIŞ SEÇİMİ: HANGİ KART, HANGİ PCM
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı gerçek PC'lerde: "farklı PC'lerden farklı sesler geliyor, tıklama
// sesi hoparlör kullanılsın".
//
// ── Düzeltilen gerçek hata: HER ŞEY "KART 0"A GİDİYORDU ─────────────────────
//
// Eski arka uç aygıt adını hiç seçmiyordu: "default", "plughw:0,0", "hw:0,0".
// Üçü de KART 0, AYGIT 0 demek. Kart 0'ın ne olduğu ise PCI tarama sırasına
// bağlı ve makineden makineye değişiyor:
//
//	masaüstü + NVIDIA/AMD ekran kartı:  kart 0 = "HDA NVidia", pcm = "HDMI 0"
//	                                    kart 1 = "HDA Intel PCH", "ALC892 Analog"
//
// O makinede sesler monitörün HDMI hattına gidiyor (hoparlörsüz monitörde
// hiç duyulmuyor), ses seviyesi tuşları da yine kart 0'ın mikserini
// değiştiriyordu. Aynı imaj başka bir makinede analog hoparlörden çalıyordu —
// "farklı PC, farklı ses" şikâyetinin ölçülebilen bir yarısı bu.
//
// Artık /proc/asound/pcm okunuyor ve çıkışlar TÜRLERİNE göre sıralanıyor.
// Dosyalar düz metin; ayrıştırma bu yüzden platformdan bağımsız ve her
// ortamda sınanabiliyor (cards_test.go).

// cikisTuru, bir çalma aygıtının sınıfı. Büyük olan tercih edilir.
type cikisTuru int

const (
	// turHDMI: monitör/TV hattı. Monitörde hoparlör olmayabilir; YALNIZCA
	// başka çıkış yoksa kullanılır.
	turHDMI cikisTuru = iota
	// turDijital: S/PDIF (optik/koaksiyel). Arkasında çoğu zaman amfi yok.
	turDijital
	// turAnalog: dahili kart — dizüstü hoparlörü, kulaklık girişi, arka
	// panel hat çıkışı. Kullanıcının "hoparlör" dediği yol.
	turAnalog
	// turUSB: sonradan TAKILMIŞ USB kulaklık/hoparlör.
	//
	// Analogdan ÖNCE geliyor: dahili analog çıkış her makinede vardır ama
	// masaüstünde arkasına hiçbir şey bağlı olmayabilir; bir USB ses aygıtı
	// ise yalnızca biri onu bilerek taktığı için oradadır. Çıkarılınca
	// yeniden tarama (backend_linux.go) dahili analoğa döner.
	turUSB
)

func (t cikisTuru) String() string {
	switch t {
	case turHDMI:
		return "HDMI"
	case turDijital:
		return "dijital"
	case turAnalog:
		return "analog"
	case turUSB:
		return "USB"
	}
	return "?"
}

// cikis, bir çalma (playback) PCM aygıtı.
type cikis struct {
	kart, aygit int
	ad          string // /proc/asound/pcm'deki ad: "ALC892 Analog", "HDMI 0"
	kartAdi     string // /proc/asound/cards'taki kısa ad: "HDA Intel PCH"
	surucu      string // /proc/asound/cards'taki sürücü: "HDA-Intel", "USB-Audio"
	tur         cikisTuru
}

// aplayAygiti, aplay'e verilen PCM adı.
//
// ── Neden plughw, neden "hw" ya da "default" değil ─────────────────────────
//
// "hw:K,A" ham donanımdır ve aplay orada oranı set_rate_near ile kurar:
// yalnızca 48 kHz kabul eden bir kartta 44,1 kHz istenirse 48 kHz'e
// YUVARLANIR ve ses %8,8 (bir buçuk yarım ton) TİZ çalar — aplay bunu yalnızca
// -q verilmediğinde "rate is not accurate" diye söyler, biz -q veriyorduk.
// Eski listenin son halkası tam buydu: aynı efekt kartına göre farklı perdede.
//
// "plughw:K,A" aynı donanıma gider ama oran/biçim/kanal dönüşümünü alsa-lib
// yapar: her kartta aynı perde. "default" ise yine kart 0'dır (yukarıdaki
// hata) ve dmix için System V IPC ister.
func (c cikis) aplayAygiti() string {
	return "plughw:" + strconv.Itoa(c.kart) + "," + strconv.Itoa(c.aygit)
}

// Tanim, ayarlar ekranı için okunur bir açıklama: "ALC892 Analog · analog".
func (c cikis) Tanim() string {
	ad := strings.TrimSpace(c.ad)
	if ad == "" {
		ad = strings.TrimSpace(c.kartAdi)
	}
	return ad + " · " + c.tur.String()
}

// ayniAygit, iki çıkışın aynı donanım uç noktası olup olmadığı.
func (c cikis) ayniAygit(d cikis) bool { return c.kart == d.kart && c.aygit == d.aygit }

// kartBilgisi, /proc/asound/cards'tan bir satır.
type kartBilgisi struct{ surucu, kisaAd string }

// parseCards reads /proc/asound/cards.
//
// Biçim (sound/core/init.c, card_info_read):
//
//	" 0 [PCH            ]: HDA-Intel - HDA Intel PCH"
//	"                      HDA Intel PCH at 0xf7f10000 irq 32"
//
// İkinci (girintili, köşeli parantezsiz) satır uzun addır; yok sayılır.
func parseCards(s string) map[int]kartBilgisi {
	out := map[int]kartBilgisi{}
	for _, satir := range strings.Split(s, "\n") {
		t := strings.TrimSpace(satir)
		ac := strings.IndexByte(t, '[')
		kapa := strings.Index(t, "]:")
		if ac <= 0 || kapa < ac {
			continue
		}
		no, err := strconv.Atoi(strings.TrimSpace(t[:ac]))
		if err != nil {
			continue
		}
		kalan := strings.TrimSpace(t[kapa+2:])
		surucu, kisa := kalan, ""
		if i := strings.Index(kalan, " - "); i >= 0 {
			surucu, kisa = strings.TrimSpace(kalan[:i]), strings.TrimSpace(kalan[i+3:])
		}
		out[no] = kartBilgisi{surucu: surucu, kisaAd: kisa}
	}
	return out
}

// parsePCM reads /proc/asound/pcm and returns the PLAYBACK devices only.
//
// Biçim (sound/core/pcm.c, snd_pcm_proc_read):
//
//	"00-00: ALC892 Analog : ALC892 Analog : playback 1 : capture 1"
//	"00-01: Intel ICH - MIC ADC : Intel 82801AA-ICH - MIC ADC : capture 1"
//
// Yalnızca kayıt yapan aygıtlar (mikrofon ADC'si) elenir: onlara aplay
// "Invalid argument" der ve her sesi bir başarısız süreçle başlatırdık.
func parsePCM(s string, kartlar map[int]kartBilgisi) []cikis {
	var out []cikis
	for _, satir := range strings.Split(s, "\n") {
		satir = strings.TrimSpace(satir)
		i := strings.Index(satir, ": ")
		if i < 0 {
			continue
		}
		kk := strings.SplitN(satir[:i], "-", 2)
		if len(kk) != 2 {
			continue
		}
		kart, err1 := strconv.Atoi(kk[0])
		aygit, err2 := strconv.Atoi(kk[1])
		if err1 != nil || err2 != nil {
			continue
		}
		alanlar := strings.Split(satir[i+2:], " : ")
		calar := false
		for _, a := range alanlar {
			if strings.HasPrefix(strings.TrimSpace(a), "playback ") {
				calar = true
			}
		}
		if !calar {
			continue
		}
		c := cikis{kart: kart, aygit: aygit}
		if len(alanlar) > 1 {
			c.ad = strings.TrimSpace(alanlar[1])
		} else {
			c.ad = strings.TrimSpace(alanlar[0])
		}
		kb := kartlar[kart]
		c.kartAdi, c.surucu = kb.kisaAd, kb.surucu
		c.tur = siniflandir(c, strings.TrimSpace(alanlar[0]))
		out = append(out, c)
	}
	return out
}

// siniflandir decides what kind of output a PCM is.
//
// ADA bakılıyor, çünkü çekirdeğin kullanıcı alanına verdiği tek ayırt edici
// bilgi bu: HDA HDMI kodeği PCM'lerine "HDMI 0..N" adı veriyor
// (patch_hdmi.c), S/PDIF'e "... Digital" (hda_generic.c), analoga
// "... Analog". USB ise kartın SÜRÜCÜSÜNDEN tanınıyor (usb/card.c:
// "USB-Audio"); USB aygıtlarının PCM adı "USB Audio" gibi genel bir şeydir.
func siniflandir(c cikis, id string) cikisTuru {
	ad := strings.ToLower(c.ad + " " + id)
	switch {
	case strings.Contains(ad, "hdmi") || strings.Contains(ad, "displayport"):
		return turHDMI
	case strings.Contains(ad, "digital") || strings.Contains(ad, "iec958") ||
		strings.Contains(ad, "spdif") || strings.Contains(ad, "s/pdif"):
		return turDijital
	case strings.EqualFold(c.surucu, "USB-Audio"):
		return turUSB
	}
	return turAnalog
}

// siraliCikislar returns every playback output, best first.
//
// Sıra: türe göre (USB > analog > dijital > HDMI), sonra kart ve aygıt
// numarasına göre. Aynı kodekte "ALC892 Analog" (aygıt 0) ile "ALC892 Alt
// Analog" (aygıt 2) varsa 0 seçilir: dizüstü hoparlörünün bağlı olduğu ana
// yol odur.
func siraliCikislar(pcm, cards string) []cikis {
	c := parsePCM(pcm, parseCards(cards))
	sort.SliceStable(c, func(i, j int) bool {
		if c[i].tur != c[j].tur {
			return c[i].tur > c[j].tur
		}
		if c[i].kart != c[j].kart {
			return c[i].kart < c[j].kart
		}
		return c[i].aygit < c[j].aygit
	})
	return c
}

// parseScontrols reads "amixer scontrols" output.
//
//	"Simple mixer control 'Master',0"
//	"Simple mixer control 'Headphone',0"
//
// Yalnızca 0 dizinli kontroller alınır: "Headphone,1" gibi ikinci kopyalar
// (bazı Realtek'lerde ön panel) aynı ada tek sset ile ulaşılmaz ve burada
// gerekmiyor.
func parseScontrols(s string) []string {
	var out []string
	for _, satir := range strings.Split(s, "\n") {
		i := strings.IndexByte(satir, '\'')
		j := strings.LastIndexByte(satir, '\'')
		if i < 0 || j <= i {
			continue
		}
		if !strings.HasSuffix(strings.TrimSpace(satir), ",0") {
			continue
		}
		out = append(out, satir[i+1:j])
	}
	return out
}

// mikserPlani, bir kartın kontrollerinden hangisinin kullanıcı seviyesini
// taşıyacağını (ana) ve hangilerinin yalnızca AÇILACAĞINI (yan) seçer.
//
// ── Düzeltilen gerçek hata: "Front" HİÇ AÇILMIYORDU ─────────────────────────
//
// Eski liste Master, PCM, Speaker, Headphone idi. HDA sürücüsü kodek
// yükselteçlerini açılışta KAPALI bırakır (alsactl restore/init yoksa kimse
// açmaz) ve "Master" yalnızca bir SANAL ana ayardır: altındaki "Front"
// kapalıyken Master'ı açmak hiçbir şey duyurmaz. VirtualBox'ın HD Audio
// kodeği (STAC9221) ve çoğu masaüstü Realtek'i hat çıkışını "Front" adıyla
// verir — kullanıcının VirtualBox'ta "ses efektleri çalışmadı" dediği yol.
//
// ── Düzeltilen gerçek hata: SEVİYE İKİ KEZ UYGULANIYORDU ────────────────────
//
// Kullanıcı seviyesi (%70) Master'a DA, Headphone'a DA, PCM'e DE yazılıyordu.
// Zincirdeki her kademe ayrı ayrı kısıyor: QEMU'nun HDA kodeğinde ölçülen
// Master %70 = -22 dB idi; üstüne Headphone %70 bir o kadar daha. Kulaklıkta
// ~-44 dB, yani "çalıyor ama duyulmuyor".
//
// Artık seviye YALNIZCA ana kontrole yazılıyor; yan kontroller 0 dB'e
// (geçirgen) açılıyor. alsactl init'in yaptığı da budur.
func mikserPlani(kontroller []string) (ana string, yan []string) {
	var var_ = map[string]bool{}
	for _, k := range kontroller {
		var_[k] = true
	}
	// Ana: kullanıcının F3/F4 ile değiştirdiği tek kontrol. Master yoksa
	// (bazı USB aygıtlarında yalnızca "PCM" ya da "Speaker" vardır) sıradaki.
	for _, k := range anaAdaylari {
		if var_[k] {
			ana = k
			break
		}
	}
	for _, k := range acilacaklar {
		if var_[k] && k != ana {
			yan = append(yan, k)
		}
	}
	return ana, yan
}

// anaAdaylari, kullanıcı seviyesini taşıyabilecek kontroller, tercih sırasıyla.
var anaAdaylari = []string{"Master", "PCM", "Speaker", "Headphone", "Front"}

// acilacaklar, çıkış yolundaki ve AÇILMASI gereken kontroller.
//
// BİLEREK dar bir liste: "Mic", "Line", "CD" gibi kontroller de "Playback"
// taşır ama onlar GİRİŞİN hoparlöre geri beslemesidir; açmak mikrofon
// uğultusu ve geri besleme ıslığı demekti. Yalnızca çıkış uçları açılıyor.
var acilacaklar = []string{
	"Master", "Front", "Speaker", "Headphone", "PCM",
	"Line Out", "Bass Speaker", "Headphone+LO", "Speaker+LO",
}
