package fbpanel

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"mcos/internal/fbui"
	"mcos/internal/ipc"
)

// ════════════════════════════════════════════════════════════════════════════
// DİSKE KURULUM: mcos-install'ı çalıştırma, ilerleme ve GERÇEK hata metni
// ════════════════════════════════════════════════════════════════════════════
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
//
// Kurulum başarısız olunca kullanıcının gördüğü tek şey şuydu:
//
//	kurulum başarısız: exit status 1
//
// Betiğin söylediği asıl sebep (ör. "bölüm tablosu oluşturulamadı" ve
// parted'ın "unable to inform the kernel" satırı) installMsg'ye yazılıyor,
// ama başarısızlık hâlinde sayfa installMsg'yi HİÇ çizmiyordu (setup_draw.go,
// drawInstall: yalnızca installing/installDone dallarında çiziliyordu).
// Kullanıcı üç kez "exit 1" bildirdi ve her seferinde sebebi göremedi.
//
// Artık betik başarısızlıkta HATA / NEDEN / ÇÖZÜM / GÜNLÜK satırları basıyor
// (mcos-install, fail()); panel bunları ayıklayıp sayfada kalıcı gösteriyor.

// installJobID: diske kurulumun arka plan işi (bkz. jobs.go).
//
// Sihirbaz ve Ayarlar > "Diske / USB'ye kur" AYNI kimliği kullanır. İki
// yoldan aynı anda mcos-install başlatılamaz: betikte kilit dosyası yok ve
// iki koşu aynı /tmp/mcos-install.status dosyasına yazıp birbirinin bağlama
// noktalarını söküyordu (bkz. setup.go, runDiskInstall).
const installJobID = "kurulum"

// startDiskInstall runs mcos-install on d as a background job.
//
// Ekrandan bağımsızdır: kullanıcı başka bir bölüme geçse de iş sürer, alt
// çubukta ilerleme ve geçen süre görünür (iş başlığı ilerlemeyle güncellenir).
//
// progress ve done (ikisi de nil olabilir) işi başlatan ekranın kendi
// durumunu yazması içindir. done, sonucu o ekranda GÖSTERDİYSE true döner;
// göstermediyse (Ayarlar yolu, ya da sihirbaz bu arada kapandıysa) sonuç ve
// betiğin GERÇEK hata satırları bir pencerede açılır — alt çubuktaki tek
// satırlık "kurulum başarısız" kullanıcıya hiçbir şey söylemiyordu.
func (a *App) startDiskInstall(d ipc.DiskTarget, progress func(string),
	done func(ok bool, msg string, lines []string) bool) bool {

	baslik := d.Device + " diskine kuruluyor"
	return a.runJobKind(installJobID, baslik, func() (fbui.EventKind, string, error) {
		out, err := runInstaller(d.Device, func(m string) {
			a.setJobTitle(installJobID, baslik+" · "+m)
			if progress != nil {
				progress(m)
			}
		})
		if err != nil {
			lines := parseInstallFailure(out, err)
			if done == nil || !done(false, "", lines) {
				a.showInstallResult(d, false, lines)
			}
			return fbui.EventError, "kurulum başarısız",
				errors.New(strings.TrimPrefix(lines[0], "Hata: "))
		}
		msg := lastLine(out)
		if msg == "" {
			msg = "Kurulum tamamlandı."
		}
		if done == nil || !done(true, msg, nil) {
			a.showInstallResult(d, true, []string{msg})
		}
		return fbui.EventOK, "Kurulum tamamlandı — USB'yi çıkarıp yeniden başlatabilirsiniz", nil
	}, nil)
}

// setJobTitle updates a running job's status-bar text (ilerleme için).
func (a *App) setJobTitle(id, title string) {
	a.mu.Lock()
	if j, ok := a.jobs[id]; ok {
		j.title = title
		a.dirty = true
	}
	a.mu.Unlock()
}

// showInstallResult opens the outcome of a background install.
//
// Kullanıcı o sırada başka bir pencerede yazıyor olabilir (parola, ağ adı):
// onu kapatıp yerine sonucu koymak yazdığını kaybettirirdi. O durumda sonuç
// yalnızca alt çubuğa düşer (runJobKind yazar) ve tam metin günlükte kalır.
func (a *App) showInstallResult(d ipc.DiskTarget, ok bool, lines []string) {
	if a.ActiveModal() != nil {
		return
	}
	if ok {
		a.OpenModal(NewConfirmModal("Kurulum tamamlandı",
			append([]string{
				d.Device + " diskine MCOS kuruldu.",
				"Yeniden başlatmadan önce USB belleği çıkarın; sistem artık",
				"diskten açılır ve kalıcı olarak diskte çalışır.",
			}, lines...),
			"Yeniden başlat", false,
			func(app *App) {
				app.mu.Lock()
				app.pending = ActReboot
				app.mu.Unlock()
			}))
		return
	}
	// Satırlar SARILIR: InfoModal satır kırmaz ve betiğin "Ne yapmalı"
	// satırı 100 sütunu aşıyor; QEMU'da (1920x1080) metin pencerenin sağ
	// kenarından taşıp kesiliyordu. 72 sütun 800x600'e de sığar.
	var sarili []string
	for _, l := range lines {
		sarili = append(sarili, wrapWords(l, 72)...)
	}
	a.OpenModal(NewInfoModal("Kurulum başarısız", sarili))
}

// installStatusFile, mcos-install'ın "yüzde|mesaj" yazdığı dosya.
var installStatusFile = "/tmp/mcos-install.status"

// installRunner, testlerde sahte bir betik koşturmak için değiştirilebilir.
var installRunner = func(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// runInstaller mcos-install'ı çalıştırır; sürerken durum dosyasındaki
// ilerlemeyi progress'e iletir. Çıktının tamamını (stdout+stderr) döndürür.
func runInstaller(device string, progress func(string)) (string, error) {
	cmd := installRunner("mcos-install", installArgs(device)...)
	var buf bytes.Buffer
	var mu sync.Mutex
	w := &lockedWriter{mu: &mu, b: &buf}
	cmd.Stdout = w
	cmd.Stderr = w

	// Önceki bir koşunun "100|tamamlandı" satırı yeni koşunun ilerlemesi
	// sanılmasın.
	_ = os.Remove(installStatusFile)

	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	tk := time.NewTicker(700 * time.Millisecond)
	defer tk.Stop()
	last := ""
	for {
		select {
		case err := <-done:
			mu.Lock()
			out := buf.String()
			mu.Unlock()
			return out, err
		case <-tk.C:
			if progress == nil {
				continue
			}
			if m := readInstallStatus(); m != "" && m != last {
				last = m
				progress(m)
			}
		}
	}
}

type lockedWriter struct {
	mu *sync.Mutex
	b  *bytes.Buffer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

// readInstallStatus "35|[4/7] Çekirdek..." satırını "%35 · [4/7] Çekirdek..."
// biçimine çevirir. HATA satırı ilerleme değildir (sonuç ayrıca gösterilir).
func readInstallStatus() string {
	b, err := os.ReadFile(installStatusFile)
	if err != nil {
		return ""
	}
	return formatInstallStatus(string(b))
}

func formatInstallStatus(s string) string {
	s = strings.TrimSpace(s)
	pct, msg, ok := strings.Cut(s, "|")
	if !ok || pct == "HATA" || msg == "" {
		return ""
	}
	return "%" + pct + " · " + msg
}

// parseInstallFailure, betiğin çıktısından kullanıcıya gösterilecek satırları
// çıkarır. Önce betiğin yapılandırılmış satırları (HATA/NEDEN/ÇÖZÜM/GÜNLÜK);
// yoksa (eski betik, çökme) çıktının son anlamlı satırları. "exit status 1"
// TEK BAŞINA asla gösterilmez: kullanıcı ondan hiçbir şey anlamıyordu.
func parseInstallFailure(out string, err error) []string {
	etiket := []struct{ onek, baslik string }{
		{"HATA:", "Hata: "},
		{"NEDEN:", "Neden: "},
		{"ÇÖZÜM:", "Ne yapmalı: "},
		{"GÜNLÜK:", "Tam günlük: "},
	}
	var satirlar []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		for _, e := range etiket {
			if strings.HasPrefix(l, e.onek) {
				if v := strings.TrimSpace(strings.TrimPrefix(l, e.onek)); v != "" {
					satirlar = append(satirlar, e.baslik+v)
				}
				break
			}
		}
	}
	if len(satirlar) > 0 {
		return satirlar
	}

	// Yapılandırılmış satır yok: son anlamlı satırları göster.
	var son []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), ">> mcos-install:"))
		if l != "" {
			son = append(son, l)
		}
	}
	if len(son) > 3 {
		son = son[len(son)-3:]
	}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee) && ee.ExitCode() < 0:
		// Sinyalle öldürüldü (ör. bellek yetersizliğinde çekirdek OOM).
		son = append(son, "Kurulum sistem tarafından durduruldu ("+err.Error()+
			"); bellek yetersiz olabilir. Açık sunucuları durdurup tekrar deneyin.")
	case errors.As(err, &ee):
		son = append(son, fmt.Sprintf("(mcos-install çıkış kodu %d)", ee.ExitCode()))
	default:
		son = append(son, "mcos-install çalıştırılamadı: "+err.Error())
	}
	if len(son) == 0 {
		son = []string{"Kurulum başarısız oldu ama betik bir sebep yazmadı; /data/log altındaki install-*.log dosyasına bakın."}
	}
	return son
}
