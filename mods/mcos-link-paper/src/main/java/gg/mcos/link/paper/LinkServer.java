package gg.mcos.link.paper;

import com.google.gson.JsonObject;
import org.bukkit.Bukkit;
import org.bukkit.entity.Player;
import org.bukkit.plugin.Plugin;

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
 * <p>Dört komut:
 * <ul>
 *   <li>{@code handoff} — gelen oyuncu verisini diske yazar,</li>
 *   <li>{@code chat} — komşudan gelen sohbet satırını yayınlar,</li>
 *   <li>{@code announce} — komşudan gelen duyuruyu yayınlar,</li>
 *   <li>{@code ping} — canlılık ve düğüm adı sorgusu.</li>
 * </ul>
 *
 * <p><b>Güvenlik.</b> Her komut, MCOS eşleştirme anahtarını taşımak
 * zorundadır. Anahtar olmadan, ağdaki herhangi biri bu porta bağlanıp
 * istediği oyuncunun envanterini DEĞİŞTİREBİLİRDİ — çünkü handoff, bir
 * oyuncunun kayıt dosyasını olduğu gibi yazar. Karşılaştırma SABİT SÜRELİ
 * yapılır: bayt bayt erken çıkan bir karşılaştırma, doğru önekin uzunluğunu
 * zamanlamadan sızdırır.
 *
 * <h2>PAPER PORTU İÇİN NOT</h2>
 *
 * <p>Yapı Fabric sürümüyle birebir aynıdır; yalnızca
 * {@code server.execute(...)} sıçramaları
 * {@code Bukkit.getScheduler().runTask(plugin, ...)} oldu. İki ek önlem var:
 * <ul>
 *   <li>{@code runTask}, eklenti KAPALIYSA istisna fırlatır (Fabric'in
 *       {@code execute}'u fırlatmaz). Her çağrı sarmalandı; aksi halde eş,
 *       zaman aşımı dolana kadar boşuna beklerdi.</li>
 *   <li>{@code runTask} görevi BİR SONRAKİ TİKE kuyruklar (en fazla 50 ms).
 *       {@code APPLY_TIMEOUT_MS} (5 sn) gönderenin 8 saniyesinin çok
 *       altında olduğu için bu gecikmenin bütçeye etkisi yok.</li>
 * </ul>
 */
public final class LinkServer {

    /**
     * Gelen kaydın oyun iş parçacığında uygulanması için beklenecek süre.
     *
     * <p>5 saniye: gönderen taraf yanıtı zaten 8 saniye bekliyor
     * ({@code HandoffService.PEER_TIMEOUT_MS}); ondan önce yanıt vermeliyiz
     * ki oyuncu "geçilemedi" cevabını zamanında alsın — soket zaman aşımı
     * yerine GERÇEK bir ret.
     */
    private static final long APPLY_TIMEOUT_MS = 5000;

    /** Oyuncu sayısı için oyun iş parçacığından yanıt bekleme süresi. */
    private static final long COUNT_TIMEOUT_MS = 2000;

    private final Plugin plugin;
    private final Coordinator coordinator;
    private final HandoffService handoff;
    private final int port;

    private volatile ServerSocket socket;
    private volatile boolean running;
    private final ExecutorService workers;

    /** Son bilinen oyuncu sayısı; ping'i asla geciktirmemek için. */
    private volatile int lastPlayerCount;

    /**
     * @param handoff aktarımla gelen oyuncuyu işaretlemek için ZORUNLU. Bu
     *                bağ koparsa, sınırı geçen her oyuncu hedef düğümde
     *                "oyuna katıldı" diye duyurulur ve modun korumaya
     *                çalıştığı "tek sunucu" yanılsaması kırılır.
     */
    public LinkServer(Plugin plugin, Coordinator coordinator,
                      HandoffService handoff, int port) {
        this.plugin = plugin;
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
     * sunucu normal şekilde açılır. Bir eklenti, sunucunun açılmasını
     * engellemeye hak kazanacak kadar önemli değildir.
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
        // shutdownNow(): ana iş parçacığından yanıt bekleyen eş iş
        // parçacıklarını BÖLER; aksi halde kapanış beş saniye uzardı.
        workers.shutdownNow();
    }

    /** "mcos-link-accept" iş parçacığı. */
    private void acceptLoop() {
        while (running) {
            try {
                Socket s = socket.accept();
                workers.execute(() -> handle(s));
            } catch (IOException e) {
                // stop() soketi kapattığında da buraya düşeriz; o durumda
                // running false'tur ve sessiz kalmak doğrudur.
                if (running) {
                    Log.warn("bağlantı kabul edilemedi: " + e.getMessage());
                }
            }
        }
    }

    /** EŞ AĞ İŞ PARÇACIĞI (mcos-link-peer). */
    private void handle(Socket s) {
        try (Socket sock = s;
             DataInputStream in = new DataInputStream(sock.getInputStream());
             DataOutputStream out = new DataOutputStream(sock.getOutputStream())) {

            // Bir eşin bağlantıyı süresiz açık tutmasını engeller.
            sock.setSoTimeout(30_000);

            LinkProtocol.Message msg = LinkProtocol.read(in);
            // Topoloji CANLI anlık görüntüden okunur: anahtar panelden
            // değiştirilmişse bir sonraki istek yeni anahtarla doğrulanır.
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
        // MessageDigest java.base içindedir; Paper'da da Fabric'te olduğu
        // gibi değişmeden vardır.
        return MessageDigest.isEqual(
                given.getBytes(java.nio.charset.StandardCharsets.UTF_8),
                expected.getBytes(java.nio.charset.StandardCharsets.UTF_8));
    }

    /**
     * Gelen oyuncu verisini kayıt klasörüne yazar.
     *
     * <p>Oyuncu birazdan bize bağlanacak; vanilla bu dosyayı okuyup envanteri,
     * canı, XP'yi ve konumu geri yükleyecek. Yani eklenti hiçbir eşyayı elle
     * serileştirmiyor — oyunun KENDİ kayıt biçimini taşıyor. Bu, sürüm
     * değişse bile çalışmaya devam eder ve hiçbir NBT etiketi kaybolmaz.
     *
     * <p>Bu metot EŞ AĞ İŞ PARÇACIĞINDA çalışır ve yalnızca ayrıştırma
     * yapar; oyunun durumuna dokunan her şey {@link #applyHandoff} içinde,
     * ana iş parçacığında olur.
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
        // dosya yazımı bu ağ iş parçacığında yapılıyordu. Çevrimiçi oyuncu
        // araması, oyun iş parçacığının her giriş/çıkışta değiştirdiği bir
        // yapı üzerinde gezer; eşzamanlı bir değişiklik sırasında MEVCUT bir
        // oyuncu için null dönebilir. O zaman denetim geçilir, Files.move
        // canlı oyuncunun playerdata/<uuid>.dat dosyasının üzerine yazar ve
        // oyuncu çıkarken vanilla kendi bellekteki durumunu geri yazınca
        // AKTARILAN ENVANTER YOK OLUR. Üstelik denetim ile yazım arasında
        // oyuncunun giriş yapması da aynı sonucu verirdi.
        //
        // Çözüm: denetim ve yazım TEK bir ana iş parçacığı görevinde, arada
        // hiçbir tik geçmeden yapılır; eş yalnızca sonucu bekler.
        CompletableFuture<String> result = new CompletableFuture<>();

        // ── SIÇRAMA: EŞ AĞ İŞ PARÇACIĞI -> ANA İŞ PARÇACIĞI ───────────────
        try {
            Bukkit.getScheduler().runTask(plugin, () -> {
                // ANA İŞ PARÇACIĞI.
                if (result.isDone()) {
                    return; // eş beklemekten vazgeçti; dosyaya hiç dokunma
                }
                try {
                    result.complete(applyHandoff(id, msg.body, name, from));
                } catch (Throwable t) {
                    result.completeExceptionally(t);
                }
            });
        } catch (Exception e) {
            // PAPER'A ÖZGÜ: eklenti kapatılmışsa runTask fırlatır. Eşi beş
            // saniye bekletmek yerine HEMEN gerçek bir ret veriyoruz.
            Log.error("gelen oyuncu verisi uygulanamadı: " + name, e);
            LinkProtocol.write(out,
                    LinkProtocol.fail("oyuncu verisi uygulanamadı"), null);
            return;
        }

        String error;
        try {
            // EŞ AĞ İŞ PARÇACIĞI burada BLOKLANIR (en fazla 5 sn).
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
     * Gelen kaydı uygular. <b>Yalnızca ANA İŞ PARÇACIĞINDAN çağrılır.</b>
     *
     * <p><b>Diske yazma da ana iş parçacığındadır ve bu BİLİNEN bir
     * bedeldir.</b> Aşağıdaki {@code Files.write} + {@code Files.move},
     * gövde boyutuyla orantılı engelleyici bir G/Ç'dir ve gövdenin tek üst
     * sınırı {@link LinkProtocol#MAX_BODY}'dir: 8 MiB. Yetkili ama bozuk bir
     * eşten gelen büyük bir gövde, saniyelerle ölçülen bir tik duraklaması
     * yaratabilir; MCOS'un hedeflediği SD kartlı donanımda tipik bir dosyanın
     * bedeli bile ihmal edilebilir değildir.
     *
     * <p>Yine de yazma buradan TAŞINMAZ. "Oyuncu bizde çevrimiçi mi"
     * denetimiyle yazma, ARADA HİÇBİR TİK GEÇMEDEN aynı görevde olmak
     * zorundadır; ayrılırlarsa ikisi arasında giriş yapan bir oyuncunun kayıt
     * dosyasının üzerine yazılır ve AKTARILAN ENVANTER YOK OLUR (bkz.
     * {@link #handleHandoff} içindeki açıklama). Bir tik duraklaması,
     * kaybolan bir envanterden iyidir. Daraltmak gerekirse doğru yer
     * protokolün genel tavanı değil — onu değiştirmek Fabric eşiyle tel
     * uyumunu bozar — handoff gövdesine konacak ayrı ve daha küçük bir
     * tavandır.
     *
     * @return başarıysa {@code null}, aksi halde eşe dönecek hata metni
     */
    private String applyHandoff(UUID id, byte[] body, String name, String from) {
        // Oyuncu şu anda BİZDE çevrimiçiyse dosyayı yazmak, çıkışta onun
        // kendi verisiyle üzerine yazılmasına ve gelen envanterin
        // kaybolmasına yol açar.
        for (Player p : Bukkit.getOnlinePlayers()) {
            if (p.getUniqueId().equals(id)) {
                return "oyuncu bu düğümde zaten çevrimiçi";
            }
        }

        // PlayerData.directory() dünya listesi boşsa IllegalStateException
        // atar. Çağrı TRY İÇİNDE: dışarıda bırakılsaydı istisna bu metodun
        // dışına sızar ve gönderen düğüm cevap yerine kopan bir soket görürdü
        // — yani oyuncu, hedefte verisi olmadan karşılanabilirdi. İçeride ise
        // düzgün bir "ok:false + sebep" cevabına dönüşür ve gönderen aktarımı
        // iptal edip oyuncuyu yerinde tutar.
        Path dir;
        Path target;
        Path tmp = null;
        try {
            dir = PlayerData.directory();
            target = dir.resolve(id + ".dat");
            tmp = dir.resolve(id + ".dat.mcos-tmp");
            Files.createDirectories(dir);
            Files.write(tmp, body);
            // Atomik taşıma: yarım yazılmış bir kayıt dosyası, oyuncunun
            // envanterini tamamen kaybetmesi demektir.
            Files.move(tmp, target, StandardCopyOption.REPLACE_EXISTING,
                    StandardCopyOption.ATOMIC_MOVE);
        } catch (IOException | IllegalStateException e) {
            // tmp, yol çözülemediyse null kalır; o durumda silinecek bir şey
            // de yoktur.
            if (tmp != null) {
                try {
                    Files.deleteIfExists(tmp);
                } catch (IOException ignored) {
                    // Geçici dosya silinemedi; asıl hatayı gizlemesine izin verme.
                }
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
        broadcast(Fmt.GRAY + name + " bu bölgeye geçti");

        return null;
    }

    /**
     * Oyuncu sayısını ANA İŞ PARÇACIĞINDAN okur.
     *
     * <p>Yakalanan gerçek hata: sayı doğrudan bu ağ iş parçacığından
     * okunuyordu; oyuncu listesi oyun iş parçacığına aittir.
     *
     * <p>Yanıt gelmezse SON BİLİNEN sayıyla devam ederiz: ping bir canlılık
     * sorgusudur, oyun iş parçacığı bir saniye meşgul diye düğümü çevrimdışı
     * göstermek doğru olmaz.
     */
    private int playerCount() {
        CompletableFuture<Integer> f = new CompletableFuture<>();
        // ── SIÇRAMA: EŞ AĞ İŞ PARÇACIĞI -> ANA İŞ PARÇACIĞI ───────────────
        try {
            Bukkit.getScheduler().runTask(plugin,
                    () -> f.complete(Bukkit.getOnlinePlayers().size()));
        } catch (Exception e) {
            // Eklenti kapanıyor; ping yine de yanıtlanmalı.
            Log.debug("oyuncu sayısı istenemedi: " + e);
            return lastPlayerCount;
        }
        try {
            lastPlayerCount = f.get(COUNT_TIMEOUT_MS, TimeUnit.MILLISECONDS);
        } catch (Exception e) {
            f.cancel(false);
            Log.debug("oyuncu sayısı zamanında alınamadı: " + e);
        }
        return lastPlayerCount;
    }

    /** EŞ AĞ İŞ PARÇACIĞI. */
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
        // makinede olduğu belli olsun. Üç ayrı renk parçası, Fabric'teki
        // üç Text parçasının birebir karşılığı.
        broadcast(Fmt.DARK_GRAY + "[" + node + "] "
                + Fmt.WHITE + "<" + player + "> "
                + Fmt.GRAY + text);
        LinkProtocol.write(out, LinkProtocol.ok(), null);
    }

    /** EŞ AĞ İŞ PARÇACIĞI. */
    private void handleAnnounce(LinkProtocol.Message msg, DataOutputStream out)
            throws IOException {
        String text = msg.str("text");
        if (!text.isEmpty()) {
            broadcast(Fmt.GRAY + text);
        }
        // Boş metin bile BAŞARIDIR: eş, yayınlanacak bir şey olmadığını
        // bilemez ve bunu bir arıza gibi günlüğe yazması gerekmez.
        LinkProtocol.write(out, LinkProtocol.ok(), null);
    }

    /**
     * Yerel oyunculara yayın yapar — her zaman ANA İŞ PARÇACIĞINDA.
     *
     * <p>ŞART: {@code chat}/{@code announce} işleyicileri bunu bir AĞ iş
     * parçacığından çağırır ve Minecraft'ın oyuncu listesine başka bir iş
     * parçacığından dokunmak, açıklanamayan çökmelerin klasik kaynağıdır.
     * Paper bunu Fabric'ten daha sert uygular: yarış yaşamak yerine doğrudan
     * istisna fırlatır.
     *
     * <p>{@code handoff} yolundan (zaten ana iş parçacığındayken) çağrılması
     * da güvenlidir: {@code runTask} o durumda görevi bir sonraki tike
     * kuyruklar. Satır içi çağırmak yerine KUYRUKLAMAYA devam ediyoruz ki
     * mesaj sırası Fabric sürümüyle aynı kalsın.
     */
    private void broadcast(String text) {
        try {
            Bukkit.getScheduler().runTask(plugin, () -> {
                // ANA İŞ PARÇACIĞI.
                for (Player p : Bukkit.getOnlinePlayers()) {
                    p.sendMessage(text);
                }
            });
        } catch (Exception e) {
            // Eklenti kapanıyorsa yayın yapılamaz; bu bir arıza değil.
            Log.debug("yerel yayın yapılamadı: " + e);
        }
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
