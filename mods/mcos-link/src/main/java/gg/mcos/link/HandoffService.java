package gg.mcos.link;

import com.google.gson.JsonObject;
import net.minecraft.network.packet.s2c.common.ServerTransferS2CPacket;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.network.ServerPlayerEntity;
import net.minecraft.text.Text;
import net.minecraft.util.Formatting;

import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.util.Map;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/**
 * Oyuncunun sınırı geçtiğini fark eder ve onu öbür düğüme aktarır.
 *
 * <p>Bu modun kalbi burasıdır.
 *
 * <h2>Aktarım sırası</h2>
 * <ol>
 *   <li>Oyuncunun chunk X'i kendi dilimimizin dışında ve <b>gecikme kadar</b>
 *       da ötesinde mi? (Sınırın üstünde duran bir oyuncu ileri-geri
 *       gittiğinde iki makine arasında sonsuz döngüye girerdi.)</li>
 *   <li>Sunucu oyuncu verisini diske yazar.</li>
 *   <li>Veri hedef düğüme gönderilir ve <b>onay beklenir</b>.</li>
 *   <li>Ancak onay geldikten sonra istemciye transfer paketi yollanır.</li>
 * </ol>
 *
 * <p><b>Sıra neden bu?</b> Önce transfer paketi gönderip sonra veriyi
 * yollasaydık, istemci karşı sunucuya bizden ÖNCE ulaşabilirdi ve oyuncu
 * boş envanterle doğardı. Veri önce gider; gönderilemezse aktarım hiç
 * yapılmaz ve oyuncu yerinde kalır — kaybolmuş bir oyuncudan iyidir.
 *
 * <p><b>Neden transfer paketi?</b> Minecraft 1.20.5 ile gelen
 * {@code ServerTransferS2CPacket}, istemciyi dünya ekranından çıkarmadan
 * başka bir sunucuya bağlar. MCOS bir hile uydurmuyor; protokolün kendi
 * mekanizmasını kullanıyor. Bu yüzden istemcide mod GEREKMEZ.
 */
public final class HandoffService {

    /**
     * İki aktarım DENEMESİ arasındaki en kısa süre.
     *
     * <p>10 saniye: ağ bir sorun yaşarsa ve oyuncu geri düşerse, saniyede
     * bir aktarım denemesi hem sunucuları hem oyuncuyu bitirir.
     *
     * <p>Başarı kadar BAŞARISIZLIĞI da kapsar: başarısız deneme de diske
     * kayıt yazıp ağa çıkar, yani tekrarı en az başarılısı kadar pahalıdır.
     */
    private static final long COOLDOWN_MS = 10_000;

    /** Hedefe bağlanma ve yanıt bekleme süresi. */
    private static final int PEER_TIMEOUT_MS = 8000;

    /**
     * Sınıra bu kadar chunk kala uyarı gösterilir.
     *
     * <p>4 chunk = 64 blok: oyuncu "birazdan öbür bölgeye geçeceğim" bilgisini
     * hareket hâlindeyken okuyabilecek kadar erken alır.
     */
    private static final int WARN_CHUNKS = 4;

    /**
     * Beklenen bir varışın geçerlilik süresi.
     *
     * <p>30 saniye: aktarım paketini alan istemci normalde 1-2 saniyede bize
     * bağlanır. Daha uzun tutmak, aktarımdan vazgeçip çok sonra elle giren
     * oyuncunun gerçek girişini de yutardı.
     */
    private static final long INCOMING_TTL_MS = 30_000;

    private final MinecraftServer server;
    private final Coordinator coordinator;
    private final ExecutorService workers;

    /** Aktarımı süren oyuncular: ikinci bir denemeyi engeller. */
    private final Map<UUID, Long> inFlight = new ConcurrentHashMap<>();
    /** Son aktarım DENEMESİ zamanları (soğuma süresi için). */
    private final Map<UUID, Long> lastTransfer = new ConcurrentHashMap<>();
    /**
     * Aktarımla GELMESİ beklenen oyuncular.
     *
     * <p>Bunların girişi "oyuna katıldı" diye duyurulmaz; onları hedef düğüm
     * zaten "bu bölgeye geçti" diye anons etti.
     */
    private final Map<UUID, Long> incoming = new ConcurrentHashMap<>();

    private volatile int handoffs;

    public HandoffService(MinecraftServer server, Coordinator coordinator) {
        this.server = server;
        this.coordinator = coordinator;
        this.workers = Executors.newCachedThreadPool(r -> {
            Thread t = new Thread(r, "mcos-link-handoff");
            t.setDaemon(true);
            return t;
        });
    }

    public void stop() {
        workers.shutdownNow();
    }

    public int handoffs() {
        return handoffs;
    }

    /**
     * "Bu oyuncu birazdan aktarımla gelecek" diye işaretler.
     *
     * <p>{@link LinkServer} gelen kaydı diske yazdıktan sonra çağırır.
     */
    public void expectIncoming(UUID id) {
        long now = System.currentTimeMillis();
        // Gelmeyen varışlar birikmesin: aktarım paketi gönderildikten sonra
        // oyunu kapatan bir oyuncunun izi haritada sonsuza kadar kalırdı.
        incoming.values().removeIf(at -> now - at > INCOMING_TTL_MS);
        incoming.put(id, now);
    }

    /**
     * Giriş, beklenen bir aktarım varışı mı? İşareti TÜKETİR.
     *
     * <p>Tüketmek şart: aynı oyuncu daha sonra gerçekten yeniden girdiğinde
     * o giriş normal şekilde duyurulmalı.
     */
    public boolean consumeIncoming(UUID id) {
        Long at = incoming.remove(id);
        return at != null && System.currentTimeMillis() - at <= INCOMING_TTL_MS;
    }

    /**
     * Her oyuncu için sınır denetimi yapar.
     *
     * <p>Oyun iş parçacığından, saniyede bir çağrılır. Her tikte çağırmak
     * gereksiz: oyuncu bir tikte en fazla birkaç blok gider ve sınır
     * gecikmesi zaten 32 bloktur.
     */
    public void tick() {
        Topology top = coordinator.topology();
        if (!top.enabled) {
            return;
        }
        Topology.Area mine = top.selfArea();
        if (mine == null) {
            return;
        }

        for (ServerPlayerEntity player : server.getPlayerManager().getPlayerList()) {
            try {
                check(top, mine, player);
            } catch (Exception e) {
                Log.error("sınır denetimi hata verdi: "
                        + player.getGameProfile().name(), e);
            }
        }
    }

    private void check(Topology top, Topology.Area mine, ServerPlayerEntity player) {
        UUID id = player.getUuid();
        if (inFlight.containsKey(id)) {
            return;
        }

        int chunkX = player.getBlockX() >> 4;

        if (mine.contains(chunkX)) {
            warnIfNearBorder(top, mine, player, chunkX);
            return;
        }

        // Sınırı ne kadar aştık? Gecikme kadar aşmadıysak henüz aktarma.
        int overshoot = overshoot(mine, chunkX);
        if (overshoot < top.hysteresisChunks) {
            return;
        }

        // Son deneme (başarılı ya da başarısız) üzerinden COOLDOWN_MS geçmeden
        // yeniden denemeyiz.
        Long last = lastTransfer.get(id);
        if (last != null && System.currentTimeMillis() - last < COOLDOWN_MS) {
            return;
        }

        String targetName = top.ownerOf(chunkX);
        if (targetName.equals(top.self)) {
            return; // sahibi biziz; aktarılacak bir şey yok
        }
        Topology.Node target = top.node(targetName);
        if (target == null) {
            Log.warn("sahip düğüm topolojide yok: " + targetName);
            return;
        }
        if (!target.online) {
            // Çevrimdışı bir düğüme göndermek, oyuncuyu bağlantısız bırakır.
            // Onu yerinde tutup durumu SÖYLÜYORUZ.
            player.sendMessage(Text.literal(targetName
                            + " şu anda çevrimdışı — bu bölgeye geçilemiyor")
                    .formatted(Formatting.RED), true);
            return;
        }

        begin(top, player, target);
    }

    /** Sınırı kaç chunk aştığımızı verir. */
    private static int overshoot(Topology.Area mine, int chunkX) {
        if (!mine.unboundedMin && chunkX < mine.minChunkX) {
            return mine.minChunkX - chunkX;
        }
        if (!mine.unboundedMax && chunkX >= mine.maxChunkX) {
            return chunkX - mine.maxChunkX + 1;
        }
        return 0;
    }

    /**
     * Sınıra yaklaşan oyuncuya eylem çubuğunda bilgi verir.
     *
     * <p>Uyarı OLMADAN aktarım, oyuncuya "sunucu beni attı" gibi gelir. Bir
     * satırlık ön bilgi, aynı olayı "bölge değiştiriyorum" yapar.
     */
    private void warnIfNearBorder(Topology top, Topology.Area mine,
                                  ServerPlayerEntity player, int chunkX) {
        int distance = Integer.MAX_VALUE;
        String neighbour = null;

        if (!mine.unboundedMin) {
            int d = chunkX - mine.minChunkX;
            if (d < distance) {
                distance = d;
                neighbour = top.ownerOf(mine.minChunkX - 1);
            }
        }
        if (!mine.unboundedMax) {
            int d = mine.maxChunkX - 1 - chunkX;
            if (d < distance) {
                distance = d;
                neighbour = top.ownerOf(mine.maxChunkX);
            }
        }
        if (neighbour == null || distance > WARN_CHUNKS) {
            return;
        }
        player.sendMessage(Text.literal(neighbour + " bölgesine "
                        + (distance * 16) + " blok kaldı")
                .formatted(Formatting.DARK_AQUA), true);
    }

    /** Aktarımı başlatır (elle komutla da çağrılabilir). */
    public void begin(Topology top, ServerPlayerEntity player, Topology.Node target) {
        UUID id = player.getUuid();
        if (inFlight.putIfAbsent(id, System.currentTimeMillis()) != null) {
            return;
        }

        String name = player.getGameProfile().name();
        player.sendMessage(Text.literal(target.name + " bölgesine geçiliyor…")
                .formatted(Formatting.AQUA), true);

        // Oyuncu verisini ŞİMDİ diske yaz. Bu, aktarımın doğruluğu için
        // zorunlu tek "eşleme duyarlı" çağrıdır: kayıt olmadan gönderilecek
        // bir dosya yok.
        server.getPlayerManager().saveAllPlayerData();

        byte[] data;
        try {
            data = PlayerData.read(server, id);
        } catch (Exception e) {
            abort(player, id, "oyuncu verisi okunamadı: " + e.getMessage());
            return;
        }
        if (data == null) {
            abort(player, id, "oyuncu kaydı bulunamadı");
            return;
        }

        String token = top.token;
        String self = top.self;

        // Ağ işi OYUN İŞ PARÇACIĞINDA YAPILMAZ: hedef yanıt vermezse tüm
        // sunucu sekiz saniye donardı.
        workers.execute(() -> {
            boolean ok = push(target, token, self, name, id, data);
            server.execute(() -> {
                inFlight.remove(id);

                // Yakalanan gerçek hata: soğuma damgası YALNIZCA başarı
                // dalında yazılıyordu. Hedef düğüm link portunu dinleyemediyse
                // (LinkServer.start() bunu ölümcül saymaz) ya da anahtar
                // uyuşmadıysa push milisaniyeler içinde başarısız olur; o
                // zaman satır 141'deki soğuma denetimi hiç tutmaz ve sınırın
                // ötesinde duran oyuncu için TÜM deneme saniyede bir tekrar
                // ederdi: dakikada ~60 kez saveAllPlayerData() (her çevrimiçi
                // oyuncunun .dat'ını yazar, oyun iş parçacığında) ve dakikada
                // ~60 kırmızı sohbet satırı. Damga artık her SONLANAN denemeye
                // yazılıyor; başarısızlık da COOLDOWN_MS'e tabi.
                lastTransfer.put(id, System.currentTimeMillis());

                ServerPlayerEntity p = server.getPlayerManager().getPlayer(id);
                if (p == null) {
                    return; // oyuncu bu arada çıkmış
                }
                if (!ok) {
                    // Çevrimdışı düğüm dalıyla (satır 158) aynı biçim: bu bir
                    // "şu an geçilemiyor" bilgisidir, sohbeti kalıcı olarak
                    // kirletmesi gerekmez.
                    p.sendMessage(Text.literal(target.name
                                    + " bölgesine geçilemedi — yerinde kaldınız")
                            .formatted(Formatting.RED), true);
                    return;
                }
                handoffs++;
                coordinator.event("handoff", name, self, target.name, null);
                Log.info(name + " -> " + target.name + " ("
                        + target.host + ":" + target.mcPort + ")");

                // İstemciyi aktar. Bu paketten sonra istemci bizden kopar;
                // başka bir şey göndermenin anlamı yok.
                p.networkHandler.sendPacket(
                        new ServerTransferS2CPacket(target.host, target.mcPort));
            });
        });
    }

    private void abort(ServerPlayerEntity player, UUID id, String why) {
        inFlight.remove(id);
        // Yakalanan gerçek hata: iptal de bir SONLANMIŞ denemedir. Damga
        // yazılmadığı için "oyuncu kaydı bulunamadı" gibi kalıcı bir durum,
        // saniyede bir saveAllPlayerData() + bir uyarı satırı üretiyordu.
        lastTransfer.put(id, System.currentTimeMillis());
        Log.warn("aktarım iptal: " + why);
        player.sendMessage(Text.literal("Bölge değişimi başarısız: " + why)
                .formatted(Formatting.RED), false);
    }

    /** Oyuncu verisini hedefe gönderir; onay gelirse true. */
    private boolean push(Topology.Node target, String token, String self,
                         String playerName, UUID id, byte[] data) {
        try (Socket s = new Socket()) {
            s.connect(new InetSocketAddress(target.host, target.linkPort),
                    PEER_TIMEOUT_MS);
            s.setSoTimeout(PEER_TIMEOUT_MS);

            try (DataOutputStream out = new DataOutputStream(s.getOutputStream());
                 DataInputStream in = new DataInputStream(s.getInputStream())) {

                JsonObject h = new JsonObject();
                h.addProperty("cmd", "handoff");
                h.addProperty("token", token);
                h.addProperty("uuid", id.toString());
                h.addProperty("player", playerName);
                h.addProperty("from", self);
                LinkProtocol.write(out, h, data);

                LinkProtocol.Message res = LinkProtocol.read(in);
                if (!res.ok()) {
                    Log.warn("hedef düğüm reddetti: " + res.error());
                    return false;
                }
                return true;
            }
        } catch (Exception e) {
            Log.warn("oyuncu verisi gönderilemedi (" + target + "): "
                    + e.getMessage());
            return false;
        }
    }

    /** Bir metni tüm eş düğümlere yayar (sohbet ve duyurular için). */
    public void broadcastToPeers(String cmd, JsonObject header) {
        Topology top = coordinator.topology();
        if (!top.enabled) {
            return;
        }
        for (Topology.Node n : top.nodes) {
            if (n.self || !n.online) {
                continue;
            }
            workers.execute(() -> {
                try (Socket s = new Socket()) {
                    s.connect(new InetSocketAddress(n.host, n.linkPort), 3000);
                    s.setSoTimeout(3000);
                    try (DataOutputStream out = new DataOutputStream(s.getOutputStream());
                         DataInputStream in = new DataInputStream(s.getInputStream())) {
                        JsonObject h = header.deepCopy();
                        h.addProperty("cmd", cmd);
                        h.addProperty("token", top.token);
                        h.addProperty("node", top.self);
                        LinkProtocol.write(out, h, null);
                        LinkProtocol.read(in);
                    }
                } catch (Exception e) {
                    // Sohbet yayını başarısız olabilir; oyunu durdurmaz.
                    Log.debug("eşe yayın başarısız (" + n + "): " + e);
                }
            });
        }
    }
}
