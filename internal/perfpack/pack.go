// Package perfpack decides and installs the "performance pack" of a server:
// the server-side mods (Fabric/Quilt/NeoForge/Forge) or configuration changes
// (Paper/Purpur/Spigot) that make a Minecraft server faster WITHOUT changing
// gameplay.
//
// ════════════════════════════════════════════════════════════════════════════
// SEÇİM İLKELERİ
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı: "internete bağlıyken sunucu kurunca o sunucu türü ve sürümü için
// ana optimizasyon ve performans modlarını/eklentilerini kursun"; hedef "2000
// oyuncuyla bile donmasın" — görsel değil, SUNUCU başarımı.
//
// Bir öğe pakete ancak şu koşulların HEPSİNİ sağlarsa girer:
//
//  1. Yalnızca sunucu tarafı: oyuncunun istemcisine bir şey kurması gerekmez
//     (Modrinth'te client_side "optional"). Aksi halde oyuncu bağlanamazdı.
//  2. Oynanışı değiştirmez: vanilla ile aynı davranış hedefleyen modlar
//     (Lithium'un açık amacı budur). Mob doğma oranı, redstone sırası, eşya
//     birleşme yarıçapı gibi "hızlandıran ama oyunu değiştiren" ayarlar
//     BİLEREK dışarıda (bkz. aşağıdaki "alınmayanlar").
//  3. Yaygın ve bakımı süren: milyonlarca indirme, sunucu sürümü için
//     Modrinth'te AÇIKÇA etiketlenmiş yapı. Etiketsiz yapı "belki çalışır"
//     demektir; kurulmaz.
//  4. Üçüncü taraf zorunlu bağımlılığı yok (fabric-api, cloth-config ...).
//     Ölçüldü (fabric.mod.json / neoforge.mods.toml, 2026-09-27): seçilen
//     modların hiçbiri fabric-api istemiyor. fabric-api'yi ortak dünya modu
//     (handlers_link.go) yönetiyor; ikinci bir kopya "duplicate mod" ile
//     açılışı düşürürdü. Zorunlu bağımlılık bildiren bir yapı ÇIKARSA mod
//     atlanır (bkz. install.go, requiredDeps).
//  5. GERÇEK sınamadan geçti: MCOS'un kendi kurulum koduyla kurulup sunucu
//     "Done" diyene kadar açıldı ve oyuncu girişi yapıldı (tablo raporda).
//
// ── Alınmayanlar ve nedenleri ───────────────────────────────────────────────
//
//   - C2ME (dünya üretimi): 26.3 ve 1.21.1 için güncel yapıları "alpha"
//     (Modrinth, 2026-09-27: 0.4.2-alpha.0.88+26.3, 0.4.0-alpha.0.29+1.21.1);
//     parça G/Ç ve üretim iş parçacıklarını baştan yazıyor. Bir hata dünya
//     verisini bozar; veri güvenliği hızdan önce gelir.
//   - spark: bir ölçüm aracı, hızlandırmaz.
//   - Alternate Current / Paper "redstone-implementation": redstone güncelleme
//     sırasını değiştirir, bazı düzenekleri bozar.
//   - ServerCore, Pufferfish DAB, entity-activation-range, merge-radius,
//     spawn-limits, ticks-per.*: hızlandırır ama OYUNU değiştirir (mob
//     davranışı, çiftlik verimi, eşya birleşmesi).
//   - Canary (Forge'da Lithium): yalnızca 1.20.1/1.20.4 yapısı var; MCOS'un
//     Forge'u ile birlikte gerçek sınama yapılmadı, bu yüzden listede yok.
package perfpack

import (
	"strings"

	"mcos/internal/catalog"
	"mcos/internal/mcver"
	"mcos/internal/model"
)

// Mod is one Modrinth project of a pack.
type Mod struct {
	// Slug, Modrinth adıdır (indirme ve kayıt bununla yapılır).
	Slug string
	// ProjectID, Modrinth'in değişmez proje kimliğidir. Kullanıcı modu panel
	// kataloğundan kurduysa aynı projeyi slug değişse de tanımak için.
	ProjectID string
	// Name, panelde gösterilen addır.
	Name string
	// IDs, jar içindeki mod kimlikleridir (fabric.mod.json "id",
	// mods.toml "modId"). Kullanıcının USB'den ya da elle eklediği aynı mod
	// dosya adından tanınamaz; kimlikten tanınır ve İKİNCİ kez kurulmaz.
	IDs []string
	// Conflicts: bu mod kimliklerinden biri klasördeyse mod KURULMAZ
	// (ör. iki ayrı ışık motoru aynı sınıfları değiştirir, açılış düşer).
	Conflicts []string
	// Unstable, "alpha"/"beta" etiketli yapıları kabul eder. Yalnızca
	// projenin HİÇ "release" yayımlamadığı durumlar için (bkz. VMP).
	Unstable bool
	// Why, neden pakette olduğudur (panel ve günlük için).
	Why string
}

// Setting is one configuration change of a pack (YAML dosyası).
type Setting struct {
	// File, sunucu klasörüne göre yoldur ("spigot.yml",
	// "config/paper-world-defaults.yml").
	File string
	// Path, YAML anahtar yoludur.
	Path []string
	// Value, yazılacak değerdir.
	Value string
	// Defaults, dokunulmamış sayılan değerlerdir. Dosyadaki değer bunlardan
	// biri DEĞİLSE kullanıcı bilerek değiştirmiştir; ona dokunulmaz.
	Defaults []string
	// Why, neden seçildiğidir.
	Why string
}

// Key returns the human form "spigot.yml: settings.x".
func (s Setting) Key() string { return s.File + ": " + strings.Join(s.Path, ".") }

// Pack is everything the performance pack does for one software/version.
type Pack struct {
	Software model.Software
	MC       string
	// Loaders, Modrinth'e sorulacak yükleyici zinciridir (quilt -> quilt,
	// fabric). Mod yoksa boştur.
	Loaders []string
	// Dir, modların kurulacağı klasördür ("mods").
	Dir      string
	Mods     []Mod
	Settings []Setting
	// Note, paket boşsa ya da kısıtlıysa nedenidir (kullanıcıya gösterilir).
	Note string
}

// Empty reports whether the pack has nothing to do.
func (p Pack) Empty() bool { return len(p.Mods) == 0 && len(p.Settings) == 0 }

// Names lists the pack's contents for the panel ("Lithium, FerriteCore").
func (p Pack) Names() []string {
	var out []string
	for _, m := range p.Mods {
		out = append(out, m.Name)
	}
	if len(p.Settings) > 0 {
		files := map[string]bool{}
		for _, s := range p.Settings {
			if !files[s.File] {
				files[s.File] = true
				out = append(out, s.File[strings.LastIndex(s.File, "/")+1:])
			}
		}
	}
	return out
}

// ── Modlar ──────────────────────────────────────────────────────────────────
//
// Proje kimlikleri Modrinth'ten (api.modrinth.com/v2/project/<slug>,
// 2026-09-27). Hepsinde client_side = "optional": oyuncu hiçbir şey kurmaz.

var (
	// Canary ve Radium, Lithium'un Forge/NeoForge'a taşınmış kopyalarıdır:
	// aynı sınıfları aynı biçimde değiştirirler, ikisi birlikte açılmaz.
	modLithium = Mod{Slug: "lithium", ProjectID: "gvQqBUqZ", Name: "Lithium",
		IDs:       []string{"lithium"},
		Conflicts: []string{"canary", "radium"},
		Why:       "Genel oyun mantığı (varlık, blok, yol bulma, çarpışma) vanilla ile birebir aynı davranışla hızlanır"}
	modFerrite = Mod{Slug: "ferrite-core", ProjectID: "uXXizFIs", Name: "FerriteCore",
		IDs: []string{"ferritecore"},
		Why: "Blok durumu ve model verisinin bellek kullanımını düşürür; aynı RAM'e daha çok parça sığar"}
	modKrypton = Mod{Slug: "krypton", ProjectID: "fQEb0iXm", Name: "Krypton",
		IDs: []string{"krypton"},
		Why: "Ağ katmanını (sıkıştırma, paket boşaltma) hızlandırır; oyuncu sayısı arttıkça kazanç büyür"}
	modModernFix = Mod{Slug: "modernfix", ProjectID: "nmDcB62a", Name: "ModernFix",
		IDs: []string{"modernfix"},
		Why: "Açılışı kısaltır, bellek sızıntılarını ve boşa ayrılan belleği giderir"}
	// ScalableLux, Starlight'ın devamı olan ışık motorudur: ışık hesabını
	// ana iş parçacığından alıp paralel yapar; yeni parça üretimindeki
	// takılmaların başlıca nedeni ışık hesabıdır. Başka bir ışık motoruyla
	// (Starlight, Phosphor, Moonrise) aynı sınıfları değiştirir; biri
	// kuruluysa atlanır (fabric.mod.json'u "breaks: phosphor" diyor).
	modScalableLux = Mod{Slug: "scalablelux", ProjectID: "Ps1zyz6x", Name: "ScalableLux",
		IDs:       []string{"scalablelux"},
		Conflicts: []string{"starlight", "phosphor", "moonrise"},
		Why:       "Işık hesabını paralel ve ana iş parçacığı dışında yapar; yeni parça üretiminde takılmayı azaltır"}
	// VMP ("Very Many Players"), kalabalık sunucu için yazılmış tek yaygın
	// moddur: varlık izleme ve parça gönderimini oyuncu sayısıyla ölçeklenir
	// hale getirir. Hedef "2000 oyuncu" olduğu için pakette.
	//
	// Unstable: VMP'nin Modrinth'teki TÜM yapıları "alpha" etiketli
	// (0.2.0+beta.7.N dizisi; 2026-09-27'de 1.20.1'den 26.3'e kadar tek bir
	// "release" yok). Etiket projenin yayımlama alışkanlığıdır, olgunluk
	// ölçüsü değil (16 milyon indirme). ScalableLux'ta durum farklı: o proje
	// normalde "release" yayımlıyor ve 26.3'ü bilerek "alpha" işaretlemiş,
	// yani yazarı henüz güvenmiyor — orada alpha KABUL EDİLMEZ.
	modVMP = Mod{Slug: "vmp-fabric", ProjectID: "wnEe9KBa", Name: "VMP",
		IDs:      []string{"vmp"},
		Unstable: true,
		Why:      "Çok oyunculu yükte varlık izleme ve parça gönderimini ölçeklenir yapar (kalabalık sunucu için)"}
)

// fabricMods is the Fabric (and Quilt) pack. Sıra kurulum sırasıdır.
//
// ModernFix'in Fabric yapısı 1.21.1'de bitiyor, Krypton'un 26.3 yapısı henüz
// yok (Modrinth, 2026-09-27): bu sürümlerde "yapı yok" diye atlanırlar.
// Liste sürüme göre elle KESİLMEZ; hangi sürümde yapı olduğunu Modrinth
// söyler, böylece yazar yeni sürüm yayımlayınca paket kendiliğinden genişler.
var fabricMods = []Mod{modLithium, modFerrite, modKrypton, modModernFix, modScalableLux, modVMP}

// neoforgeMods is the NeoForge pack (Krypton ve VMP yalnızca Fabric).
var neoforgeMods = []Mod{modLithium, modFerrite, modModernFix, modScalableLux}

// forgeMods is the (legacy) Forge pack. Modern Forge (1.21+) için bu
// projelerin hiçbirinin yapısı yok; 1.20.x'te FerriteCore ve ModernFix var.
var forgeMods = []Mod{modFerrite, modModernFix}

// ── Paper / Purpur / Spigot ayarları ────────────────────────────────────────
//
// Paper zaten optimize edilmiş bir çatal; gerçek kazanç YAPILANDIRMADA.
// Kaynak: YouHaveTrouble/minecraft-optimization (en çok atıf alan rehber).
// Rehberin önerilerinden YALNIZCA oynanışı değiştirmeyenler alındı.

// paperConfigMin, paper-world-defaults.yml'ın ilk geldiği sürümdür. Daha
// eskisinde ayarlar tek bir paper.yml'da ve başka yollarda durur.
const paperConfigMin = "1.19"

var paperSettings = []Setting{
	{
		File: "config/paper-world-defaults.yml", Path: []string{"chunks", "prevent-moving-into-unloaded-chunks"},
		Value: "true", Defaults: []string{"false"},
		// DONMANIN BAŞ NEDENİ: yüklenmemiş parçaya yürüyen (elytra, hızlı
		// at) oyuncu, parçanın ANA iş parçacığında eşzamanlı yüklenmesine yol
		// açar ve o sürede TÜM sunucu durur. Açıkken yalnızca o oyuncu parça
		// gelene kadar kenarda tutulur. 2000 oyuncuda bu fark belirleyici.
		Why: "Yüklenmemiş parçaya giren oyuncu ana iş parçacığını durduramaz",
	},
	{
		File: "config/paper-world-defaults.yml", Path: []string{"environment", "optimize-explosions"},
		Value: "true", Defaults: []string{"false"},
		// TNT yığını klasik bir takılma/saldırı yöntemi. Paper'ın algoritması
		// aynı patlamada aynı kutuya düşen yoğunluk hesabını önbelleğe alır;
		// kırılan bloklar aynı, fark yalnızca aynı tik içindeki ardışık
		// patlamaların hasar hesabında (rehber: "genelde fark edilmez").
		Why: "Patlama hesabı önbellekli yapılır; TNT yığını sunucuyu kilitlemez",
	},
	// Mermi sınırları: yüz binlerce ok/kartopu biriktirilmiş bir parça
	// açılışta sunucuyu çökertebilir ("chunk ban"). Yalnızca TOPLANAMAYAN ya
	// da anlamsız mermiler sınırlanır. Ender incisi (inci durağı düzenekleri),
	// deneyim küresi (XP çiftliği) ve trident (oyuncunun eşyası) BİLEREK
	// sınırsız bırakıldı: onları silmek oyuncunun emeğini silmek olurdu.
	{
		File: "config/paper-world-defaults.yml", Path: []string{"chunks", "entity-per-chunk-save-limit", "arrow"},
		Value: "16", Defaults: []string{"-1"},
		Why: "Bir parçada saklanan ok sayısı sınırlı; ok yığınıyla çökertme engellenir",
	},
	{
		File: "config/paper-world-defaults.yml", Path: []string{"chunks", "entity-per-chunk-save-limit", "snowball"},
		Value: "8", Defaults: []string{"-1"},
		Why: "Kartopu yığınıyla parça çökertme engellenir",
	},
	{
		File: "config/paper-world-defaults.yml", Path: []string{"chunks", "entity-per-chunk-save-limit", "fireball"},
		Value: "8", Defaults: []string{"-1"},
		Why: "Ateş topu yığınıyla parça çökertme engellenir",
	},
	{
		File: "config/paper-world-defaults.yml", Path: []string{"chunks", "entity-per-chunk-save-limit", "small_fireball"},
		Value: "8", Defaults: []string{"-1"},
		Why: "Küçük ateş topu yığınıyla parça çökertme engellenir",
	},
	// Moblarin ve yaratıcı moddaki oyuncunun attığı oklar ZATEN yerden
	// alınamaz; vanilla onları 60 sn tutup her tik işler. 1 sn'de silinmeleri
	// oynanışı değiştirmez (rehber: "players can't pick these up anyway").
	{
		File: "config/paper-world-defaults.yml", Path: []string{"entities", "spawning", "non-player-arrow-despawn-rate"},
		Value: "20", Defaults: []string{"default", "-1"},
		Why: "Alınamayan mob okları 1 sn'de silinir, boşa işlenmez",
	},
	{
		File: "config/paper-world-defaults.yml", Path: []string{"entities", "spawning", "creative-arrow-despawn-rate"},
		Value: "20", Defaults: []string{"default", "-1"},
		Why: "Yaratıcı modda atılan (alınamayan) oklar 1 sn'de silinir",
	},
}

// spigotSettings Spigot'ta ve onu içeren her çatalda (Paper, Purpur) geçerli.
var spigotSettings = []Setting{
	{
		File: "spigot.yml", Path: []string{"settings", "save-user-cache-on-stop-only"},
		Value: "true", Defaults: []string{"false"},
		// usercache.json (ad -> UUID önbelleği) varsayılanda HER girişte
		// baştan yazılır; oyuncu sayısıyla büyüyen bir dosyanın her girişte
		// yazılması kalabalık girişlerde disk ve iş parçacığı yükü demek.
		// Kaybı zararsız: çökmede yalnızca önbellek eksik kalır, Mojang'dan
		// yeniden öğrenilir.
		Why: "Oyuncu ad önbelleği her girişte değil yalnızca kapanışta yazılır",
	},
}

// For returns the performance pack for a software and Minecraft version.
//
// Vanilla için paket BOŞTUR ve bu bilinçli bir karar:
//   - network-compression-threshold: sıkıştırma Netty iş parçacıklarında
//     yapılır, ana tiki (donmayı) etkilemez; eşiği yükseltmek yalnızca bant
//     genişliğini artırır. MCOS sunucuları çoğunlukla ev bağlantısı / playit
//     tüneli arkasında ve orada kıt olan bant genişliği. Rehber de 256'yı
//     (varsayılan) öneriyor.
//   - sync-chunk-writes=false: bölge dosyası yazımını hızlandırır ama elektrik
//     kesilmesinde parça bozulur. MCOS USB'den ve ev PC'sinden çalışıyor;
//     veri güvenliği önce gelir. Vanilla'da yazım zaten G/Ç iş parçacığında,
//     ana tiki bekletmez.
//
// Folia bilerek boş: bölgesel iş parçacığı modeli, neyin pahalı olduğunu
// değiştiriyor ve Paper ayarları üzerinde gerçek sınama yapılmadı.
func For(sw model.Software, mc string) Pack {
	p := Pack{Software: sw, MC: strings.TrimSpace(mc)}
	switch sw {
	case model.SoftwareFabric, model.SoftwareQuilt:
		// Quilt, Fabric modlarını yükler; zincir önce quilt sonra fabric
		// etiketine bakar (catalog.LoaderChain).
		p.Mods, p.Dir = fabricMods, "mods"
	case model.SoftwareNeoForge:
		p.Mods, p.Dir = neoforgeMods, "mods"
	case model.SoftwareForge:
		p.Mods, p.Dir = forgeMods, "mods"
	case model.SoftwarePaper, model.SoftwarePurpur:
		p.Settings = append(p.Settings, spigotSettings...)
		if mcver.Compare(p.MC, paperConfigMin) >= 0 {
			p.Settings = append(p.Settings, paperSettings...)
		} else {
			p.Note = "Minecraft " + paperConfigMin + " öncesinde Paper ayarları başka dosyada; yalnızca spigot.yml ayarlanır"
		}
	case model.SoftwareSpigot:
		p.Settings = append(p.Settings, spigotSettings...)
	case model.SoftwareVanilla:
		p.Note = "Vanilla'da oynanışı ve veri güvenliğini bozmadan kazanç sağlayan ayar yok; Fabric önerilir"
	case model.SoftwareFolia:
		p.Note = "Folia için performans paketi yok (sınanmadı)"
	default:
		p.Note = "Bu yazılım için performans paketi yok"
	}
	if len(p.Mods) > 0 {
		p.Loaders = catalog.LoaderChain(sw.ModrinthLoader())
	}
	return p
}
