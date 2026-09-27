package gg.mcos.link;

import java.util.function.Predicate;

/**
 * {@code /mcoslink} komutlarının oyundan istediği üç şey.
 *
 * <p>Komut ağacı Brigadier ile kurulur ve Brigadier Minecraft'tan ayrı,
 * eşlenmemiş bir kütüphanedir: {@code literal}, {@code argument},
 * {@code executes} her sürümde aynıdır. Sürüme göre değişen tek şey
 * "komut kaynağı" türüdür (Yarn'da {@code ServerCommandSource}, 26.x'te
 * {@code CommandSourceStack}); bu arayüz onu {@code S} tür parametresinin
 * arkasına saklar.
 *
 * @param <S> sürümün komut kaynağı türü
 */
public interface CommandHost<S> {

    /**
     * Komutu verene yanıt yazar.
     *
     * @param toOps {@code true} ise çevrimiçi operatörlere de bildirilir
     *              (dünyayı değiştiren komutlar için)
     */
    void reply(S source, Msg msg, boolean toOps);

    /**
     * Eski "yetki seviyesi 2"nin (gamemaster) karşılığı.
     *
     * <p><b>Neden bağdaştırıcıda?</b> Bu denetim sürümler arasında GERÇEKTEN
     * değişti: 1.21.10'a kadar {@code hasPermissionLevel(2)}, 1.21.11'de
     * {@code requirePermissionLevel(GAMEMASTERS_CHECK)}. 1.21.11 için
     * derlenen jar 1.21.1'e yüklendiğinde sunucuyu açılışta
     * {@code NoSuchFieldError} ile düşüren alan tam da buydu.
     */
    Predicate<S> gamemaster();

    /** Komutu veren bir oyuncuysa o, konsol ya da komut bloğuysa {@code null}. */
    GamePlayer playerOf(S source);
}
