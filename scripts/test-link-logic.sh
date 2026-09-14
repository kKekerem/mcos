#!/bin/sh
# test-link-logic.sh — PC eşleştirme ve ortak dünya (MCOS Link).
#
# ════════════════════════════════════════════════════════════════════════════
# NEDEN BU TEST VAR
# ════════════════════════════════════════════════════════════════════════════
#
# Ortak dünya, iki ayrı makinede çalışan iki ayrı programın (Go daemon'u ve
# Java modu) AYNI sözleşmeye uymasını gerektirir. Sözleşme sessizce
# ayrıştığında belirti şudur: oyuncu sınırı geçer ve BAĞLANTI KOPAR — hangi
# tarafın hatalı olduğu anlaşılmaz.
#
# Bu betik iki tarafın da aynı alan adlarını, aynı portları ve aynı güvenlik
# kurallarını kullandığını doğrular.
#
# Ağa çıkmaz, hiçbir şey çalıştırmaz.

set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

fails=0
pass() { printf '  \033[32mOK\033[0m   %s\n' "$1"; }
fail() { printf '  \033[31mHATA\033[0m %s\n' "$1"; fails=$((fails + 1)); }

LINKGO="$ROOT/internal/cluster/link.go"
DISC="$ROOT/internal/cluster/discover.go"
CLUSTER="$ROOT/internal/cluster/cluster.go"
MODELGO="$ROOT/internal/model/link.go"
DAEMON="$ROOT/internal/daemon/handlers_link.go"
PEERS="$ROOT/internal/fbpanel/screen_peers.go"

MODDIR="$ROOT/mods/mcos-link"
TOPO="$MODDIR/src/main/java/gg/mcos/link/Topology.java"
COORD="$MODDIR/src/main/java/gg/mcos/link/Coordinator.java"
PROTO="$MODDIR/src/main/java/gg/mcos/link/LinkProtocol.java"
SRV="$MODDIR/src/main/java/gg/mcos/link/LinkServer.java"
HAND="$MODDIR/src/main/java/gg/mcos/link/HandoffService.java"
MODJSON="$MODDIR/src/main/resources/fabric.mod.json"

for f in "$LINKGO" "$DISC" "$CLUSTER" "$MODELGO" "$DAEMON" "$PEERS" \
         "$TOPO" "$COORD" "$PROTO" "$SRV" "$HAND" "$MODJSON"; do
    [ -f "$f" ] || { echo "eksik dosya: $f" >&2; exit 2; }
done

# nc, yorumları atarak arar (Go ve Java aynı // biçimini kullanıyor).
nc() { sed 's://.*::' "$1" | grep -q "$2"; }

# jsonfield, bir JSON alan adının dosyada geçtiğini doğrular.
#
# Go etiketleri `json:"token,omitempty"` biçiminde olabilir; düz '"token"'
# araması bunları KAÇIRIR ve test yanlışlıkla "alan yok" der. Alan adından
# sonra tırnak VEYA virgül kabul ediyoruz.
jsonfield() { # dosya alan
    grep -qE "\"$2[\",]" "$1"
}

echo "== 1. Portlar iki tarafta AYNI =="

go_coord="$(sed -n 's/^const CoordinatorPort = \([0-9]*\)$/\1/p' "$MODELGO")"
go_link="$(sed -n 's/^const DefaultLinkPort = \([0-9]*\)$/\1/p' "$MODELGO")"
java_coord="$(grep -o 'http://127\.0\.0\.1:[0-9]*' "$COORD" | head -1 | sed 's/.*://')"
java_link="$(sed -n 's/.*DEFAULT_PORT = \([0-9]*\);.*/\1/p' "$PROTO" | head -1)"

if [ "$go_coord" = "$java_coord" ]; then
    pass "koordinatör portu eşleşiyor ($go_coord)"
else
    fail "koordinatör portu ayrışmış: Go=$go_coord Java=$java_coord"
fi
if [ "$go_link" = "$java_link" ]; then
    pass "düğümler arası port eşleşiyor ($go_link)"
else
    fail "düğüm portu ayrışmış: Go=$go_link Java=$java_link"
fi

echo "== 2. Topoloji sözleşmesi =="

go_ver="$(sed -n 's/^const TopologyVersion = \([0-9]*\)$/\1/p' "$LINKGO")"
java_ver="$(sed -n 's/.*SUPPORTED_VERSION = \([0-9]*\);.*/\1/p' "$TOPO" | head -1)"
if [ "$go_ver" = "$java_ver" ]; then
    pass "sözleşme sürümü eşleşiyor ($go_ver)"
else
    fail "sözleşme sürümü ayrışmış: Go=$go_ver Java=$java_ver"
fi

# Go'nun ürettiği her JSON alanını Java'nın okuduğundan emin ol.
for field in enabled self difficulty slabChunks hysteresisChunks nodes \
             territories token; do
    if ! jsonfield "$LINKGO" "$field"; then
        fail "Go topolojisinde '$field' alanı yok"
        continue
    fi
    if jsonfield "$TOPO" "$field"; then
        pass "'$field' iki tarafta da tanınıyor"
    else
        fail "Java '$field' alanını okumuyor"
    fi
done

for field in minChunkX maxChunkX unboundedMin unboundedMax; do
    if jsonfield "$MODELGO" "$field" && jsonfield "$TOPO" "$field"; then
        pass "dilim alanı '$field' iki tarafta da var"
    else
        fail "dilim alanı '$field' ayrışmış"
    fi
done

for field in name host mcPort linkPort; do
    if jsonfield "$MODELGO" "$field" && jsonfield "$TOPO" "$field"; then
        pass "düğüm alanı '$field' iki tarafta da var"
    else
        fail "düğüm alanı '$field' ayrışmış"
    fi
done

echo "== 3. Dilim matematiği =="

if nc "$MODELGO" 'func Territories('; then
    pass "dilim hesabı Go tarafında tanımlı"
else
    fail "Territories yok"
fi

# Java, sahipliği KENDİSİ hesaplamamalı: topolojiden okumalı. Aksi halde iki
# taraf farklı sonuç üretebilir ve oyuncu iki makine arasında gidip gelir.
if grep -q 'Math.round' "$TOPO"; then
    fail "Java sınırları kendisi hesaplıyor — tek kaynak Go olmalı"
else
    pass "Java sınırları hesaplamıyor, topolojiden okuyor"
fi

if nc "$MODELGO" 'HandoffHysteresisChunks' && nc "$HAND" 'hysteresisChunks'; then
    pass "sınır gecikmesi (hysteresis) iki tarafta da uygulanıyor"
else
    fail "gecikme yok — oyuncu sınırda iki makine arasında döngüye girer"
fi

echo "== 4. Güvenlik =="

if nc "$LINKGO" 't.Token = c.mgr.Secret()'; then
    pass "topoloji eşleştirme anahtarını taşıyor"
else
    fail "anahtar gönderilmiyor — mod eşlere kimlik doğrulayamaz"
fi

if nc "$SRV" 'MessageDigest.isEqual'; then
    pass "mod anahtarı sabit sürede karşılaştırıyor"
else
    fail "anahtar karşılaştırması zamanlama sızdırıyor"
fi

if nc "$SRV" 'if (expected == null || expected.isEmpty())'; then
    pass "anahtar yoksa HİÇBİR istek kabul edilmiyor"
else
    fail "anahtar boşken istekler kabul ediliyor olabilir"
fi

if nc "$SRV" 'isUuid(uuid)'; then
    pass "gelen UUID doğrulanıyor (yol dışına çıkma engellendi)"
else
    fail "UUID doğrulanmıyor — dosya adı olarak kullanılıyor"
fi

if nc "$CLUSTER" 'case "linkSpec":' &&
   sed 's://.*::' "$CLUSTER" | grep -A6 'case "linkSpec":' | grep -q 'authorizeTask'; then
    pass "linkSpec isteği yetkilendirmeden geçiyor"
else
    fail "linkSpec kimlik doğrulaması yok — LAN'daki herkes sunucu kurdurabilir"
fi

# Koordinatör YALNIZCA 127.0.0.1'e bağlanmalı: topoloji eş adreslerini içerir.
if nc "$LINKGO" 'net.JoinHostPort("127.0.0.1"'; then
    pass "koordinatör yalnızca yerel arayüze bağlanıyor"
else
    fail "koordinatör ağa açık — altyapı haritası sızar"
fi

echo "== 5. Eşleştirme akışı =="

if nc "$DISC" 'func (m \*Manager) ScanLAN'; then
    pass "etkin LAN taraması var (multicast engelliyse de bulur)"
else
    fail "etkin tarama yok"
fi

if nc "$DISC" 'func (m \*Manager) AddManual'; then
    pass "elle IP ile eşleştirme var"
else
    fail "elle eşleştirme yok — tarama başarısızsa çıkış yolu kalmaz"
fi

# Tarama bir GÜVEN işlemi değildir: bulunan düğüm otomatik eşleşmemeli.
if sed 's://.*::' "$DISC" | grep -A12 'func (m \*Manager) upsertProbed' |
        grep -q 'p.Paired = true'; then
    fail "tarama bulduğu düğümü otomatik eşleştiriyor"
else
    pass "tarama otomatik eşleştirme yapmıyor"
fi

if nc "$PEERS" 'startPeerScan' && nc "$PEERS" 'openManualPair'; then
    pass "eşleştirme ekranı hem tarama hem elle giriş sunuyor"
else
    fail "eşleştirme ekranı eksik"
fi

if nc "$PEERS" 'u.ScanBanner'; then
    pass "tarama sırasında dönen animasyon gösteriliyor"
else
    fail "tarama animasyonu yok"
fi

echo "== 6. Mod paketi =="

if grep -q '"environment": "server"' "$MODJSON"; then
    pass "mod yalnızca sunucu tarafı (istemcide mod gerekmiyor)"
else
    fail "mod istemci tarafı da istiyor — kullanıcılar bağlanamaz"
fi

if nc "$HAND" 'ServerTransferS2CPacket'; then
    pass "aktarım Minecraft'ın kendi transfer paketiyle yapılıyor"
else
    fail "transfer paketi kullanılmıyor"
fi

# Veri ÖNCE gitmeli, transfer paketi SONRA: tersi durumda oyuncu boş
# envanterle doğar.
push_line="$(sed 's://.*::' "$HAND" | grep -n 'boolean ok = push(' | head -1 | cut -d: -f1)"
xfer_line="$(sed 's://.*::' "$HAND" | grep -n 'new ServerTransferS2CPacket' | head -1 | cut -d: -f1)"
if [ -n "$push_line" ] && [ -n "$xfer_line" ] && [ "$push_line" -lt "$xfer_line" ]; then
    pass "oyuncu verisi, transfer paketinden ÖNCE gönderiliyor"
else
    fail "transfer paketi veriden önce gidiyor — envanter kaybolur"
fi

if nc "$DAEMON" 'linkModName' && nc "$DAEMON" 'func (d \*Daemon) installLinkMod'; then
    pass "daemon modu sunucunun mods/ klasörüne kuruyor"
else
    fail "mod kurulumu yok"
fi

if nc "$DAEMON" 'st.ModInstalled'; then
    pass "panel modun kurulu olup olmadığını bildiriyor"
else
    fail "mod eksikse kullanıcı bunu göremez"
fi


# ─────────────────────────────────────────────────────────────────────────────
# ORTAK DÜNYA EKLENTİSİNİN İKİ YAPISI
# ─────────────────────────────────────────────────────────────────────────────
#
# ── Kullanıcının isteği ─────────────────────────────────────────────────────
# "mod varsayılan olarak her sunucuya gelmeli fabric ve paper icin derle"
#
# Fabric modları mods/ altından, Paper eklentileri plugins/ altından yüklenir
# ve ikisi birbirinin dosyasını TANIMAZ. Yanlış klasöre konan bir dosya
# SESSİZCE yüklenmez: sunucu açılır, ortak dünya çalışmaz, hiçbir hata
# görünmez.
#
# iki-yapi
echo ""
echo "iki yapı: Fabric modu + Paper eklentisi"

HL="$ROOT/internal/daemon/handlers_link.go"

if nc "$HL" 'linkModName' && nc "$HL" 'mcos-link.jar'; then
    pass "Fabric modu adı sabit"
else
    fail "mcos-link.jar adı yok"
fi

if nc "$HL" 'linkPluginName' && nc "$HL" 'mcos-link-paper.jar'; then
    pass "Paper eklentisi adı sabit"
else
    fail "mcos-link-paper.jar adı yok"
fi

# Doğru klasör seçimi: Fabric -> mods/, eklenti sunucuları -> plugins/
#
# ── Bu kontrol NEDEN değişti ───────────────────────────────────────────────
# Burada eskiden `SupportsMods` aranıyordu. O yordam Forge ve NeoForge'u da
# TRUE sayar; oysa mcos-link bir FABRIC modudur ve Forge onu hiç tanımaz.
# Sonuç sinsiydi: jar Forge sunucusunun mods/ klasörüne kopyalanıyor, panel
# "mod kurulu" diyor, ortak dünya hiç çalışmıyordu. Artık yalnızca Fabric.
if ! sed 's://.*::' "$HL" | grep -A10 'func linkArtifact' | grep -q 'SoftwareFabric'; then
    fail "linkArtifact Fabric'i açıkça seçmiyor — Forge/NeoForge'a da " \
         "Fabric modu kopyalanır"
elif sed 's://.*::' "$HL" | grep -A10 'func linkArtifact' | grep -q 'SupportsMods'; then
    fail "linkArtifact hâlâ SupportsMods kullanıyor — Forge ve NeoForge da " \
         "kapsama girer ve mod oraya boşuna kopyalanır"
elif sed 's://.*::' "$HL" | grep -A10 'func linkArtifact' | grep -q 'plugins'; then
    pass "yazılıma göre doğru klasör seçiliyor (yalnızca Fabric -> mods, eklenti -> plugins)"
else
    fail "plugins klasörü hiç kullanılmıyor — Paper sunucusu eklentiyi bulamaz"
fi

# ── fabric-api olmadan mod KURULMAMALI ─────────────────────────────────────
#
# fabric.mod.json fabric-api'yi SERT bağımlılık olarak bildiriyor. Eksikse
# Fabric Loader modu atlamaz, SUNUCUYU HİÇ AÇMAZ (gerçek bir 1.21.11
# sunucusunda görüldü):
#
#   Incompatible mods found!
#   - Mod 'MCOS Link' requires any version of fabric-api, which is missing!
#
# ensureLinkArtifact modu VARSAYILAN olarak her sunucuya kurduğu için bu,
# MCOS'un kurduğu her Fabric sunucusunun açılmaması demekti.
if grep -q 'func (d \*Daemon) ensureFabricAPI' "$HL"; then
    pass "fabric-api sağlama yordamı var"
else
    fail "ensureFabricAPI yok — mod fabric-api olmadan kurulur ve Fabric " \
         "sunucusu HİÇ açılmaz"
fi

if sed 's://.*::' "$HL" | grep -A40 'func (d \*Daemon) installLinkMod' \
        | grep -q 'ensureFabricAPI'; then
    pass "mod kopyalanmadan ÖNCE fabric-api sağlanıyor"
else
    fail "installLinkMod fabric-api'yi sağlamadan modu kopyalıyor"
fi

# Sağlanamazsa HATA dönmeli: sessizce devam etmek, açılmayan bir sunucu bırakır.
if sed 's://.*::' "$HL" | grep -A40 'func (d \*Daemon) installLinkMod' \
        | grep -A3 'ensureFabricAPI' | grep -q 'return err'; then
    pass "fabric-api sağlanamazsa mod kurulmuyor (hata dönüyor)"
else
    fail "fabric-api sağlanamasa bile mod kopyalanıyor — sunucu açılmaz hâle gelir"
fi

# Önce YEREL depo: MCOS internetsiz çalışabilmeli.
if grep -q 'findBundledFabricAPI' "$HL"; then
    pass "fabric-api önce yerel/çevrimdışı depodan aranıyor"
else
    fail "fabric-api yalnızca internetten alınıyor — çevrimdışı kurulum kırılır"
fi

# Her sunucuya kurulmalı, yalnızca ortak dünya açıkken değil.
if grep -rq 'ensureLinkArtifact' "$ROOT/internal/daemon/handlers.go" \
        "$ROOT/internal/daemon/handlers_catalog.go" 2>/dev/null; then
    pass "eklenti HER sunucu kurulumunda yükleniyor"
else
    fail "eklenti yalnızca ortak dünya açılınca kuruluyor"
fi

# Masaüstü düğüm programı da aynı adlandırmayı kullanmalı: kullanıcı aynı
# klasörü iki makineye kopyalayabilmeli.
NH="$ROOT/cmd/mcos-node/host.go"
if [ -f "$NH" ]; then
    if nc "$NH" 'linkPluginName'; then
        pass "düğüm programı da iki yapıyı biliyor"
    else
        fail "düğüm programı yalnızca Fabric modunu biliyor"
    fi
fi

# Paper modülü gerçekten var mı?
if [ -f "$ROOT/mods/mcos-link-paper/build.gradle" ]; then
    pass "Paper modülü mevcut"
else
    fail "mods/mcos-link-paper yok — Paper eklentisi hiç derlenemez"
fi

if grep -q 'mod-paper:' "$ROOT/Makefile"; then
    pass "make mod-paper hedefi var"
else
    fail "Paper eklentisini derleyen hedef yok"
fi

echo ""
if [ "$fails" -eq 0 ]; then
    printf '\033[32mORTAK DÜNYA SÖZLEŞMESİ TAMAM\033[0m\n'
    exit 0
fi
printf '\033[31m%d SORUN\033[0m\n' "$fails"
exit 1
