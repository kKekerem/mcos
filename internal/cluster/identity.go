package cluster

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strings"

	"mcos/internal/log"
	"mcos/internal/store"
)

// Bu dosya tek bir soruyu yanıtlar: "bu düğüm ben miyim?"
//
// ════════════════════════════════════════════════════════════════════════════
// YAKALANAN GERÇEK HATA
// ════════════════════════════════════════════════════════════════════════════
//
// Kimlik eskiden GÖRÜNEN ADLA belirleniyordu (cfg.Cluster.NodeName). Ama o ad
// her kurulumda aynı geliyor: model.DefaultConfig() onu "mcos-1" olarak
// sabitliyor ve kurulum sihirbazı yalnızca boşsa üzerine yazıyordu — hiç boş
// olmadığı için de asla yazmıyordu.
//
// Sonuç: İKİ TAZE MCOS MAKİNESİ BİRBİRİYLE HİÇ EŞLEŞEMİYORDU.
//
//	PC-A ve PC-B'nin ikisi de kendine "mcos-1" diyor.
//	 • Tarama: PC-B'nin yanıtı "kendimiz" sanılıp atılıyor → "0 cihaz bulundu"
//	 • Çoklu yayın: komşunun işareti "kendi işaretimiz" sanılıp atılıyor
//	 • Elle IP girme (tam da bu durum için var olan son çare):
//	   "bu adres bu makinenin kendisi" — düpedüz yanlış
//
// Yani PC eşleştirme ve ona bağlı ortak dünya özelliği, varsayılan ayarlarla
// ULAŞILAMAZ durumdaydı.
//
// Çözüm: görünen ad artık yalnızca bir ETİKET. Kimlik, kurulumda bir kez
// üretilip diske yazılan rastgele bir kimliktir. Kullanıcı iki makineye de
// aynı adı verse bile eşleştirme çalışır.

// nodeIDBytes is how much randomness the node identity carries.
//
// 16 bayt = 128 bit: çakışma olasılığı, ev ağındaki birkaç makine için
// pratikte sıfır.
const nodeIDBytes = 16

// ensureNodeID loads this machine's stable identity, creating it once.
//
// Diske yazılır (store.Paths.NodeIDFile), çünkü kimliğin YENİDEN BAŞLATMAYA
// DAYANMASI gerekir: her açılışta değişse, eşleştirilmiş komşular bizi yeni
// bir makine sanardı.
func ensureNodeID(st *store.Store, lg *log.Logger) string {
	if st == nil {
		// Depo yoksa (testler) bellekte kalan bir kimlik yine de ada
		// düşmekten iyidir.
		return randomID()
	}
	path := st.Paths.NodeIDFile()
	if b, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(b)); id != "" {
			return id
		}
	}

	id := randomID()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(id+"\n"), 0o600); err == nil {
			if err := os.Rename(tmp, path); err != nil {
				os.Remove(tmp)
			}
		}
	}
	if lg != nil {
		lg.Infof("cluster: düğüm kimliği üretildi (%s…)", id[:8])
	}
	return id
}

func randomID() string {
	b := make([]byte, nodeIDBytes)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand başarısız olursa kimlik üretemeyiz; boş dönmek,
		// isSelf'i ada düşürür ve eski davranışa geri döneriz.
		return ""
	}
	return hex.EncodeToString(b)
}

// isSelf reports whether a remote node is this machine.
//
// Kimlik varsa YALNIZCA kimliğe bakılır. Ada düşmek yalnızca karşı taraf
// kimlik göndermiyorsa (1.0.1 öncesi bir düğüm) söz konusudur; iki 1.0.1
// makinesi arasında bu yola hiç girilmez.
func (m *Manager) isSelf(remoteID, remoteName string) bool {
	if remoteID != "" && m.nodeID != "" {
		return remoteID == m.nodeID
	}
	return remoteName != "" && remoteName == m.nodeName
}

// peerKey is the stable map key for a peer.
//
// Kimlik varsa ONA dayanır: böylece aynı makine IP değiştirdiğinde (DHCP
// yenilemesi, kablodan Wi-Fi'ye geçiş) ikinci bir kayıt olarak görünmez.
func peerKey(nodeID, name, ip string) string {
	if nodeID != "" {
		return "id:" + nodeID
	}
	return name + "@" + ip
}

// isLocalAddress reports whether ip belongs to this machine.
//
// İkinci bir emniyet: kullanıcı elle eşleştirmede KENDİ adresini yazarsa
// "bu adres bu makinenin kendisi" demek doğrudur. Kimlik denetimi bunu zaten
// yakalar, ama bu denetim düğüm hiç yanıt vermeden önce çalışır ve hata
// mesajını doğru kılar.
func isLocalAddress(ip string) bool {
	target := net.ParseIP(ip)
	if target == nil {
		return false
	}
	if target.IsLoopback() {
		return true
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.Equal(target) {
			return true
		}
	}
	return false
}
