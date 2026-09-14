package gg.mcos.link.paper;

import com.google.gson.JsonObject;
import org.bukkit.Bukkit;
import org.bukkit.event.EventHandler;
import org.bukkit.event.EventPriority;
import org.bukkit.event.Listener;
import org.bukkit.event.player.AsyncPlayerChatEvent;
import org.bukkit.event.player.PlayerJoinEvent;
import org.bukkit.plugin.java.JavaPlugin;

/**
 * MCOS Link (Paper) — giriş noktası.
 *
 * <p>Birden çok MCOS cihazının AYNI dünyayı çalıştırmasını sağlar: dünya X
 * ekseninde dilimlere bölünür, her cihaz kendi dilimini simüle eder ve
 * oyuncu sınırı geçtiğinde envanteriyle birlikte kesintisiz aktarılır.
 *
 * <p><b>Yalnızca sunucu tarafı.</b> İstemcide hiçbir mod gerekmez; aktarım,
 * Minecraft'ın 1.20.5 ile eklediği kendi transfer paketiyle yapılır
 * ({@code Player#transfer}).
 *
 * <p><b>Fabric eşiyle konuşur.</b> Tel protokolü
 * ({@link LinkProtocol}) {@code gg.mcos.link.LinkProtocol} ile bayt bayt
 * aynıdır, yani bir Paper düğümü ile bir Fabric düğümü aynı ortak dünyayı
 * paylaşabilir.
 *
 * <h2>Yaşam döngüsü</h2>
 * <pre>
 *   onEnable()                       (load: POSTWORLD — dünyalar hazır)
 *      └─ Coordinator başlar         → 5 sn'de bir topolojiyi çeker
 *      └─ HandoffService kurulur
 *      └─ LinkServer başlar          → 27893'te eşleri dinler
 *      └─ her 20 tikte HandoffService.tick()
 *   onDisable()
 *      └─ linkServer → handoff → coordinator, bu SIRAYLA durur
 * </pre>
 *
 * <h2>SUNUCU ÖN KOŞULU: accepts-transfers=true</h2>
 *
 * <p>Vanilla'da bu ayar VARSAYILAN OLARAK FALSE'tur ve gelen istemci
 * bağlantısı kesilir. KARŞILAYAN her düğümün {@code server.properties}
 * dosyasında {@code accepts-transfers=true} olmalıdır. Unutulursa belirti
 * tam olarak şudur: oyuncu sınırda kaybolur, üstelik verisi çoktan
 * gönderilmiştir — yani aktarım sıralamasının (önce veri, sonra transfer)
 * engellemek için var olduğu "kaybolmuş oyuncu" arızası.
 *
 * <h2>FOLIA BİLEREK DESTEKLENMİYOR</h2>
 *
 * <p>{@code plugin.yml} içinde {@code folia-supported} YOKTUR. Folia'da her
 * bölge ayrı bir iş parçacığında çalışır ve buradaki "ana iş parçacığı"
 * varsayımlarının hiçbiri geçerli olmaz. Folia'da {@code BukkitScheduler}
 * metotları {@code UnsupportedOperationException} fırlatır — yani yanlış bir
 * beyan sessizce değil, GÜRÜLTÜYLE başarısız olur. Ayrıca Folia tek dünyayı
 * iş parçacıklarına böler; MCOS tek dünyayı MAKİNELERE böler. Küçük kartlar
 * üzerinde Folia'nın kazandıracağı bir şey yok.
 */
public final class McosLinkPlugin extends JavaPlugin implements Listener {

    /**
     * Sınır denetimi sıklığı (tik).
     *
     * <p>20 tik = 1 saniye. Her tikte denetlemek gereksiz: oyuncu bir tikte
     * en fazla birkaç blok gider ve aktarım gecikmesi zaten 32 bloktur.
     * Saniyede bir denetim, 100 oyuncuda bile ölçülemeyecek kadar ucuzdur.
     */
    private static final int CHECK_INTERVAL_TICKS = 20;

    // volatile ŞART — burada bir kez "yalnızca ana iş parçacığından
    // okunur/yazılır, bu yüzden volatile gereksiz" yazıyordu. O cümle YANLIŞTI.
    //
    // Yazma gerçekten yalnızca ana iş parçacığındadır (onEnable/onDisable).
    // OKUMA değil: aşağıdaki onChat, Paper'ın ASENKRON sohbet iş
    // parçacığında çalışır (metodun kendi açıklaması da bunu söylüyor) ve
    // orada handoff alanı okunur. Yani bu alanlar iş parçacıkları ARASINDA
    // paylaşılıyor ve eklentinin kendisi hiçbir "önce-olur" ilişkisi kurmuyor;
    // volatile olmadan asenkron okuma bayat bir değer görebilir.
    //
    // İkinci ve pratikte daha sık görülen sebep: onDisable alanları NULL'lar.
    // O yazmanın asenkron iş parçacığında görünmesi gerekir, yoksa kapanışla
    // yarışan bir sohbet satırı kapatılmış havuza iş bırakmaya çalışır.
    private volatile Coordinator coordinator;
    private volatile LinkServer linkServer;
    private volatile HandoffService handoff;

    /**
     * Fabric'in {@code onInitializeServer} + {@code SERVER_STARTED} ikilisi
     * burada TEK metoda iner: {@code plugin.yml} varsayılanı
     * {@code load: POSTWORLD} olduğu için dünyalar bu noktada zaten yüklüdür.
     */
    @Override
    public void onEnable() {
        coordinator = new Coordinator();
        coordinator.start();

        handoff = new HandoffService(this, coordinator);

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
        // LinkServer, gelen aktarımı HandoffService'e bildirmek ZORUNDA:
        // aksi halde aktarımla gelen oyuncunun girişi "oyuna katıldı" diye
        // duyurulur. Bağlantı koparsa arıza sessizdir; bu yüzden kurucuda
        // zorunlu parametre.
        linkServer = new LinkServer(this, coordinator, handoff, port);
        // start() bağlanamazsa YALNIZCA uyarır: bir eklenti, sunucunun
        // açılmasını engellemeye hak kazanacak kadar önemli değildir.
        linkServer.start();

        getServer().getPluginManager().registerEvents(this, this);

        // Komut plugin.yml'de tanımlıdır; burada yalnızca yürütücüyü
        // bağlıyoruz. Servisler yukarıda KURULDUKTAN sonra bağlanır, yani
        // Fabric'teki "kayıt servislerden önce" tehlikesi Paper'da yok.
        var cmd = getCommand("mcoslink");
        if (cmd == null) {
            Log.warn("plugin.yml içinde mcoslink komutu tanımlı değil"
                    + " — /mcoslink çalışmayacak");
        } else {
            cmd.setExecutor(new LinkCommands(this, () -> coordinator,
                    () -> handoff));
        }

        // ── ANA İŞ PARÇACIĞI, saniyede bir ────────────────────────────────
        // Consumer<BukkitTask> aşırı yüklemesi kullanılıyor; görev tutamağına
        // ihtiyacımız yok çünkü onDisable tüm görevleri toplu iptal ediyor.
        //
        // PlayerMoveEvent BİLEREK kullanılmadı: API'nin en sık tetiklenen
        // olayıdır (yalnızca kafa çevirmede bile tetiklenir) ve
        // PlayerTeleportEvent ondan türer. Bkz. HandoffService#tick.
        Bukkit.getScheduler().runTaskTimer(this, task -> {
            if (handoff == null) {
                return;
            }
            handoff.tick();
        }, CHECK_INTERVAL_TICKS, CHECK_INTERVAL_TICKS);

        Log.info("yüklendi — MCOS daemon'una bağlanılacak");
    }

    /**
     * Fabric'teki {@code SERVER_STOPPING} karşılığı.
     *
     * <p>SIRA ÖNEMLİ: önce dinleyici kapanır (yeni istek gelmesin), sonra
     * aktarım havuzu, en son koordinatör. Üç havuz da DAEMON'dur, yani
     * kaçırılmış bir kapatma bile JVM'i ayakta tutamaz.
     *
     * <p><b>Kapanışta hiçbir şey DRENAJ EDİLMEZ.</b> Üç {@code stop()} da
     * {@code shutdownNow()} çağırır: kuyruktaki işler atılır, süren işler
     * bölünür. Buraya "son bildirimler gidebilsin" diye bir sıra kurulmadı;
     * daemon'a kapanış olayı GÖNDERİLMEZ, kapanışın tek izi aşağıdaki
     * {@code Log.info("durduruldu")} satırıdır. Bu bilinçli: kapanan bir
     * sunucuyu ağ beklemesiyle uzatmak, panelde bir satır eksik kalmasından
     * daha kötüdür.
     */
    @Override
    public void onDisable() {
        if (linkServer != null) {
            linkServer.stop();
        }
        if (handoff != null) {
            handoff.stop();
        }
        if (coordinator != null) {
            coordinator.stop();
        }

        // Alanları BİLEREK null'la.
        //
        // Yakalanan gerçek hata: onChat asenkron bir iş parçacığında çalışır
        // ve kapanışla YARIŞABİLİR. Alanlar null'lanmasaydı, handoff.stop()
        // (workers.shutdownNow) ile eşzamanlı gelen bir sohbet satırı
        // broadcastToPeers'a girer, orada kapatılmış havuza iş bırakmaya
        // çalışır ve MONITOR önceliğindeki bir dinleyiciden
        // RejectedExecutionException fırlardı: sunucunun her kapanışında
        // "Could not pass event AsyncPlayerChatEvent to MCOSLink" yığın izi.
        // Alanlar volatile olduğu için bu yazma asenkron iş parçacığında da
        // görünür.
        //
        // Bu YALNIZCA yarışın büyük bölümünü kapatır; null denetimini çoktan
        // geçmiş bir çağrı için ikinci savunma HandoffService.submit
        // içindedir.
        linkServer = null;
        handoff = null;
        coordinator = null;

        // Fabric'te karşılığı yok: Bukkit zamanlayıcı görevleri eklentiden
        // bağımsız yaşar, açıkça iptal edilmeleri gerekir. Saniyelik tarama
        // görevi de böylece durur.
        Bukkit.getScheduler().cancelTasks(this);
        Log.info("durduruldu");
    }

    /**
     * Sohbeti eşlere yayar.
     *
     * <p>"Tek sunucu gibi görünmesi" isteğinin en görünür parçası budur:
     * oyuncular farklı makinelerde ama aynı sohbette olmalı.
     *
     * <p><b>ASENKRON İŞ PARÇACIĞI.</b> Fabric'in sohbet olayı oyun iş
     * parçacığında tetikleniyordu; Paper'ınki tetiklenmez. Burada yalnızca
     * bir JSON kuruluyor ve {@code broadcastToPeers} çağrılıyor — o da
     * anında kendi havuzuna iş bırakıp dönüyor. BAŞKA HİÇBİR Bukkit
     * durumuna burada dokunulamaz.
     *
     * <p>{@code MONITOR} + {@code ignoreCancelled}: yalnızca GÖZLEMLİYORUZ.
     * İptal edilmiş bir sohbet (örneğin susturma eklentisi) eşlere de
     * gitmemelidir.
     *
     * <h3>KULLANILAN SINIF @Deprecated'TİR — BİLEREK</h3>
     *
     * <p>{@code org.bukkit.event.player.AsyncPlayerChatEvent}, Paper'da
     * {@code io.papermc.paper.event.player.AsyncChatEvent} lehine
     * kullanımdan kaldırılmış sayılır. Sınıf 1.21.x paper-api'de HÂLÂ VARDIR
     * ve çalışır; tek bedeli bir derleme uyarısıdır ve o uyarı aşağıdaki
     * {@code @SuppressWarnings} ile bastırılıyor.
     *
     * <p>Yenisine GEÇİLMEDİ çünkü {@code AsyncChatEvent}, mesajı
     * {@code String} değil Adventure {@code Component} olarak verir; düz
     * metne çevirmek için {@code net.kyori.adventure.text.serializer.plain}
     * paketinden bir serileştirici gerekir. Bu sınıfların hiçbirinin imzası
     * elimizdeki DOĞRULANMIŞ API dosyasında yok. Tahminle yazılmış bir imza,
     * derlenmeyen ya da daha kötüsü çalışma anında
     * {@code NoSuchMethodError} veren bir eklenti demektir — ve telde
     * taşıdığımız şey zaten düz metindir ({@code text} alanı, Fabric eşiyle
     * bayt bayt aynı), yani {@code Component} hiçbir şey kazandırmaz.
     *
     * <p>Doğrulanınca yapılacak: olay {@code AsyncChatEvent}'e alınır,
     * {@code event.message()} düz metne serileştirilir ve bu not silinir.
     * Telde HİÇBİR ŞEY değişmez.
     */
    @SuppressWarnings("deprecation") // AsyncPlayerChatEvent — bkz. yukarıdaki not
    @EventHandler(priority = EventPriority.MONITOR, ignoreCancelled = true)
    public void onChat(AsyncPlayerChatEvent event) {
        HandoffService h = handoff;
        if (h == null) {
            return;
        }
        JsonObject o = new JsonObject();
        o.addProperty("player", event.getPlayer().getName());
        o.addProperty("text", event.getMessage());
        h.broadcastToPeers("chat", o);
    }

    /**
     * Girişi eşlere duyurur — AKTARIMLA GELEN oyuncu hariç.
     *
     * <p><b>Yakalanan gerçek hata:</b> bu geri çağrı, GERÇEK bir girişi
     * sınır geçişiyle GELEN oyuncudan ayırt etmiyordu ve her geçişte tüm
     * eşlere "X oyuna katıldı" yayınlıyordu. Ali mcos-lab'dan mcos-oda'ya
     * geçtiğinde mcos-lab'daki oyuncular önce vanilla'nın "Ali left the
     * game" satırını, hemen ardından mcos-oda'dan itilen "Ali oyuna katıldı"
     * satırını görüyordu — yani Ali çıkıp yeniden girmiş gibi. Bu eklentinin
     * var olma sebebi tam da o yanılsamanın kırılmaması.
     *
     * <p>YEREL vanilla giriş/çıkış mesajlarına DOKUNULMAZ
     * ({@code event.setJoinMessage} çağrılmaz): bastırılan tek şey EŞLERE
     * giden duyurudur. Yerel mesajı da kısmak, aynı makinedeki oyuncular
     * için gerçek bir girişi görünmez yapardı.
     *
     * <p>ANA İŞ PARÇACIĞI.
     */
    @EventHandler(priority = EventPriority.MONITOR)
    public void onJoin(PlayerJoinEvent event) {
        HandoffService h = handoff;
        if (h == null) {
            return;
        }
        if (h.consumeIncoming(event.getPlayer().getUniqueId())) {
            // Hedef düğüm onu zaten "bu bölgeye geçti" diye anons etti.
            return;
        }
        JsonObject o = new JsonObject();
        o.addProperty("text", event.getPlayer().getName() + " oyuna katıldı");
        h.broadcastToPeers("announce", o);
    }

    // ════════════════════════════════════════════════════════════════════
    // BİLEREK PORTLANMAYAN İKİ ÖZELLİK — SESSİZ DEĞİL, KAYITLI EKSİKLİK
    // ════════════════════════════════════════════════════════════════════
    //
    // Fabric sürümünde iki kozmetik/yardımcı iş daha vardı. İkisinin de TELE
    // HİÇBİR ETKİSİ YOKTUR: bir Paper düğümü, bunlar olmadan da bir Fabric
    // düğümüyle sorunsuz ortak dünya paylaşır.
    //
    // 1) ZORLUK EŞİTLEME (McosLink#applyDifficulty)
    //    Fabric: server.setDifficulty(Difficulty, true) — sunucu geneli, tek
    //    çağrı. Bukkit'te zorluk DÜNYA BAŞINADIR ve sunucunun dünyalarını
    //    gezmek gerekir. Gereken üyelerin (dünya listesi, zorluk ayarlayıcı,
    //    zorluk sabitleri) imzaları elimizdeki DOĞRULANMIŞ API dosyasında
    //    YOK. Tahminle yazılmış bir imza, derlenmeyen ya da daha kötüsü
    //    çalışma anında NoSuchMethodError veren bir eklenti demektir; bu
    //    yüzden yazılmadı.
    //    Etkisi: MCOS panelinden seçilen zorluk bu düğümde UYGULANMAZ;
    //    server.properties / level.dat içindeki zorluk geçerli olur. Karışık
    //    bir kümede iki dilimde farklı zorluk görülebilir. Düzeltmek için
    //    ilgili Bukkit üyeleri doğrulanıp buraya applyDifficulty() eklenmeli.
    //
    // 2) SEKME LİSTESİ BAŞLIĞI (McosLink#updateTabList, 100 tik)
    //    Fabric: PlayerListHeaderS2CPacket. Bukkit'in başlık/altbilgi üyesi
    //    de doğrulanmış API dosyasında yok; aynı gerekçeyle yazılmadı ve
    //    100 tiklik zamanlayıcı hiç kurulmadı.
    //    Etkisi: sekme listesinde "MCOS Link — N oyuncu · M/K cihaz" özeti
    //    görünmez. Oyun mantığı etkilenmez.
    //    Korunması gereken tasarım kararı: UZAK oyuncular sekme listesine
    //    GERÇEK satır olarak EKLENMEZ. Bu, istemciye sahte profil paketleri
    //    göndermeyi gerektirir ve kaydı olmayan oyuncularda istemci
    //    hatalarına yol açar. Özellik geri eklenirse yine yalnızca
    //    başlık/altbilgi yazılmalıdır.
}
