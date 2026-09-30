package sound

import (
	"reflect"
	"testing"
)

// Gerçek bir masaüstünden alınmış biçim: kart 0 ekran kartının HDMI sesi,
// kart 1 anakartın analog çıkışı. Eski kod burada HDMI'ye çalıyordu.
const (
	ornekCards = ` 0 [NVidia         ]: HDA-Intel - HDA NVidia
                      HDA NVidia at 0xf7080000 irq 17
 1 [PCH            ]: HDA-Intel - HDA Intel PCH
                      HDA Intel PCH at 0xf7f10000 irq 32
`
	ornekPCM = `00-03: HDMI 0 : HDMI 0 : playback 1
00-07: HDMI 1 : HDMI 1 : playback 1
01-00: ALC892 Analog : ALC892 Analog : playback 1 : capture 1
01-01: ALC892 Digital : ALC892 Digital : playback 1
01-02: ALC892 Alt Analog : ALC892 Alt Analog : capture 1
`
)

func TestHoparlorHDMIyeTercihEdiliyor(t *testing.T) {
	c := siraliCikislar(ornekPCM, ornekCards)
	if len(c) != 4 {
		t.Fatalf("4 çalma aygıtı bekleniyordu (kayıt-yalnız elenmeli), %d geldi: %+v", len(c), c)
	}
	if got := c[0].aplayAygiti(); got != "plughw:1,0" {
		t.Fatalf("ilk aday analog çıkış olmalı, %s geldi", got)
	}
	if c[0].tur != turAnalog || c[1].tur != turDijital || c[2].tur != turHDMI {
		t.Fatalf("sıra analog > dijital > HDMI olmalı: %v %v %v", c[0].tur, c[1].tur, c[2].tur)
	}
	if got := c[0].Tanim(); got != "ALC892 Analog · analog" {
		t.Fatalf("tanım: %q", got)
	}
}

func TestUSBSesDahiliKartinOnunde(t *testing.T) {
	cards := ornekCards + ` 2 [Headset        ]: USB-Audio - USB Headset
                      Logitech USB Headset at usb-0000:00:14.0-2, full speed
`
	pcm := ornekPCM + "02-00: USB Audio : USB Audio : playback 1 : capture 1\n"
	c := siraliCikislar(pcm, cards)
	if c[0].kart != 2 || c[0].tur != turUSB {
		t.Fatalf("takılı USB kulaklık önde olmalı: %+v", c[0])
	}
}

// VirtualBox'ın AC'97'si (snd_intel8x0): tek analog çıkış, ADC'ler kayıt-yalnız.
func TestVirtualBoxAC97(t *testing.T) {
	cards := " 0 [I82801AAICH    ]: ICH - Intel 82801AA-ICH\n"
	pcm := "00-00: Intel ICH : Intel 82801AA-ICH : playback 1 : capture 1\n" +
		"00-01: Intel ICH - MIC ADC : Intel 82801AA-ICH - MIC ADC : capture 1\n"
	c := siraliCikislar(pcm, cards)
	if len(c) != 1 || c[0].aplayAygiti() != "plughw:0,0" || c[0].tur != turAnalog {
		t.Fatalf("AC'97 tek analog çıkış vermeli: %+v", c)
	}
}

func TestBosProcAdaySiz(t *testing.T) {
	if c := siraliCikislar("", ""); len(c) != 0 {
		t.Fatalf("boş girdi aday üretmemeli: %+v", c)
	}
}

func TestMikserPlaniFrontAciliyor(t *testing.T) {
	// VirtualBox HD Audio (STAC9221): hat çıkışı "Front".
	out := `Simple mixer control 'Master',0
Simple mixer control 'Headphone',0
Simple mixer control 'Front',0
Simple mixer control 'PCM',0
Simple mixer control 'Mic',0
Simple mixer control 'Headphone',1
`
	k := parseScontrols(out)
	if !reflect.DeepEqual(k, []string{"Master", "Headphone", "Front", "PCM", "Mic"}) {
		t.Fatalf("kontroller: %v", k)
	}
	ana, yan := mikserPlani(k)
	if ana != "Master" {
		t.Fatalf("ana Master olmalı, %q", ana)
	}
	want := []string{"Front", "Headphone", "PCM"}
	if !reflect.DeepEqual(yan, want) {
		t.Fatalf("yan kontroller %v olmalı, %v geldi (Mic ASLA açılmamalı)", want, yan)
	}
}

func TestMikserPlaniMasterYoksa(t *testing.T) {
	ana, _ := mikserPlani([]string{"PCM", "Mic"})
	if ana != "PCM" {
		t.Fatalf("Master yoksa PCM ana olmalı, %q", ana)
	}
}
