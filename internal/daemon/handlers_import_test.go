package daemon

import (
	"testing"

	"mcos/internal/files"
	"mcos/internal/ipc"
	"mcos/internal/model"
)

// Aktarılan sunucu klasördeki AYARLARLA açılmalı (port, oyuncu sınırı,
// zorluk, kip); çakışan ad/port güvenle değiştirilmeli.
func TestAktarmaAyarlariKlasordenGelir(t *testing.T) {
	d := newRemoteTestDaemon(t)
	info := files.ServerDirInfo{Software: "paper", MCVersion: "1.21.1", HasWorld: true,
		Props: map[string]string{"server-port": "25570", "max-players": "50", "difficulty": "hard",
			"gamemode": "creative", "online-mode": "false", "motd": "Eski sunucum", "view-distance": "12"}}
	p, err := d.importParams(ipc.ImportUSBParams{Device: "/dev/sdb1", RelPath: "Sunucular/Hayatta"}, info)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Hayatta" || p.Software != model.SoftwarePaper || p.MCVersion != "1.21.1" ||
		p.Port != 25570 || p.MaxPlayers != 50 || p.Difficulty != "hard" || p.Gamemode != "creative" ||
		p.OnlineMode == nil || *p.OnlineMode || p.MOTD != "Eski sunucum" || p.ViewDistance != 12 {
		t.Fatalf("ayarlar aktarılmadı: %+v", p)
	}
}

func TestAktarmaCakisanAdVePort(t *testing.T) {
	d := newRemoteTestDaemon(t)
	if err := d.store.SaveServer(&model.Server{ID: "srv_eski", Name: "Hayatta", Port: 25570,
		Software: model.SoftwarePaper, MCVersion: "1.21.1"}); err != nil {
		t.Fatal(err)
	}
	info := files.ServerDirInfo{Software: "paper", MCVersion: "1.21.1", Props: map[string]string{"server-port": "25570"}}
	p, err := d.importParams(ipc.ImportUSBParams{RelPath: "Hayatta"}, info)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Hayatta (2)" {
		t.Fatalf("aynı adlı sunucu varken ad %q", p.Name)
	}
	if p.Port != 0 {
		t.Fatalf("başka sunucunun portu (%d) verildi; boş port seçilmeliydi", p.Port)
	}
}

// Sürüm anlaşılamıyorsa TAHMİN YAPILMAZ: dünyayı yanlış (eski) sürümle
// açmak onu bozar.
func TestSurumYoksaAktarmaReddedilir(t *testing.T) {
	d := newRemoteTestDaemon(t)
	if _, err := d.importParams(ipc.ImportUSBParams{RelPath: "x"}, files.ServerDirInfo{Software: "paper"}); err == nil {
		t.Fatal("sürümsüz klasör aktarıldı")
	}
}
