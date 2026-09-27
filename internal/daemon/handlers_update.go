package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"mcos/internal/files"
	"mcos/internal/ipc"
	"mcos/internal/version"
)

// ════════════════════════════════════════════════════════════════════════════
// SİSTEM GÜNCELLEMESİ: USB'deki yeni ISO ile, veri kaybı olmadan
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının isteği: "USB'yi takıp içinde yeni bir ISO bulunursa hiçbir
// veri kaybı olmadan sistemi güncelleyebileyim."
//
// Asıl iş rootfs-overlay/usr/bin/mcos-update'te: ISO'dan yalnızca açılış
// bölümündeki (p1) bzImage/initrd.img değişir. Yeni sistem bir sonraki
// açılışta /init tarafından kök bölümüne kopyalanır; /data hiç yazılmaz.
//
// Buradaki üç çağrı da HIZLI döner: panelin IPC zaman aşımı var ve ISO'yu
// USB'den okumak dakikalar sürebilir. updateScan uzun sürebilir (her bölüm
// ve her ISO bağlanır), bu yüzden istemci onu kendi bağlantısında çağırır
// (ipcclient.UpdateScan → callLong); update arka planda koşar, ilerleme
// updateStatus ile yoklanır.

// Testlerde değiştirilebilen yollar.
var (
	updateScript     = "mcos-update"
	updateStatusFile = "/run/mcos/update.status"
	buildIDFile      = "/etc/mcos-build-id"
	mountInfoFile    = "/proc/self/mountinfo"
)

// updateJob: süren/biten son güncellemenin bellekteki durumu. Daemon tek
// örnek olduğu için paket düzeyinde; aynı anda tek güncelleme olabilir
// (betik de kilit tutar).
var updateJob struct {
	mu       sync.Mutex
	running  bool
	finished bool
	out      string
	err      error
}

// readBuildID returns the running system's build id ("" = eski derleme).
func readBuildID() string {
	b, err := os.ReadFile(buildIDFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
}

// rootIsLive reports whether / is in RAM (canlı USB/ISO açılışı).
//
// mountinfo'nun 3. alanı major:minor'dur; rootfs/tmpfs'in major'ı 0. Kurulu
// sistemde kök gerçek bir blok aygıtıdır (ext4, p2). Dosya okunamazsa canlı
// DEĞİL sayılır: son sözü mcos-update söyler, kendi denetimi var.
func rootIsLive() bool {
	b, err := os.ReadFile(mountInfoFile)
	if err != nil {
		return false
	}
	mm := ""
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 5 && f[4] == "/" {
			mm = f[2] // üst üste bağlamada SONUNCUSU geçerli
		}
	}
	return mm == "" || strings.HasPrefix(mm, "0:")
}

// compareBuild classifies candidate against the running build.
//
// Kimlik "YYYYAAGG-SSDD-özet" biçimindedir; ilk 13 karakter sıralanabilir
// zaman damgasıdır. Çalışan sistemin kimliği yoksa (güncelleme sistemi
// gelmeden önceki derleme) her kimlikli ISO daha yenidir.
func compareBuild(current, candidate string) string {
	switch {
	case candidate == "":
		return ipc.UpdateInvalid
	case candidate == current:
		return ipc.UpdateSame
	case current == "":
		return ipc.UpdateNewer
	}
	ts := func(s string) string {
		if len(s) >= 13 {
			return s[:13]
		}
		return s
	}
	if ts(candidate) < ts(current) {
		return ipc.UpdateOlder
	}
	// Aynı dakikada iki farklı derleme: farklı olduğu kesin, yeni say.
	return ipc.UpdateNewer
}

// inspectISO runs "mcos-update --denetle" and returns the ISO's build/version.
func inspectISO(ctx context.Context, abs string) (id, ver string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, updateScript, "--denetle", abs)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		if v, ok := strings.CutPrefix(l, "KIMLIK="); ok {
			id = v
		} else if v, ok := strings.CutPrefix(l, "SURUM="); ok {
			ver = v
		}
	}
	if runErr != nil || id == "" {
		msg := ""
		for _, l := range strings.Split(stderr.String(), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(l), "HATA:"); ok {
				msg = strings.TrimSpace(v)
				break
			}
		}
		if msg == "" && runErr != nil {
			msg = "ISO okunamadı: " + runErr.Error()
		}
		if msg == "" {
			msg = "derleme kimliği yok"
		}
		return "", ver, errors.New(msg)
	}
	return id, ver, nil
}

// sortUpdateItems: önce güncellemeye uygun olanlar (yeni → aynı → eski),
// kendi içlerinde en yeni derleme üstte; geçersizler en altta.
func sortUpdateItems(items []ipc.UpdateISO) {
	rank := map[string]int{ipc.UpdateNewer: 0, ipc.UpdateSame: 1, ipc.UpdateOlder: 2, ipc.UpdateInvalid: 3}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if rank[a.Compare] != rank[b.Compare] {
			return rank[a.Compare] < rank[b.Compare]
		}
		if a.BuildID != b.BuildID {
			return a.BuildID > b.BuildID
		}
		return a.Name < b.Name
	})
}

func (d *Daemon) handleSystemUpdateScan(ctx context.Context, _ json.RawMessage) (any, error) {
	res := ipc.UpdateScanResult{
		Live:           rootIsLive(),
		CurrentBuild:   readBuildID(),
		CurrentVersion: version.Version,
		Items:          []ipc.UpdateISO{},
	}
	// Canlı sistemde güncellenecek disk yok: USB'yi boşuna taramayız.
	if res.Live {
		return res, nil
	}
	err := files.ScanUSBISOs(func(f files.USBISOFile) {
		it := ipc.UpdateISO{Device: f.Device, Path: f.RelPath, Name: path.Base(f.RelPath), SizeBytes: f.Size}
		id, ver, err := inspectISO(ctx, f.Abs)
		it.BuildID, it.Version = id, ver
		it.Compare = compareBuild(res.CurrentBuild, id)
		if err != nil {
			it.Compare, it.Error = ipc.UpdateInvalid, err.Error()
		}
		res.Items = append(res.Items, it)
	})
	if err != nil {
		return nil, err
	}
	sortUpdateItems(res.Items)
	return res, nil
}

func (d *Daemon) handleSystemUpdate(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.UpdateParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	p.Device, p.Path = strings.TrimSpace(p.Device), strings.TrimSpace(p.Path)
	if p.Device == "" || p.Path == "" {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "device ve path gerekli"}
	}
	if rootIsLive() {
		return nil, errors.New("Canlı sistemde güncelleme yok: yeni ISO ile açın ya da Ayarlar'dan diske kurun")
	}
	updateJob.mu.Lock()
	if updateJob.running {
		updateJob.mu.Unlock()
		return nil, errors.New("güncelleme zaten sürüyor")
	}
	updateJob.running, updateJob.finished, updateJob.out, updateJob.err = true, false, "", nil
	updateJob.mu.Unlock()
	// Önceki koşunun "TAMAM|..." satırı bu koşunun sonucu sanılmasın.
	_ = os.Remove(updateStatusFile)

	d.log.Infof("update: %s:%s ile güncelleme başladı", p.Device, p.Path)
	go func() {
		var out []byte
		err := files.WithUSBFile(p.Device, p.Path, func(abs string) error {
			var e error
			out, e = exec.Command(updateScript, "--iso", abs).CombinedOutput()
			return e
		})
		updateJob.mu.Lock()
		updateJob.running, updateJob.finished = false, true
		updateJob.out, updateJob.err = string(out), err
		updateJob.mu.Unlock()
		if err != nil {
			d.log.Warnf("update: başarısız: %v: %s", err, strings.TrimSpace(string(out)))
			return
		}
		d.log.Infof("update: hazır, yeniden başlatınca kurulacak")
	}()
	return ipc.OKResult{OK: true, Message: "güncelleme başladı"}, nil
}

func (d *Daemon) handleSystemUpdateStatus(_ context.Context, _ json.RawMessage) (any, error) {
	updateJob.mu.Lock()
	running, finished, out, runErr := updateJob.running, updateJob.finished, updateJob.out, updateJob.err
	updateJob.mu.Unlock()

	st := readUpdateStatus()
	st.Running = running
	if running {
		return st, nil
	}
	switch {
	case finished && runErr != nil:
		st.Failed, st.Done = true, false
		st.Lines = updateFailureLines(out, runErr)
		st.Message = strings.TrimPrefix(st.Lines[0], "Hata: ")
	case finished:
		st.Done, st.Failed, st.Percent = true, false, 100
		if st.Message == "" {
			st.Message = lastNonEmptyLine(out)
		}
	case st.Failed:
		// Daemon yeniden başlamış, betik hata yazmış: satırı ilet.
		st.Lines = []string{"Hata: " + st.Message}
	}
	return st, nil
}

// readUpdateStatus parses the "yüzde|mesaj" / "TAMAM|mesaj" / "HATA|mesaj"
// line mcos-update writes.
func readUpdateStatus() ipc.UpdateStatusResult {
	var st ipc.UpdateStatusResult
	b, err := os.ReadFile(updateStatusFile)
	if err != nil {
		return st
	}
	return parseUpdateStatus(string(b))
}

func parseUpdateStatus(s string) ipc.UpdateStatusResult {
	var st ipc.UpdateStatusResult
	head, msg, ok := strings.Cut(strings.TrimSpace(s), "|")
	if !ok {
		return st
	}
	st.Message = strings.TrimSpace(msg)
	switch head {
	case "TAMAM":
		st.Done, st.Percent = true, 100
	case "HATA":
		st.Failed = true
	default:
		st.Percent, _ = strconv.Atoi(head)
	}
	return st
}

// updateFailureLines turns the script's HATA/NEDEN/ÇÖZÜM/GÜNLÜK lines into
// what the user sees. "exit status 1" TEK BAŞINA gösterilmez (mcos-install'da
// kullanıcı ondan hiçbir şey anlamıyordu).
func updateFailureLines(out string, err error) []string {
	etiket := []struct{ onek, baslik string }{
		{"HATA:", "Hata: "}, {"NEDEN:", "Neden: "},
		{"ÇÖZÜM:", "Ne yapmalı: "}, {"GÜNLÜK:", "Tam günlük: "},
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		for _, e := range etiket {
			if v, ok := strings.CutPrefix(l, e.onek); ok {
				if v = strings.TrimSpace(v); v != "" {
					lines = append(lines, e.baslik+v)
				}
				break
			}
		}
	}
	if len(lines) > 0 {
		return lines
	}
	if l := lastNonEmptyLine(out); l != "" {
		return []string{"Hata: " + strings.TrimPrefix(l, ">> mcos-update: ")}
	}
	if err != nil {
		return []string{"Hata: güncelleme çalıştırılamadı: " + err.Error()}
	}
	return []string{"Hata: güncelleme başarısız"}
}

func lastNonEmptyLine(s string) string {
	ls := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(ls) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(ls[i]); l != "" {
			return l
		}
	}
	return ""
}
