package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mcos/internal/files"
	"mcos/internal/ipc"
	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// USB'DEN SUNUCU AKTARMA
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı: "Sunucu klasörü aktarma ekle: flaşı seçip bir klasör seçip onu
// aktarabilelim; içindeki sunucu, dünya, her şey, modlar." Akış:
//  1. Klasör /data üzerinde geçici bir yere kopyalanır (.aktar-<rastgele>).
//  2. Klasördeki server.properties modele okunur (port, oyuncu sınırı,
//     zorluk, oyun kipi...) — aktarılan sunucu AYNI ayarlarla açılır.
//  3. Sunucu normal oluşturma yoluyla kaydedilir; kopya, yazılım kurulumu
//     başlamadan veri klasörüne taşınır (createServer, ImportFrom). Kurulum
//     yalnızca yazılımın jar'ını indirir; dünya/mod/eklentiye dokunmaz.

func (d *Daemon) handleServerScanUSBFolders(_ context.Context, _ json.RawMessage) (any, error) {
	items, err := files.ScanUSBServerFolders()
	if err != nil {
		return nil, &ipc.Error{Code: ipc.CodeUnavailable, Message: err.Error()}
	}
	return ipc.USBFoldersResult{Items: items}, nil
}

func (d *Daemon) handleServerImportUSB(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.ImportUSBParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Device) == "" {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "USB aygıtı seçilmedi"}
	}
	var rb [6]byte
	_, _ = rand.Read(rb[:])
	// /data kökünde: sunucu klasörüyle AYNI dosya sisteminde olmalı ki
	// yerleştirme bir yeniden adlandırma olsun (bkz. createServer).
	staging := filepath.Join(d.store.Paths.Root, ".aktar-"+hex.EncodeToString(rb[:]))
	info, err := files.CopyUSBFolder(p.Device, p.RelPath, staging)
	if err != nil {
		_ = os.RemoveAll(staging)
		return nil, &ipc.Error{Code: ipc.CodeInternalError,
			Message: "klasör kopyalanamadı: " + err.Error() + " (USB'de yer/dosya hatası olabilir; /data'da yeterli boş alan olmalı)"}
	}

	params, err := d.importParams(p, info)
	if err != nil {
		_ = os.RemoveAll(staging)
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	params.ImportFrom = staging
	res, err := d.createServer(params)
	if err != nil {
		_ = os.RemoveAll(staging) // taşınamadıysa hazırlık kopyası kalmasın
		return nil, err
	}
	d.log.Infof("import: %s:%s -> %q (%s %s, dünya %v, %d mod, %d eklenti)",
		p.Device, p.RelPath, params.Name, params.Software, params.MCVersion, info.HasWorld, info.Mods, info.Plugins)
	return res, nil
}

// importParams builds creation parameters from the folder's own settings.
func (d *Daemon) importParams(p ipc.ImportUSBParams, info files.ServerDirInfo) (ipc.ServerCreateParams, error) {
	sw := model.Software(info.Software)
	if sw == "" {
		sw = model.SoftwareVanilla
	}
	if info.MCVersion == "" {
		return ipc.ServerCreateParams{}, fmt.Errorf("klasördeki Minecraft sürümü anlaşılamadı " +
			"(sunucu jar'ı ya da dünyanın level.dat dosyası yok) — sunucuyu elle oluşturup dosyaları " +
			"Dosyalar sekmesinden kopyalayın")
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = filepath.Base(filepath.FromSlash(p.RelPath))
		if name == "." || name == "" || name == string(filepath.Separator) {
			name = "Aktarılan sunucu"
		}
	}
	existing, _ := d.store.ListServers()
	taken := map[string]bool{}
	usedPorts := map[int]bool{}
	for _, s := range existing {
		taken[strings.ToLower(s.Name)] = true
		usedPorts[s.Port] = true
	}
	base := name
	for i := 2; taken[strings.ToLower(name)]; i++ {
		name = fmt.Sprintf("%s (%d)", base, i)
	}
	port := info.PropInt("server-port")
	if port <= 0 || usedPorts[port] {
		port = 0 // başka bir sunucu kullanıyor: boş port seçilsin
	}
	ram := p.RAMMB
	if ram <= 0 {
		ram = 2048
	}
	on, pvp := info.PropBool("online-mode", true), info.PropBool("pvp", true)
	return ipc.ServerCreateParams{
		Name: name, Software: sw, MCVersion: info.MCVersion, RAMMB: ram, Port: port,
		MaxPlayers: info.PropInt("max-players"), MOTD: info.Props["motd"],
		Gamemode: info.Props["gamemode"], Difficulty: info.Props["difficulty"],
		ViewDistance: info.PropInt("view-distance"), SimDistance: info.PropInt("simulation-distance"),
		OnlineMode: &on, PVP: &pvp,
		Hardcore:   info.PropBool("hardcore", false),
		Whitelist:  info.PropBool("white-list", false),
		AutoBackup: true,
	}, nil
}
