package model

import (
	"math"
	"time"
)

// ════════════════════════════════════════════════════════════════════════════
// MCOS LINK — birden çok PC'nin AYNI dünyayı çalıştırması
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının isteği aynen şuydu:
//
//	"iki sunucu aynı dünyayı calıstıracak ama dünyanın yarısı diğer pc de
//	 calısırken diğeri diğer pc de, ancak aynı dünya olacak, tek bi sw gibi
//	 gorunecek; sw1 den sw2 nin alanına gidince orada gecis yapacaz;
//	 pc eslestirme sayısı sınırsız olacak"
//
// ── Nasıl çalışıyor ─────────────────────────────────────────────────────────
//
// Dünya, X ekseni boyunca DİLİMLERE bölünür. Her düğüm bir dilimin sahibidir
// ve YALNIZCA kendi dilimini simüle eder:
//
//	    düğüm 0          düğüm 1          düğüm 2
//	 ───────────────┼────────────────┼───────────────►  chunkX
//	   x < -16          -16..16          x >= 16
//
// Oyuncu sınırı geçtiğinde, sahibi olan düğüm:
//
//  1. oyuncunun tüm durumunu (envanter, can, XP, konum) hedef düğüme yollar,
//  2. istemciye Minecraft'ın kendi "transfer" paketini gönderir.
//
// İstemci HİÇ dünya ekranından çıkmadan öbür sunucuya bağlanır. Bu, oyunun
// 1.20.5'te eklediği gerçek bir özelliktir (ServerTransferS2CPacket) — MCOS
// bir hile uydurmuyor, protokolün kendi mekanizmasını kullanıyor.
//
// ── Neden X ekseni dilimleri, neden karo/hash değil? ────────────────────────
// Sahipliği karoya göre hash'lemek, oyuncunun her 16 blokta bir başka
// makineye atlamasına yol açar — saniyede bir sunucu değiştirmek demektir.
// Bitişik dilimler, kullanıcının tarif ettiği "SW1'den SW2'nin alanına
// gitmek" davranışını verir: geçiş NADİRDİR ve fark edilir bir yerdedir.
//
// ── Neden sınırsız düğüm? ───────────────────────────────────────────────────
// Sınır düzlemleri düğüm sayısından TÜRETİLİR (bkz. Territories). İki
// düğümde sınır x=0'dır; üç düğümde iki sınır oluşur. Tablo yok, sabit yok.

// LinkMode selects how a server uses paired machines.
type LinkMode string

const (
	// LinkOff: eşler yalnızca yedek/analiz işleri alır (klasik davranış).
	LinkOff LinkMode = "off"
	// LinkOffload: ağır yan işler (yedekleme, günlük analizi) eşe gider,
	// dünya tek makinede kalır.
	LinkOffload LinkMode = "offload"
	// LinkSharedWorld: dünya dilimlere bölünür, her düğüm kendi dilimini
	// çalıştırır. Kullanıcının asıl istediği kip.
	LinkSharedWorld LinkMode = "shared-world"
)

// LinkDifficulty mirrors Minecraft's difficulty so the panel can offer it
// when a shared world is created.
//
// Kullanıcının isteği: "hatta zorluğu bile secebilecez". Zorluk TÜM
// düğümlerde aynı olmak zorundadır: bir dilimde peaceful, diğerinde hard
// olsaydı oyuncu sınırı geçtiğinde canavarların kaybolduğunu görürdü.
type LinkDifficulty string

const (
	DifficultyPeaceful LinkDifficulty = "peaceful"
	DifficultyEasy     LinkDifficulty = "easy"
	DifficultyNormal   LinkDifficulty = "normal"
	DifficultyHard     LinkDifficulty = "hard"
)

// DifficultyLabel returns the Turkish display name.
func DifficultyLabel(d LinkDifficulty) string {
	switch d {
	case DifficultyPeaceful:
		return "Barışçıl"
	case DifficultyEasy:
		return "Kolay"
	case DifficultyHard:
		return "Zor"
	default:
		return "Normal"
	}
}

// AllDifficulties is the selectable list, in increasing order.
var AllDifficulties = []LinkDifficulty{
	DifficultyPeaceful, DifficultyEasy, DifficultyNormal, DifficultyHard,
}

// DefaultSlabChunks is how wide each node's territory is, in chunks.
//
// 32 chunk = 512 blok. Bu sayı bilerek büyük: oyuncunun sınırı kazara
// geçip geri dönmesi (ve iki kez aktarılması) mümkün olmamalı. 512 blok,
// normal bir oyun oturumunda kolay kolay aşılmaz.
const DefaultSlabChunks = 32

// HandoffHysteresisChunks is how far past the border a player must be before
// the transfer fires.
//
// NEDEN GEREKLİ: sınırın tam üstünde duran bir oyuncu, bir adım ileri bir
// adım geri gittiğinde iki makine arasında sonsuz döngüye girerdi. 2 chunk
// (32 blok) gecikme bunu imkânsız kılar.
const HandoffHysteresisChunks = 2

// LinkConfig is the shared-world setup for one server.
type LinkConfig struct {
	// Mode selects off / offload / shared-world.
	Mode LinkMode `json:"mode"`
	// Difficulty applies to EVERY node running this world.
	Difficulty LinkDifficulty `json:"difficulty,omitempty"`
	// SlabChunks is the territory width in chunks (0 = DefaultSlabChunks).
	SlabChunks int `json:"slabChunks,omitempty"`
	// Nodes lists the participating node names, in a STABLE order.
	//
	// Sıra önemlidir: dilim sahipliği bu sıradan hesaplanır. Sıra değişirse
	// dünyanın yarısı el değiştirir, bu yüzden liste yalnızca düğüm
	// eklenirken SONUNA eklenerek büyütülür.
	Nodes []string `json:"nodes,omitempty"`
	// Seed is the shared world seed, so every node generates the same terrain.
	Seed string `json:"seed,omitempty"`
	// LinkPort is the TCP port the Minecraft-side mod listens on.
	LinkPort int `json:"linkPort,omitempty"`
}

// PairingPort is where MCOS nodes accept pairing and shared-world pushes.
//
// 2222: kullanicinin sectigi port. Hem MCOS kutusu hem masaustu dugum
// uygulamasi (cmd/mcos-node) burayi dinler, boylece "eslestirme portu" diye
// tek bir sayi vardir -- kullanici elle adres girerken hangi portu yazacagini
// dusunmek zorunda kalmaz.
//
// Eski varsayilan 27890 hala taraniyor (bkz. cluster.probePorts), cunku daha
// once yapilandirilmis bir kurulum onu kullaniyor olabilir.
const PairingPort = 2222

// LegacyPairingPort is the port MCOS used before 2222.
const LegacyPairingPort = 27890

// DefaultLinkPort is where the mcos-link mod listens for peer traffic.
//
// 27893: cluster (27890) ve link koordinatörü (27892) ile çakışmaz, ve
// Minecraft'ın 25565'inden uzaktır.
const DefaultLinkPort = 27893

// CoordinatorPort is where mcosd serves topology to the local mod.
//
// YALNIZCA 127.0.0.1'e bağlanır: topoloji, eş adreslerini ve düğüm adlarını
// içerir; ağa açmanın hiçbir yararı yok.
const CoordinatorPort = 27892

// Territory is one node's slice of the world, in chunk coordinates.
//
// MinChunkX dahil, MaxChunkX hariçtir. Uçlardaki dilimler sonsuzdur; bunu
// Unbounded* bayraklarıyla söylüyoruz çünkü "çok büyük bir sayı" yazmak,
// taşma hatalarına davetiye çıkarır.
type Territory struct {
	Node          string `json:"node"`
	MinChunkX     int    `json:"minChunkX"`
	MaxChunkX     int    `json:"maxChunkX"`
	UnboundedMin  bool   `json:"unboundedMin,omitempty"`
	UnboundedMax  bool   `json:"unboundedMax,omitempty"`
	PlayersOnline int    `json:"playersOnline,omitempty"`
}

// Contains reports whether a chunk X coordinate belongs to this territory.
func (t Territory) Contains(chunkX int) bool {
	if !t.UnboundedMin && chunkX < t.MinChunkX {
		return false
	}
	if !t.UnboundedMax && chunkX >= t.MaxChunkX {
		return false
	}
	return true
}

// Territories splits the world among nodes, in the listed order.
//
// n düğüm için n-1 sınır düzlemi, spawn (x=0) etrafında SİMETRİK yerleştirilir.
// Böylece iki düğümde sınır tam olarak x=0'dır ve "dünyanın yarısı" ifadesi
// harfiyen doğrudur.
//
// slabChunks <= 0 ise DefaultSlabChunks kullanılır.
func Territories(nodes []string, slabChunks int) []Territory {
	if len(nodes) == 0 {
		return nil
	}
	if slabChunks <= 0 {
		slabChunks = DefaultSlabChunks
	}
	n := len(nodes)
	if n == 1 {
		return []Territory{{
			Node: nodes[0], UnboundedMin: true, UnboundedMax: true,
		}}
	}

	// Sınırlar: n-1 adet, spawn (x=0) etrafında ortalanmış.
	//   n=2 → {0}                  → "dünyanın yarısı" harfiyen doğru
	//   n=3 → {-slab/2, +slab/2}   → orta düğüm spawn'ı içerir
	//   n=4 → {-slab, 0, +slab}
	//
	// KAYAN NOKTADAN yuvarlanır: tamsayı bölmesi negatif değerlerde sıfıra
	// doğru yuvarlar ve sınırlar asimetrik olur (-15 / +16 gibi). Asimetri,
	// iki dilim arasında bir chunk'lık sahipsiz şerit bırakabilirdi —
	// oyuncunun hiçbir sunucuya ait olmadığı bir yer.
	bounds := make([]int, n-1)
	for i := 0; i < n-1; i++ {
		bounds[i] = int(math.Round(
			(float64(i) - float64(n-2)/2) * float64(slabChunks)))
	}

	out := make([]Territory, n)
	for i, name := range nodes {
		t := Territory{Node: name}
		if i == 0 {
			t.UnboundedMin = true
		} else {
			t.MinChunkX = bounds[i-1]
		}
		if i == n-1 {
			t.UnboundedMax = true
		} else {
			t.MaxChunkX = bounds[i]
		}
		out[i] = t
	}
	return out
}

// OwnerOf returns the index of the node owning a chunk X coordinate.
//
// Her zaman geçerli bir indeks döner: dilimler dünyayı BOŞLUKSUZ kaplar.
func OwnerOf(ts []Territory, chunkX int) int {
	for i, t := range ts {
		if t.Contains(chunkX) {
			return i
		}
	}
	// Buraya düşmek bir hesap hatasıdır; sahipsiz oyuncu bırakmaktansa
	// ilk düğüme vermek daha az zararlıdır.
	return 0
}

// LinkNode is one participant, as reported to the panel and the mod.
type LinkNode struct {
	Name string `json:"name"`
	// Host is the address the Minecraft client will be transferred to.
	Host string `json:"host"`
	// MCPort is that node's Minecraft server port.
	MCPort int `json:"mcPort"`
	// LinkPort is that node's mod-to-mod port.
	LinkPort int `json:"linkPort"`
	// Self marks the local node.
	Self bool `json:"self,omitempty"`
	// Online reflects the last successful contact.
	Online bool `json:"online"`
	// Players currently on this node.
	Players int `json:"players,omitempty"`
	// LastSeen is when this node last answered.
	LastSeen time.Time `json:"lastSeen,omitempty"`
}

// LinkStatus is what the panel shows on the pairing screen.
type LinkStatus struct {
	Mode        LinkMode       `json:"mode"`
	ServerID    string         `json:"serverId,omitempty"`
	ServerName  string         `json:"serverName,omitempty"`
	Difficulty  LinkDifficulty `json:"difficulty,omitempty"`
	SlabChunks  int            `json:"slabChunks,omitempty"`
	Nodes       []LinkNode     `json:"nodes,omitempty"`
	Territories []Territory    `json:"territories,omitempty"`
	// ModInstalled reports whether mcos-link.jar is in the server's mods dir.
	ModInstalled bool `json:"modInstalled"`
	// ModVersion is the installed mod version, if known.
	ModVersion string `json:"modVersion,omitempty"`
	// Note carries a human-readable explanation for the panel.
	Note string `json:"note,omitempty"`
	// Handoffs counts player transfers since the server started.
	Handoffs int `json:"handoffs,omitempty"`
}

// LinkSpec is everything a peer needs to create its half of a shared world.
//
// TEK MESAJDA hepsi gider. Kullanıcıdan her makinede aynı sekiz alanı elle
// doldurmasını istemek, birinde yapılan tek harflik hatanın iki AYRI dünya
// üretmesi demektir — ve hata ancak oyuncu sınırı geçtiğinde ortaya çıkar.
type LinkSpec struct {
	// Mode is off / offload / shared-world.
	Mode LinkMode `json:"mode"`
	// ServerName is the display name used on every node.
	ServerName string `json:"serverName"`
	// Software and MCVersion must match exactly across nodes: farklı
	// sürümler farklı dünya üretir ve aktarılan oyuncu düşer.
	Software  string `json:"software"`
	MCVersion string `json:"mcVersion"`
	// Seed makes every node generate identical terrain.
	Seed string `json:"seed"`
	// Difficulty applies everywhere (bkz. LinkDifficulty).
	Difficulty LinkDifficulty `json:"difficulty"`
	// RAMMB is the per-node heap. Eşler farklı donanımda olabilir; bu
	// ÖNERİLEN değerdir, eş kendi sınırına göre kısabilir.
	RAMMB int `json:"ramMB"`
	// Port is the Minecraft port used on every node.
	Port int `json:"port"`
	// LinkPort is the mod-to-mod port.
	LinkPort int `json:"linkPort"`
	// SlabChunks is the territory width.
	SlabChunks int `json:"slabChunks"`
	// Origin is the node that created this world (for logs).
	Origin string `json:"origin,omitempty"`
}

// Normalize fills in defaults for a spec received over the wire.
//
// Eşten gelen veriye GÜVENİLMEZ: eski sürüm bir düğüm eksik alanlarla
// gönderebilir, ve sıfır bir port sunucuyu açılışta çökertirdi.
func (s LinkSpec) Normalize() LinkSpec {
	if s.Mode == "" {
		s.Mode = LinkOff
	}
	if s.Difficulty == "" {
		s.Difficulty = DifficultyNormal
	}
	if s.SlabChunks <= 0 {
		s.SlabChunks = DefaultSlabChunks
	}
	if s.LinkPort <= 0 {
		s.LinkPort = DefaultLinkPort
	}
	if s.Port <= 0 {
		s.Port = 25565
	}
	if s.RAMMB <= 0 {
		s.RAMMB = 2048
	}
	if s.ServerName == "" {
		s.ServerName = "Ortak Dünya"
	}
	return s
}
