package fbpanel

import (
	"image"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/ipcclient"
	"mcos/internal/model"
)

// Tünel ekranı GERÇEKÇİ durumlarla çizilir. Metinler daemon'un ürettiği
// cümlelerdir (tunnel/playit_errors.go, daemon/handlers_playit.go).
//
// MCOS_SHOT_DIR verilirse PNG'ler oraya yazılır (gözle bakmak için).

func tunnelShotStatus() ipcclient.PlayitStatus {
	return ipcclient.PlayitStatus{
		Installed: true, Claimed: true, Running: true,
		Address: "mavi-kedi.gl.joinmc.link",
		Note:    "Tünel açık: mavi-kedi.gl.joinmc.link",
		Log: []string{
			"2026-09-27T04:08:24Z  INFO playitd::daemon: Starting playitd version=1.0.10",
			"2026-09-27T04:08:25Z  INFO playitd::daemon: tunnel ready",
		},
		PlayitSync: ipcclient.PlayitSync{Tunnels: []ipc.PlayitTunnel{
			{ID: "t1", Name: "MCOS Survival", Address: "mavi-kedi.gl.joinmc.link",
				LocalPort: 25565, Type: "minecraft-java", State: ipc.PlayitTunnelOpen,
				ServerID: "a", ServerName: "Survival"},
			{ID: "t2", Name: "MCOS Yaratici", LocalPort: 25566, Type: "minecraft-java",
				State: ipc.PlayitTunnelPending, Detail: "allocating port",
				ServerID: "b", ServerName: "Yaratıcı"},
			{ID: "t9", Name: "web", Address: "147.185.221.16:40123", LocalPort: 8080,
				State: ipc.PlayitTunnelOpen},
		}, Syncing: true},
	}
}

// tunnelMinInk: dolu bir tünel ekranının içerik panelindeki metin pikseli
// alt sınırı. Ölçüldü (1280×800, 16 px): açık tünel ekranı 12993, drawTunnel
// çerçeveden sonra hiçbir şey çizmezse 2097.
const tunnelMinInk = 6000

func tunnelShotApp(t *testing.T, st ipcclient.PlayitStatus, w, h int) *App {
	var a *App
	if w == 0 {
		a, _ = newTestApp(t)
	} else {
		a, _ = newSizedApp(t, w, h)
	}
	a.mu.Lock()
	a.playit = st
	a.dirty = true
	a.mu.Unlock()
	a.gotoSection(SecTunnel)
	a.setFocus(FocusContent)
	return a
}

func shotTunnel(t *testing.T, a *App, name string) int {
	t.Helper()
	a.Draw()
	img := a.ui.Canvas()
	if ink := countInk(img); ink < 20000 {
		t.Fatalf("tünel ekranı neredeyse boş (%d piksel)", ink)
	}
	writePNG(t, filepath.Join(peersShotDir(t), name), img)
	// Yalnızca İÇERİK panelindeki metin: kenar çubuğu ve arka plan tek
	// başına 20000 pikseli geçiyor, yani yukarıdaki sınama boş bir tünel
	// ekranını da geçirirdi.
	return textInk(img, image.Rect(a.sidebarWidth()+a.ui.M.PadX*3, a.ui.F.CellH*2,
		img.Bounds().Dx()-a.ui.M.PadX*2, img.Bounds().Dy()-a.ui.StatusBarH()-a.ui.M.PadY*2))
}

// textInk counts bright (text) pixels inside r.
func textInk(img *image.RGBA, r image.Rectangle) int {
	n := 0
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := img.PixOffset(x, y)
			if int(img.Pix[i])+int(img.Pix[i+1])+int(img.Pix[i+2]) > 300 {
				n++
			}
		}
	}
	return n
}

// Genel adres BÜYÜK yazılır; sığmazsa küçülür (2× → 1,5× → normal).
func TestTunnelBigAddress(t *testing.T) {
	a, _ := newTestApp(t)
	u := a.ui
	addr := "mavi-kedi.gl.joinmc.link"
	if h := a.bigText(0, 2000, 0, addr, u.Pal.Accent); h < u.F.CellH*18/10 {
		t.Fatalf("geniş alanda adres büyük yazılmadı: yükseklik %d, hücre %d", h, u.F.CellH)
	}
	// 2× sığmaz ama 1,5× sığar.
	w := len(addr) * u.F.CellW * 17 / 10
	if h := a.bigText(0, w, 0, addr, u.Pal.Accent); h < u.F.CellH*13/10 || h >= u.F.CellH*18/10 {
		t.Fatalf("dar alanda 1,5× beklenirdi: yükseklik %d, hücre %d", h, u.F.CellH)
	}
}

// Açık tünel: adres büyük yazılır, sunucu başına satırlar seçilebilir.
func TestTunnelScreenShotOpen(t *testing.T) {
	a := tunnelShotApp(t, tunnelShotStatus(), 0, 0)
	// 3 adım + 3 demo sunucusu (Survival, Yaratıcı, Test).
	if n := a.contentRows(); n != 6 {
		t.Fatalf("satır sayısı %d, 6 bekleniyordu (imleç sunuculara inemez)", n)
	}
	rows := a.tunnelServerRows()
	if rows[0].Tunnel == nil || rows[0].Tunnel.Address != "mavi-kedi.gl.joinmc.link" {
		t.Fatalf("Survival tüneli eşlenmedi: %+v", rows[0])
	}
	if rows[1].Tunnel == nil || rows[1].Tunnel.State != ipc.PlayitTunnelPending {
		t.Fatalf("Yaratıcı bekleyen tüneli eşlenmedi: %+v", rows[1])
	}
	if rows[2].Tunnel != nil {
		t.Fatalf("Test sunucusunun tüneli yok ama eşlendi: %+v", rows[2])
	}
	a.SetCursor(4) // Yaratıcı
	if ink := shotTunnel(t, a, "tunel-acik.png"); ink < tunnelMinInk {
		t.Fatalf("tünel içeriği çizilmedi (%d metin pikseli)", ink)
	}
}

// Küçük ekranda (1024×600) da sığmalı.
func TestTunnelScreenShotSmall(t *testing.T) {
	a := tunnelShotApp(t, tunnelShotStatus(), 1024, 600)
	if ink := shotTunnel(t, a, "tunel-kucuk.png"); ink < tunnelMinInk*6/10 {
		t.Fatalf("küçük ekranda tünel içeriği çizilmedi (%d metin pikseli)", ink)
	}
}

// Hesap e-postası doğrulanmamış + misafir hesap: nedenler renkli ve Türkçe.
func TestTunnelScreenShotError(t *testing.T) {
	st := tunnelShotStatus()
	st.Address = ""
	st.Note = "Tünel açılamadı: playit hesabınızın e-postasını doğrulayın (playit.gg → hesap)"
	st.TunnelError = "playit hesabınızın e-postasını doğrulayın (playit.gg → hesap)"
	st.Notices = []string{"misafir hesap: playit.gg'de giriş yapın/hesabı kaydedin"}
	st.Tunnels = []ipc.PlayitTunnel{{Name: "MCOS Survival", LocalPort: 25565,
		Type: "minecraft-java", State: ipc.PlayitTunnelError, Detail: st.TunnelError,
		ServerID: "a", ServerName: "Survival"}}
	st.Syncing = false
	a := tunnelShotApp(t, st, 0, 0)
	if ink := shotTunnel(t, a, "tunel-hata.png"); ink < tunnelMinInk {
		t.Fatalf("hata ekranı çizilmedi (%d metin pikseli)", ink)
	}
	if state, sub, _ := a.tunnelRowState(a.tunnelServerRows()[0]); state != "açılamadı" ||
		sub != st.TunnelError {
		t.Fatalf("sunucu satırı nedeni göstermiyor: %q / %q", state, sub)
	}
}

// Hesap yeni bağlandı: tünel açılıyor, adres yok.
func TestTunnelScreenShotCreating(t *testing.T) {
	st := tunnelShotStatus()
	st.Address = ""
	st.Note = "Tünel hazırlanıyor — playit adres atıyor…"
	st.Tunnels = []ipc.PlayitTunnel{{Name: "MCOS Survival", LocalPort: 25565,
		Type: "minecraft-java", State: ipc.PlayitTunnelCreating, ServerID: "a",
		ServerName: "Survival"}}
	a := tunnelShotApp(t, st, 0, 0)
	if ink := shotTunnel(t, a, "tunel-aciliyor.png"); ink < tunnelMinInk {
		t.Fatalf("açılıyor ekranı çizilmedi (%d metin pikseli)", ink)
	}
}

// Anahtar reddedildi: 2. adım "yeniden bağlayın" der ve Enter bağlamayı
// yeniden BAŞLATIR ("zaten bağlı" penceresi açmaz).
func TestTunnelKeyInvalidOffersRelink(t *testing.T) {
	st := tunnelShotStatus()
	st.Address = ""
	st.KeyInvalid = true
	st.Tunnels = nil
	st.TunnelError = "anahtar geçersiz; hesabı yeniden bağlayın"
	a := tunnelShotApp(t, st, 0, 0)
	state, _, _ := a.tunnelStepState(stepAccount)
	if state != "yeniden bağlayın" {
		t.Fatalf("2. adım durumu: %q", state)
	}
	a.cl = &ipcclient.Client{} // bağlantısız: çağrı hata döner ama önce olay yazılır
	a.SetCursor(int(stepAccount))
	a.Key("enter")
	if m := a.ActiveModal(); m != nil {
		t.Fatal("anahtar geçersizken 'zaten bağlı' penceresi açıldı — kullanıcı çıkmaza girer")
	}
	shotTunnel(t, a, "tunel-anahtar.png")
}

// Sunucu satırında Enter, o sunucunun tünelini ister (playit.tunnel).
func TestTunnelServerRowEnterRequestsTunnel(t *testing.T) {
	a := tunnelShotApp(t, tunnelShotStatus(), 0, 0)
	a.cl = &ipcclient.Client{}
	a.SetCursor(int(tunnelStepCount) + 2) // Test
	a.Key("enter")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range a.Events() {
			if strings.Contains(e.Text, "Test: tünel hazırlanıyor") {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("sunucu satırında Enter tünel istemedi; olaylar: %+v", a.Events())
}

// Ajan başladığı AN gelen durum eşitleyicinin ilk turundan önce olabilir
// (Syncing=false, adres yok); ekran yine de yenilemeyi sürdürmeli.
func TestTunnelWatchStartsBeforeFirstRound(t *testing.T) {
	st := ipcclient.PlayitStatus{Installed: true, Claimed: true, Running: true}
	if !tunnelInProgress(st) {
		t.Fatal("adres yokken ekran yenilenmez — kullanıcı 'hazırlanıyor'da takılır")
	}
	st.Address = "mavi-kedi.gl.joinmc.link"
	if tunnelInProgress(st) {
		t.Fatal("adres gelmişken yoklama sürüyor")
	}
	st.Address, st.KeyInvalid = "", true
	if tunnelInProgress(st) {
		t.Fatal("anahtar geçersizken boşuna yoklanıyor")
	}
}

// Ajan DURDURULDU ama durum hâlâ son adresi taşıyor: "ARKADAŞLARINIZA
// VERECEĞİNİZ ADRES" büyük yazılmamalı ve sunucu satırı "açık" dememeli —
// ajan kapalıyken o adrese bağlanan arkadaş sunucuya ulaşamaz.
func TestTunnelStoppedAgentNoBigAddress(t *testing.T) {
	st := tunnelShotStatus()
	st.Running = false
	st.Syncing = false
	st.Note = "Hesap bağlı. Ajanı başlatın."
	a := tunnelShotApp(t, st, 0, 0)
	if tunnelShowAddress(st) {
		t.Fatal("ajan kapalıyken genel adres büyük gösteriliyor")
	}
	if state, sub, _ := a.tunnelRowState(a.tunnelServerRows()[0]); state == "açık" ||
		!strings.Contains(sub, "mavi-kedi.gl.joinmc.link") {
		t.Fatalf("ajan kapalıyken satır: %q / %q", state, sub)
	}
	shotTunnel(t, a, "tunel-ajan-kapali.png")
	st.Running = true
	if !tunnelShowAddress(st) {
		t.Fatal("ajan çalışırken adres gösterilmiyor")
	}
}

// Çok sunuculu küçük ekranda (1024×600, büyük adresin altında yalnızca birkaç
// satır sığar) imleç görünmeyen bir sunucu satırına inerse liste kaymalı:
// eskiden satır hiç çizilmiyordu, imleç kayboluyor ve Enter görünmeyen bir
// sunucuda tünel açıyordu.
func TestTunnelManyServersScrollToCursor(t *testing.T) {
	a := tunnelShotApp(t, tunnelShotStatus(), 1024, 600)
	var servers []*model.Server
	for i := 0; i < 9; i++ {
		servers = append(servers, &model.Server{ID: "s" + itoa(i), Name: "Sunucu " + itoa(i),
			Port: 25565 + i, State: model.StateStopped})
	}
	a.SetServers(servers)
	last := int(tunnelStepCount) + len(servers) - 1
	if n := a.contentRows(); n != last+1 {
		t.Fatalf("satır sayısı %d, %d bekleniyordu", n, last+1)
	}
	a.SetCursor(last)
	shotTunnel(t, a, "tunel-cok-sunucu.png")
	if !tunnelRowDrawn(a, last) {
		t.Fatal("imleçteki son sunucu satırı çizilmedi (liste kaymıyor)")
	}
	a.SetCursor(int(tunnelStepCount))
	a.Draw()
	if !tunnelRowDrawn(a, int(tunnelStepCount)) {
		t.Fatal("yukarı dönünce ilk sunucu satırı çizilmedi")
	}
}

// tunnelRowDrawn reports whether row idx got a click zone in the last frame
// that lies fully inside the screen.
func tunnelRowDrawn(a *App, idx int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	h := a.ui.Canvas().Bounds().Dy() - a.ui.StatusBarH()
	for _, z := range a.zones {
		if z.kind == zoneRow && z.idx == idx && z.r.Max.Y <= h {
			return true
		}
	}
	return false
}

// Uzun sunucu adı sağdaki durum yazısının üstüne taşmamalı; kesilen ad olur,
// port KALIR (kullanıcı satırı portundan tanır).
func TestTunnelRowLabelFits(t *testing.T) {
	long := strings.Repeat("Çok uzun sunucu adı ", 8)
	got := tunnelRowLabel(long, 25570, 30)
	if r := []rune(got); len(r) > 30 || !strings.HasSuffix(got, ":25570") {
		t.Fatalf("etiket sığmadı ya da port düştü: %q (%d)", got, len(r))
	}
	if got := tunnelRowLabel("Survival", 25565, 30); got != "Survival  :25565" {
		t.Fatalf("kısa ad değişti: %q", got)
	}
	a := tunnelShotApp(t, tunnelShotStatus(), 1024, 600)
	a.SetServers([]*model.Server{{ID: "a", Name: long, Port: 25565, State: model.StateRunning}})
	shotTunnel(t, a, "tunel-uzun-ad.png")
	// Çizimde: durum yazısının ("açık") hemen solundaki iki hücre boş kalmalı.
	u := a.ui
	var row image.Rectangle
	a.mu.Lock()
	for _, z := range a.zones {
		if z.kind == zoneRow && z.idx == int(tunnelStepCount) {
			row = z.r
		}
	}
	a.mu.Unlock()
	if row.Empty() {
		t.Fatal("sunucu satırı çizilmedi")
	}
	stateX := row.Max.X - u.M.PadX - u.TextWidth("açık")
	ty := row.Min.Y + u.M.PadY/2
	if n := textInk(u.Canvas(), image.Rect(stateX-u.F.CellW*2, ty, stateX-2, ty+u.F.CellH)); n > 0 {
		t.Fatalf("sunucu adı durum yazısına taşıyor (%d piksel)", n)
	}
}
