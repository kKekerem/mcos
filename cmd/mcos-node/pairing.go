package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"mcos/internal/cluster"
	mlog "mcos/internal/log"
)

// Bu dosya anahtarsız (kodla) eşleştirmenin DÜĞÜM tarafıdır.
//
// ════════════════════════════════════════════════════════════════════════════
// NEDEN
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı: "Eşleştirme anahtarını elle girme gerekmesin, oto tarasın,
// doğrulasın." Eskiden düğüm anahtarsız BAŞLAMIYORDU: pencerede anahtar
// kutusu, konsolda "Anahtarı buraya yapıştırın" istemi vardı ve 32 haneli
// anahtar MCOS ekranından elle yazılıyordu. Protokol cluster/pairoffer.go'da:
// MCOS teklif gönderir, iki ekranda AYNI 6 haneli kod çıkar, burada "Kabul
// et", MCOS'ta "Kodlar aynı" → anahtar şifreli gelir ve kalıcı yazılır.
//
// ════════════════════════════════════════════════════════════════════════════
// KARAR NASIL GELİR: küçük bir dosya, ağ değil
// ════════════════════════════════════════════════════════════════════════════
//
// Teklifler ARKA PLANDAKİ düğüm sürecindedir (--arkaplan, oturum açılışında
// penceresiz); "Kabul et" düğmesi ise AYRI bir süreçte (pencere) ya da başka
// bir terminalde ("mcos-node --kabul 482913"). İki süreç zaten durum.json
// ve durdur.istek dosyalarıyla konuşuyor (status.go, main.go requestStop);
// kararlar da aynı yoldan gider:
//
//   - teklifler durum.json'a yazılır (kod, ad, IP, kalan süre),
//   - karar "esleme-<teklif>.karar" dosyasıdır; düğüm yarım saniyede bir
//     okur, uygular ve siler.
//
// Neden güvenli: veri klasörü kullanıcının kendi klasörüdür (Windows'ta
// %LOCALAPPDATA%, Linux'ta ~/.local/share); dosyayı ancak aynı kullanıcı
// yazabilir — "durdur.istek" ile aynı güven sınırı. Yeni bir yerel ağ portu
// AÇILMAZ (açılsaydı aynı makinedeki her süreç ona istek atabilirdi). Karar
// kullanıcının GÖRDÜĞÜ kodu da taşır: süresi dolup yenilenen bir teklif,
// eski bir kararla kabul edilemez.

const (
	pairDecisionPrefix = "esleme-"
	pairDecisionSuffix = ".karar"
	// pairTick: kararın uygulanma gecikmesi. "Kabul et"e basan kullanıcı
	// yarım saniyeden uzun beklerse düğmenin çalışmadığını sanıyor.
	pairTick = 500 * time.Millisecond
)

// offerInfo is one pending pairing request, as durum.json carries it.
type offerInfo struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	IP       string    `json:"ip"`
	Code     string    `json:"code"`
	Accepted bool      `json:"accepted"`
	At       time.Time `json:"at"`
	// LeftSec: teklifin kalan ömrü; pencere "2 dk 40 sn içinde" der.
	LeftSec int `json:"leftSec"`
}

// pairEvent is the last pairing outcome ("mcos-kutu ile eşleşildi").
//
// Pencere ve "--kabul" sonucu buradan öğrenir: teklif listeden kaybolduğunda
// eşleşme mi oldu, süre mi doldu, ayırt edilebilmeli.
type pairEvent struct {
	Msg string    `json:"msg"`
	OK  bool      `json:"ok"`
	At  time.Time `json:"at"`
}

// pairDecision is the content of one decision file.
type pairDecision struct {
	ID     string    `json:"id"`
	Code   string    `json:"kod"`
	Accept bool      `json:"kabul"`
	At     time.Time `json:"zaman"`
}

// offerSource is the part of cluster.Manager the tracker uses.
//
// NEDEN ARAYÜZ: sınamalar ağ açmadan teklif/karar akışını sınayabilsin.
type offerSource interface {
	Offers() []cluster.InOffer
	DecideOffer(id string, accept bool) error
}

// pairTracker carries offers and decisions between the node and its viewers.
type pairTracker struct {
	dataRoot string
	src      offerSource
	log      *mlog.Logger

	mu     sync.Mutex
	event  pairEvent
	seen   map[string]bool // günlüğe yazılmış teklifler
	sig    string          // son yayımlanan teklif listesinin özeti
	saveFn func(key string) error
}

func newPairTracker(dataRoot string, src offerSource, lg *mlog.Logger) *pairTracker {
	t := &pairTracker{dataRoot: dataRoot, src: src, log: lg, seen: map[string]bool{}}
	t.saveFn = func(key string) error { return saveReceivedKey(dataRoot, key) }
	return t
}

// offers lists pending (not rejected) offers, oldest first.
func (t *pairTracker) offers() []offerInfo {
	if t == nil || t.src == nil {
		return nil
	}
	var out []offerInfo
	for _, o := range t.src.Offers() {
		if o.Rejected {
			// Reddedilen teklif MCOS bir sonraki "pairKey"i gönderene kadar
			// bellekte durur (MCOS reddi öyle öğrenir); ekranda kalmamalı.
			continue
		}
		left := int((cluster.PairOfferTTL - time.Since(o.At)).Seconds())
		if left < 0 {
			left = 0
		}
		out = append(out, offerInfo{
			ID: o.ID, Name: o.Name, IP: o.IP, Code: o.Code,
			Accepted: o.Accepted, At: o.At, LeftSec: left,
		})
	}
	sort.Slice(out, func(i, k int) bool { return out[i].At.Before(out[k].At) })
	return out
}

func (t *pairTracker) lastEvent() *pairEvent {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.event.Msg == "" {
		return nil
	}
	e := t.event
	return &e
}

func (t *pairTracker) setEvent(msg string, ok bool) {
	t.mu.Lock()
	t.event = pairEvent{Msg: msg, OK: ok, At: time.Now()}
	t.mu.Unlock()
}

// keyReceived stores the key MCOS sent for an accepted offer.
//
// cluster.Manager SetSecret'i zaten yaptı; burada kalıcılık var: düğüm
// yeniden açıldığında (oturum açılışı) aynı anahtarla başlamalı, yoksa
// eşleşme her yeniden başlatmada kaybolurdu.
func (t *pairTracker) keyReceived(key string, from cluster.InOffer) error {
	err := t.saveFn(key)
	if err != nil {
		t.setEvent(fmt.Sprintf("%s ile eşleşildi ama anahtar diske yazılamadı: %v "+
			"(düğüm yeniden başlarsa eşleşme kaybolur)", from.Name, err), false)
		return err
	}
	t.setEvent(from.Name+" ile eşleşildi — anahtar otomatik alındı", true)
	if t.log != nil {
		t.log.Infof("node: %s (%s) ile kodla eşleşildi; anahtar kaydedildi", from.Name, from.IP)
	}
	return nil
}

// saveReceivedKey writes a received key into node.json.
func saveReceivedKey(dataRoot, key string) error {
	s, err := loadSettings(dataRoot)
	if err != nil {
		return err
	}
	s.Key = key
	return saveSettings(dataRoot, s)
}

// tick applies pending decisions and reports whether the offer list changed.
func (t *pairTracker) tick() bool {
	changed := t.applyDecisions()
	offs := t.offers()
	var sb strings.Builder
	for _, o := range offs {
		fmt.Fprintf(&sb, "%s/%s/%v;", o.ID, o.Code, o.Accepted)
		t.mu.Lock()
		yeni := !t.seen[o.ID]
		t.seen[o.ID] = true
		t.mu.Unlock()
		if yeni && t.log != nil {
			// Günlükte KABUL YOLU da yazsın: systemd ile penceresiz çalışan
			// bir Linux kullanıcısı yalnızca günlüğü görür.
			t.log.Infof("node: eşleştirme isteği: %s (%s), kod %s — kabul için pencerede "+
				"\"Kabul et\" ya da: mcos-node --kabul %s", o.Name, o.IP, o.Code, digitsOnly(o.Code))
		}
	}
	t.mu.Lock()
	if sb.String() != t.sig {
		t.sig, changed = sb.String(), true
	}
	t.mu.Unlock()
	return changed
}

// loop runs tick every pairTick; kick asks statusLoop to write NOW.
func (t *pairTracker) loop(ctx context.Context, kick chan<- struct{}) {
	tk := time.NewTicker(pairTick)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			if t.tick() {
				select {
				case kick <- struct{}{}:
				default:
				}
			}
		}
	}
}

// applyDecisions reads, applies and removes decision files.
func (t *pairTracker) applyDecisions() bool {
	files, _ := filepath.Glob(filepath.Join(t.dataRoot, pairDecisionPrefix+"*"+pairDecisionSuffix))
	if len(files) == 0 {
		return false
	}
	offs := t.src.Offers()
	for _, f := range files {
		b, err := os.ReadFile(f)
		_ = os.Remove(f) // okunamasa da kalmasın: her yarım saniyede yeniden denenirdi
		if err != nil {
			continue
		}
		var d pairDecision
		if json.Unmarshal(b, &d) != nil || filepath.Base(f) != decisionFileName(d.ID) {
			continue
		}
		if time.Since(d.At) > cluster.PairOfferTTL {
			continue // düğüm kapalıyken kalmış eski bir karar
		}
		var hedef *cluster.InOffer
		for i := range offs {
			if offs[i].ID == d.ID {
				hedef = &offs[i]
				break
			}
		}
		switch {
		case hedef == nil:
			t.setEvent("Bu eşleştirme isteğinin süresi doldu — MCOS'ta yeniden eşleştirin", false)
		case digitsOnly(hedef.Code) != digitsOnly(d.Code):
			// Kullanıcı başka bir kodu onayladı (teklif bu arada yenilendi).
			t.setEvent("Kod değişti — ekrandaki YENİ kodu karşılaştırıp yeniden onaylayın", false)
			if t.log != nil {
				t.log.Warnf("node: %s için karar eski kodla (%s) geldi; uygulanmadı", hedef.Name, d.Code)
			}
		default:
			if err := t.src.DecideOffer(d.ID, d.Accept); err != nil {
				t.setEvent(err.Error(), false)
				continue
			}
			if d.Accept {
				t.setEvent(hedef.Name+" kabul edildi — MCOS ekranında \"Kodlar aynı, onayla\"ya basın", true)
			} else {
				t.setEvent(hedef.Name+" isteği reddedildi", true)
			}
			if t.log != nil {
				t.log.Infof("node: %s eşleştirme isteği %s (kod %s)", hedef.Name,
					map[bool]string{true: "kabul edildi", false: "reddedildi"}[d.Accept], hedef.Code)
			}
		}
	}
	// Karar uygulanmasa bile (süresi dolmuş) olay iletisi değişti: pencere
	// NEDENİ hemen görsün.
	return true
}

// validOfferID: teklif kimliği dosya adına girer; yol ayırıcısı ("..", "/")
// taşıyan bir kimlik veri klasörünün DIŞINA yazdırırdı.
func validOfferID(id string) bool {
	if id == "" || len(id) > 32 {
		return false
	}
	for _, r := range id {
		ok := r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !ok {
			return false
		}
	}
	return true
}

func decisionFileName(id string) string { return pairDecisionPrefix + id + pairDecisionSuffix }

// writePairDecision drops a decision for the running node (atomik).
func writePairDecision(dataRoot, id, code string, accept bool) error {
	if !validOfferID(id) {
		return fmt.Errorf("geçersiz teklif kimliği")
	}
	b, err := json.Marshal(pairDecision{ID: id, Code: code, Accept: accept, At: time.Now()})
	if err != nil {
		return err
	}
	path := filepath.Join(dataRoot, decisionFileName(id))
	// Geçici adın uzantısı .karar DEĞİL: düğüm yarım yazılmış dosyayı
	// okuyup "bozuk" diye silmesin.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("karar yazılamadı: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("karar yazılamadı: %w", err)
	}
	return nil
}

// digitsOnly: "482 913" ile "482913" aynı koddur (kullanıcı boşluğu yazmaz).
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// findOffer picks the offer a typed code (or "" = the newest) points at.
func findOffer(offs []offerInfo, code string) (offerInfo, bool) {
	want := digitsOnly(code)
	for i := len(offs) - 1; i >= 0; i-- {
		if want == "" || digitsOnly(offs[i].Code) == want {
			return offs[i], true
		}
	}
	return offerInfo{}, false
}

// ── Komut satırı: mcos-node --kabul 482913 / --reddet 482913 ────────────────

// cliDecide accepts or rejects an offer of the running node from a terminal.
//
// Penceresiz (systemd, SSH) bir düğümde "Kabul et" düğmesi yok; kod ve bu
// komut günlükte ve konsol ekranında yazılı.
func cliDecide(dataRoot, code string, accept bool, out io.Writer, wait time.Duration) int {
	st, ok := readStatus(dataRoot)
	if !ok || time.Since(st.Updated) > statusFresh {
		fmt.Fprintln(out, "  Çalışan bir düğüm yok. Önce mcos-node'u başlatın.")
		return 1
	}
	o, found := findOffer(st.Offers, code)
	if !found {
		fmt.Fprintf(out, "  %q koduyla bekleyen bir eşleştirme isteği yok.\n", code)
		if len(st.Offers) == 0 {
			fmt.Fprintln(out, "  Şu an bekleyen istek yok. MCOS'ta: MCOS Paylaşım → Ağı tara → bu PC → Eşleştir")
		}
		for _, x := range st.Offers {
			fmt.Fprintf(out, "    • %s (%s) — kod %s\n", x.Name, x.IP, x.Code)
		}
		return 1
	}
	if err := writePairDecision(dataRoot, o.ID, o.Code, accept); err != nil {
		fmt.Fprintln(out, "  "+err.Error())
		return 1
	}
	if !accept {
		fmt.Fprintf(out, "  %s isteği reddedildi.\n", o.Name)
		return waitDecisionApplied(dataRoot, o.ID, out)
	}
	fmt.Fprintf(out, "  %s (kod %s) kabul edildi.\n", o.Name, o.Code)
	if rc := waitDecisionApplied(dataRoot, o.ID, out); rc != 0 {
		return rc
	}
	fmt.Fprintln(out, "  Şimdi MCOS ekranında \"Kodlar aynı, onayla\"ya basın; eşleşme bekleniyor…")
	start := time.Now()
	for time.Since(start) < wait {
		time.Sleep(300 * time.Millisecond)
		st, ok := readStatus(dataRoot)
		if !ok {
			fmt.Fprintln(out, "  Düğüm kapandı.")
			return 1
		}
		if _, still := findOfferByID(st.Offers, o.ID); still {
			continue
		}
		// Teklif listeden düştü: ya anahtar geldi (olay iletisi yeni), ya
		// süresi doldu / MCOS vazgeçti. Eski bir eşleşmeden kalan KeySet
		// burada ölçüt DEĞİL: yanlışlıkla "eşleşildi" dedirtirdi.
		if e := st.PairEvent; e != nil && e.At.After(start) {
			fmt.Fprintln(out, "  "+e.Msg)
			if e.OK && st.KeySet {
				return 0
			}
			return 1
		}
		fmt.Fprintln(out, "  İstek düştü (süresi doldu ya da MCOS'ta iptal edildi); MCOS'ta yeniden eşleştirin.")
		return 1
	}
	fmt.Fprintln(out, "  MCOS'tan onay gelmedi; istek süresi dolunca kendiliğinden düşer.")
	return 1
}

// waitDecisionApplied waits until the node picked the decision file up.
func waitDecisionApplied(dataRoot, id string, out io.Writer) int {
	path := filepath.Join(dataRoot, decisionFileName(id))
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return 0
		}
		time.Sleep(250 * time.Millisecond)
	}
	fmt.Fprintln(out, "  Düğüm kararı 5 saniyede almadı — düğüm yanıt vermiyor olabilir; günlüğe bakın.")
	return 1
}

func findOfferByID(offs []offerInfo, id string) (offerInfo, bool) {
	for _, o := range offs {
		if o.ID == id {
			return o, true
		}
	}
	return offerInfo{}, false
}

// consoleKeys lets the console screen accept with "K" + Enter.
//
// Konsol ekranında kullanıcı kodu GÖRÜYOR; ikinci bir terminal açıp
// "--kabul" yazmasını beklemek gereksiz. Girdi yoksa (systemd, yönlendirme)
// okuma hemen biter ve hiçbir şey olmaz.
func consoleKeys(dataRoot string, in io.Reader, say func(string)) {
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		f := strings.Fields(strings.ToLower(sc.Text()))
		if len(f) == 0 {
			continue
		}
		var accept bool
		switch f[0] {
		case "k", "e", "kabul", "evet":
			accept = true
		case "r", "h", "reddet", "hayir", "hayır":
			accept = false
		default:
			continue
		}
		code := strings.Join(f[1:], "")
		st, ok := readStatus(dataRoot)
		if !ok {
			say("Düğüm çalışmıyor.")
			continue
		}
		o, found := findOffer(st.Offers, code)
		if !found {
			say("Bekleyen eşleştirme isteği yok.")
			continue
		}
		if err := writePairDecision(dataRoot, o.ID, o.Code, accept); err != nil {
			say(err.Error())
			continue
		}
		if accept {
			say(o.Name + " kabul edildi — MCOS ekranında onaylayın.")
		} else {
			say(o.Name + " reddedildi.")
		}
	}
}
