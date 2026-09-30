package gg.mcos.link;

import java.util.UUID;

/**
 * Çevrimiçi bir oyuncu — sürümden bağımsız görünüm.
 *
 * <p>{@link Game} gibi bu arayüz de hiçbir Minecraft sınıfı içe aktarmaz;
 * bağdaştırıcı onu oyunun kendi oyuncu nesnesinin üstüne ince bir kabuk
 * olarak kurar. Kabuk saklanmaz: her denetimde yeniden alınır, çünkü oyuncu
 * nesnesi ölüm/yeniden doğuşta DEĞİŞİR ve eski nesneye paket göndermek
 * hiçbir yere ulaşmaz.
 *
 * <p>Metotlar yalnızca oyun iş parçacığından çağrılır.
 */
public interface GamePlayer {

    UUID uuid();

    /** Oyuncu adı (profil adı). */
    String name();

    int blockX();

    int blockZ();

    /** Eylem çubuğunda (sohbetin üstünde, kalıcı olmayan) bir satır. */
    void actionBar(Msg msg);

    /** Sohbete yazılan, kalıcı bir satır. */
    void chat(Msg msg);

    /**
     * İstemciyi Minecraft'ın transfer paketiyle başka bir sunucuya yollar.
     *
     * <p>Paket 1.20.5 ile geldi; modun desteklediği en eski sürümün 1.20.5
     * olmasının sebebi budur.
     */
    void transfer(String host, int port);

    /**
     * Oyuncuyu Velocity vekilinin arkasındaki başka bir sunucuya geçirir
     * ("bungeecord:main" kanalında "Connect" mesajı; bkz. {@link ProxyMessage}).
     *
     * <p>{@link #transfer}'in aksine istemci bağlantısı KOPMAZ ve oyuncu yeni
     * bir adres görmez: geçişi vekil yapar. Kullanıcının isteği buydu —
     * "DonutSMP gibi sessizce geçirsin, hepsi tek IP'den çıksın".
     *
     * @throws RuntimeException mesaj gönderilemezse (çağıran yakalar)
     */
    void proxyConnect(String backend);
}
