package gg.mcos.link;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;

import java.util.ArrayList;
import java.util.List;

/**
 * Dünyanın hangi parçasının kime ait olduğunun anlık görüntüsü.
 *
 * <p>MCOS daemon'undan gelir ({@link Coordinator}); mod bunu KENDİSİ
 * hesaplamaz. Sebebi basit: eşleştirme listesi, düğüm adresleri ve zorluk
 * MCOS tarafındadır. Aynı bilgiyi iki yerde tutmak, er ya da geç ikisinin
 * ayrılması ve hatanın ancak oyuncu sınırı geçerken ortaya çıkması demektir.
 *
 * <p>Bu sınıf DEĞİŞMEZDİR (immutable): topoloji arka planda yenilenirken oyun
 * iş parçacığı onu okuyor olabilir. Değişmez bir nesneyi atomik olarak
 * değiştirmek, kilit almaktan hem daha hızlı hem daha güvenlidir.
 */
public final class Topology {

    /** Mod ile koordinatör arasındaki sözleşme sürümü. */
    public static final int SUPPORTED_VERSION = 1;

    /** Hiçbir şeyin açık olmadığı, güvenli varsayılan. */
    public static final Topology DISABLED =
            new Topology(SUPPORTED_VERSION, false, "", "normal", 32, 2,
                    List.of(), List.of(), "", "ortak dünya kapalı");

    public final int version;
    public final boolean enabled;
    /** Bu makinenin düğüm adı. */
    public final String self;
    public final String difficulty;
    public final int slabChunks;
    /** Sınırı kaç chunk aştıktan sonra aktarım yapılacağı. */
    public final int hysteresisChunks;
    public final List<Node> nodes;
    public final List<Area> areas;
    /** Eşler arası kimlik doğrulama anahtarı. */
    public final String token;
    /** Kapalıysa nedenini açıklar (panelde/günlükte gösterilir). */
    public final String note;

    private Topology(int version, boolean enabled, String self, String difficulty,
                     int slabChunks, int hysteresisChunks, List<Node> nodes,
                     List<Area> areas, String token, String note) {
        this.version = version;
        this.enabled = enabled;
        this.self = self;
        this.difficulty = difficulty;
        this.slabChunks = slabChunks;
        this.hysteresisChunks = hysteresisChunks;
        this.nodes = List.copyOf(nodes);
        this.areas = List.copyOf(areas);
        this.token = token;
        this.note = note;
    }

    /** Bir düğüm: adı, oyuncunun aktarılacağı adres ve eşler arası port. */
    public static final class Node {
        public final String name;
        public final String host;
        public final int mcPort;
        public final int linkPort;
        public final boolean self;
        public final boolean online;
        public final int players;

        Node(String name, String host, int mcPort, int linkPort,
             boolean self, boolean online, int players) {
            this.name = name;
            this.host = host;
            this.mcPort = mcPort;
            this.linkPort = linkPort;
            this.self = self;
            this.online = online;
            this.players = players;
        }

        @Override
        public String toString() {
            return name + " (" + host + ":" + mcPort + ")";
        }
    }

    /**
     * Bir dilim: chunk X aralığı.
     *
     * <p>minChunkX DAHİL, maxChunkX HARİÇTİR. Uçlardaki dilimler sonsuzdur;
     * bunu "çok büyük bir sayı" ile değil açık bayrakla söylüyoruz — taşma
     * hataları en sinsi hata türüdür.
     */
    public static final class Area {
        public final String node;
        public final int minChunkX;
        public final int maxChunkX;
        public final boolean unboundedMin;
        public final boolean unboundedMax;

        Area(String node, int minChunkX, int maxChunkX,
             boolean unboundedMin, boolean unboundedMax) {
            this.node = node;
            this.minChunkX = minChunkX;
            this.maxChunkX = maxChunkX;
            this.unboundedMin = unboundedMin;
            this.unboundedMax = unboundedMax;
        }

        public boolean contains(int chunkX) {
            if (!unboundedMin && chunkX < minChunkX) {
                return false;
            }
            return unboundedMax || chunkX < maxChunkX;
        }

        /**
         * Dilimi blok koordinatlarıyla anlatır.
         *
         * <p>CHUNK DEĞİL BLOK: oyuncu F3 ekranında blok koordinatı görür.
         * "x &lt; -512" anlamlıdır; "chunkX &lt; -32" değildir.
         */
        public String describe() {
            if (unboundedMin && unboundedMax) {
                return "tüm dünya";
            }
            if (unboundedMin) {
                return "x < " + (maxChunkX * 16);
            }
            if (unboundedMax) {
                return "x >= " + (minChunkX * 16);
            }
            return (minChunkX * 16) + " .. " + (maxChunkX * 16);
        }
    }

    /** Verilen chunk X'in sahibi olan düğümün adı; bulunamazsa kendimiz. */
    public String ownerOf(int chunkX) {
        for (Area a : areas) {
            if (a.contains(chunkX)) {
                return a.node;
            }
        }
        // Buraya düşmek bir hesap hatasıdır. Sahipsiz oyuncu bırakmaktansa
        // yerinde tutmak her zaman daha az zararlıdır.
        return self;
    }

    /** Bu makinenin dilimi; yoksa null. */
    public Area selfArea() {
        for (Area a : areas) {
            if (a.node.equals(self)) {
                return a;
            }
        }
        return null;
    }

    /** Ada göre düğüm; yoksa null. */
    public Node node(String name) {
        for (Node n : nodes) {
            if (n.name.equals(name)) {
                return n;
            }
        }
        return null;
    }

    /**
     * Koordinatörün JSON yanıtını çözer.
     *
     * <p>Hatalı veya tanınmayan sürümlü bir yanıt {@link #DISABLED} döndürür —
     * asla kısmen yorumlanmış bir topoloji döndürmez. Yanlış yorumlanmış bir
     * topoloji, oyuncuyu var olmayan bir sunucuya gönderir; kapalı bir
     * topoloji yalnızca özelliği devre dışı bırakır.
     */
    public static Topology parse(JsonObject o) {
        if (o == null) {
            return DISABLED;
        }
        int version = getInt(o, "version", 0);
        if (version != SUPPORTED_VERSION) {
            return withNote("koordinatör sürümü uyumsuz (" + version
                    + ", beklenen " + SUPPORTED_VERSION + ")");
        }

        boolean enabled = o.has("enabled") && o.get("enabled").getAsBoolean();
        String self = getStr(o, "self", "");
        String note = getStr(o, "note", "");
        String token = getStr(o, "token", "");
        String difficulty = getStr(o, "difficulty", "normal");
        int slab = getInt(o, "slabChunks", 32);
        int hyst = getInt(o, "hysteresisChunks", 2);

        List<Node> nodes = new ArrayList<>();
        if (o.has("nodes") && o.get("nodes").isJsonArray()) {
            JsonArray arr = o.getAsJsonArray("nodes");
            for (int i = 0; i < arr.size(); i++) {
                JsonObject n = arr.get(i).getAsJsonObject();
                nodes.add(new Node(
                        getStr(n, "name", ""),
                        getStr(n, "host", ""),
                        getInt(n, "mcPort", 25565),
                        getInt(n, "linkPort", 27893),
                        n.has("self") && n.get("self").getAsBoolean(),
                        n.has("online") && n.get("online").getAsBoolean(),
                        getInt(n, "players", 0)));
            }
        }

        List<Area> areas = new ArrayList<>();
        if (o.has("territories") && o.get("territories").isJsonArray()) {
            JsonArray arr = o.getAsJsonArray("territories");
            for (int i = 0; i < arr.size(); i++) {
                JsonObject t = arr.get(i).getAsJsonObject();
                areas.add(new Area(
                        getStr(t, "node", ""),
                        getInt(t, "minChunkX", 0),
                        getInt(t, "maxChunkX", 0),
                        t.has("unboundedMin") && t.get("unboundedMin").getAsBoolean(),
                        t.has("unboundedMax") && t.get("unboundedMax").getAsBoolean()));
            }
        }

        // Tutarlılık: açık bir topolojide en az iki düğüm ve her düğüm için
        // bir dilim olmalı. Eksik bir dilim, o düğümdeki oyuncuların
        // sahipsiz kalması demektir.
        if (enabled && (nodes.size() < 2 || areas.size() != nodes.size())) {
            return withNote("topoloji tutarsız (" + nodes.size() + " düğüm, "
                    + areas.size() + " dilim)");
        }

        return new Topology(version, enabled, self, difficulty, slab, hyst,
                nodes, areas, token, note);
    }

    private static Topology withNote(String note) {
        return new Topology(SUPPORTED_VERSION, false, "", "normal", 32, 2,
                List.of(), List.of(), "", note);
    }

    private static String getStr(JsonObject o, String k, String def) {
        return o.has(k) && !o.get(k).isJsonNull() ? o.get(k).getAsString() : def;
    }

    private static int getInt(JsonObject o, String k, int def) {
        return o.has(k) && !o.get(k).isJsonNull() ? o.get(k).getAsInt() : def;
    }
}
