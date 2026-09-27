package gg.mcos.link;

import com.google.gson.JsonObject;
import com.google.gson.JsonParser;

import java.io.ByteArrayOutputStream;
import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;

/**
 * Düğümler arasındaki tel biçimi.
 *
 * <pre>
 *   MCOSLINK1\n            sihirli satır (sürüm dahil)
 *   {"cmd":"...", ...}\n   tek satır JSON başlık
 *   &lt;len bayt&gt;             başlıkta "len" varsa gövde
 * </pre>
 *
 * <p>Yanıt aynı biçimdedir ve her zaman {@code {"ok":true|false,...}} taşır.
 *
 * <p><b>Neden HTTP değil?</b> Modun içine bir HTTP sunucusu koymak,
 * {@code jdk.httpserver} modülüne bağımlı olmak demektir; küçültülmüş bir
 * JRE'de o modül bulunmayabilir ve mod açılışta çöker. Bu protokol 100
 * satırdır, {@code java.base} dışında hiçbir şey istemez ve tam olarak
 * ihtiyacımız olanı yapar: bir başlık ve bir bayt dizisi taşımak.
 *
 * <p><b>Neden sihirli satır?</b> Port taraması yapan bir program ya da
 * yanlışlıkla aynı porta bağlanan başka bir servis, JSON ayrıştırıcısını
 * beslememelidir. İlk satır uymuyorsa bağlantı hemen kapatılır.
 */
public final class LinkProtocol {

    /** Protokol imzası. Sürüm değişirse bu satır değişir. */
    public static final String MAGIC = "MCOSLINK1";

    /** Varsayılan düğümler arası port. */
    public static final int DEFAULT_PORT = 27893;

    /**
     * Tek bir gövdenin üst sınırı.
     *
     * <p>Oyuncu verisi (.dat) tipik olarak 5-60 KB'dır; shulker kutularıyla
     * dolu bir envanter 1 MB'ı geçmez. 8 MB, her gerçek durumu karşılar ve
     * bozuk/kötü niyetli bir gönderimin belleği tüketmesini engeller.
     */
    public static final int MAX_BODY = 8 * 1024 * 1024;

    private LinkProtocol() {
    }

    /** Bir istek veya yanıt: başlık + isteğe bağlı gövde. */
    public static final class Message {
        public final JsonObject header;
        public final byte[] body;

        public Message(JsonObject header, byte[] body) {
            this.header = header;
            this.body = body;
        }

        public String cmd() {
            return header.has("cmd") ? header.get("cmd").getAsString() : "";
        }

        public String str(String key) {
            return header.has(key) && !header.get(key).isJsonNull()
                    ? header.get(key).getAsString() : "";
        }

        public boolean ok() {
            return header.has("ok") && header.get("ok").getAsBoolean();
        }

        public String error() {
            return str("error");
        }
    }

    /** Başlığı ve varsa gövdeyi yazar. */
    public static void write(DataOutputStream out, JsonObject header, byte[] body)
            throws IOException {
        if (body != null) {
            header.addProperty("len", body.length);
        }
        out.write((MAGIC + "\n").getBytes(StandardCharsets.UTF_8));
        out.write((header.toString() + "\n").getBytes(StandardCharsets.UTF_8));
        if (body != null) {
            out.write(body);
        }
        out.flush();
    }

    /** Başlığı ve varsa gövdeyi okur. */
    public static Message read(DataInputStream in) throws IOException {
        String magic = readLine(in, 64);
        if (!MAGIC.equals(magic)) {
            throw new IOException("beklenmeyen protokol imzası: " + magic);
        }
        String headerLine = readLine(in, 64 * 1024);
        JsonObject header;
        try {
            header = JsonParser.parseString(headerLine).getAsJsonObject();
        } catch (Exception e) {
            throw new IOException("başlık çözülemedi: " + e.getMessage());
        }

        byte[] body = null;
        if (header.has("len")) {
            int len = header.get("len").getAsInt();
            if (len < 0 || len > MAX_BODY) {
                throw new IOException("gövde boyutu sınır dışı: " + len);
            }
            body = new byte[len];
            in.readFully(body);
        }
        return new Message(header, body);
    }

    /**
     * Satır okur (LF ile biter).
     *
     * <p>{@code BufferedReader} KULLANILMAZ: karakter akışına sarılmış bir
     * girdiden sonra ham baytları (gövdeyi) okumak, okuyucunun tamponuna
     * takılan baytlar yüzünden bozulur. Bu, ikili veri taşıyan protokollerde
     * en sık yapılan hatadır.
     *
     * <p><b>Yakalanan gerçek hata:</b> baytlar tek tek {@code (char) b} ile
     * biriktiriliyordu; bu bir Latin-1 çözümüdür. {@link #write} ise başlığı
     * UTF-8 olarak yazıyor ve Gson ASCII dışı harfleri {@code \\u} ile
     * kaçırmıyor. Sonuç: "Ali oyuna katıldı" duyurusu her eşte "Ali oyuna
     * katÄ±ldÄ±" olarak, düğümler arası sohbet "Şu tarafta köy var" ise
     * "Åu tarafta kÃ¶y var" olarak görünüyordu — HER geçişte, HER düğümde.
     * Ham baytları biriktirip SONUNDA bir kez UTF-8 çözüyoruz; ham bayt
     * okuma (yani gövdenin aynı akıştan gelmesi) böylece korunuyor.
     *
     * <p>{@code max} artık karakter değil BAYT sınırıdır; zaten korunmak
     * istenen şey tel üzerindeki satır uzunluğudur.
     */
    private static String readLine(DataInputStream in, int max) throws IOException {
        ByteArrayOutputStream buf = new ByteArrayOutputStream();
        for (int i = 0; i < max; i++) {
            int b = in.read();
            if (b < 0) {
                throw new IOException("bağlantı beklenmedik şekilde kapandı");
            }
            if (b == '\n') {
                return new String(buf.toByteArray(), StandardCharsets.UTF_8);
            }
            if (b != '\r') {
                buf.write(b);
            }
        }
        throw new IOException("satır çok uzun");
    }

    /** Başarılı yanıt başlığı. */
    public static JsonObject ok() {
        JsonObject o = new JsonObject();
        o.addProperty("ok", true);
        return o;
    }

    /** Hata yanıtı başlığı. */
    public static JsonObject fail(String why) {
        JsonObject o = new JsonObject();
        o.addProperty("ok", false);
        o.addProperty("error", why);
        return o;
    }
}
