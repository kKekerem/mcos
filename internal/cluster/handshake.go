package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"syscall"
	"time"

	"mcos/internal/model"
)

// Bu dosya EŞLEŞME EL SIKIŞMASINI ("hello") ve hataların kullanıcıya
// anlatılmasını uygular.
//
// ════════════════════════════════════════════════════════════════════════════
// YAKALANAN GERÇEK HATALAR
// ════════════════════════════════════════════════════════════════════════════
//
//  1. Eşleştirme TEK TARAFLIYDI. MCOS paneli "EŞLEŞTİ" diyordu ama bu
//     yalnızca yerel bir bayraktı: anahtar hiç sınanmıyordu. Anahtar yanlışsa
//     bu ancak kullanıcı ortak dünyayı açtığında "yetkisiz" diye ortaya
//     çıkıyordu. Artık eşleşme anında anahtar karşı tarafta doğrulanıyor.
//
//  2. Düğüm (cmd/mcos-node) MCOS'u KİMLİKSİZ kaydediyordu: ad "host", port
//     tahmini 2222. Ortak dünya dilimleri ADA göre sıralandığı için iki
//     taraf farklı sıralar hesaplayabiliyordu — ör. düğüm adı "kkekerem-pc"
//     iken MCOS [kkekerem-pc, mcos-1], düğüm ise [host, kkekerem-pc]
//     görüyordu ve İKİ MAKİNE DE dünyanın x ≥ 0 yarısını sahipleniyordu.
//     El sıkışması artık çağıranın kimliğini, adını ve portunu taşıyor.
//
//  3. Hata nedenleri kayboluyordu: kullanıcı "eşleştirilemedi: dial tcp
//     10.0.0.5:2222: i/o timeout" görüyordu. Bunun Türkçesi "güvenlik
//     duvarı 2222'yi engelliyor olabilir"dir; describeNetErr bunu söyler.

// LinkProto is the pairing/shared-world wire protocol version.
//
// Uygulama sürümünden AYRI: iki makine farklı MCOS sürümlerinde olabilir ve
// yine de anlaşabilir; yalnızca TEL biçimi değiştiğinde bu sayı artar.
// 2: "hello" el sıkışması, çağıran kimliği ve kurucu katılımcı listesi.
const LinkProto = 2

// peerHello identifies the caller in hello/linkSpec requests.
type peerHello struct {
	NodeID   string `json:"nodeId"`
	NodeName string `json:"nodeName"`
	// Port, çağıranın KENDİ eşleştirme portudur. Karşı taraf bizi yoklarken
	// tahmin etmek yerine bunu kullanır.
	Port    int    `json:"port"`
	Version string `json:"version"`
	Proto   int    `json:"proto"`
}

// helloReply is the answer to a hello.
type helloReply struct {
	OK bool `json:"ok"`
	// Reason, OK=false iken makine tarafından okunacak nedendir
	// (reasonKey, reasonNoKey, reasonUnpaired).
	Reason string `json:"reason,omitempty"`
	// Paired: karşı taraf bizi eşleştirilmiş sayıyor mu.
	Paired   bool   `json:"paired"`
	NodeID   string `json:"nodeId"`
	NodeName string `json:"nodeName"`
	Version  string `json:"version"`
	Proto    int    `json:"proto"`
	// Error, eski sürümlerin "bilinmeyen metot" yanıtını yakalamak için.
	Error string `json:"error,omitempty"`
}

// Makine tarafından okunan ret nedenleri. Tel üzerinde gider: DEĞİŞTİRMEYİN.
const (
	reasonKey      = "key"      // anahtar yanlış
	reasonNoKey    = "no-key"   // karşı tarafta anahtar yok
	reasonUnpaired = "unpaired" // anahtar doğru ama karşı taraf eşleştirmedi
)

// authError is a rejected request with a machine-readable reason.
type authError struct {
	Reason string
	Msg    string
}

func (e *authError) Error() string { return e.Msg }

// selfHello describes this node to a peer.
func (m *Manager) selfHello() *peerHello {
	return &peerHello{
		NodeID:   m.nodeID,
		NodeName: m.name(),
		Port:     m.listenPort,
		Version:  m.version,
		Proto:    LinkProto,
	}
}

// NodeID returns this machine's stable identity.
func (m *Manager) NodeID() string { return m.nodeID }

// NodeName returns this machine's display name.
func (m *Manager) NodeName() string { return m.name() }

func (m *Manager) name() string {
	v, _ := m.nodeNameV.Load().(string)
	return v
}

// SetNodeName changes the announced name while running.
//
// ── Yakalanan gerçek hata (uçtan uca sınamada ölçüldü) ──────────────────────
// Ad yalnızca kurucuda (NewManager) okunuyordu. Kullanıcı kurulum
// sihirbazında ya da Ayarlar'da PC adını "mcos-kutu" yaptığında config.json
// güncelleniyor ama eşleştirme hâlâ "mcos-1" duyuruyordu: düğüm MCOS'u
// "mcos-1" diye kaydetti, ortak dünya dilimi de o adla açıldı. İki taze
// kurulumun ikisi de "mcos-1" olduğundan bu, iki makineyi ayırt etmenin
// tek insan-okur yolunu da siliyordu.
//
// Dönüş: ad gerçekten değişti mi. Değiştiyse ortak dünya eşitlemesi
// dürtülür — katılımcı listesi ve dilim adları adla hesaplanıyor.
func (m *Manager) SetNodeName(n string) bool {
	n = strings.TrimSpace(n)
	if n == "" || n == m.name() {
		return false
	}
	old := m.name()
	m.nodeNameV.Store(n)
	if m.log != nil {
		m.log.Infof("cluster: PC adı %q → %q", old, n)
	}
	if c := m.LinkCoord(); c != nil {
		c.Kick()
	}
	return true
}

// sayHello performs the authenticated handshake with one address.
func (m *Manager) sayHello(ctx context.Context, ip string, port int) (helloReply, error) {
	var rep helloReply
	d := net.Dialer{Timeout: 4 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return rep, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(6 * time.Second))
	if err := json.NewEncoder(conn).Encode(peerRequest{
		Method: "hello", Token: m.Secret(), From: m.selfHello(),
	}); err != nil {
		return rep, err
	}
	if err := json.NewDecoder(conn).Decode(&rep); err != nil {
		return rep, err
	}
	return rep, nil
}

// helloProblem turns a hello outcome into a user-facing Turkish sentence.
//
// Boş dönüş: sorun yok. İkinci dönüş: eşleştirme YEREL olarak yapılabilir mi
// (anahtar doğru ama karşı taraf henüz eşleştirmediyse evet — sorun yalnızca
// karşı tarafta bir adım eksik olması).
func (m *Manager) helloProblem(rep helloReply, err error, port int) (string, bool) {
	if err != nil {
		return describeNetErr(err, port), false
	}
	if rep.Error != "" && rep.Proto == 0 && rep.NodeID == "" {
		// Eski sürüm "hello"yu tanımıyor.
		return versionProblem(m.version, ""), false
	}
	if rep.Proto != LinkProto {
		return versionProblem(m.version, rep.Version), false
	}
	if rep.OK {
		return "", true
	}
	switch rep.Reason {
	case reasonKey:
		return "anahtar yanlış — o cihaza bu MCOS'un eşleştirme anahtarını girin " +
			"(MCOS Paylaşım → Eşleştirme anahtarını göster)", false
	case reasonNoKey:
		return "karşı cihazda eşleştirme anahtarı yok — orada PC paylaşımını açın", false
	case reasonUnpaired:
		return "anahtar doğru ama karşı MCOS bu cihazı henüz eşleştirmedi — " +
			"orada da MCOS Paylaşım ekranından bu cihazı eşleştirin", true
	}
	return "karşı cihaz eşleşmeyi reddetti", false
}

// versionProblem explains a protocol mismatch.
func versionProblem(ours, theirs string) string {
	if theirs == "" {
		theirs = "eski bir sürüm"
	}
	return fmt.Sprintf("farklı sürüm (bu cihaz %s, karşı cihaz %s) — "+
		"ikisini de aynı MCOS sürümüne güncelleyin", ours, theirs)
}

// rejectProblem explains a linkSpec/assignTask rejection by reason code.
func rejectProblem(reason, fallback string) string {
	switch reason {
	case reasonKey:
		return "anahtar yanlış — o cihaza bu MCOS'un eşleştirme anahtarını girin"
	case reasonNoKey:
		return "karşı cihazda eşleştirme anahtarı yok"
	case reasonUnpaired:
		return "karşı cihaz bu MCOS'u eşleştirmemiş — orada da eşleştirin"
	}
	if fallback == "" {
		return "karşı cihaz reddetti"
	}
	return fallback
}

// describeNetErr says in plain Turkish WHY a peer could not be reached.
//
// Üç ayrı durum var ve çözümleri farklı:
//   - reddedildi: makine açık ama o portta kimse dinlemiyor → program
//     çalışmıyor ya da port farklı;
//   - zaman aşımı: paketler yutuluyor → neredeyse her zaman güvenlik duvarı
//     (Windows Defender, 2222'yi varsayılan olarak engeller);
//   - yol yok: ağ farklı ya da cihaz kapalı.
func describeNetErr(err error, port int) string {
	if err == nil {
		return ""
	}
	s := strings.ToLower(err.Error())
	var ne net.Error
	// Windows hata METNİ yerelleştirilmiştir (Türkçe Windows: "Hedef makine
	// etkin olarak reddettiğinden bağlantı kurulamadı") ve Go'nun
	// syscall.ECONNREFUSED'u Windows'taki WSAECONNREFUSED (10061) ile
	// eşleşmez. Windows'ta ölçüldü: bu yüzden ham hata gösteriliyordu.
	// Sayısal WSA kodlarına bakıyoruz; Linux'ta bu sayılar hiçbir errno'ya
	// denk gelmez.
	var errno syscall.Errno
	hasErrno := errors.As(err, &errno)
	isErrno := func(codes ...syscall.Errno) bool {
		if !hasErrno {
			return false
		}
		for _, c := range codes {
			if errno == c {
				return true
			}
		}
		return false
	}
	const (
		wsaECONNRESET   syscall.Errno = 10054
		wsaETIMEDOUT    syscall.Errno = 10060
		wsaECONNREFUSED syscall.Errno = 10061
		wsaENETUNREACH  syscall.Errno = 10051
		wsaEHOSTUNREACH syscall.Errno = 10065
	)
	switch {
	case errors.Is(err, syscall.ECONNREFUSED) || isErrno(wsaECONNREFUSED) ||
		strings.Contains(s, "refused"):
		return fmt.Sprintf("bağlantı reddedildi — o bilgisayarda mcos-node/MCOS "+
			"çalışmıyor ya da %d portunu dinlemiyor", port)
	case (errors.As(err, &ne) && ne.Timeout()) || isErrno(wsaETIMEDOUT) ||
		strings.Contains(s, "timeout") || strings.Contains(s, "timed out"):
		return fmt.Sprintf("yanıt yok — güvenlik duvarı %d portunu engelliyor "+
			"olabilir (Windows'ta bir kez \"mcos-node --kur\" çalıştırın)", port)
	case errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) ||
		isErrno(wsaEHOSTUNREACH, wsaENETUNREACH) ||
		strings.Contains(s, "no route") || strings.Contains(s, "unreachable"):
		return "cihaza ulaşılamıyor — kapalı olabilir ya da iki cihaz aynı ağda değil"
	case strings.Contains(s, "no such host"):
		return "adres çözülemedi — IP adresini doğru yazdığınızdan emin olun"
	case strings.Contains(s, "eof") || strings.Contains(s, "reset") || isErrno(wsaECONNRESET):
		return fmt.Sprintf("bağlantı koptu — %d portunda MCOS olmayan başka bir "+
			"program olabilir", port)
	}
	return "bağlanılamadı: " + err.Error()
}

// ── Eşe ait sorunlar ────────────────────────────────────────────────────────

// setNetProblem records (or clears, with "") a reachability problem.
func (m *Manager) setNetProblem(id, msg string) {
	m.mu.Lock()
	if msg == "" {
		delete(m.netProb, id)
	} else {
		m.netProb[id] = msg
	}
	m.mu.Unlock()
}

// setLinkProblem records (or clears) a pairing/shared-world problem.
//
// Ağ sorunundan AYRI tutulur: başarılı bir yoklama "yanıt yok"u siler ama
// "anahtar yanlış"ı silmemeli — makine yanıt veriyor diye anahtar düzelmez.
func (m *Manager) setLinkProblem(id, msg string) {
	m.mu.Lock()
	if msg == "" {
		delete(m.linkProb, id)
	} else {
		m.linkProb[id] = msg
	}
	m.mu.Unlock()
}

// SetLinkProblem is the exported form for the daemon/node.
func (m *Manager) SetLinkProblem(id, msg string) { m.setLinkProblem(id, msg) }

// problemOf composes what the panel shows for one peer. Caller holds m.mu.
func (m *Manager) problemOf(id string) string {
	if p := m.netProb[id]; p != "" {
		return p
	}
	return m.linkProb[id]
}

// pairCaller records a caller that proved it knows the pairing key.
//
// Kimlik (from) varsa kayıt ONA göre tutulur; eskiden burada "ip:<adres>"
// adlı, adı "host" olan bir kayıt açılıyordu (bkz. dosya başı, hata 2).
func (m *Manager) pairCaller(ip string, from *peerHello) {
	m.mu.Lock()
	key := ""
	if from != nil && (from.NodeID != "" || from.NodeName != "") {
		key = peerKey(from.NodeID, from.NodeName, ip)
	}
	var p *model.Peer
	if key != "" {
		p = m.peers[key]
	}
	if p == nil {
		for k, q := range m.peers {
			if q.IP != ip {
				continue
			}
			// Kimliği BİLİNEN başka bir makineyi ezme: aynı NAT'ın
			// arkasında iki makine aynı adresten gelebilir.
			if key != "" && strings.HasPrefix(k, "id:") && k != key {
				continue
			}
			p = q
			if key != "" && k != key {
				delete(m.peers, k)
			}
			break
		}
	}
	isNew := p == nil
	if p == nil {
		p = &model.Peer{Port: model.PairingPort, Name: "MCOS"}
	}
	if key == "" {
		key = p.ID
		if key == "" {
			key = "ip:" + ip
		}
	}
	p.ID = key
	p.IP = ip
	if from != nil {
		if from.NodeName != "" {
			p.Name = from.NodeName
		}
		if from.Port > 0 {
			p.Port = from.Port
		}
		if from.Version != "" {
			p.Version = from.Version
		}
	}
	wasPaired := p.Paired
	p.Paired = true
	p.State = model.PeerAvailable
	p.LastSeen = time.Now()
	m.peers[key] = p
	m.mu.Unlock()

	if (isNew || !wasPaired) && m.log != nil {
		m.log.Infof("cluster: %s (%s) anahtarla eşleşti", p.Name, ip)
	}
	go m.savePeers()
}

// verifyPeer runs the handshake against an already-listed peer.
//
// Panelden "Eşleştir" denildiğinde çağrılır (Pair). Sonuç eşin Problem
// alanına yazılır; başarılıysa ortak dünya eşitlemesi dürtülür, böylece
// açık bir ortak dünya yeni eşe HEMEN kurulur.
func (m *Manager) verifyPeer(id, ip string, port int) {
	if ip == "" {
		return
	}
	if port == 0 {
		port = model.PairingPort
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	rep, err := m.sayHello(ctx, ip, port)
	msg, _ := m.helloProblem(rep, err, port)
	// Sonuç GÜNLÜĞE de yazılır: panel yalnızca son durumu gösterir; bir
	// kullanıcı "dün eşleşmişti, bugün olmuyor" dediğinde elde neden
	// olmalı.
	if m.log != nil {
		if msg == "" {
			m.log.Infof("cluster: %s:%d ile eşleşme doğrulandı (%s)", ip, port, rep.NodeName)
		} else {
			m.log.Warnf("cluster: %s:%d eşleşmesi: %s", ip, port, msg)
		}
	}
	if err != nil {
		m.setNetProblem(id, msg)
		return
	}
	m.setNetProblem(id, "")
	m.setLinkProblem(id, msg)
	m.mu.Lock()
	if p, ok := m.peers[id]; ok && rep.Version != "" {
		p.Version = rep.Version
	}
	m.mu.Unlock()
	if msg == "" {
		if c := m.LinkCoord(); c != nil {
			c.Kick()
		}
	}
}
