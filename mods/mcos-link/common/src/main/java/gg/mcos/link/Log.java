package gg.mcos.link;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/**
 * Modun tek günlük noktası.
 *
 * <p>Her sınıfta ayrı bir logger kurmak yerine tek bir ön ek kullanıyoruz:
 * sunucu günlüğünde "[MCOS Link]" araması, modun yaptığı HER ŞEYİ döker.
 * Sorun bildiren bir kullanıcıdan istenecek tek şey budur.
 *
 * <p>SLF4J, Minecraft sunucusuyla birlikte gelir; ek bağımlılık yoktur.
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
