package gg.mcos.link.paper;

/**
 * Fabric'in {@code net.minecraft.util.Formatting} değerlerinin Bukkit
 * karşılığı: eski "bölüm işareti" (§) renk kodları.
 *
 * <h2>Neden ayrı bir sınıf?</h2>
 *
 * <p>Fabric sürümü metni {@code Text.literal(...).formatted(Formatting.AQUA)}
 * ile kurar. Paper tarafında bunun iki karşılığı var: Adventure
 * {@code Component} API'si ya da düz {@link String} + § kodu. § kodunu
 * seçtik, çünkü:
 * <ul>
 *   <li>istemcide <b>aynı</b> görünür — § kodları oyunun kendi biçimlendirme
 *       mekanizmasıdır, Component de sonunda onlara dönüşür;</li>
 *   <li>doğrulanmış API listesinde olmayan hiçbir sınıf imzası
 *       gerektirmez ({@code Component}, {@code TextComponent},
 *       {@code NamedTextColor}, serileştiriciler... hiçbiri gerekmez);</li>
 *   <li>bu eklenti yalnızca kısa durum satırları yazar — tıklanabilir ya da
 *       üzerine gelinince açıklama gösteren zengin metne ihtiyacı yok.</li>
 * </ul>
 *
 * <p>Kodları TEK YERDE tutuyoruz. Üç ayrı sınıfa kopyalanmış bir "§c"
 * dizisi, er ya da geç birinde yanlış yazılır ve oyuncu ekranında ham
 * "§c" görür.
 *
 * <p>Kodlar {@code \\u00A7} kaçışıyla yazılır: § karakterini dosyaya
 * doğrudan gömmek, derleyici {@code -encoding UTF-8} almadığında ya da bir
 * araç dosyayı yeniden kodladığında sessizce bozulur. Kaçış dizisi,
 * kaynak dosyanın kodlamasından bağımsız olarak her zaman doğrudur.
 *
 * <p>Eşleme (Fabric {@code Formatting} -&gt; kod): AQUA=b, DARK_AQUA=3,
 * GRAY=7, DARK_GRAY=8, WHITE=f, GREEN=a, RED=c.
 */
public final class Fmt {

    /** Fabric: {@code Formatting.AQUA} */
    public static final String AQUA = "\u00A7b";
    /** Fabric: {@code Formatting.DARK_AQUA} */
    public static final String DARK_AQUA = "\u00A73";
    /** Fabric: {@code Formatting.GRAY} */
    public static final String GRAY = "\u00A77";
    /** Fabric: {@code Formatting.DARK_GRAY} */
    public static final String DARK_GRAY = "\u00A78";
    /** Fabric: {@code Formatting.WHITE} */
    public static final String WHITE = "\u00A7f";
    /** Fabric: {@code Formatting.GREEN} */
    public static final String GREEN = "\u00A7a";
    /** Fabric: {@code Formatting.RED} */
    public static final String RED = "\u00A7c";

    private Fmt() {
    }
}
