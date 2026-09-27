package cluster

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"mcos/internal/model"
)

// Bu dosya ORTAK DÜNYA MOD/EKLENTİ EŞİTLEMESİNİ uygular.
//
// ── Neden ───────────────────────────────────────────────────────────────────
// Kullanıcının isteği: "eşleşince ona sunucu kurulacak, bölge belirlenecek,
// mod kurulacak, senkron olacak, başlayacak". Eskiden düğüme yalnızca
// mcos-link gidiyordu. Kurucuya kurulan her mod/eklenti düğümde YOKTU:
//
//   - Fabric'te modlu bir blok/eşya sınırı geçince "bilinmeyen kayıt" olur
//     ve SİLİNİR; modlu istemci, modsuz yarıya aktarılınca bağlantısı kopar.
//   - Paper'da koruma/ekonomi eklentileri dünyanın yalnızca yarısında çalışır.
//
// ── Nasıl ───────────────────────────────────────────────────────────────────
// Kurucu, LinkSpec.Files içinde her jar'ın adını, boyunu ve SHA-256'sını
// gönderir. Düğüm eksik ya da farklı olanları kurucudan "linkFile" isteğiyle
// ÇEKER (anahtarla, eşleştirme portu üzerinden) ve özeti doğrulamadan
// yerine koymaz. Kurucu yalnızca LİSTEDEKİ dosyaları verir: istek bir yol
// değil, bir (klasör, ad, özet) üçlüsüdür; listede yoksa ret.
//
// Silme de eşitlenir ama YALNIZCA bizim getirdiğimiz dosyalar için: düğümde
// kullanıcının kendi koyduğu bir jar'a dokunulmaz (.mcos-link-files.json).

// linkFilesManifest records which jars we synced, so removals stay scoped.
const linkFilesManifest = ".mcos-link-files.json"

// linkFileTimeout bounds one jar transfer. Büyük mod paketleri 100 MB'ı
// bulabilir; yavaş bir Wi-Fi'de bile 10 dakika fazlasıyla yeter.
const linkFileTimeout = 10 * time.Minute

// maxLinkFileBytes: tek bir jar için üst sınır. Bozuk ya da kötü niyetli bir
// "boy" alanı diski doldurmasın.
const maxLinkFileBytes = 512 << 20

// linkFileDirs are the only folders synced.
var linkFileDirs = []string{"mods", "plugins"}

// skipLinkFile reports jars every node installs by itself.
//
// mcos-link: her düğüm kendi kopyasını programın yanından kurar (aynı dosya).
// fabric-api: düğüm onu kendi Minecraft sürümü için kurar; kurucunun
// kopyasını da getirmek mods/ içinde İKİ fabric-api bırakırdı ve Fabric
// Loader aynı kimlikli iki modda sunucuyu HİÇ AÇMAZ.
func skipLinkFile(name string) bool {
	n := strings.ToLower(name)
	switch n {
	case "mcos-link.jar", "mcos-link-paper.jar":
		return true
	}
	return strings.HasPrefix(n, "fabric-api") || strings.Contains(n, "-fabric-api-") ||
		strings.HasPrefix(n, "fabric_api")
}

// validLinkFile rejects anything that could escape the server folder.
func validLinkFile(f model.LinkFile) error {
	okDir := false
	for _, d := range linkFileDirs {
		if f.Dir == d {
			okDir = true
		}
	}
	if !okDir {
		return fmt.Errorf("geçersiz klasör %q", f.Dir)
	}
	if f.Name == "" || f.Name != filepath.Base(f.Name) || strings.ContainsAny(f.Name, `/\`) ||
		strings.HasPrefix(f.Name, ".") || !strings.EqualFold(filepath.Ext(f.Name), ".jar") {
		return fmt.Errorf("geçersiz dosya adı %q", f.Name)
	}
	if f.Size < 0 || f.Size > maxLinkFileBytes {
		return fmt.Errorf("geçersiz boy %d", f.Size)
	}
	if len(f.SHA256) != 64 {
		return fmt.Errorf("geçersiz özet")
	}
	return nil
}

// ── Kurucu tarafı: listeyi çıkar ────────────────────────────────────────────

type hashEntry struct {
	size  int64
	mtime time.Time
	sum   string
}

// hashCache: yol → (boy, değişiklik zamanı, özet).
//
// NEDEN: LinkSpec, modun her topoloji yoklamasında (birkaç saniyede bir)
// hesaplanıyor. Her seferinde 100 MB'lık mod klasörünü baştan özetlemek
// SD kartlı bir kutuda CPU'yu boşuna yakardı. Boy ya da zaman değişmediyse
// özet de değişmemiştir.
var (
	hashMu    sync.Mutex
	hashCache = map[string]hashEntry{}
)

func fileSHA256(path string, st os.FileInfo) (string, error) {
	hashMu.Lock()
	if e, ok := hashCache[path]; ok && e.size == st.Size() && e.mtime.Equal(st.ModTime()) {
		hashMu.Unlock()
		return e.sum, nil
	}
	hashMu.Unlock()
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	hashMu.Lock()
	hashCache[path] = hashEntry{size: st.Size(), mtime: st.ModTime(), sum: sum}
	hashMu.Unlock()
	return sum, nil
}

// ScanLinkFiles lists the mod/plugin jars a shared world must share.
//
// Sıra SABİTTİR (klasör, ad): liste kurulum özetine giriyor; oynak bir sıra
// kurulumu durmadan yeniden göndertirdi.
func ScanLinkFiles(dataDir string) []model.LinkFile {
	var out []model.LinkFile
	for _, dir := range linkFileDirs {
		entries, err := os.ReadDir(filepath.Join(dataDir, dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.Type().IsRegular() || skipLinkFile(e.Name()) ||
				!strings.EqualFold(filepath.Ext(e.Name()), ".jar") ||
				strings.HasPrefix(e.Name(), ".") {
				continue
			}
			path := filepath.Join(dataDir, dir, e.Name())
			st, err := os.Stat(path)
			if err != nil || st.Size() == 0 || st.Size() > maxLinkFileBytes {
				continue
			}
			sum, err := fileSHA256(path, st)
			if err != nil {
				continue
			}
			out = append(out, model.LinkFile{Dir: dir, Name: e.Name(),
				Size: st.Size(), SHA256: sum})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir < out[j].Dir
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// LinkFileHost is optionally implemented by a LinkHost that can serve jars.
//
// İsteğe bağlı bir arayüz: LinkHost'a yöntem eklemek, onu uygulayan her
// sınama sahtesini kırardı; dosya sunmayan bir düğüm yine çalışır.
type LinkFileHost interface {
	// LinkDataDir returns the shared-world server's data folder.
	LinkDataDir() (string, bool)
}

// serveLinkFile answers a "linkFile" request: JSON header line, then bytes.
func (c *LinkCoordinator) serveLinkFile(conn net.Conn, enc *json.Encoder, f *model.LinkFile) {
	fail := func(msg string) { _ = enc.Encode(map[string]any{"ok": false, "error": msg}) }
	if f == nil {
		fail("dosya belirtilmedi")
		return
	}
	if err := validLinkFile(*f); err != nil {
		fail(err.Error())
		return
	}
	fh, ok := c.host.(LinkFileHost)
	if !ok {
		fail("bu düğüm dosya sunmuyor")
		return
	}
	dir, ok := fh.LinkDataDir()
	if !ok {
		fail("ortak dünya kapalı")
		return
	}
	// Yalnızca GÜNCEL listedeki dosya verilir; istek bir yol değildir.
	listed := false
	for _, have := range ScanLinkFiles(dir) {
		if have.Dir == f.Dir && have.Name == f.Name && have.SHA256 == f.SHA256 {
			listed = true
			break
		}
	}
	if !listed {
		fail("dosya ortak dünya listesinde yok (kurucuda değişmiş olabilir)")
		return
	}
	file, err := os.Open(filepath.Join(dir, f.Dir, f.Name))
	if err != nil {
		fail("dosya açılamadı")
		return
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil {
		fail("dosya okunamadı")
		return
	}
	// Aktarım uzun sürebilir: bağlantının genel süre sınırını uzat.
	_ = conn.SetDeadline(time.Now().Add(linkFileTimeout))
	if err := enc.Encode(map[string]any{"ok": true, "size": st.Size()}); err != nil {
		return
	}
	_, _ = io.CopyN(conn, file, st.Size())
}

// ── Alıcı tarafı: eksikleri çek ─────────────────────────────────────────────

func readManifest(dataDir string) []model.LinkFile {
	b, err := os.ReadFile(filepath.Join(dataDir, linkFilesManifest))
	if err != nil {
		return nil
	}
	var out []model.LinkFile
	_ = json.Unmarshal(b, &out)
	return out
}

func writeManifest(dataDir string, files []model.LinkFile) error {
	b, _ := json.MarshalIndent(files, "", "  ")
	tmp := filepath.Join(dataDir, linkFilesManifest+".tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dataDir, linkFilesManifest))
}

// localMatches reports whether dataDir already holds exactly this jar.
func localMatches(dataDir string, f model.LinkFile) bool {
	path := filepath.Join(dataDir, f.Dir, f.Name)
	st, err := os.Stat(path)
	if err != nil || st.Size() != f.Size {
		return false
	}
	sum, err := fileSHA256(path, st)
	return err == nil && sum == f.SHA256
}

// LinkFilesPending reports whether applying files would change dataDir.
//
// Çalışan bir sunucuyu yeniden başlatıp başlatmamaya bununla karar verilir:
// mod eklenip çıkarılmadıysa oyuncular boşuna atılmamalı.
func LinkFilesPending(dataDir string, files []model.LinkFile) bool {
	want := map[string]bool{}
	for _, f := range files {
		if validLinkFile(f) != nil {
			continue
		}
		want[f.Dir+"/"+f.Name] = true
		if !localMatches(dataDir, f) {
			return true
		}
	}
	for _, old := range readManifest(dataDir) {
		if !want[old.Dir+"/"+old.Name] && localMatches(dataDir, old) {
			return true // kurucudan kalkmış, bizde duruyor
		}
	}
	return false
}

// originAddr is where the last shared-world setup came from.
func (c *LinkCoordinator) originAddr() (string, error) {
	c.mu.RLock()
	key, ip := c.lastOrigin, c.lastOriginIP
	c.mu.RUnlock()
	for _, p := range c.mgr.Peers() {
		if key != "" && p.ID == key && p.IP != "" {
			port := p.Port
			if port == 0 {
				port = model.PairingPort
			}
			return net.JoinHostPort(p.IP, strconv.Itoa(port)), nil
		}
	}
	if ip != "" {
		return net.JoinHostPort(ip, strconv.Itoa(model.PairingPort)), nil
	}
	return "", errors.New("kurucu bilinmiyor — ortak dünya kurulumu henüz gelmedi")
}

// SyncLinkFiles brings dataDir's mods/plugins in line with the origin's list.
//
// Dönüş: bir şey değişti mi (çağıran buna göre yeniden başlatır). Bir dosya
// indirilemezse diğerleri yine denenir ve hata TOPLANIR: tek bir eksik mod
// yüzünden bütün eşitlemeyi bırakmak, düğümü daha da farklı bırakırdı.
func (c *LinkCoordinator) SyncLinkFiles(ctx context.Context, dataDir string,
	files []model.LinkFile) (bool, error) {

	c.fileMu.Lock()
	defer c.fileMu.Unlock()

	var (
		changed bool
		errs    []string
		kept    []model.LinkFile
	)
	want := map[string]bool{}
	for _, f := range files {
		if err := validLinkFile(f); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		want[f.Dir+"/"+f.Name] = true
		if localMatches(dataDir, f) {
			kept = append(kept, f)
			continue
		}
		if err := c.fetchLinkFile(ctx, dataDir, f); err != nil {
			errs = append(errs, f.Name+": "+err.Error())
			continue
		}
		changed = true
		kept = append(kept, f)
		if c.mgr.log != nil {
			c.mgr.log.Infof("link: %s/%s kurucudan alındı (%d KB)", f.Dir, f.Name, f.Size/1024)
		}
	}
	// Kurucudan KALKAN dosyaları sil — yalnızca bizim getirdiklerimizi ve
	// yalnızca hâlâ getirdiğimiz hâldeyse (kullanıcı değiştirdiyse dokunma).
	for _, old := range readManifest(dataDir) {
		if want[old.Dir+"/"+old.Name] || validLinkFile(old) != nil {
			continue
		}
		if localMatches(dataDir, old) {
			if err := os.Remove(filepath.Join(dataDir, old.Dir, old.Name)); err == nil {
				changed = true
				if c.mgr.log != nil {
					c.mgr.log.Infof("link: %s/%s kurucuda kaldırıldığı için silindi", old.Dir, old.Name)
				}
			}
		}
	}
	if err := writeManifest(dataDir, kept); err != nil {
		errs = append(errs, "eşitleme kaydı yazılamadı: "+err.Error())
	}
	if len(errs) > 0 {
		return changed, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return changed, nil
}

// fetchLinkFile downloads one jar from the origin and verifies it.
func (c *LinkCoordinator) fetchLinkFile(ctx context.Context, dataDir string, f model.LinkFile) error {
	addr, err := c.originAddr()
	if err != nil {
		return err
	}
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		_, port, _ := net.SplitHostPort(addr)
		p, _ := strconv.Atoi(port)
		return fmt.Errorf("%s", describeNetErr(err, p))
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(linkFileTimeout))
	fc := f
	if err := json.NewEncoder(conn).Encode(peerRequest{
		Method: "linkFile", Token: c.mgr.Secret(), From: c.mgr.selfHello(), File: &fc,
	}); err != nil {
		return err
	}
	br := bufio.NewReader(conn)
	line, err := br.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("kurucu yanıt vermedi: %w", err)
	}
	var hdr struct {
		OK     bool   `json:"ok"`
		Size   int64  `json:"size"`
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(line, &hdr); err != nil {
		return fmt.Errorf("kurucunun yanıtı çözülemedi")
	}
	if !hdr.OK {
		return fmt.Errorf("%s", rejectProblem(hdr.Reason, hdr.Error))
	}
	if hdr.Size != f.Size {
		return fmt.Errorf("boy uyuşmuyor (%d ≠ %d)", hdr.Size, f.Size)
	}

	dir := filepath.Join(dataDir, f.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Geçici dosyanın adı ".jar" ile BİTMEZ: yarım bir dosyayı sunucu
	// yükleyicisi mod sanıp açılışta çökerdi.
	tmp := filepath.Join(dir, "."+f.Name+".mcos-tmp")
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.CopyN(io.MultiWriter(out, h), br, f.Size)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("aktarım yarıda kaldı: %w", err)
	}
	// ÖZET DOĞRULANMADAN yerine konmaz: yolda bozulmuş bir jar, sunucunun
	// açılışta anlaşılmaz bir hatayla çökmesi demektir.
	if sum := hex.EncodeToString(h.Sum(nil)); sum != f.SHA256 {
		os.Remove(tmp)
		return fmt.Errorf("özet uyuşmuyor — dosya yolda bozuldu")
	}
	return os.Rename(tmp, filepath.Join(dir, f.Name))
}
