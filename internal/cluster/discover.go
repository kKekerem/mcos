package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"mcos/internal/model"
)

// Bu dosya ETKİN taramayı ve ELLE eşleştirmeyi uygular.
//
// ── Neden pasif keşif yetmiyor ──────────────────────────────────────────────
// cluster.go, UDP çok noktaya yayın (multicast) ile 5 saniyede bir işaret
// yollar ve komşuların işaretlerini dinler. Bu ucuzdur ama ÜÇ durumda
// tamamen sessiz kalır:
//
//  1. Wi-Fi erişim noktalarının çoğu istemciler arası multicast'i engeller
//     ("client isolation" / "AP isolation" ayarı, birçok modemde VARSAYILAN).
//  2. Kablolu + kablosuz karışık ağlarda köprü multicast'i geçirmeyebilir.
//  3. Karşı taraf henüz açılmış ve ilk işaretini göndermemiş olabilir.
//
// Kullanıcı "otomatik ağda tarasın, bulamazsak IP girelim" dedi. İşte bu
// yüzden: ETKİN tarama, her adrese TCP ile tek tek bağlanmayı dener.
// Multicast engellense bile TCP çalışır.
//
// ── Neden yalnızca /24 ve daha dar ağlar ────────────────────────────────────
// /16 bir ağ 65 534 adres demektir; hepsini denemek dakikalar sürer ve
// ağ ekipmanını yorar. Ev ve küçük ofis ağları neredeyse her zaman /24'tür.
// Daha geniş bir maske görürsek YALNIZCA kendi adresimizin /24'ünü tararız
// ve bunu kullanıcıya söyleriz.

// scanConcurrency is how many hosts are probed at once.
//
// 128: tek bir /24 (254 adres) iki dalgada biter. Daha yükseği zayıf
// yönlendiricilerin ARP tablosunu taşırır ve paket kaybına yol açar.
const scanConcurrency = 128

// scanDialTimeout is how long one host probe may take.
//
// 400 ms: yerel ağda bir TCP el sıkışması 1-5 ms sürer. 400 ms, kablosuzda
// uyuyan bir cihaza bile yeter ama boş adreslerde beklemeyi kısa tutar
// (çoğu boş adres zaten anında RST veya "no route" döner).
const scanDialTimeout = 400 * time.Millisecond

// ScanProgress reports live scan state so the panel can animate.
type ScanProgress struct {
	// Total is how many addresses will be probed.
	Total int
	// Done is how many have finished.
	Done int
	// Found is how many MCOS nodes answered so far.
	Found int
}

// ScanLAN actively probes the local networks for MCOS peers.
//
// Bulunan her düğüm, eşleştirilmemiş olarak eş listesine eklenir; kullanıcı
// hangisiyle eşleşeceğine kendisi karar verir (otomatik eşleştirme YOK —
// bkz. upsertPeer'daki güvenlik notu).
//
// onProgress nil olabilir. ctx iptal edilirse tarama o ana kadar bulduklarıyla
// döner; yarıda kesilen bir tarama HATA DEĞİLDİR.
func (m *Manager) ScanLAN(ctx context.Context, onProgress func(ScanProgress)) ([]model.Peer, error) {
	targets, note, err := scanTargets(m.listenPort)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("taranacak ağ bulunamadı — kablo takılı mı, Wi-Fi bağlı mı?")
	}
	if note != "" && m.log != nil {
		m.log.Infof("cluster: %s", note)
	}

	var (
		mu    sync.Mutex
		found []model.Peer
		done  atomic.Int64
		hits  atomic.Int64
	)
	report := func() {
		if onProgress == nil {
			return
		}
		onProgress(ScanProgress{
			Total: len(targets),
			Done:  int(done.Load()),
			Found: int(hits.Load()),
		})
	}
	report()

	sem := make(chan struct{}, scanConcurrency)
	var wg sync.WaitGroup
	for _, ip := range targets {
		select {
		case <-ctx.Done():
			wg.Wait()
			return found, nil
		default:
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				done.Add(1)
				report()
			}()

			st, port, err := m.probeAnyPort(ctx, ip)
			if err != nil {
				return
			}
			_ = port
			if st.NodeName == "" || m.isSelf(st.NodeID, st.NodeName) {
				return // kendimiz veya MCOS olmayan bir servis
			}
			hits.Add(1)
			p := m.upsertProbedPort(st, ip, port)
			mu.Lock()
			found = append(found, p)
			mu.Unlock()
		}(ip)
	}
	wg.Wait()
	report()

	sort.Slice(found, func(i, j int) bool { return found[i].Name < found[j].Name })
	return found, nil
}

// nodeStatus is the "status" RPC reply.
type nodeStatus struct {
	// Bkz. identity.go: kimlik addan AYRIDIR ve kararlidir.
	NodeID   string `json:"nodeId"`
	NodeName string `json:"nodeName"`
	Version  string `json:"version"`
	Tier     string `json:"tier"`
	Cores    int    `json:"cores"`
	RAMMB    int    `json:"ramMB"`
	Role     string `json:"role"`
}

// probeNode asks one address whether it is an MCOS node.
//
// "status" kimlik doğrulaması istemez (salt-okunur) — keşfin çalışabilmesi
// için gereklidir. Gizli hiçbir şey döndürmez: yalnızca ad, sürüm ve
// donanım özeti.
func probeNode(ctx context.Context, ip string, port int) (nodeStatus, error) {
	var st nodeStatus
	d := net.Dialer{Timeout: scanDialTimeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return st, err
	}
	defer conn.Close()

	// Toplam süre sınırı: bağlantı kurulduktan sonra yanıt vermeyen bir
	// servis (yanlışlıkla aynı portu kullanan başka bir program) taramayı
	// kilitlememeli.
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if err := json.NewEncoder(conn).Encode(peerRequest{Method: "status"}); err != nil {
		return st, err
	}
	if err := json.NewDecoder(conn).Decode(&st); err != nil {
		return st, err
	}
	return st, nil
}

// probeFirstPort tries the given ports in order and returns the first answer.
func probeFirstPort(ctx context.Context, ip string, ports []int) (nodeStatus, int, error) {
	var lastErr error
	for _, p := range ports {
		st, err := probeNode(ctx, ip, p)
		if err == nil {
			return st, p, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("yanıt yok")
	}
	return nodeStatus{}, 0, lastErr
}

// probePorts lists the ports a node may be listening on.
//
// Iki port: yeni varsayilan (2222) ve yapilandirilmis port. Masaustu dugum
// uygulamasi her zaman 2222 dinler; daha once kurulmus bir MCOS kutusu ise
// hala 27890 kullaniyor olabilir.
func (m *Manager) probePorts() []int {
	seen := map[int]bool{}
	var out []int
	add := func(p int) {
		if p > 0 && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	add(model.PairingPort)
	add(m.listenPort)
	add(model.LegacyPairingPort)
	return out
}

// probeAnyPort tries each candidate port and returns the first that answers.
func (m *Manager) probeAnyPort(ctx context.Context, ip string) (nodeStatus, int, error) {
	return probeFirstPort(ctx, ip, m.probePorts())
}

// upsertProbed records a node found by active scanning.
func (m *Manager) upsertProbed(st nodeStatus, ip string) model.Peer {
	return m.upsertProbedPort(st, ip, m.listenPort)
}

func (m *Manager) upsertProbedPort(st nodeStatus, ip string, port int) model.Peer {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := peerKey(st.NodeID, st.NodeName, ip)
	p, ok := m.peers[id]
	if !ok {
		p = &model.Peer{ID: id, Name: st.NodeName, IP: ip, Port: port}
		m.peers[id] = p
	}
	p.IP = ip
	p.Port = port
	p.Name = st.NodeName
	p.Cores = st.Cores
	p.RAMMB = st.RAMMB
	p.State = model.PeerAvailable
	p.LastSeen = time.Now()
	// Paired ALANINA DOKUNULMAZ: tarama bir güven işlemi değildir.
	return *p
}

// AddManual pairs with a node at an explicitly typed address.
//
// Kullanıcının isteği: "bulamazsak ipyi girelim". Otomatik tarama
// başarısız olduğunda (farklı alt ağ, VPN, istemci yalıtımı) tek çıkış
// yolu budur.
//
// Adres "192.168.1.50" veya "192.168.1.50:27890" biçiminde olabilir.
// Eşleştirme, taramadan FARKLI olarak doğrudan Paired=true yapar: kullanıcı
// adresi elle yazarak niyetini zaten açıkça belirtmiştir.
func (m *Manager) AddManual(ctx context.Context, addr string) (model.Peer, error) {
	host, port, err := splitPeerAddr(addr, model.PairingPort)
	if err != nil {
		return model.Peer{}, err
	}
	// Adreste ":" varsa kullanici portu ACIKCA yazmistir; o zaman tahmin
	// yurutmeyiz.
	explicitPort := strings.Contains(strings.TrimSpace(addr), ":")

	// Bir ad verildiyse çöz: kullanıcı "mcos-2.local" yazabilmeli.
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil || len(ips) == 0 {
		return model.Peer{}, fmt.Errorf("adres çözülemedi: %s", host)
	}

	// Kullanici yalnizca bir IP yazdiysa HANGI portu kastettigini bilmiyoruz:
	// masaustu dugum uygulamasi 2222 dinler, daha once kurulmus bir MCOS
	// kutusu 27890 dinliyor olabilir. Ikisini de deniyoruz -- kullanicinin
	// port numarasi ezberlemesi gerekmemeli.
	portsToTry := []int{port}
	if !explicitPort {
		portsToTry = m.probePorts()
	}

	var lastErr error
	for _, ip := range ips {
		st, port, err := probeFirstPort(ctx, ip, portsToTry)
		if err != nil {
			lastErr = err
			continue
		}
		if st.NodeName == "" {
			lastErr = fmt.Errorf("%s bir MCOS düğümü gibi yanıt vermedi", ip)
			continue
		}
		// KIMLIKLE karsilastir, adla degil: iki taze kurulum da kendine
		// "mcos-1" der ve adla karsilastirmak, komsuyu "bu makinenin
		// kendisi" sanip eslestirmeyi biten bir cikmaza sokuyordu.
		if m.isSelf(st.NodeID, st.NodeName) || isLocalAddress(ip) {
			return model.Peer{}, fmt.Errorf("bu adres bu makinenin kendisi")
		}

		m.mu.Lock()
		id := peerKey(st.NodeID, st.NodeName, ip)
		p, ok := m.peers[id]
		if !ok {
			p = &model.Peer{ID: id, Name: st.NodeName, IP: ip}
			m.peers[id] = p
		}
		p.IP = ip
		p.Name = st.NodeName
		p.Port = port
		p.Cores = st.Cores
		p.RAMMB = st.RAMMB
		p.State = model.PeerAvailable
		p.LastSeen = time.Now()
		p.Paired = true
		out := *p
		m.mu.Unlock()

		m.savePeers()
		if m.log != nil {
			m.log.Infof("cluster: %s (%s) elle eşleştirildi", st.NodeName, ip)
		}
		return out, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("yanıt yok")
	}
	return model.Peer{}, fmt.Errorf("%s adresine bağlanılamadı: %w", addr, lastErr)
}

// splitPeerAddr parses "host" or "host:port".
func splitPeerAddr(addr string, defPort int) (string, int, error) {
	if addr == "" {
		return "", 0, fmt.Errorf("adres boş")
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		// Port yok: tamamı ana makine adıdır.
		return addr, defPort, nil
	}
	p, err := strconv.Atoi(portStr)
	if err != nil || p <= 0 || p > 65535 {
		return "", 0, fmt.Errorf("geçersiz port: %s", portStr)
	}
	return host, p, nil
}

// scanTargets lists every IPv4 address worth probing on the local networks.
//
// Kendi adresimiz, yayın adresi ve ağ adresi atlanır. Geri döngü (127.x) ve
// bağlantı-yerel (169.254.x) ağlar da atlanır: oralarda başka bir makine
// olamaz.
func scanTargets(port int) ([]string, string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, "", err
	}

	seen := map[string]bool{}
	var out []string
	note := ""

	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipn.IP.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}
			ones, bits := ipn.Mask.Size()
			if bits != 32 {
				continue
			}
			if ones < 24 {
				// Çok geniş ağ: yalnızca kendi /24'ümüzü tara.
				note = fmt.Sprintf(
					"ağ /%d çok geniş; yalnızca %d.%d.%d.0/24 taranıyor",
					ones, ip4[0], ip4[1], ip4[2])
				ones = 24
				ipn = &net.IPNet{IP: ip4, Mask: net.CIDRMask(24, 32)}
			}
			if ones > 30 {
				continue // /31, /32: tarayacak komşu yok
			}

			base := ip4.Mask(ipn.Mask)
			count := 1 << uint(32-ones)
			for i := 1; i < count-1; i++ { // ağ ve yayın adreslerini atla
				cand := net.IPv4(base[0], base[1], base[2], base[3]).To4()
				v := uint32(cand[0])<<24 | uint32(cand[1])<<16 |
					uint32(cand[2])<<8 | uint32(cand[3])
				v += uint32(i)
				s := fmt.Sprintf("%d.%d.%d.%d",
					byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
				if s == ip4.String() || seen[s] {
					continue
				}
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out, note, nil
}
