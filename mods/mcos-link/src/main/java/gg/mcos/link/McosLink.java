package gg.mcos.link;

import com.google.gson.JsonObject;
import net.fabricmc.api.DedicatedServerModInitializer;
import net.fabricmc.fabric.api.command.v2.CommandRegistrationCallback;
import net.fabricmc.fabric.api.event.lifecycle.v1.ServerLifecycleEvents;
import net.fabricmc.fabric.api.event.lifecycle.v1.ServerTickEvents;
import net.fabricmc.fabric.api.message.v1.ServerMessageEvents;
import net.fabricmc.fabric.api.networking.v1.ServerPlayConnectionEvents;
import net.minecraft.network.packet.s2c.play.PlayerListHeaderS2CPacket;
import net.minecraft.server.MinecraftServer;
import net.minecraft.text.Text;
import net.minecraft.util.Formatting;
import net.minecraft.world.Difficulty;

/**
 * MCOS Link — giriş noktası.
 *
 * <p>Birden çok MCOS cihazının AYNI dünyayı çalıştırmasını sağlar: dünya X
 * ekseninde dilimlere bölünür, her cihaz kendi dilimini simüle eder ve
 * oyuncu sınırı geçtiğinde envanteriyle birlikte kesintisiz aktarılır.
 *
 * <p><b>Yalnızca sunucu tarafı.</b> İstemcide hiçbir mod gerekmez; aktarım,
 * Minecraft'ın 1.20.5 ile eklediği kendi transfer paketiyle yapılır.
 *
 * <h2>Yaşam döngüsü</h2>
 * <pre>
 *   sunucu açılır
 *      └─ Coordinator başlar     → 5 sn'de bir topolojiyi çeker
 *      └─ LinkServer başlar      → 27893'te eşleri dinler
 *      └─ her saniye HandoffService.tick()
 *   sunucu kapanır
 *      └─ hepsi düzgünce durur
 * </pre>
 */
public final class McosLink implements DedicatedServerModInitializer {

    /**
     * Sınır denetimi sıklığı (tik).
     *
     * <p>20 tik = 1 saniye. Her tikte denetlemek gereksiz: oyuncu bir tikte
     * en fazla birkaç blok gider ve aktarım gecikmesi zaten 32 bloktur.
     * Saniyede bir denetim, 100 oyuncuda bile ölçülemeyecek kadar ucuzdur.
     */
    private static final int CHECK_INTERVAL_TICKS = 20;

    /**
     * Sekme listesi başlığının yenilenme sıklığı.
     *
     * <p>5 saniye: oyuncu sayısı bu kadar hızlı değişmez ve her oyuncuya
     * paket göndermek bedavaya gelmez.
     */
    private static final int TAB_INTERVAL_TICKS = 100;

    private Coordinator coordinator;
    private LinkServer linkServer;
    private HandoffService handoff;
    private int tickCounter;
    private String appliedDifficulty = "";

    @Override
    public void onInitializeServer() {
        ServerLifecycleEvents.SERVER_STARTED.register(this::onStarted);
        ServerLifecycleEvents.SERVER_STOPPING.register(this::onStopping);
        ServerTickEvents.END_SERVER_TICK.register(this::onTick);

        // Sohbeti eşlere yay: "tek sunucu gibi görünmesi" isteğinin en
        // görünür parçası budur. Oyuncular farklı makinelerde ama aynı
        // sohbette olmalı.
        ServerMessageEvents.CHAT_MESSAGE.register((message, sender, params) -> {
            if (handoff == null) {
                return;
            }
            JsonObject h = new JsonObject();
            h.addProperty("player", sender.getGameProfile().name());
            h.addProperty("text", message.getContent().getString());
            handoff.broadcastToPeers("chat", h);
        });

        ServerPlayConnectionEvents.JOIN.register((netHandler, sender, server) -> {
            if (handoff == null) {
                return;
            }

            // Yakalanan gerçek hata: bu geri çağrı, GERÇEK bir girişi sınır
            // geçişiyle GELEN oyuncudan ayırt etmiyordu ve her geçişte tüm
            // eşlere "X oyuna katıldı" yayınlıyordu. Ali mcos-lab'dan
            // mcos-oda'ya geçtiğinde mcos-lab'daki oyuncular önce vanilla'nın
            // "Ali left the game" satırını, hemen ardından mcos-oda'dan
            // itilen "Ali oyuna katıldı" satırını görüyordu — yani Ali
            // çıkıp yeniden girmiş gibi. Modun var olma sebebi tam da bu
            // yanılsamanın kırılmaması. Aktarımla gelen oyuncu için duyuru
            // yapılmaz; hedef düğüm onu zaten "bu bölgeye geçti" diye anons
            // etti (LinkServer.applyHandoff).
            if (handoff.consumeIncoming(netHandler.getPlayer().getUuid())) {
                return;
            }

            JsonObject h = new JsonObject();
            h.addProperty("text", netHandler.getPlayer().getGameProfile().name()
                    + " oyuna katıldı");
            handoff.broadcastToPeers("announce", h);
        });

        // Yakalanan gerçek hata: burada "coordinator != null" denetimi vardı.
        // Fabric bu geri çağrıyı CommandManager kurucusundan, yani dünya
        // yüklenirken ve SERVER_STARTED'dan ÖNCE tetikler; o anda coordinator
        // hâlâ null olduğu için /mcoslink hiçbir normal açılışta
        // KAYDEDİLMİYORDU. Servisleri o anki DEĞERLERİYLE yakalamak yerine
        // tembel çözüyoruz: kayıt koşulsuz yapılır, servisler komut
        // çalıştığında okunur.
        CommandRegistrationCallback.EVENT.register((dispatcher, registry, env) ->
                LinkCommands.register(dispatcher, () -> coordinator, () -> handoff));

        Log.info("yüklendi — MCOS daemon'una bağlanılacak");
    }

    private void onStarted(MinecraftServer server) {
        coordinator = new Coordinator();
        coordinator.start();

        handoff = new HandoffService(server, coordinator);

        int port = LinkProtocol.DEFAULT_PORT;
        String env = System.getenv("MCOS_LINK_PORT");
        if (env != null && !env.isBlank()) {
            try {
                port = Integer.parseInt(env.trim());
            } catch (NumberFormatException e) {
                Log.warn("MCOS_LINK_PORT sayı değil: " + env + " — " + port
                        + " kullanılıyor");
            }
        }
        // LinkServer, gelen aktarımı HandoffService'e bildirmek zorunda:
        // aksi halde aktarımla gelen oyuncunun girişi "oyuna katıldı" diye
        // duyurulur.
        linkServer = new LinkServer(server, coordinator, handoff, port);
        linkServer.start();
    }

    private void onStopping(MinecraftServer server) {
        if (linkServer != null) {
            linkServer.stop();
        }
        if (handoff != null) {
            handoff.stop();
        }
        if (coordinator != null) {
            coordinator.stop();
        }
        Log.info("durduruldu");
    }

    private void onTick(MinecraftServer server) {
        tickCounter++;

        if (tickCounter % CHECK_INTERVAL_TICKS == 0 && handoff != null) {
            handoff.tick();
            applyDifficulty(server);
        }
        if (tickCounter % TAB_INTERVAL_TICKS == 0) {
            updateTabList(server);
        }
    }

    /**
     * Zorluğu topolojiyle eşitler.
     *
     * <p>ŞART: bir dilimde peaceful, ötekinde hard olsaydı, oyuncu sınırı
     * geçtiğinde canavarların kaybolduğunu görürdü — "aynı dünya" yanılsaması
     * anında kırılır. MCOS panelinde seçilen zorluk buradan uygulanır.
     *
     * <p>Yalnızca DEĞİŞTİĞİNDE uygulanır: her saniye setDifficulty çağırmak
     * kayıt dosyasına gereksiz yazma yapar.
     */
    private void applyDifficulty(MinecraftServer server) {
        Topology t = coordinator.topology();
        if (!t.enabled || t.difficulty.isEmpty()
                || t.difficulty.equals(appliedDifficulty)) {
            return;
        }
        Difficulty d = switch (t.difficulty) {
            case "peaceful" -> Difficulty.PEACEFUL;
            case "easy" -> Difficulty.EASY;
            case "hard" -> Difficulty.HARD;
            case "normal" -> Difficulty.NORMAL;
            default -> null;
        };
        if (d == null) {
            Log.warn("bilinmeyen zorluk: " + t.difficulty);
            appliedDifficulty = t.difficulty; // tekrar tekrar uyarma
            return;
        }
        server.setDifficulty(d, true);
        appliedDifficulty = t.difficulty;
        Log.info("zorluk topolojiyle eşitlendi: " + t.difficulty);
    }

    /**
     * Sekme listesine düğüm bilgisini yazar.
     *
     * <p>Oyuncu listesine UZAK oyuncuları gerçek satır olarak eklemek,
     * istemciye sahte profil paketleri göndermeyi gerektirir; bu, kaydı
     * olmayan oyuncularda istemci hatalarına yol açar. Bunun yerine başlık
     * ve altbilgide toplam sayıyı gösteriyoruz — dürüst ve kırılgan değil.
     */
    private void updateTabList(MinecraftServer server) {
        if (coordinator == null) {
            return;
        }
        Topology t = coordinator.topology();
        if (!t.enabled) {
            return;
        }

        int total = 0;
        int online = 0;
        StringBuilder nodes = new StringBuilder();
        for (Topology.Node n : t.nodes) {
            int count = n.self
                    ? server.getPlayerManager().getCurrentPlayerCount()
                    : n.players;
            total += count;
            if (n.online) {
                online++;
            }
            if (nodes.length() > 0) {
                nodes.append("   ");
            }
            nodes.append(n.self ? "▸" : " ").append(n.name)
                    .append(" ").append(count);
        }

        Text header = Text.literal("MCOS Link").formatted(Formatting.AQUA)
                .append(Text.literal("   " + total + " oyuncu · "
                                + online + "/" + t.nodes.size() + " cihaz")
                        .formatted(Formatting.GRAY));
        Text footer = Text.literal(nodes.toString()).formatted(Formatting.DARK_GRAY);

        PlayerListHeaderS2CPacket packet = new PlayerListHeaderS2CPacket(header, footer);
        server.getPlayerManager().getPlayerList()
                .forEach(p -> p.networkHandler.sendPacket(packet));
    }
}
