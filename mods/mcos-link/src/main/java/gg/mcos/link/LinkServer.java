package gg.mcos.link;

import com.google.gson.JsonObject;
import net.minecraft.server.MinecraftServer;
import net.minecraft.text.Text;
import net.minecraft.util.Formatting;

import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.io.IOException;
import java.net.ServerSocket;
import java.net.Socket;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.security.MessageDigest;
import java.util.UUID;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;

/**
 * Diğer düğümlerden gelen bağlantıları karşılar.
 *
 * <p>Üç komut:
 * <ul>
 *   <li>{@code handoff} — gelen oyuncu verisini diske yazar,</li>
 *   <li>{@code chat} — komşudan gelen sohbet satırını yayınlar,</li>
 *   <li>{@code ping} — canlılık ve düğüm adı sorgusu.</li>
 * </ul>
 *
 * <p><b>Güvenlik.</b> Her komut, MCOS eşleştirme anahtarını taşımak
 * zorundadır. Anahtar olmadan, ağdaki herhangi biri bu porta bağlanıp
 * istediği oyuncunun envanterini DEĞİŞTİREBİLİRDİ — çünkü handoff, bir
 * oyuncunun kayıt dosyasını olduğu gibi yazar. Karşılaştırma SABİT SÜRELİ
 * yapılır: bayt bayt erken çıkan bir karşılaştırma, doğru önekin uzunluğunu
 * zamanlamadan sızdırır.
 */
public final class LinkServer {

    /**
     * Gelen kaydın oyun iş parçacığında uygulanması için beklenecek süre.
     *
     * <p>5 saniye: gönderen taraf yanıtı zaten 8 saniye bekliyor
     * ({@code HandoffService.PEER_TIMEOUT_MS}); ondan önce yanıt vermeliyiz
     * ki oyuncu "geçilemedi" cevabını zamanında alsın.
     */
    private static final long APPLY_TIMEOUT_MS = 5000;

    /** Oyuncu sayısı için oyun iş parçacığından yanıt bekleme süresi. */
    private static final long COUNT_TIMEOUT_MS = 2000;

    private final MinecraftServer server;
    private final Coordinator coordinator;
    private final HandoffService handoff;
    private final int port;

    private volatile ServerSocket socket;
    private volatile boolean running;
    private final ExecutorService workers;

    /** Son bilinen oyuncu sayısı; ping'i asla geciktirmemek için. */
    private volatile int lastPlayerCount;

    public LinkServer(MinecraftServer server, Coordinator coordinator,
                      HandoffService handoff, int port) {
        this.server = server;
        this.coordinator = coordinator;
        this.handoff = handoff;
        this.port = port;
        this.workers = Executors.newCachedThreadPool(r -> {
            Thread t = new Thread(r, "mcos-link-peer");
            t.setDaemon(true);
            return t;
        });
    }

    /**
     * Dinlemeye başlar.
     *
     * <p>Başarısızlık ÖLÜMCÜL DEĞİLDİR: port doluysa ortak dünya çalışmaz ama
     * sunucu normal şekilde açılır. Bir mod, sunucunun açılmasını engellemeye
     * hak kazanacak kadar önemli değildir.
     */
    public void start() {
        try {
            socket = new ServerSocket(port);
            running = true;
            Thread t = new Thread(this::acceptLoop, "mcos-link-accept");
            t.setDaemon(true);
            t.start();
            Log.info("düğümler arası port dinleniyor: " + port);
        } catch (IOException e) {
            Log.warn("port " + port + " dinlenemedi: " + e.getMessage()
                    + " — ortak dünya bu düğümde çalışmayacak");
        }
    }

    public void stop() {
        running = false;
        try {
            if (socket != null) {
                socket.close();
            }
        } catch (IOException ignored) {
            // kapanışta önemsiz
        }
        workers.shutdownNow();
    }

    private void acceptLoop() {
        while (running) {
            try {
                Socket s = socket.accept();
                workers.execute(() -> handle(s));
            } catch (IOException e) {
                if (running) {
                    Log.warn("bağlantı kabul edilemedi: " + e.getMessage());
                }
            }
        }
    }

    private void handle(Socket s) {
        try (Socket sock = s;
             DataInputStream in = new DataInputStream(sock.getInputStream());
             DataOutputStream out = new DataOutputStream(sock.getOutputStream())) {

            // Bir eşin bağlantıyı süresiz açık tutmasını engeller.
            sock.setSoTimeout(30_000);

            LinkProtocol.Message msg = LinkProtocol.read(in);
            Topology top = coordinator.topology();

            if (!authorized(msg.str("token"), top.token)) {
                Log.warn("yetkisiz " + msg.cmd() + " isteği reddedildi: "
                        + sock.getInetAddress());
                LinkProtocol.write(out, LinkProtocol.fail("yetkisiz"), null);
                return;
            }

            switch (msg.cmd()) {
                case "ping" -> {
                    JsonObject o = LinkProtocol.ok();
                    o.addProperty("node", top.self);
                    o.addProperty("players", playerCount());
                    LinkProtocol.write(out, o, null);
                }
                case "handoff" -> handleHandoff(msg, out);
                case "chat" -> handleChat(msg, out);
                case "announce" -> handleAnnounce(msg, out);
                default -> LinkProtocol.write(out,
                        LinkProtocol.fail("bilinmeyen komut: " + msg.cmd()), null);
            }
        } catch (Exception e) {
            Log.debug("eş bağlantısı hata verdi: " + e);
        }
    }

    /** Sabit süreli anahtar karşılaştırması. */
    private static boolean authorized(String given, String expected) {
        if (expected == null || expected.isEmpty()) {
            // Anahtar yapılandırılmamışsa HİÇBİR isteği kabul etmeyiz.
            // "Anahtar yoksa serbest" kuralı, güvenliği kazara kapatmanın
            // en yaygın yoludur.
            return false;
        }
        if (given == null) {
            return false;
        }
        return MessageDigest.isEqual(
                given.getBytes(java.nio.charset.StandardCharsets.UTF_8),
                expected.getBytes(java.nio.charset.StandardCharsets.UTF_8));
    }

    /**
     * Gelen oyuncu verisini kayıt klasörüne yazar.
     *
     * <p>Oyuncu birazdan bize bağlanacak; vanilla bu dosyayı okuyup envanteri,
     * canı, XP'yi ve konumu geri yükleyecek. Yani mod hiçbir eşyayı elle
     * serileştirmiyor — oyunun KENDİ kayıt biçimini taşıyor. Bu, sürüm
     * değişse bile çalışmaya devam eder ve hiçbir NBT etiketi kaybolmaz.
     *
     * <p>Bu metot EŞ AĞ İŞ PARÇACIĞINDA çalışır ve yalnızca ayrıştırma
     * yapar; oyunun durumuna dokunan her şey {@link #applyHandoff} içinde,
     * oyun iş parçacığında olur.
     */
    private void handleHandoff(LinkProtocol.Message msg, DataOutputStream out)
            throws IOException {

        String uuid = msg.str("uuid");
        String name = msg.str("player");
        String from = msg.str("from");

        if (uuid.isEmpty() || msg.body == null || msg.body.length == 0) {
            LinkProtocol.write(out, LinkProtocol.fail("eksik veri"), null);
            return;
        }
        // UUID biçimini DOĞRULA: doğrudan dosya adına yazılıyor ve
        // "../../etc/passwd" gibi bir değer yol dışına çıkardı.
        if (!isUuid(uuid)) {
            LinkProtocol.write(out, LinkProtocol.fail("geçersiz uuid"), null);
            return;
        }
        UUID id = UUID.fromString(uuid);

        // Yakalanan gerçek hata: "oyuncu bizde çevrimiçi mi" denetimi ile
        // dosya yazımı bu ağ iş parçacığında yapılıyordu. getPlayer(UUID),
        // Yarn'da oyun iş parçacığının her giriş/çıkışta değiştirdiği düz bir
        // HashMap araması; eşzamanlı bir yeniden boyutlandırma sırasında
        // MEVCUT bir oyuncu için null dönebilir. O zaman denetim geçilir,
        // Files.move canlı oyuncunun playerdata/<uuid>.dat dosyasının üzerine
        // yazar ve oyuncu çıkarken vanilla kendi bellekteki durumunu geri
        // yazınca AKTARILAN ENVANTER YOK OLUR — applyHandoff'taki yorumun
        // tam olarak engellemek için var olduğu kayıp. Üstelik denetim ile
        // yazım arasında oyuncunun giriş yapması da aynı sonucu verirdi.
        //
        // Çözüm: denetim ve yazım TEK bir oyun iş parçacığı görevinde, arada
        // hiçbir tik geçmeden yapılır; eş yalnızca sonucu bekler.
        CompletableFuture<String> result = new CompletableFuture<>();
        server.execute(() -> {
            if (result.isDone()) {
                return; // eş beklemekten vazgeçti; dosyaya hiç dokunma
            }
            try {
                result.complete(applyHandoff(id, msg.body, name, from));
            } catch (Throwable t) {
                result.completeExceptionally(t);
            }
        });

        String error;
        try {
            error = result.get(APPLY_TIMEOUT_MS, TimeUnit.MILLISECONDS);
        } catch (Exception e) {
            // Görev henüz sırada bekliyorsa yukarıdaki isDone() denetimi onu
            // durdurur: gönderene "olmadı" deyip dosyayı yine de yazmak,
            // iki düğümde birbirinden habersiz iki kayıt bırakırdı.
            result.cancel(false);
            if (e instanceof InterruptedException) {
                // Kapanışta workers.shutdownNow() bizi böler; bayrağı geri
                // koymazsak havuz kapandığını anlayamaz.
                Thread.currentThread().interrupt();
            }
            Log.error("gelen oyuncu verisi uygulanamadı: " + name, e);
            LinkProtocol.write(out,
                    LinkProtocol.fail("oyuncu verisi uygulanamadı"), null);
            return;
        }
        if (error != null) {
            LinkProtocol.write(out, LinkProtocol.fail(error), null);
            return;
        }
        LinkProtocol.write(out, LinkProtocol.ok(), null);
    }

    /**
     * Gelen kaydı uygular. <b>Yalnızca oyun iş parçacığından çağrılır.</b>
     *
     * @return başarıysa {@code null}, aksi halde eşe dönecek hata metni
     */
    private String applyHandoff(UUID id, byte[] body, String name, String from) {
        // Oyuncu şu anda BİZDE çevrimiçiyse dosyayı yazmak, çıkışta onun
        // kendi verisiyle üzerine yazılmasına ve gelen envanterin
        // kaybolmasına yol açar.
        if (server.getPlayerManager().getPlayer(id) != null) {
            return "oyuncu bu düğümde zaten çevrimiçi";
        }

        Path dir = PlayerData.directory(server);
        Path target = dir.resolve(id + ".dat");
        Path tmp = dir.resolve(id + ".dat.mcos-tmp");
        try {
            Files.createDirectories(dir);
            Files.write(tmp, body);
            // Atomik taşıma: yarım yazılmış bir kayıt dosyası, oyuncunun
            // envanterini tamamen kaybetmesi demektir.
            Files.move(tmp, target, StandardCopyOption.REPLACE_EXISTING,
                    StandardCopyOption.ATOMIC_MOVE);
        } catch (IOException e) {
            try {
                Files.deleteIfExists(tmp);
            } catch (IOException ignored) {
                // Geçici dosya silinemedi; asıl hatayı gizlemesine izin verme.
            }
            Log.error("gelen oyuncu verisi yazılamadı: " + name, e);
            return e.getMessage() == null
                    ? "oyuncu verisi yazılamadı" : e.getMessage();
        }

        // Oyuncu birazdan girecek; girişi "oyuna katıldı" diye
        // duyurulmamalı — aşağıda zaten "bu bölgeye geçti" diyoruz.
        handoff.expectIncoming(id);

        Log.info(name + " geliyor (" + from + " düğümünden, "
                + body.length + " bayt)");
        coordinator.event("arrive", name, from, coordinator.topology().self, null);

        // Diğer oyunculara haber ver: tek sunucu hissi için, gelen kişinin
        // "katıldı" diye değil "yan bölgeden geldi" diye görünmesi gerekir.
        broadcast(Text.literal(name + " bu bölgeye geçti")
                .formatted(Formatting.GRAY));

        return null;
    }

    /**
     * Oyuncu sayısını OYUN İŞ PARÇACIĞINDAN okur.
     *
     * <p>Yakalanan gerçek hata: {@code getCurrentPlayerCount()} de bu ağ iş
     * parçacığından çağrılıyordu; oyuncu listesi oyun iş parçacığına aittir.
     *
     * <p>Yanıt gelmezse SON BİLİNEN sayıyla devam ederiz: ping bir canlılık
     * sorgusudur, oyun iş parçacığı bir saniye meşgul diye düğümü çevrimdışı
     * göstermek doğru olmaz.
     */
    private int playerCount() {
        CompletableFuture<Integer> f = new CompletableFuture<>();
        server.execute(() -> f.complete(
                server.getPlayerManager().getCurrentPlayerCount()));
        try {
            lastPlayerCount = f.get(COUNT_TIMEOUT_MS, TimeUnit.MILLISECONDS);
        } catch (Exception e) {
            f.cancel(false);
            Log.debug("oyuncu sayısı zamanında alınamadı: " + e);
        }
        return lastPlayerCount;
    }

    private void handleChat(LinkProtocol.Message msg, DataOutputStream out)
            throws IOException {
        String node = msg.str("node");
        String player = msg.str("player");
        String text = msg.str("text");
        if (player.isEmpty() || text.isEmpty()) {
            LinkProtocol.write(out, LinkProtocol.fail("eksik veri"), null);
            return;
        }
        // Uzak oyuncuyu ayırt edilebilir yaz: aynı dünyada ama başka bir
        // makinede olduğu belli olsun.
        broadcast(Text.literal("[" + node + "] ").formatted(Formatting.DARK_GRAY)
                .append(Text.literal("<" + player + "> ").formatted(Formatting.WHITE))
                .append(Text.literal(text).formatted(Formatting.GRAY)));
        LinkProtocol.write(out, LinkProtocol.ok(), null);
    }

    private void handleAnnounce(LinkProtocol.Message msg, DataOutputStream out)
            throws IOException {
        String text = msg.str("text");
        if (!text.isEmpty()) {
            broadcast(Text.literal(text).formatted(Formatting.GRAY));
        }
        LinkProtocol.write(out, LinkProtocol.ok(), null);
    }

    /**
     * Oyun iş parçacığında yayın yapar.
     *
     * <p>ŞART: {@code chat}/{@code announce} işleyicileri bunu bir AĞ iş
     * parçacığından çağırır ve Minecraft'ın oyuncu listesine başka bir iş
     * parçacığından dokunmak, açıklanamayan çökmelerin klasik kaynağıdır.
     *
     * <p>{@code handoff} yolundan (zaten oyun iş parçacığındayken) çağrılması
     * da güvenlidir: {@code execute} o durumda görevi sıraya alır.
     */
    private void broadcast(Text text) {
        server.execute(() -> server.getPlayerManager().broadcast(text, false));
    }

    private static boolean isUuid(String s) {
        try {
            UUID.fromString(s);
            return true;
        } catch (IllegalArgumentException e) {
            return false;
        }
    }
}
