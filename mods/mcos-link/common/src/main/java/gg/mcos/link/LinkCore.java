package gg.mcos.link;

import com.google.gson.JsonObject;

import java.util.UUID;

/**
 * Modun sürümden bağımsız yaşam döngüsü.
 *
 * <p>Eskiden bu mantık doğrudan Fabric giriş noktasındaydı ve Minecraft
 * sınıflarıyla iç içeydi; tek bir sürüm için derlenebiliyordu. Şimdi
 * bağdaştırıcının giriş noktası ({@code gg.mcos.link.fabric.McosLink}) yalnızca
 * Fabric olaylarını buraya iletir, kararların tamamı burada verilir.
 *
 * <h2>Yaşam döngüsü</h2>
 * <pre>
 *   sunucu açılır              → {@link #started}
 *      └─ Coordinator başlar     → 5 sn'de bir topolojiyi çeker
 *      └─ LinkServer başlar      → 27893'te eşleri dinler
 *   her tik                    → {@link #tick}
 *      └─ saniyede bir HandoffService.tick() ve zorluk eşitleme
 *      └─ 5 sn'de bir sekme listesi
 *   sunucu kapanır             → {@link #stopping}
 *      └─ hepsi düzgünce durur
 * </pre>
 *
 * <p>Alanlar {@code volatile}: komutlar ve olaylar servisleri tembel okur
 * (bkz. {@link LinkCommands#register}) ve {@link #started} öncesinde
 * {@code null} görmeleri BEKLENEN bir durumdur.
 */
public final class LinkCore {

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

    private volatile Game game;
    private volatile Coordinator coordinator;
    private volatile HandoffService handoff;
    private LinkServer linkServer;
    private int tickCounter;
    private String appliedDifficulty = "";

    public Game game() {
        return game;
    }

    public Coordinator coordinator() {
        return coordinator;
    }

    public HandoffService handoff() {
        return handoff;
    }

    /** Sunucu tamamen açıldı ({@code SERVER_STARTED}). */
    public void started(Game g) {
        game = g;
        coordinator = new Coordinator();
        coordinator.start();

        handoff = new HandoffService(g, coordinator);

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
        linkServer = new LinkServer(g, coordinator, handoff, port);
        linkServer.start();
    }

    /** Sunucu kapanıyor ({@code SERVER_STOPPING}). */
    public void stopping() {
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

    /** Her sunucu tikinin sonunda ({@code END_SERVER_TICK}). */
    public void tick() {
        if (game == null) {
            return; // SERVER_STARTED'dan önceki tikler
        }
        tickCounter++;

        if (tickCounter % CHECK_INTERVAL_TICKS == 0 && handoff != null) {
            handoff.tick();
            applyDifficulty();
        }
        if (tickCounter % TAB_INTERVAL_TICKS == 0) {
            updateTabList();
        }
    }

    /**
     * Bir oyuncu sohbete yazdı.
     *
     * <p>Sohbeti eşlere yay: "tek sunucu gibi görünmesi" isteğinin en
     * görünür parçası budur. Oyuncular farklı makinelerde ama aynı sohbette
     * olmalı.
     */
    public void chat(String player, String text) {
        HandoffService h = handoff;
        if (h == null) {
            return;
        }
        JsonObject o = new JsonObject();
        o.addProperty("player", player);
        o.addProperty("text", text);
        h.broadcastToPeers("chat", o);
    }

    /** Bir oyuncu bu sunucuya bağlandı ({@code JOIN}). */
    public void joined(UUID id, String name) {
        HandoffService h = handoff;
        if (h == null) {
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
        if (h.consumeIncoming(id)) {
            return;
        }

        JsonObject o = new JsonObject();
        o.addProperty("text", name + " oyuna katıldı");
        h.broadcastToPeers("announce", o);
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
    private void applyDifficulty() {
        Topology t = coordinator.topology();
        if (!t.enabled || t.difficulty.isEmpty()
                || t.difficulty.equals(appliedDifficulty)) {
            return;
        }
        Game.Difficulty d = Game.Difficulty.parse(t.difficulty);
        if (d == null) {
            Log.warn("bilinmeyen zorluk: " + t.difficulty);
            appliedDifficulty = t.difficulty; // tekrar tekrar uyarma
            return;
        }
        game.setDifficulty(d);
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
    private void updateTabList() {
        Coordinator c = coordinator;
        if (c == null) {
            return;
        }
        Topology t = c.topology();
        if (!t.enabled) {
            return;
        }

        int total = 0;
        int online = 0;
        StringBuilder nodes = new StringBuilder();
        for (Topology.Node n : t.nodes) {
            int count = n.self ? game.playerCount() : n.players;
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

        Msg header = Msg.of("MCOS Link", Msg.Color.AQUA)
                .then("   " + total + " oyuncu · " + online + "/"
                        + t.nodes.size() + " cihaz", Msg.Color.GRAY);
        Msg footer = Msg.of(nodes.toString(), Msg.Color.DARK_GRAY);
        game.sendTabList(header, footer);
    }
}
