package gg.mcos.link.paper;

import com.google.gson.JsonObject;
import org.bukkit.Bukkit;
import org.bukkit.Location;
import org.bukkit.entity.Player;
import org.bukkit.plugin.Plugin;

import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.util.Map;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.RejectedExecutionException;

/**
 * Oyuncunun sınırı geçtiğini fark eder ve onu öbür düğüme aktarır.
 *
 * <p>Bu eklentinin kalbi burasıdır.
 *
 * <h2>Aktarım sırası</h2>
 * <ol>
 *   <li>Oyuncunun chunk X'i kendi dilimimizin dışında ve <b>gecikme kadar</b>
 *       da ötesinde mi? (Sınırın üstünde duran bir oyuncu ileri-geri
 *       gittiğinde iki makine arasında sonsuz döngüye girerdi.)</li>
 *   <li>Sunucu oyuncu verisini diske yazar.</li>
 *   <li>Veri hedef düğüme gönderilir ve <b>onay beklenir</b>.</li>
 *   <li>Ancak onay geldikten sonra istemciye transfer isteği yollanır.</li>
 * </ol>
 *
 * <p><b>Sıra neden bu?</b> Önce transfer isteğini gönderip sonra veriyi
 * yollasaydık, istemci karşı sunucuya bizden ÖNCE ulaşabilirdi ve oyuncu
 * boş envanterle doğardı. Veri önce gider; gönderilemezse aktarım hiç
 * yapılmaz ve oyuncu yerinde kalır — kaybolmuş bir oyuncudan iyidir.
 *
 * <p><b>Neden transfer?</b> Minecraft 1.20.5 ile gelen transfer paketi,
 * istemciyi dünya ekranından çıkarmadan başka bir sunucuya bağlar. MCOS bir
 * hile uydurmuyor; protokolün kendi mekanizmasını kullanıyor. Bu yüzden
 * istemcide mod GEREKMEZ.
 *
 * <h2>PAPER PORTU: İKİ DEĞİŞİKLİK, BİR YENİ TEHLİKE</h2>
 *
 * <ol>
 *   <li>{@code p.networkHandler.sendPacket(new ServerTransferS2CPacket(...))}
 *       yerine {@code player.transfer(host, port)}. Bukkit metodu
 *       {@code IllegalStateException} FIRLATABİLİR ("if a transfer cannot
 *       take place at this time"); Fabric'te ham paket gönderimi fırlatmazdı.
 *       Yakalanmazsa: zamanlayıcı yığın izi basar ve — daha kötüsü — oyuncu
 *       {@code inFlight}'ta asılı kalabilirdi. Aşağıda yakalanıyor ve
 *       başarısız gönderimle AYNI şekilde ele alınıyor.</li>
 *   <li>{@code server.getPlayerManager().saveAllPlayerData()} yerine
 *       {@code player.saveData()}. Gözlemlenebilir sonuç aynı (diskte güncel
 *       bir .dat), ama yalnızca AKTARILAN oyuncunun dosyası yazılır. Eski
 *       hâli, her aktarımda ÇEVRİMİÇİ HER oyuncunun dosyasını yazıyordu;
 *       yavaş depolamalı küçük bir kartta bu, oyuncu sayısıyla orantılı
 *       görünür bir duraklamadır. Telde hiçbir etkisi yok.</li>
 *   <li><b>Yeni tehlike:</b> Paper'da ana iş parçacığı kuralı Fabric'ten daha
 *       SERTTİR — Bukkit birçok dünya/oyuncu erişiminde yarış yaşamak yerine
 *       doğrudan istisna fırlatır. Aşağıdaki her iş parçacığı sıçraması
 *       yorumda açıkça işaretlendi; "sadeleştirmek" için kaldırılmamalıdır.</li>
 * </ol>
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
     * <p>30 saniye: aktarım isteğini alan istemci normalde 1-2 saniyede bize
     * bağlanır. Daha uzun tutmak, aktarımdan vazgeçip çok sonra elle giren
     * oyuncunun gerçek girişini de yutardı.
     */
    private static final long INCOMING_TTL_MS = 30_000;

    /** Zamanlayıcıya ana iş parçacığı görevi vermek için gereken tutamak. */
    private final Plugin plugin;
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

    /**
     * @param plugin ana iş parçacığına dönmek için gereken eklenti örneği
     */
    public HandoffService(Plugin plugin, Coordinator coordinator) {
        this.plugin = plugin;
        this.coordinator = coordinator;
        // KENDİ havuzumuz. Gerekçe bir kez YANLIŞ yazıldı: "Bukkit kapanışta
        // bekleyen görevleri iptal eder, kendi havuzumuz kesilmez" deniyordu.
        // Bu doğru DEĞİL ve tersi de doğru değil:
        //   · stop() aşağıda shutdownNow() çağırır, yani süren sekiz saniyelik
        //     bir gönderimi BİZ böleriz — üstelik Bukkit'in görevleri iptal
        //     edeceği anın tam olarak aynısında (onDisable);
        //   · CraftScheduler zaten ÇALIŞMAKTA olan asenkron görevleri
        //     bölmez, yalnızca henüz başlamamışları iptal eder.
        // Yani kapanışta hayatta kalma açısından iki seçenek arasında fark
        // yok. Kapanışta HİÇBİR ŞEY drenaj edilmez; bu bilinçli bir karardır.
        //
        // Havuzun gerçek gerekçeleri şunlar:
        //   · İZLENEBİLİRLİK: iş parçacıkları "mcos-link-handoff" adını taşır.
        //     Bir yığın dökümünde sekiz saniye asılı kalan soketin kime ait
        //     olduğu tek bakışta görülür; Bukkit'in ortak asenkron havuzunda
        //     görünmezdi.
        //   · YALITIM: yavaş bir eş, sunucudaki BAŞKA eklentilerin asenkron
        //     görevlerini bekletmez; havuzu doldurmanın bedelini yalnızca biz
        //     öderiz.
        //   · DAEMON: iş parçacıkları daemon'dur, yani stop() hiç
        //     çağrılmasa bile JVM'in kapanmasını engelleyemezler.
        this.workers = Executors.newCachedThreadPool(r -> {
            Thread t = new Thread(r, "mcos-link-handoff");
            t.setDaemon(true);
            return t;
        });
    }

    /**
     * Havuzu keser. {@code onDisable}'dan çağrılır.
     *
     * <p>{@code shutdownNow()}: kuyruktaki gönderimler ATILIR, süren gönderim
     * BÖLÜNÜR. Kapanan bir sunucuyu sekiz saniyelik bir sokete bağlı tutmanın
     * anlamı yok. Bundan sonra {@link #submit} her işi reddeder.
     */
    public void stop() {
        workers.shutdownNow();
    }

    /**
     * Havuza iş bırakır; havuz kapalıysa {@code false} döner.
     *
     * <p>HER iş parçacığından çağrılabilir.
     *
     * <p><b>Yakalanan gerçek hata.</b> {@code workers.execute(...)} doğrudan
     * çağrılıyordu ve {@link #broadcastToPeers}, Paper'ın ASENKRON sohbet
     * olayından besleniyor. Kapanışla yarışan bir sohbet satırı —
     * {@code handoff.stop()} çoktan çalışmışken — kapatılmış havuza iş
     * bırakmayı dener ve {@code RejectedExecutionException} fırlatırdı. O
     * istisna MONITOR önceliğindeki bir dinleyicinin içinden çıktığı için
     * sonuç, sunucunun her kapanışında "Could not pass event
     * AsyncPlayerChatEvent to MCOSLink" yığın iziydi: gerçek bir arıza
     * olmadığı hâlde arıza gibi görünen bir kayıt.
     *
     * <p>{@code McosLinkPlugin.onDisable} alanları null'layarak yarışın
     * çoğunu zaten kapatır; burası, denetimi geçmiş çağrılar için İKİNCİ
     * savunmadır.
     */
    private boolean submit(Runnable task) {
        try {
            workers.execute(task);
            return true;
        } catch (RejectedExecutionException e) {
            // Kapanış. Arıza değil; hata ayıklama seviyesinde kalmalı.
            Log.debug("aktarım havuzu kapalı, iş bırakılamadı: " + e);
            return false;
        }
    }

    /**
     * Eylem çubuğuna tek satır yazar. <b>ANA İŞ PARÇACIĞI.</b>
     *
     * <h3>KULLANILAN AŞIRI YÜKLEME @Deprecated'TİR — BİLEREK</h3>
     *
     * <p>Paper, {@code Player#sendActionBar(String)}'i Adventure tabanlı
     * {@code sendActionBar(Component)} lehine kullanımdan kaldırılmış sayar.
     * {@code String} aşırı yüklemesi 1.21.x paper-api'de HÂLÂ VARDIR ve
     * çözümlemede belirsizlik YOKTUR ({@code String}, {@code ComponentLike}
     * değildir; {@code BaseComponent...} varargs aşırı yüklemesi de üçüncü
     * evreye hiç kalmaz) — tek bedeli bir derleme uyarısıdır.
     *
     * <p>Adventure'a GEÇİLMEDİ çünkü {@code Component} ve kurucularının
     * imzaları elimizdeki DOĞRULANMIŞ API dosyasında yok; oradaki tek
     * gönderme, bu metodun ADIDIR ("player.sendActionBar() ... ana iş
     * parçacığına zamanlanmalı"). Ayrıca renkler bu eklentide zaten §
     * kodlarıyla taşınıyor ({@link Fmt}) ve o kodlar Fabric düğümünün
     * ürettiğiyle aynı görünmek zorunda.
     *
     * <p>Bu sarmalayıcı YALNIZCA bunun için var: uyarı tek bir yerde
     * bastırılsın ve gerekçe beş çağrı yerinde tekrarlanmasın.
     */
    @SuppressWarnings("deprecation") // sendActionBar(String) — bkz. yukarıdaki not
    private static void actionBar(Player player, String text) {
        player.sendActionBar(text);
    }

    public int handoffs() {
        return handoffs;
    }

    /**
     * "Bu oyuncu birazdan aktarımla gelecek" diye işaretler.
     *
     * <p>{@link LinkServer} gelen kaydı diske yazdıktan sonra çağırır.
     * ANA İŞ PARÇACIĞI.
     */
    public void expectIncoming(UUID id) {
        long now = System.currentTimeMillis();
        // Gelmeyen varışlar birikmesin: aktarım isteği gönderildikten sonra
        // oyunu kapatan bir oyuncunun izi haritada sonsuza kadar kalırdı.
        incoming.values().removeIf(at -> now - at > INCOMING_TTL_MS);
        incoming.put(id, now);
    }

    /**
     * Giriş, beklenen bir aktarım varışı mı? İşareti TÜKETİR.
     *
     * <p>Tüketmek şart: aynı oyuncu daha sonra gerçekten yeniden girdiğinde
     * o giriş normal şekilde duyurulmalı. ANA İŞ PARÇACIĞI
     * ({@code PlayerJoinEvent}).
     */
    public boolean consumeIncoming(UUID id) {
        Long at = incoming.remove(id);
        return at != null && System.currentTimeMillis() - at <= INCOMING_TTL_MS;
    }

    /**
     * Her oyuncu için sınır denetimi yapar.
     *
     * <p>ANA İŞ PARÇACIĞI: {@code runTaskTimer} ile saniyede bir çağrılır.
     * Her tikte çağırmak gereksiz: oyuncu bir tikte en fazla birkaç blok
     * gider ve sınır gecikmesi zaten 32 bloktur.
     *
     * <p>{@code PlayerMoveEvent} BİLEREK kullanılmadı: Bukkit'in EN SIK
     * tetiklenen olayıdır, yalnızca kafa çevirmede bile tetiklenir ve
     * {@code PlayerTeleportEvent} ondan TÜREDİĞİ için her ender incisi,
     * her portal bu mantığa yeniden girerdi. Saniyede bir yoklama, 32 blokluk
     * gecikmeyle zaten fazlasıyla uyumludur.
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

        for (Player player : Bukkit.getOnlinePlayers()) {
            try {
                check(top, mine, player);
            } catch (Exception e) {
                // Tek bir bozuk oyuncu, taramanın tamamını iptal etmemeli.
                Log.error("sınır denetimi hata verdi: " + player.getName(), e);
            }
        }
    }

    /** ANA İŞ PARÇACIĞI. */
    private void check(Topology top, Topology.Area mine, Player player) {
        UUID id = player.getUniqueId();
        if (inFlight.containsKey(id)) {
            return;
        }

        // Konum BİR KEZ okunur. Chunk, bit kaydırmayla hesaplanır:
        // Location#getChunk() çağırmak, chunk yüklü değilse onu ANA İŞ
        // PARÇACIĞINDA yükler ya da ÜRETİR.
        Location loc = player.getLocation();
        int chunkX = loc.getBlockX() >> 4;

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
            // Onu yerinde tutup durumu SÖYLÜYORUZ. Eylem çubuğu: geçici bir
            // durum bilgisidir, sohbeti kirletmesi gerekmez.
            actionBar(player, Fmt.RED + targetName
                    + " şu anda çevrimdışı — bu bölgeye geçilemiyor");
            return;
        }

        begin(top, player, target);
    }

    /**
     * Sınırı kaç chunk aştığımızı verir.
     *
     * <p>maxChunkX HARİÇ olduğu için üst uçta {@code +1} var: chunkX tam
     * maxChunkX'e eşitken zaten bir chunk dışarıdayız.
     */
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
     *
     * <p>ANA İŞ PARÇACIĞI.
     */
    private void warnIfNearBorder(Topology top, Topology.Area mine,
                                  Player player, int chunkX) {
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
        actionBar(player, Fmt.DARK_AQUA + neighbour + " bölgesine "
                + (distance * 16) + " blok kaldı");
    }

    /**
     * Aktarımı başlatır (elle {@code /mcoslink send} ile de çağrılabilir).
     *
     * <p><b>Bu metodun ilk yarısı ANA İŞ PARÇACIĞINDA çalışır</b> —
     * {@code saveData()} ve eylem çubuğu başka türlü çağrılamaz — ve
     * {@link #submit} satırında AĞ havuzuna geçer.
     */
    public void begin(Topology top, Player player, Topology.Node target) {
        UUID id = player.getUniqueId();
        // Atomik: elle verilen /mcoslink send komutu ile saniyelik tarama
        // yarışamaz. Girişi yalnızca biri kazanır.
        if (inFlight.putIfAbsent(id, System.currentTimeMillis()) != null) {
            return;
        }

        String name = player.getName();
        actionBar(player, Fmt.AQUA + target.name + " bölgesine geçiliyor…");

        // ANA İŞ PARÇACIĞI. Oyuncu verisini ŞİMDİ diske yaz. Bu, aktarımın
        // doğruluğu için zorunlu tek "sıralama duyarlı" çağrıdır: kayıt
        // olmadan gönderilecek bir dosya yok.
        //
        // Fabric burada saveAllPlayerData() çağırıyordu; Paper'ın oyuncu
        // başına saveData()'sı aynı sonucu verir, daha az G/Ç yapar ve
        // ilgisiz hiçbir oyuncunun dosyasına dokunmaz.
        player.saveData();

        byte[] data;
        try {
            data = PlayerData.read(id);
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

        // ── SIÇRAMA 1: ANA İŞ PARÇACIĞI -> mcos-link-handoff havuzu ────────
        // Ağ işi OYUN İŞ PARÇACIĞINDA YAPILMAZ: hedef yanıt vermezse tüm
        // sunucu sekiz saniye donardı.
        boolean queued = submit(() -> {
            // AĞ İŞ PARÇACIĞI: burada hiçbir Bukkit çağrısı yapılamaz.
            boolean ok = push(target, token, self, name, id, data);

            // ── SIÇRAMA 2: havuz -> ANA İŞ PARÇACIĞI (bir sonraki tik) ────
            try {
                Bukkit.getScheduler().runTask(plugin, () -> {
                    // ANA İŞ PARÇACIĞI.
                    //
                    // inFlight ÖNCE bırakılır: aşağıdaki transfer bir istisna
                    // fırlatırsa bile giriş sızmaz, yoksa o oyuncu bir daha
                    // asla aktarılamazdı.
                    inFlight.remove(id);

                    // Yakalanan gerçek hata: soğuma damgası YALNIZCA başarı
                    // dalında yazılıyordu. Hedef düğüm link portunu
                    // dinleyemediyse (LinkServer.start() bunu ölümcül saymaz)
                    // ya da anahtar uyuşmadıysa push milisaniyeler içinde
                    // başarısız olur; o zaman check()'teki soğuma denetimi hiç
                    // tutmaz ve sınırın ötesinde duran oyuncu için TÜM deneme
                    // saniyede bir tekrar ederdi: dakikada ~60 kez kayıt yazma
                    // ve dakikada ~60 kırmızı satır. Damga artık her SONLANAN
                    // denemeye yazılıyor; başarısızlık da COOLDOWN_MS'e tabi.
                    lastTransfer.put(id, System.currentTimeMillis());

                    Player p = online(id);
                    if (p == null) {
                        return; // oyuncu bu arada çıkmış
                    }
                    if (!ok) {
                        // Çevrimdışı düğüm dalıyla aynı biçim: bu bir "şu an
                        // geçilemiyor" bilgisidir, sohbeti kalıcı olarak
                        // kirletmesi gerekmez.
                        actionBar(p, Fmt.RED + target.name
                                + " bölgesine geçilemedi — yerinde kaldınız");
                        return;
                    }
                    handoffs++;
                    coordinator.event("handoff", name, self, target.name, null);
                    Log.info(name + " -> " + target.name + " ("
                            + target.host + ":" + target.mcPort + ")");

                    // İstemciyi aktar. Bundan sonra istemci bizden kopar;
                    // başka bir şey göndermenin anlamı yok.
                    //
                    // PAPER'A ÖZGÜ: transfer(), Fabric'teki ham paketin
                    // aksine IllegalStateException fırlatabilir. Yakalanmazsa
                    // zamanlayıcıda yığın izi basar ve oyuncu hiçbir şey
                    // olmamış gibi kalır. Başarısız gönderimle aynı muamele.
                    try {
                        p.transfer(target.host, target.mcPort);
                    } catch (IllegalStateException e) {
                        Log.warn("aktarım paketi gönderilemedi (" + name
                                + "): " + e.getMessage());
                        actionBar(p, Fmt.RED + target.name
                                + " bölgesine geçilemedi — yerinde kaldınız");
                    }
                });
            } catch (Exception e) {
                // Eklenti kapanmışsa runTask fırlatır. Kapanışta inFlight'ın
                // süreçle birlikte ölmesi zaten sorun değil; sunucuyu
                // kapatırken yığın izi basmanın da anlamı yok.
                Log.debug("aktarım sonucu ana iş parçacığına verilemedi: " + e);
            }
        });

        if (!queued) {
            // Havuz kapalı (eklenti duruyor). İş HİÇ başlamadığı için
            // yukarıdaki geri çağrı da hiç çalışmayacak; inFlight girişini
            // burada geri almazsak sızardı. Oyuncuya bir şey söylemiyoruz:
            // sunucu kapanıyor, mesajın gideceği yer yok.
            inFlight.remove(id);
            lastTransfer.put(id, System.currentTimeMillis());
        }
    }

    /**
     * Denemeyi iptal eder. ANA İŞ PARÇACIĞI.
     *
     * <p>Yakalanan gerçek hata: iptal de bir SONLANMIŞ denemedir. Damga
     * yazılmadığı için "oyuncu kaydı bulunamadı" gibi KALICI bir durum,
     * saniyede bir kayıt yazma + bir uyarı satırı üretiyordu.
     */
    private void abort(Player player, UUID id, String why) {
        inFlight.remove(id);
        lastTransfer.put(id, System.currentTimeMillis());
        Log.warn("aktarım iptal: " + why);
        // Eylem çubuğu DEĞİL sohbet: bu kalıcı bir arıza bildirimidir,
        // oyuncunun sonradan yukarı kaydırıp okuyabilmesi gerekir.
        player.sendMessage(Fmt.RED + "Bölge değişimi başarısız: " + why);
    }

    /**
     * Verilen UUID'li çevrimiçi oyuncu; yoksa null. ANA İŞ PARÇACIĞI.
     *
     * <p>Çevrimiçi listesi üzerinde geziyoruz; imzası doğrulanmış API
     * listesinde bulunan tek erişim yolu budur ({@code Bukkit
     * .getOnlinePlayers()} -> {@code Collection<? extends Player>}).
     * Liste bir düğümde en fazla birkaç düzine kişidir.
     */
    private static Player online(UUID id) {
        for (Player p : Bukkit.getOnlinePlayers()) {
            if (p.getUniqueId().equals(id)) {
                return p;
            }
        }
        return null;
    }

    /**
     * Oyuncu verisini hedefe gönderir; onay gelirse true.
     *
     * <p><b>AĞ İŞ PARÇACIĞI</b> (mcos-link-handoff). Burada tek bir Bukkit
     * çağrısı bile yapılamaz.
     */
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

    /**
     * Bir metni tüm eş düğümlere yayar (sohbet ve duyurular için).
     *
     * <p>Çağıran iş parçacığında HİÇBİR engelleyici iş yapmaz: her eş için
     * havuza bir görev bırakır ve döner. Bu yüzden Paper'ın ASENKRON sohbet
     * olayından çağrılması güvenlidir — burada Bukkit'e dokunulmuyor,
     * yalnızca JSON kuruluyor ve havuza iş veriliyor.
     *
     * <p><b>Kapanışla yarışabilir.</b> Asenkron sohbet olayı,
     * {@code onDisable} çoktan {@link #stop} çağırmışken buraya girebilir; bu
     * yüzden iş {@link #submit} üzerinden bırakılıyor. Havuz kapalıysa satır
     * sessizce düşer — kapanmakta olan bir sunucuda doğru davranış budur ve
     * alternatifi, dinleyicinin içinden fırlayan bir
     * {@code RejectedExecutionException} yığın iziydi.
     */
    public void broadcastToPeers(String cmd, JsonObject header) {
        Topology top = coordinator.topology();
        if (!top.enabled) {
            return;
        }
        for (Topology.Node n : top.nodes) {
            if (n.self || !n.online) {
                continue;
            }
            submit(() -> {
                // AĞ İŞ PARÇACIĞI.
                try (Socket s = new Socket()) {
                    s.connect(new InetSocketAddress(n.host, n.linkPort), 3000);
                    s.setSoTimeout(3000);
                    try (DataOutputStream out = new DataOutputStream(s.getOutputStream());
                         DataInputStream in = new DataInputStream(s.getInputStream())) {
                        // deepCopy, çağıranın alanlarını SIRAYLA korur;
                        // addProperty sona ekler. Tel üzerindeki alan sırası
                        // bu yüzden belirlidir ve Fabric eşle birebir aynıdır.
                        JsonObject h = header.deepCopy();
                        h.addProperty("cmd", cmd);
                        h.addProperty("token", top.token);
                        h.addProperty("node", top.self);
                        LinkProtocol.write(out, h, null);
                        // Yanıt OKUNUR ama kullanılmaz: eşin yanıtı
                        // beklenmeden soket kapatılırsa karşı taraf yazarken
                        // hata alır ve günlüğünü kirletir.
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
