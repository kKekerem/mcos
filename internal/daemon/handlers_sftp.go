package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mcos/internal/files"
	"mcos/internal/ipc"
	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// WinSCP / SFTP: SUNUCULAR KLASÖRÜ
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının isteği: "WinSCP ile bağlanınca bizi sunucular klasörünü
// göstersin sadece; oraya sunucu klasörü atıp SSH ile restart felan
// atabilelim."
//
// Sunucular diskte KİMLİKLE durur (/data/servers/3f9c…/data); WinSCP'de bu
// anlamsız bir liste olurdu. Bu yüzden /data/sunucular altında her sunucu
// KENDİ ADIYLA görünür: "Survival" -> /data/servers/<id>/data bağı. sshd'nin
// SFTP alt sistemi bu klasörde açılır (bkz. sshd.SFTPStartDir).
//
// Klasöre bağ olmayan GERÇEK bir klasör atılırsa bu bir yüklemedir: dosyalar
// bir süre değişmeyince (yükleme bitti) ve klasör bir sunucuya benziyorsa
// (server.properties, dünya, jar...) USB aktarımıyla AYNI yoldan sunucu
// olarak eklenir. Hemen eklemek için SSH'ta "ekle <klasör>".

// SFTPDirName, veri kökü altındaki sunucular klasörüdür.
const SFTPDirName = "sunucular"

// sftpScanEvery: bir yüklemenin "bitti" sayılması için dosyaların değişmeden
// kalması gereken süre. WinSCP büyük bir dünyayı dakikalarca yükler; yarım
// kopyayı içe aktarmak bozuk bir dünya demekti. İki tarama arası 10 sn,
// iki tur aynı imza = en az 20 sn sessizlik.
const sftpScanEvery = 10 * time.Second

func (d *Daemon) sftpRoot() string { return filepath.Join(d.store.Paths.Root, SFTPDirName) }

// sftpLinkName turns a server name into a safe folder name.
func sftpLinkName(name string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-",
		"\"", "-", "<", "-", ">", "-", "|", "-", "\x00", "")
	n := strings.TrimSpace(r.Replace(name))
	n = strings.Trim(n, ". ")
	if n == "" {
		n = "sunucu"
	}
	return n
}

// syncSFTPLinks keeps one link per server in the SFTP folder.
//
// Gerçek klasörlere (yüklemeler) ASLA dokunmaz; yalnızca kendi bağlarını
// kurar, günceller ve sunucusu silinmiş bağları kaldırır.
func (d *Daemon) syncSFTPLinks() error {
	root := d.sftpRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	list, err := d.store.ListServers()
	if err != nil {
		return err
	}
	want := map[string]string{} // bağ adı -> hedef
	for _, s := range list {
		if s.IsSibling() {
			continue // aynı makinedeki kopyalar menüde de görünmüyor
		}
		name := sftpLinkName(s.Name)
		if _, dup := want[name]; dup {
			name = name + " (" + s.ID[:min(6, len(s.ID))] + ")"
		}
		want[name] = d.serverDataDir(s)
	}
	ents, _ := os.ReadDir(root)
	for _, e := range ents {
		p := filepath.Join(root, e.Name())
		if e.Type()&fs.ModeSymlink == 0 {
			continue // gerçek klasör: yükleme, dokunma
		}
		if target, ok := want[e.Name()]; ok {
			if cur, _ := os.Readlink(p); cur == target {
				delete(want, e.Name())
				continue
			}
		}
		_ = os.Remove(p) // eski ya da yanlış hedefli bağ
	}
	for name, target := range want {
		p := filepath.Join(root, name)
		if _, err := os.Lstat(p); err == nil {
			continue // aynı adda bir yükleme var; ona dokunma
		}
		_ = os.MkdirAll(target, 0o755)
		if err := os.Symlink(target, p); err != nil {
			d.log.Warnf("sftp: %s bağı kurulamadı: %v", name, err)
		}
	}
	return nil
}

// uploadSig summarises a folder so an in-progress upload can be detected.
type uploadSig struct {
	files  int
	size   int64
	newest time.Time
}

func dirSignature(dir string) uploadSig {
	var s uploadSig
	_ = filepath.WalkDir(dir, func(_ string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return nil
		}
		if fi, err := e.Info(); err == nil {
			s.files++
			s.size += fi.Size()
			if fi.ModTime().After(s.newest) {
				s.newest = fi.ModTime()
			}
		}
		return nil
	})
	return s
}

// sftpState remembers upload signatures between scans.
type sftpState struct {
	mu   sync.Mutex
	sigs map[string]uploadSig
	// failed: tanınmayan klasörler; her taramada günlüğü doldurmasın.
	failed map[string]uploadSig
}

// sftpLoop keeps links in sync and imports finished uploads.
func (d *Daemon) sftpLoop(ctx context.Context) {
	st := &sftpState{sigs: map[string]uploadSig{}, failed: map[string]uploadSig{}}
	tk := time.NewTicker(sftpScanEvery)
	defer tk.Stop()
	for {
		if err := d.syncSFTPLinks(); err != nil {
			d.log.Warnf("sftp: %v", err)
		}
		d.scanUploads(st)
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
		}
	}
}

func (d *Daemon) scanUploads(st *sftpState) {
	ents, err := os.ReadDir(d.sftpRoot())
	if err != nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	seen := map[string]bool{}
	for _, e := range ents {
		if !e.IsDir() || e.Type()&fs.ModeSymlink != 0 || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		name := e.Name()
		seen[name] = true
		dir := filepath.Join(d.sftpRoot(), name)
		sig := dirSignature(dir)
		prev, had := st.sigs[name]
		st.sigs[name] = sig
		if !had || prev != sig || sig.files == 0 {
			continue // yükleme sürüyor (ya da yeni başladı)
		}
		if f, bad := st.failed[name]; bad && f == sig {
			continue // aynı içerik zaten reddedildi
		}
		srv, err := d.importUploaded(dir, name)
		if err != nil {
			st.failed[name] = sig
			d.log.Warnf("sftp: %q içe aktarılmadı: %v", name, err)
			continue
		}
		delete(st.sigs, name)
		d.log.Infof("sftp: %q klasörü sunucu olarak eklendi (%s %s)", name, srv.Software, srv.MCVersion)
	}
	for name := range st.sigs {
		if !seen[name] {
			delete(st.sigs, name)
			delete(st.failed, name)
		}
	}
}

// importUploaded turns an uploaded folder into a server (USB aktarımıyla aynı).
func (d *Daemon) importUploaded(dir, name string) (*model.Server, error) {
	info := files.DetectServerDir(dir)
	if info.Score == 0 {
		return nil, fmt.Errorf("klasör bir Minecraft sunucusuna benzemiyor " +
			"(server.properties, dünya klasörü ya da sunucu jar'ı yok)")
	}
	params, err := d.importParams(ipc.ImportUSBParams{Name: name, RelPath: name}, info)
	if err != nil {
		return nil, err
	}
	// Klasör zaten /data üzerinde: createServer onu veri klasörüne TAŞIR
	// (kopya yok, yarım disk alanı yok).
	params.ImportFrom = dir
	res, err := d.createServer(params)
	if err != nil {
		return nil, err
	}
	sr, _ := res.(ipc.ServerResult)
	if sr.Server == nil {
		return nil, fmt.Errorf("sunucu oluşturuldu ama kaydı okunamadı")
	}
	_ = d.syncSFTPLinks() // aynı adla bağ hemen görünsün
	return sr.Server, nil
}

// ImportFolderParams: SSH'taki "ekle <klasör>" komutu.
type importFolderParams struct {
	Name string `json:"name"`
}

// handleServerImportFolder imports /data/sunucular/<name> right away.
func (d *Daemon) handleServerImportFolder(_ context.Context, raw json.RawMessage) (any, error) {
	var p importFolderParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(p.Name)
	if name == "" || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "klasör adı geçersiz"}
	}
	dir := filepath.Join(d.sftpRoot(), name)
	fi, err := os.Lstat(dir)
	switch {
	case err != nil:
		return nil, &ipc.Error{Code: ipc.CodeNotFound,
			Message: fmt.Sprintf("%s altında %q klasörü yok", d.sftpRoot(), name)}
	case fi.Mode()&fs.ModeSymlink != 0:
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams,
			Message: fmt.Sprintf("%q zaten bir sunucu", name)}
	case !fi.IsDir():
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: fmt.Sprintf("%q bir klasör değil", name)}
	}
	srv, err := d.importUploaded(dir, name)
	if err != nil {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: err.Error()}
	}
	return ipc.ServerResult{Server: srv}, nil
}
