package gg.mcos.link.velocity;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import com.velocitypowered.api.proxy.ProxyServer;
import com.velocitypowered.api.proxy.server.RegisteredServer;
import com.velocitypowered.api.proxy.server.ServerInfo;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.Reader;
import java.net.HttpURLConnection;
import java.net.InetSocketAddress;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.Locale;
import java.util.Map;
import java.util.regex.Pattern;
import org.slf4j.Logger;

/**
 * MCOS'un arka uç listesini (/link/proxy) çalışan proxy'ye uygular.
 *
 * <p>Yalnızca FARK uygulanır: yeni ya da adresi değişen sunucu kaydedilir,
 * listeden çıkan sunucunun kaydı silinir. Kaydı silinen sunucudaki oyuncular
 * ATILMAZ — Velocity kaydı silmekle bağlantıyı kesmez; sunucu kendisi
 * kapanınca oyuncular {@code KickedFromServerEvent} ile kurucuya döner.
 *
 * <p>HTTP için {@link HttpURLConnection} (java.base): java.net.http modülü
 * küçültülmüş bir JRE'de olmayabilir; o zaman eklenti yüklenemez, MCOS ise
 * "eklenti yerinde" sanıp proxy'yi yeniden başlatmayı bırakırdı ve yeni PC'ler
 * hiç eklenmezdi. Ortak dünya modları da aynı sınıfı kullanıyor.
 */
final class BackendSync {

    /** Sözleşme: adlar küçük harf, [a-z0-9_-] (model.ProxyBackendName). */
    private static final Pattern NAME = Pattern.compile("[a-z0-9_-]{1,64}");

    /** 127.0.0.1'e istek; 2 sn'den uzun süren bir yanıt zaten bozuktur. */
    private static final int TIMEOUT_MS = 2000;

    private final ProxyServer proxy;
    private final Logger log;
    private final String url;

    private volatile String tryName = "";
    private volatile int maxPlayers;
    private volatile boolean warnedUnreachable;

    BackendSync(ProxyServer proxy, Logger log, String coordinatorEnv) {
        this.proxy = proxy;
        this.log = log;
        this.url = baseUrl(coordinatorEnv) + "/link/proxy";
    }

    /**
     * Ortam değişkeninden taban URL. Şemasız değer ("127.0.0.1:27892")
     * "http://" ile tamamlanır: ortak dünya modunda bu hata yaşandı, URI
     * onu reddedip koordinatöre hiç bağlanılamıyordu.
     */
    static String baseUrl(String env) {
        if (env == null || env.isBlank()) {
            return "http://127.0.0.1:27892";
        }
        String v = env.trim();
        if (!v.contains("://")) {
            v = "http://" + v;
        }
        while (v.endsWith("/")) {
            v = v.substring(0, v.length() - 1);
        }
        return v;
    }

    String url() {
        return url;
    }

    String tryName() {
        return tryName;
    }

    int maxPlayers() {
        return maxPlayers;
    }

    /** Bir yoklama turu. Hiçbir hata dışarı sızmaz: zamanlayıcı görevi ölmesin. */
    void poll() {
        try {
            JsonObject body = fetch();
            if (body == null) {
                return;
            }
            apply(body);
        } catch (Exception ex) {
            if (!warnedUnreachable) {
                warnedUnreachable = true;
                log.warn("MCOS Link: arka uç listesi okunamadı ({}): {}", url, ex.toString());
            }
        }
    }

    /** null: MCOS "proxy kapalı" dedi (503); o tur atlanır, liste korunur. */
    private JsonObject fetch() throws Exception {
        HttpURLConnection c = (HttpURLConnection) URI.create(url).toURL().openConnection();
        c.setConnectTimeout(TIMEOUT_MS);
        c.setReadTimeout(TIMEOUT_MS);
        c.setUseCaches(false);
        try {
            int code = c.getResponseCode();
            if (code != 200) {
                return null;
            }
            try (InputStream in = c.getInputStream();
                    Reader r = new InputStreamReader(in, StandardCharsets.UTF_8)) {
                JsonElement e = JsonParser.parseReader(r);
                return e.isJsonObject() ? e.getAsJsonObject() : null;
            }
        } finally {
            c.disconnect();
        }
    }

    private void apply(JsonObject body) {
        if (warnedUnreachable) {
            warnedUnreachable = false;
            log.info("MCOS Link: koordinatöre yeniden ulaşıldı");
        }
        Map<String, InetSocketAddress> want = new LinkedHashMap<>();
        JsonElement arr = body.get("backends");
        if (arr != null && arr.isJsonArray()) {
            for (JsonElement el : (JsonArray) arr) {
                if (!el.isJsonObject()) {
                    continue;
                }
                JsonObject o = el.getAsJsonObject();
                String name = str(o, "name").toLowerCase(Locale.ROOT);
                String host = str(o, "host");
                int port = o.has("port") && o.get("port").isJsonPrimitive() ? o.get("port").getAsInt() : 0;
                if (!NAME.matcher(name).matches() || host.isEmpty() || port <= 0 || port > 65535) {
                    continue;
                }
                want.put(name, new InetSocketAddress(host, port));
            }
        }
        String t = str(body, "try").toLowerCase(Locale.ROOT);
        int max = body.has("maxPlayers") && body.get("maxPlayers").isJsonPrimitive()
                ? body.get("maxPlayers").getAsInt() : 0;
        // Boş liste UYGULANMAZ: geçici bir aksaklıkta (topoloji henüz
        // hesaplanmadı) bütün arka uçları silmek, geçiş yapan herkesi
        // boşluğa düşürürdü.
        if (want.isEmpty()) {
            return;
        }
        tryName = t;
        maxPlayers = max;

        Map<String, RegisteredServer> have = new HashMap<>();
        for (RegisteredServer rs : proxy.getAllServers()) {
            have.put(rs.getServerInfo().getName().toLowerCase(Locale.ROOT), rs);
        }
        for (Map.Entry<String, InetSocketAddress> e : want.entrySet()) {
            RegisteredServer old = have.get(e.getKey());
            if (old != null && sameAddress(old.getServerInfo().getAddress(), e.getValue())) {
                continue;
            }
            try {
                if (old != null) {
                    // Aynı ada farklı adres: Velocity registerServer'da
                    // reddeder, önce eski kayıt silinmeli. Eski sunucudaki
                    // oyuncular bağlı kalır.
                    proxy.unregisterServer(old.getServerInfo());
                }
                proxy.registerServer(new ServerInfo(e.getKey(), e.getValue()));
                log.info("MCOS Link: arka uç {} {} = {}", old == null ? "eklendi" : "güncellendi",
                        e.getKey(), text(e.getValue()));
            } catch (RuntimeException ex) {
                log.warn("MCOS Link: arka uç {} kaydedilemedi: {}", e.getKey(), ex.toString());
            }
        }
        for (Map.Entry<String, RegisteredServer> e : have.entrySet()) {
            // "try" hiçbir zaman silinmez: oyuncunun ilk durağı ve düşenlerin
            // döndüğü yer.
            if (want.containsKey(e.getKey()) || e.getKey().equals(t)) {
                continue;
            }
            try {
                proxy.unregisterServer(e.getValue().getServerInfo());
                log.info("MCOS Link: arka uç kaldırıldı {} (içindeki oyuncular atılmadı)", e.getKey());
            } catch (RuntimeException ex) {
                log.warn("MCOS Link: arka uç {} kaldırılamadı: {}", e.getKey(), ex.toString());
            }
        }
    }

    /**
     * Adres karşılaştırması ÇÖZÜLMÜŞ IP ile yapılır: velocity.toml'dan gelen
     * kayıt ile buradaki aynı adresi farklı yazabilir (ör. IPv6 "fe80::1" ve
     * "fe80:0:0:0:0:0:0:1"); equals ile karşılaştırılsaydı sunucu her turda
     * silinip yeniden kaydedilirdi.
     */
    private static boolean sameAddress(InetSocketAddress a, InetSocketAddress b) {
        if (a == null || b == null || a.getPort() != b.getPort()) {
            return false;
        }
        if (!a.isUnresolved() && !b.isUnresolved()) {
            return a.getAddress().equals(b.getAddress());
        }
        return a.getHostString().equalsIgnoreCase(b.getHostString());
    }

    private static String text(InetSocketAddress a) {
        return a.getHostString() + ":" + a.getPort();
    }

    private static String str(JsonObject o, String key) {
        JsonElement e = o.get(key);
        return e != null && e.isJsonPrimitive() ? e.getAsString().trim() : "";
    }
}
