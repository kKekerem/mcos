package gg.mcos.link.paper;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/**
 * Eklentinin tek günlük noktası.
 *
 * <p>Her sınıfta ayrı bir logger kurmak yerine tek bir ön ek kullanıyoruz:
 * sunucu günlüğünde "MCOS Link" araması, eklentinin yaptığı HER ŞEYİ döker.
 * Sorun bildiren bir kullanıcıdan istenecek tek şey budur.
 *
 * <h2>PAPER PORTU İÇİN NOT</h2>
 *
 * <p>SLF4J, Paper sunucusuyla da Fabric'le olduğu gibi birlikte gelir (Paper
 * konsolu zaten SLF4J üzerinden akar), bu yüzden
 * {@code LoggerFactory.getLogger("MCOS Link")} değişmeden çalışır ve ön ek
 * Fabric düğümündekiyle AYNI kalır — karışık bir kümede iki makinenin
 * günlüğünü yan yana koyup aynı kalıpla aramak mümkün olsun diye.
 *
 * <p>{@code JavaPlugin#getLogger()} da kullanılabilirdi ama o,
 * {@code java.util.logging} tabanlıdır ve ön eki eklentinin adıdır
 * ("MCOSLink"); iki düğümün günlüğü farklı ön ek taşırdı. Tek giriş noktası
 * ve tek ön ek, bu sınıfın tasarım amacıdır — statik olması da bu yüzden:
 * {@link LinkProtocol} dışındaki her sınıf, eklenti örneğini taşımadan
 * günlüğe yazabilmelidir.
 */
public final class Log {

    private static final Logger LOG = LoggerFactory.getLogger("MCOS Link");

    /** Ayrıntılı günlük, MCOS_LINK_DEBUG=1 ile açılır. */
    private static final boolean DEBUG =
            "1".equals(System.getenv("MCOS_LINK_DEBUG"));

    private Log() {
    }

    public static void info(String msg) {
        LOG.info(msg);
    }

    public static void warn(String msg) {
        LOG.warn(msg);
    }

    public static void error(String msg, Throwable t) {
        LOG.error(msg, t);
    }

    /**
     * Yalnızca hata ayıklama açıkken yazar.
     *
     * <p>Aktarım kararları her oyuncu için saniyede bir verilir; bunları her
     * zaman günlüğe yazmak, sunucu günlüğünü kullanılamaz hale getirir.
     */
    public static void debug(String msg) {
        if (DEBUG) {
            LOG.info("[debug] " + msg);
        }
    }

    public static boolean debugEnabled() {
        return DEBUG;
    }
}
