package cluster

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

// ════════════════════════════════════════════════════════════════════════════
// ANAHTARSIZ EŞLEŞTİRME: iki ekranda aynı 6 haneli kod, iki onay
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı: "Eşleştirme anahtarını elle girmek gerekmesin, otomatik tarasın,
// doğrulasın." Eskiden MCOS'ta gösterilen uzun anahtar Windows uygulamasına
// elle yazılıyordu; yarım kopyalanan anahtar "anahtar yanlış" ile bitiyordu.
//
// Akış (Bluetooth eşleştirmesi gibi):
//  1. MCOS, ağda bulduğu düğüme "pairOffer" gönderir. İki taraf geçici X25519
//     anahtarlarıyla ortak bir sır türetir; o sırdan İKİ EKRANDA DA AYNI 6
//     haneli kod çıkar (SAS).
//  2. Düğümde kullanıcı "Kabul et"e, MCOS'ta "Kodlar aynı, onayla"ya basar.
//  3. MCOS küme anahtarını ortak sırla AES-GCM'de mühürleyip "pairKey" ile
//     gönderir; düğüm yalnızca KABUL EDİLMİŞ bir teklif için açar ve saklar.
//
// Neden güvenli: aradaki bir saldırgan (aynı ağda) iki tarafla AYRI sırlar
// kurmak zorunda kalır ve iki ekrandaki kodlar tutmaz — kullanıcı onaylamaz,
// anahtar hiç gönderilmez. Kod karşılaştırması kaldırılamaz: MCOS'taki onay
// kodun kendisinin gösterildiği ekranda istenir.

const (
	maxInOffers  = 5
	pairOfferTTL = 3 * time.Minute
	pairInfo     = "mcos-esleme-v1"
)

// PairOfferTTL: bir teklifin geçerli kaldığı süre. Düğüm penceresi kalan
// süreyi gösterir ve eski karar dosyalarını bununla ayıklar; ayrı bir sabit
// yazılsaydı iki yer farklı sürelerle "süresi doldu" derdi.
const PairOfferTTL = pairOfferTTL

// Durumlar (pairKey yanıtı).
const (
	PairDone     = "tamam"
	PairWaiting  = "bekliyor"   // düğümde henüz "Kabul et"e basılmadı
	PairRejected = "reddedildi" // düğümde "Reddet"e basıldı
	PairGone     = "yok"        // teklif süresi doldu ya da bilinmiyor
)

// ErrNeedsCodePairing: elle eklenen eşte anahtar yok; eş eşleşmemiş olarak
// kaydedildi, kodla eşleştirme (PairOffer) başlatılmalı.
var ErrNeedsCodePairing = errors.New("bu cihaz kodla eşleştirilmeli")

// ErrPairUnsupported: karşı taraf anahtarsız eşleştirmeyi desteklemiyor
// (eski sürüm ya da başka bir MCOS) — çağıran eski yola düşer.
var ErrPairUnsupported = errors.New("karşı taraf kodla eşleştirmeyi desteklemiyor")

type pairWire struct {
	Pub    string `json:"pairPub,omitempty"`
	Sealed string `json:"sealed,omitempty"`
}

type pairReply struct {
	OK     bool   `json:"ok"`
	Pub    string `json:"pairPub,omitempty"`
	State  string `json:"state,omitempty"`
	Reason string `json:"reason,omitempty"`
	Name   string `json:"name,omitempty"`
}

// sasCode derives the 6-digit comparison code. Sıra önemli: iki taraf da
// (başlatanın, yanıtlayanın) açık anahtarını aynı sırayla katar.
func sasCode(shared, pubA, pubB []byte) string {
	h := sha256.New()
	h.Write([]byte(pairInfo + "/kod"))
	h.Write(shared)
	h.Write(pubA)
	h.Write(pubB)
	n := binary.BigEndian.Uint32(h.Sum(nil)[:4]) % 1000000
	s := fmt.Sprintf("%06d", n)
	return s[:3] + " " + s[3:]
}

func sealKey(shared []byte) []byte {
	h := sha256.Sum256(append([]byte(pairInfo+"/anahtar"), shared...))
	return h[:]
}

func seal(shared []byte, plain string) (string, error) {
	blk, err := aes.NewCipher(sealKey(shared))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func unseal(shared []byte, sealed string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	blk, err := aes.NewCipher(sealKey(shared))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		return "", err
	}
	if len(b) < gcm.NonceSize() {
		return "", errors.New("mühür kısa")
	}
	p, err := gcm.Open(nil, b[:gcm.NonceSize()], b[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("mühür açılamadı (kodlar uyuşmuyor olabilir)")
	}
	return string(p), nil
}

// ── Başlatan taraf (MCOS) ───────────────────────────────────────────────────

type outOffer struct {
	priv   *ecdh.PrivateKey
	pubA   []byte
	shared []byte
	code   string
	at     time.Time
}

type pairState struct {
	mu  sync.Mutex
	out map[string]*outOffer // eş kimliğine göre
	in  map[string]*InOffer  // teklif kimliğine göre (yanıtlayan taraf)
	// onKey, yanıtlayan tarafta kabul edilmiş bir teklifin anahtarı geldiğinde
	// çağrılır (düğüm: ayarlara yaz, SetSecret). nil ise teklifler REDDEDİLİR
	// (MCOS'un kendisi teklif kabul etmez; bkz. EnablePairOffers).
	onKey func(key string, from InOffer) error
}

func (m *Manager) pairSt() *pairState {
	m.pairOnce.Do(func() {
		m.pair = &pairState{out: map[string]*outOffer{}, in: map[string]*InOffer{}}
	})
	return m.pair
}

// pairCall sends one pairing request and decodes the reply.
func (m *Manager) pairCall(ctx context.Context, ip string, port int, method string, w pairWire) (pairReply, error) {
	var rep pairReply
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return rep, fmt.Errorf("%s", describeNetErr(err, port))
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	req := peerRequest{Method: method, From: m.selfHello(), PairPub: w.Pub, Sealed: w.Sealed}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return rep, err
	}
	if err := json.NewDecoder(conn).Decode(&rep); err != nil {
		// Eski sürüm tanımadığı yöntemi yanıtsız kapatır.
		return rep, ErrPairUnsupported
	}
	return rep, nil
}

// PairOffer starts code pairing with a discovered peer and returns the code
// to show. Karşı taraf desteklemiyorsa ErrPairUnsupported.
func (m *Manager) PairOffer(ctx context.Context, id string) (string, error) {
	m.mu.RLock()
	p, ok := m.peers[id]
	var ip string
	var port int
	if ok {
		ip, port = p.IP, p.Port
	}
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("cihaz bulunamadı (ağdan düşmüş olabilir)")
	}
	if port == 0 {
		port = m.listenPort
	}
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	pubA := priv.PublicKey().Bytes()
	rep, err := m.pairCall(ctx, ip, port, "pairOffer", pairWire{Pub: base64.StdEncoding.EncodeToString(pubA)})
	if err != nil {
		return "", err
	}
	if !rep.OK {
		if rep.Reason == "desteklenmiyor" {
			return "", ErrPairUnsupported
		}
		return "", fmt.Errorf("teklif reddedildi: %s", rep.Reason)
	}
	pubBb, err := base64.StdEncoding.DecodeString(rep.Pub)
	if err != nil {
		return "", fmt.Errorf("geçersiz yanıt")
	}
	pubB, err := ecdh.X25519().NewPublicKey(pubBb)
	if err != nil {
		return "", fmt.Errorf("geçersiz yanıt")
	}
	shared, err := priv.ECDH(pubB)
	if err != nil {
		return "", err
	}
	code := sasCode(shared, pubA, pubBb)
	ps := m.pairSt()
	ps.mu.Lock()
	ps.out[id] = &outOffer{priv: priv, pubA: pubA, shared: shared, code: code, at: time.Now()}
	ps.mu.Unlock()
	return code, nil
}

// PairConfirm sends the sealed key for a confirmed offer. Dönen durum
// PairDone ise eş eşleştirildi; PairWaiting ise düğümde henüz kabul edilmedi
// (çağıran kısa aralıklarla yeniden dener).
func (m *Manager) PairConfirm(ctx context.Context, id string) (string, error) {
	ps := m.pairSt()
	ps.mu.Lock()
	o := ps.out[id]
	ps.mu.Unlock()
	if o == nil || time.Since(o.at) > pairOfferTTL {
		return PairGone, fmt.Errorf("teklifin süresi doldu — yeniden eşleştirin")
	}
	m.mu.RLock()
	p, ok := m.peers[id]
	var ip string
	var port int
	if ok {
		ip, port = p.IP, p.Port
	}
	m.mu.RUnlock()
	if !ok {
		return PairGone, fmt.Errorf("cihaz bulunamadı")
	}
	if port == 0 {
		port = m.listenPort
	}
	sealed, err := seal(o.shared, m.Secret())
	if err != nil {
		return "", err
	}
	rep, err := m.pairCall(ctx, ip, port, "pairKey", pairWire{Pub: base64.StdEncoding.EncodeToString(o.pubA), Sealed: sealed})
	if err != nil {
		return "", err
	}
	switch rep.State {
	case PairDone:
		ps.mu.Lock()
		delete(ps.out, id)
		ps.mu.Unlock()
		m.Pair(id) // yerel kayıt + anahtarla doğrulama (verifyPeer)
		return PairDone, nil
	case PairRejected:
		ps.mu.Lock()
		delete(ps.out, id)
		ps.mu.Unlock()
		return PairRejected, fmt.Errorf("%s eşleşmeyi reddetti", p.Name)
	case PairWaiting:
		return PairWaiting, nil
	}
	return PairGone, fmt.Errorf("teklif karşı tarafta bulunamadı (süresi dolmuş olabilir)")
}

// PairCancel forgets an outgoing offer.
func (m *Manager) PairCancel(id string) {
	ps := m.pairSt()
	ps.mu.Lock()
	delete(ps.out, id)
	ps.mu.Unlock()
}

// ── Yanıtlayan taraf (düğüm) ────────────────────────────────────────────────

// InOffer is an incoming pairing offer shown to the user.
type InOffer struct {
	ID       string    `json:"id"`
	FromID   string    `json:"fromId"`
	Name     string    `json:"name"`
	IP       string    `json:"ip"`
	Port     int       `json:"port"`
	Code     string    `json:"code"`
	Accepted bool      `json:"accepted"`
	Rejected bool      `json:"rejected"`
	At       time.Time `json:"at"`

	shared []byte
	pubA   string
}

// EnablePairOffers lets this node accept code pairing. onKey kabul edilen
// teklifin anahtarını saklar (ayarlara yazma + SetSecret çağıranın işi değil:
// burada SetSecret zaten yapılır; onKey kalıcılık içindir).
func (m *Manager) EnablePairOffers(onKey func(key string, from InOffer) error) {
	ps := m.pairSt()
	ps.mu.Lock()
	ps.onKey = onKey
	ps.mu.Unlock()
}

// Offers lists pending incoming offers (süresi dolanlar atılır).
func (m *Manager) Offers() []InOffer {
	ps := m.pairSt()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	out := make([]InOffer, 0, len(ps.in))
	for id, o := range ps.in {
		if time.Since(o.At) > pairOfferTTL {
			delete(ps.in, id)
			continue
		}
		out = append(out, *o)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].At.Before(out[k].At) })
	return out
}

// DecideOffer records the user's decision for an incoming offer.
func (m *Manager) DecideOffer(id string, accept bool) error {
	ps := m.pairSt()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	o, ok := ps.in[id]
	if !ok || time.Since(o.At) > pairOfferTTL {
		return fmt.Errorf("teklifin süresi doldu — MCOS'ta yeniden eşleştirin")
	}
	o.Accepted, o.Rejected = accept, !accept
	return nil
}

// handlePairOffer answers "pairOffer" (yanıtlayan taraf).
func (m *Manager) handlePairOffer(remoteIP string, from *peerHello, w pairWire) pairReply {
	ps := m.pairSt()
	ps.mu.Lock()
	acik := ps.onKey != nil
	ps.mu.Unlock()
	if !acik {
		return pairReply{Reason: "desteklenmiyor"}
	}
	pubAb, err := base64.StdEncoding.DecodeString(w.Pub)
	if err != nil {
		return pairReply{Reason: "geçersiz"}
	}
	pubA, err := ecdh.X25519().NewPublicKey(pubAb)
	if err != nil {
		return pairReply{Reason: "geçersiz"}
	}
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return pairReply{Reason: "iç hata"}
	}
	shared, err := priv.ECDH(pubA)
	if err != nil {
		return pairReply{Reason: "geçersiz"}
	}
	pubB := priv.PublicKey().Bytes()
	o := &InOffer{
		ID: base64.RawURLEncoding.EncodeToString(pubAb[:9]), IP: remoteIP,
		Code: sasCode(shared, pubAb, pubB), At: time.Now(),
		shared: shared, pubA: w.Pub,
	}
	if from != nil {
		o.FromID, o.Name, o.Port = from.NodeID, from.NodeName, from.Port
	}
	if o.Name == "" {
		o.Name = remoteIP
	}
	ps.mu.Lock()
	// Aynı ağdaki biri teklif yağdırıp ekranı doldurmasın.
	if len(ps.in) >= maxInOffers {
		ps.mu.Unlock()
		return pairReply{Reason: "çok fazla bekleyen teklif"}
	}
	// Aynı MCOS'tan gelen eski teklif yenisiyle değişir (kullanıcı yeniden
	// denedi): ekranda iki ayrı kod durmasın.
	for id, eski := range ps.in {
		if eski.FromID != "" && eski.FromID == o.FromID {
			delete(ps.in, id)
		}
	}
	ps.in[o.ID] = o
	ps.mu.Unlock()
	if m.log != nil {
		m.log.Infof("cluster: %s (%s) eşleşmek istiyor, kod %s", o.Name, remoteIP, o.Code)
	}
	return pairReply{OK: true, Pub: base64.StdEncoding.EncodeToString(pubB), Name: m.name()}
}

// handlePairKey answers "pairKey" (yanıtlayan taraf).
func (m *Manager) handlePairKey(w pairWire) pairReply {
	ps := m.pairSt()
	ps.mu.Lock()
	var o *InOffer
	var oid string
	for id, x := range ps.in {
		if x.pubA == w.Pub {
			o, oid = x, id
			break
		}
	}
	if o == nil || time.Since(o.At) > pairOfferTTL {
		ps.mu.Unlock()
		return pairReply{State: PairGone}
	}
	if o.Rejected {
		delete(ps.in, oid)
		ps.mu.Unlock()
		return pairReply{State: PairRejected}
	}
	if !o.Accepted {
		ps.mu.Unlock()
		return pairReply{State: PairWaiting}
	}
	onKey := ps.onKey
	kopya := *o
	ps.mu.Unlock()

	key, err := unseal(o.shared, w.Sealed)
	if err != nil || len(key) < 8 {
		return pairReply{State: PairGone, Reason: "mühür açılamadı"}
	}
	m.SetSecret(key)
	if onKey != nil {
		if err := onKey(key, kopya); err != nil && m.log != nil {
			m.log.Warnf("cluster: eşleştirme anahtarı saklanamadı: %v", err)
		}
	}
	ps.mu.Lock()
	delete(ps.in, oid)
	ps.mu.Unlock()
	if m.log != nil {
		m.log.Infof("cluster: %s ile kodla eşleşildi", kopya.Name)
	}
	return pairReply{OK: true, State: PairDone}
}
