package gg.mcos.link;

import java.nio.file.Path;
import java.util.List;
import java.util.UUID;

/**
 * Minecraft sunucusuna açılan TEK kapı.
 *
 * <p>Ortak kodun ({@link HandoffService}, {@link LinkServer},
 * {@link LinkCore}, {@link LinkCommands}) oyundan istediği her şey
 * buradadır ve bu dosya hiçbir Minecraft sınıfı içe aktarmaz. Her sürüm
 * bağdaştırıcısı ({@code fabric-yarn}, {@code fabric-mojang}) bunu kendi
 * eşleme adlarıyla uygular.
 *
 * <p><b>Neden bu sınır?</b> Mod önceden yalnızca 1.21.11 için derleniyordu
 * ve {@code fabric.mod.json} "&gt;=1.20.5" dediği için 1.21.1'e de
 * yükleniyordu: sunucu açılışta {@code NoSuchFieldError} ile düşüyordu,
 * çünkü 1.21.11'e özgü bir alan eski sürümde yoktu. Sürüme duyarlı her
 * çağrıyı bu arayüzün arkasına almak, "hangi satır hangi sürümde kırılır"
 * sorusunu birkaç küçük bağdaştırıcı sınıfına indirir.
 *
 * <p><b>İş parçacığı.</b> {@link #execute} dışındaki her metot YALNIZCA oyun
 * iş parçacığından çağrılır; Minecraft'ın oyuncu listesi eşzamanlı erişime
 * dayanıklı değildir.
 */
public interface Game {

    /** Zorluk seviyeleri (MCOS panelindeki adlarla). */
    enum Difficulty {
        PEACEFUL, EASY, NORMAL, HARD;

        /** Topolojideki adı çevirir; tanınmıyorsa {@code null}. */
        public static Difficulty parse(String s) {
            return switch (s) {
                case "peaceful" -> PEACEFUL;
                case "easy" -> EASY;
                case "normal" -> NORMAL;
                case "hard" -> HARD;
                default -> null;
            };
        }
    }

    /**
     * Görevi oyun iş parçacığında çalıştırır. Her iş parçacığından
     * çağrılabilir: başka bir iş parçacığından gelen görev sıraya konur ve
     * sonraki tikte çalışır.
     */
    void execute(Runnable task);

    /** Çevrimiçi oyuncular (anlık kopya). */
    List<GamePlayer> players();

    /** Çevrimiçi değilse {@code null}. */
    GamePlayer player(UUID id);

    /** Çevrimiçi değilse {@code null}. */
    GamePlayer player(String name);

    int playerCount();

    /** Her çevrimiçi oyuncunun kaydını diske yazar. */
    void saveAllPlayers();

    /**
     * Oyuncu kayıtlarının klasörü: 1.21.11'e kadar {@code <dünya>/playerdata},
     * 26.x'te {@code <dünya>/players/data}. Yol bu yüzden elle yazılmaz;
     * bağdaştırıcı oyunun kendi sabitini döndürür.
     */
    Path playerDataDir();

    /** Tüm oyunculara sohbet satırı (sistem iletisi). */
    void broadcast(Msg msg);

    void setDifficulty(Difficulty difficulty);

    /** Sekme listesinin başlığını ve altbilgisini herkese gönderir. */
    void sendTabList(Msg header, Msg footer);
}
