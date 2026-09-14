package gg.mcos.link.paper;

import com.google.gson.JsonObject;
import com.google.gson.JsonParser;

import java.io.ByteArrayOutputStream;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.Executors;
import java.util.concurrent.RejectedExecutionException;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.Consumer;

/**
 * MCOS daemon'undaki link koordinatörüne konuşur.
 *
 * <p>İki iş yapar:
 * <ul>
 *   <li>topolojiyi düzenli aralıklarla çeker (5 sn),</li>
 *   <li>olan biteni geri bildirir (aktarım, varış, hata) — böylece panel
 *       "Ahmet mcos-1'den mcos-2'ye geçti" yazabilir.</li>
 * </ul>
 *
 * <p><b>Neden HttpURLConnection?</b> {@code java.net.http.HttpClient} daha
 * modern ama ayrı bir modülde ({@code java.net.http}); küçültülmüş bir JRE'de
 * bulunmayabilir. {@code HttpURLConnection} {@code java.base} içindedir ve
 * HER Java kurulumunda vardır. Bu bir sunucu eklentisidir; çalıştığı ortamı
 * seçemez.
 *
 * <p><b>Neden ayrı iş parçacığı?</b> Ağ çağrısı oyun iş parçacığında
 * yapılırsa, daemon bir saniye yanıt vermediğinde SUNUCU DONAR. Topoloji
 * arka planda yenilenir; oyun yalnızca son bilinen değeri okur.
 *
 * <p><b>Bu sınıfın DIŞARI AÇTIĞI HİÇBİR METOT ENGELLEMEZ.</b> Çağıran
 * hangi iş parçacığında olursa olsun {@link #topology()} bir alan okur,
 * {@link #event} ve {@link #refreshAsync} havuza iş bırakıp döner. HTTP,
 * yalnızca "mcos-link-coordinator" iş parçacığında yapılır. Buraya
 * engelleyen bir metot eklemek, sınıfın var olma sebebini ortadan kaldırır:
 * {@code HttpURLConnection.setConnectTimeout} DNS çözümlemesini
 * SINIRLAMAZ — {@code MCOS_LINK_COORDINATOR} bir IP değil ad taşıyorsa,
 * takılmış bir sorgu çağıranı SINIRSIZ süre bekletir. Bu iş parçacığında
 * bunun bedeli yalnızca gecikmiş bir yoklamadır; oyun iş parçacığında
 * Paper'ın gözcüsü (watchdog) sunucuyu öldürürdü.
 *
 * <h2>PAPER PORTU İÇİN NOT</h2>
 *
 * <p>Bu sınıfta hiçbir Minecraft/Bukkit API'si yok. Fabric sürümünden
 * ({@code gg.mcos.link.Coordinator}) üç noktada ayrılır: paket satırı,
 * {@code refreshNow()} yerine {@link #refreshAsync} (aşağıda anlatılıyor) ve
 * {@link #event}'in reddedilen gönderimi yutması. Bunun dışında birebir
 * aynıdır ve öyle kalması iki dosyayı yan yana karşılaştırılabilir tutar.
 *
 * <p>Kendi tek iş parçacıklı havuzunu kullanır — {@code BukkitScheduler
 * .runTaskTimerAsynchronously} ile değiştirilmedi, çünkü:
 * <ul>
 *   <li>havuzun TEK iş parçacıklı olması bir özelliktir: olay POST'ları
 *       yoklamayla aynı sıraya girer ve daemon'a aynı anda iki istek
 *       gitmez;</li>
 *   <li>bu sınıf {@code Plugin} örneği TAŞIMAZ; Bukkit zamanlayıcısına
 *       geçmek, yalnızca bunun için buraya bir eklenti tutamağı sokmayı
 *       gerektirirdi;</li>
 *   <li>iş parçacığı DAEMON'dur, yani sunucunun kapanmasını engelleyemez.</li>
 * </ul>
 *
 * <p><b>Kapanışta ne OLMAZ.</b> Burada bir zamanlar "kapanış sırasında bile
 * son bildirimi gönderebilmek için kendi havuzumuz var" yazıyordu; bu yanlış
 * bir gerekçeydi. {@link #stop()}, {@code shutdownNow()} çağırır: kuyruktaki
 * her olay POST'u ATILIR, süren istek BÖLÜNÜR. Ayrıca hiçbir yerde kapanış
 * olayı gönderilmez — kapanışın tek izi {@code McosLinkPlugin.onDisable}
 * içindeki {@code Log.info("durduruldu")} satırıdır. Panelde "düğüm
 * kapandı" satırı isteniyorsa, {@code stop()}'tan ÖNCE açıkça bir
 * {@link #event} gönderilip havuzun boşalması beklenmelidir; bugün bu
 * BİLEREK yapılmıyor, çünkü kapanan bir sunucuyu ağ beklemesiyle uzatmaya
 * değmez.
 */
public final class Coordinator {

    /** Topoloji yenileme aralığı. */
    private static final long POLL_SECONDS = 5;

    /**
     * Tek bir isteğin üst sınırı.
     *
     * <p>2 saniye: daemon aynı makinededir, normalde 1 ms sürer. Uzun bir
     * zaman aşımı, daemon çöktüğünde havuzu tıkardı.
     */
    private static final int TIMEOUT_MS = 2000;

    private final String baseUrl;
    private final AtomicReference<Topology> current =
            new AtomicReference<>(Topology.DISABLED);
    private final ScheduledExecutorService exec;
    private volatile boolean warnedUnreachable;

    public Coordinator() {
        String env = System.getenv("MCOS_LINK_COORDINATOR");
        this.baseUrl = (env == null || env.isBlank())
                ? "http://127.0.0.1:27892" : env.trim();
        this.exec = Executors.newSingleThreadScheduledExecutor(r -> {
            Thread t = new Thread(r, "mcos-link-coordinator");
            // Arka plan iş parçacığı sunucunun kapanmasını ENGELLEMEMELİ.
            t.setDaemon(true);
            return t;
        });
    }

    /** Düzenli yenilemeyi başlatır. */
    public void start() {
        exec.scheduleWithFixedDelay(this::refreshQuietly, 0, POLL_SECONDS,
                TimeUnit.SECONDS);
    }

    /**
     * Yenilemeyi durdurur.
     *
     * <p>{@code shutdownNow()}: kuyruktaki olay POST'ları ATILIR, süren
     * istek BÖLÜNÜR. Bundan sonra {@link #event} ve {@link #refreshAsync}
     * sessizce hiçbir şey yapmaz.
     */
    public void stop() {
        exec.shutdownNow();
    }

    /**
     * Son bilinen topoloji. Asla null dönmez.
     *
     * <p>HER iş parçacığından güvenle çağrılır: {@code AtomicReference} bir
     * DEĞİŞMEZ {@link Topology} tutar. Oyun iş parçacığı, eş ağ iş
     * parçacıkları ve aktarım havuzu aynı anda okuyabilir.
     */
    public Topology topology() {
        return current.get();
    }

    /**
     * Topolojiyi elle yeniler (komutla tetiklenir) ve SONUCU geri çağrıya
     * verir.
     *
     * <p><b>ENGELLEMEZ.</b> Çağıran iş parçacığı hiçbir şey beklemez; iş,
     * koordinatör iş parçacığına bırakılır ve geri çağrı ORADA çalışır —
     * yani geri çağrının içinde HİÇBİR Bukkit çağrısı yapılamaz, çağıran
     * önce ana iş parçacığına dönmek zorundadır (bkz.
     * {@code LinkCommands#reload}).
     *
     * <p><b>Yakalanan gerçek hata.</b> Burada bir {@code refreshNow()}
     * vardı: {@code refreshQuietly()}'yi ÇAĞIRANIN iş parçacığında
     * çalıştırıyor ve topolojiyi döndürüyordu. Tek çağıranı
     * {@code /mcoslink reload}'dur ve Bukkit komutları ANA İŞ PARÇACIĞINDA
     * dağıtır. Yani komut, oyun döngüsünün üzerinde engelleyici bir HTTP
     * GET yapıyordu: 2 sn bağlanma + 2 sn okuma = en kötü durumda ~4 sn
     * donmuş sunucu — üstelik tam da bir operatörün bu komutu yazacağı
     * durumda (daemon yanıt vermiyor). Daha kötüsü,
     * {@code setConnectTimeout} DNS çözümlemesini sınırlamaz:
     * {@code MCOS_LINK_COORDINATOR} bir ad taşıyorsa takılmış bir sorgu ana
     * iş parçacığını SINIRSIZ bekletir ve Paper'ın gözcüsü sunucuyu öldürür.
     * Bu, sınıfın kendi açıklamasındaki "ağ çağrısı oyun iş parçacığında
     * yapılırsa SUNUCU DONAR" cümlesinin birebir ihlaliydi.
     *
     * <p>{@code refreshNow()} geri getirilmemeli: aynı tuzağı yeniden
     * kurar.
     *
     * @param done yenileme bittiğinde son topolojiyle çağrılır; havuz
     *             kapalıysa HİÇ çağrılmaz
     */
    public void refreshAsync(Consumer<Topology> done) {
        try {
            exec.execute(() -> {
                refreshQuietly();
                try {
                    done.accept(current.get());
                } catch (Exception e) {
                    // Geri çağrının hatası, yenilemeyi geçersiz kılmaz ve
                    // havuzun tek iş parçacığını öldürmemeli.
                    Log.debug("yenileme geri çağrısı hata verdi: " + e);
                }
            });
        } catch (RejectedExecutionException e) {
            // Eklenti kapanıyor. Komut çıktısı yazılamaz; arıza değil.
            Log.debug("topoloji yenilenemedi, havuz kapalı: " + e);
        }
    }

    private void refreshQuietly() {
        try {
            String body = get(baseUrl + "/link/topology");
            JsonObject o = JsonParser.parseString(body).getAsJsonObject();
            Topology t = Topology.parse(o);
            Topology old = current.getAndSet(t);
            warnedUnreachable = false;

            if (old.enabled != t.enabled) {
                if (t.enabled) {
                    Log.info("ortak dünya AÇIK — " + t.nodes.size()
                            + " düğüm, zorluk " + t.difficulty);
                } else {
                    Log.info("ortak dünya kapalı"
                            + (t.note.isEmpty() ? "" : " (" + t.note + ")"));
                }
            }
        } catch (Exception e) {
            // Daemon yeniden başlatılıyor olabilir; her saniye hata basmak
            // günlüğü doldurur ve gerçek sorunları gizler. BİR KEZ uyar.
            if (!warnedUnreachable) {
                warnedUnreachable = true;
                Log.warn("MCOS daemon'una ulaşılamıyor (" + baseUrl
                        + "): " + e.getMessage()
                        + " — ortak dünya devre dışı");
            }
            current.set(Topology.DISABLED);
        }
    }

    /**
     * Olan biteni daemon'a bildirir (ateşle-unut).
     *
     * <p>Başarısızlığı YUTAR: bildirim, oyunun akışını etkilememeli. Panelde
     * bir satır eksik kalması, oyuncunun aktarımının başarısız olmasından
     * kıyaslanamayacak kadar önemsizdir.
     *
     * <p>Oyun iş parçacığından çağrılır ve HİÇBİR ŞEYİ beklemez: gövde
     * burada kurulur, POST koordinatör iş parçacığında yapılır.
     */
    public void event(String kind, String player, String from, String to, String text) {
        try {
            exec.execute(() -> {
                try {
                    JsonObject o = new JsonObject();
                    o.addProperty("kind", kind);
                    if (player != null) o.addProperty("player", player);
                    if (from != null) o.addProperty("from", from);
                    if (to != null) o.addProperty("to", to);
                    if (text != null) o.addProperty("text", text);
                    post(baseUrl + "/link/event", o.toString());
                } catch (Exception ignored) {
                    // bilerek sessiz
                }
            });
        } catch (RejectedExecutionException e) {
            // Havuz kapandıktan sonra (kapanış) gelen bildirim. "Ateşle-unut"
            // sözünün gereği: çağırana istisna SIZDIRMAZ. İçerideki try/catch
            // yalnızca POST'un kendisini kapsıyordu; execute'un kendisi
            // reddedilince istisna ana iş parçacığına çıkardı.
            Log.debug("bildirim gönderilemedi, havuz kapalı: " + kind);
        }
    }

    private static String get(String url) throws Exception {
        HttpURLConnection c = open(url, "GET");
        try (InputStream in = c.getInputStream()) {
            return readAll(in);
        } finally {
            c.disconnect();
        }
    }

    private static void post(String url, String json) throws Exception {
        HttpURLConnection c = open(url, "POST");
        c.setDoOutput(true);
        c.setRequestProperty("Content-Type", "application/json");
        byte[] body = json.getBytes(StandardCharsets.UTF_8);
        try (OutputStream out = c.getOutputStream()) {
            out.write(body);
        }
        // Yanıtı okumak ZORUNLU: okunmayan bir yanıt bağlantıyı havuzda
        // yarım bırakır ve sonraki istek beklenmedik şekilde takılır.
        try (InputStream in = c.getInputStream()) {
            readAll(in);
        } catch (Exception ignored) {
            // 204 No Content gövdesizdir; sorun değil.
        } finally {
            c.disconnect();
        }
    }

    private static HttpURLConnection open(String url, String method) throws Exception {
        HttpURLConnection c = (HttpURLConnection) URI.create(url).toURL().openConnection();
        c.setRequestMethod(method);
        c.setConnectTimeout(TIMEOUT_MS);
        c.setReadTimeout(TIMEOUT_MS);
        c.setUseCaches(false);
        return c;
    }

    private static String readAll(InputStream in) throws Exception {
        ByteArrayOutputStream buf = new ByteArrayOutputStream();
        byte[] tmp = new byte[4096];
        int n;
        // Üst sınır: bozuk bir yanıt belleği tüketmemeli.
        while (buf.size() < (1 << 20) && (n = in.read(tmp)) > 0) {
            buf.write(tmp, 0, n);
        }
        return buf.toString(StandardCharsets.UTF_8);
    }
}
