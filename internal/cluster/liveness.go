package cluster

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"mcos/internal/model"
)

// Bu dosya eşleştirilmiş komşuların HAYATTA KALMASINI sağlar: hem çalışırken
// (yoklama) hem de yeniden başlatmalar arasında (kalıcılık).

// ════════════════════════════════════════════════════════════════════════════
// 1. YOKLAMA — "eş çevrimdışı" yalanını düzeltir
// ════════════════════════════════════════════════════════════════════════════
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
// LastSeen yalnızca üç yerde yazılıyordu: çoklu yayın işareti, kullanıcının
// başlattığı tarama, ve elle eşleştirme. Sürekli çalışan hiçbir canlılık
// denetimi yoktu. janitorLoop ise 10 saniyede bir çalışıp 20 saniyeden eski
// her eşi PeerOffline yapıyordu.
//
// Ev modemlerinin çoğu istemciler arası çoklu yayını engeller (AP isolation)
// — zaten elle eşleştirmenin var olma nedeni bu. Böyle bir ağda:
//
//	kullanıcı PC-B'yi IP yazarak eşleştirir  → ekranda "kullanılabilir"
//	~20 saniye sonra                          → "çevrimdışı"
//	ve BİR DAHA ASLA geri gelmez.
//
// Sonuç: pickHelper nil döner, iş aktarımı hiç olmaz, ortak dünya
// topolojisi eşi ölü sayar. Eşleştirme çalışıyor gibi görünüp sessizce
// ölüyordu.
//
// Artık çoklu yayın bir ENİYİLEME; canlılığın kaynağı doğrudan TCP yoklaması.

// peerProbeInterval is how often paired peers are checked.
//
// peerTimeout 20 sn; 8 sn'de bir yoklama, tek bir kayıp yanıtın eşi
// çevrimdışı göstermesine izin vermez (iki deneme sığar).
const peerProbeInterval = 8 * time.Second

// peerProbeTimeout bounds one probe so a dead peer cannot stall the loop.
const peerProbeTimeout = 3 * time.Second

func (m *Manager) healthLoop() {
	ticker := time.NewTicker(peerProbeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.probePairedPeers()
		}
	}
}

// probePairedPeers refreshes LastSeen for every paired peer that answers.
func (m *Manager) probePairedPeers() {
	// Hedefleri KİLİT ALTINDA kopyala, ağ işini kilit dışında yap: bir
	// yoklama 3 saniye sürebilir ve o sürede panel eş listesini okuyamazdı.
	type target struct {
		key  string
		ip   string
		port int
	}
	m.mu.Lock()
	var targets []target
	for key, p := range m.peers {
		if p.Paired && p.IP != "" {
			// Port bilinmiyorsa KENDI portumuz degil, standart eslestirme
			// portu denenir: esin bizimle ayni portta dinlediginin hicbir
			// garantisi yok.
			port := p.Port
			if port == 0 {
				port = model.PairingPort
			}
			targets = append(targets, target{key: key, ip: p.IP, port: port})
		}
	}
	m.mu.Unlock()

	for _, t := range targets {
		select {
		case <-m.ctx.Done():
			return
		default:
		}

		ctx, cancel := context.WithTimeout(m.ctx, peerProbeTimeout)
		st, err := probeNode(ctx, t.ip, t.port)
		cancel()
		if err != nil {
			// Yanıt yok: janitor'a bırak. Burada çevrimdışı İŞARETLEMİYORUZ,
			// çünkü tek bir kayıp paket eşi düşürmemeli.
			continue
		}

		m.mu.Lock()
		if p, ok := m.peers[t.key]; ok {
			p.LastSeen = time.Now()
			p.State = model.PeerAvailable
			if st.Cores > 0 {
				p.Cores = st.Cores
			}
			if st.RAMMB > 0 {
				p.RAMMB = st.RAMMB
			}
			if st.NodeName != "" {
				p.Name = st.NodeName
			}
		}
		m.mu.Unlock()
	}
}

// ════════════════════════════════════════════════════════════════════════════
// 2. KALICILIK — eşleştirme yeniden başlatmayı atlatmalı
// ════════════════════════════════════════════════════════════════════════════
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
// Eşleştirme durumu YALNIZCA BELLEKTEYDİ. mcosd her yeniden başladığında
// (güncelleme, çökme, makinenin kapanıp açılması) bütün eşleştirmeler
// sessizce siliniyordu. Kullanıcı ertesi gün paneli açtığında eş listesi
// boştu ve ortak dünya durmuştu — hiçbir hata mesajı olmadan.
//
// Yalnızca EŞLEŞTİRİLMİŞ eşler yazılır: keşfedilenler zaten geçicidir ve
// her açılışta yeniden bulunur.

type persistedPeer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	IP    string `json:"ip"`
	Port  int    `json:"port"`
	Cores int    `json:"cores,omitempty"`
	RAMMB int    `json:"ramMB,omitempty"`
}

// savePeers writes the paired set to disk.
//
// Sessizce başarısız olur: eşleştirme çalışmaya devam etmeli, yalnızca
// yeniden başlatmaya dayanmaz. Diske yazamamak, çalışan bir özelliği
// durdurmak için yeterli bir sebep değil.
func (m *Manager) savePeers() {
	if m.store == nil {
		return
	}
	m.mu.Lock()
	var out []persistedPeer
	for _, p := range m.peers {
		if p.Paired {
			out = append(out, persistedPeer{
				ID: p.ID, Name: p.Name, IP: p.IP, Port: p.Port,
				Cores: p.Cores, RAMMB: p.RAMMB,
			})
		}
	}
	m.mu.Unlock()

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return
	}
	path := m.store.Paths.PeersFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	// Geçici dosya + yeniden adlandırma: yazma sırasında elektrik giderse
	// yarım bir dosya kalmasın.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
	}
}

// loadPeers restores the paired set at startup.
//
// Geri yüklenen eşler ÇEVRİMDIŞI başlar: gerçekten açık olup olmadıklarını
// bilmiyoruz. İlk yoklama (healthLoop) birkaç saniye içinde durumu düzeltir.
func (m *Manager) loadPeers() {
	if m.store == nil {
		return
	}
	b, err := os.ReadFile(m.store.Paths.PeersFile())
	if err != nil {
		return
	}
	var in []persistedPeer
	if err := json.Unmarshal(b, &in); err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, pp := range in {
		if pp.ID == "" {
			continue
		}
		if _, exists := m.peers[pp.ID]; exists {
			continue
		}
		m.peers[pp.ID] = &model.Peer{
			ID: pp.ID, Name: pp.Name, IP: pp.IP, Port: pp.Port,
			Cores: pp.Cores, RAMMB: pp.RAMMB,
			Paired: true,
			State:  model.PeerOffline,
		}
	}
	if m.log != nil && len(in) > 0 {
		m.log.Infof("cluster: %d eşleştirilmiş cihaz geri yüklendi", len(in))
	}
}
