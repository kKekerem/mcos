package gg.mcos.link;

import com.google.gson.JsonObject;
import com.google.gson.JsonParser;

import java.io.ByteArrayOutputStream;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

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
 * HER Java kurulumunda vardır. Bu mod bir sunucu eklentisidir; çalıştığı
 * ortamı seçemez.
 *
 * <p><b>Neden ayrı iş parçacığı?</b> Ağ çağrısı oyun iş parçacığında
 * yapılırsa, daemon bir saniye yanıt vermediğinde SUNUCU DONAR. Topoloji
 * arka planda yenilenir; oyun yalnızca son bilinen değeri okur.
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

    /** Yenilemeyi durdurur. */
    public void stop() {
        exec.shutdownNow();
    }

    /** Son bilinen topoloji. Asla null dönmez. */
    public Topology topology() {
        return current.get();
    }

    /** Topolojiyi HEMEN yeniler (komutla elle tetiklenir). */
    public Topology refreshNow() {
        refreshQuietly();
        return current.get();
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
     */
    public void event(String kind, String player, String from, String to, String text) {
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
