package gg.mcos.link.paper;

import org.bukkit.Bukkit;
import org.bukkit.Server;
import org.bukkit.World;

import java.io.IOException;
import java.lang.reflect.Method;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.UUID;

/**
 * Oyuncu kayıt dosyasına erişim.
 *
 * <p><b>Tasarım kararı.</b> Aktarımda oyuncu durumunu ELLE serileştirmiyoruz
 * (her eşya, her efekt, her NBT etiketi). Bunun yerine Minecraft'ın kendi
 * kayıt dosyasını ({@code playerdata/&lt;uuid&gt;.dat}; 26.1 ve sonrasında
 * {@code players/data/&lt;uuid&gt;.dat}) olduğu gibi taşıyoruz.
 *
 * <p>Nedenleri:
 * <ul>
 *   <li><b>Eksiksizlik.</b> Elle yazılan bir serileştirici er ya da geç bir
 *       şeyi unutur — ender sandığı, ateş süresi, ilerlemeler, kullanılan
 *       eşyanın dayanıklılığı. Kayıp eşya, oyuncu için affedilmez bir hatadır.</li>
 *   <li><b>Sürüm dayanıklılığı.</b> Minecraft her sürümde NBT şemasını
 *       değiştirir. Dosyayı taşımak, şemayı hiç yorumlamamak demektir.</li>
 *   <li><b>Basitlik.</b> Bu sınıf 60 satır; elle serileştirici 600 olurdu.</li>
 * </ul>
 *
 * <h2>PAPER PORTU: KAYIT KLASÖRÜ NEREDE — İKİ AYRI DÜZEN</h2>
 *
 * <p>Fabric {@code server.getSavePath(WorldSavePath.PLAYERDATA)} çağırıyordu,
 * yani yolu oyunun KENDİSİNE soruyordu. Bukkit'te bunun karşılığı yok; yol
 * burada kuruluyor ve Minecraft 26.1 ile yol DEĞİŞTİ. Tek jar 1.20.5'ten
 * 26.3'e kadar yüklendiği için ikisini de bilmek zorunda.
 *
 * <p>Gerçek sunucularda ölçülen düzen (düz dünya, ilk açılış, hiç oyuncu
 * girmeden — klasörü sunucu açılışta KENDİSİ oluşturuyor):
 * <pre>
 *   1.20.5 .. 1.21.11   world/playerdata/              (ana dünya klasörü = seviye klasörü)
 *                       world_nether/DIM-1, world_the_end/DIM1
 *   26.1.1 .. 26.3      world/players/data/            (seviye klasörü)
 *                       world/dimensions/minecraft/overworld   (= World#getWorldPath)
 * </pre>
 * 26.1.2 sunucu jar'ında {@code LevelResource.PLAYER_DATA_DIR} sabiti
 * {@code "players/data"}'dır (javap ile bakıldı).
 *
 * <p><b>Yakalanan gerçek hata:</b> bu dosya eskiden
 * {@code getWorlds().get(0).getWorldPath().resolve("playerdata")}
 * kullanıyordu. 26.1.2'de gelen bir aktarım
 * {@code world/dimensions/minecraft/overworld/playerdata/<uuid>.dat}
 * dosyasına yazıldı — sunucunun HİÇ okumadığı bir klasöre. Eş "ok" alıyor,
 * istemciyi aktarıyor ve oyuncu hedefte BOŞ envanterle doğuyordu; giden
 * yönde de {@code saveData()} sonrası dosya bulunamıyor ve her aktarım
 * "oyuncu kaydı bulunamadı" ile iptal oluyordu.
 *
 * <p>1.21.11'de açılmış bir dünyayı 26.1.2 ile açınca sunucu
 * {@code world/playerdata/*.dat} dosyalarını {@code world/players/data/}
 * altına TAŞIYOR ve eski klasör kalmıyor (ölçüldü). Yani yükseltilmiş bir
 * dünyada da doğru yer yeni düzendir.
 *
 * <h3>Seviye klasörü nasıl bulunuyor</h3>
 *
 * <p>{@code Server#getLevelDirectory()} 26.1 ile GELDİ: 1.20.6 ve 1.21.11
 * paper-api'de YOK, 26.1.2 ve 26.3 paper-api'de VAR (javap). Eklenti 1.20.6
 * API'siyle derlendiği için bu metot yansımayla (reflection) aranıyor;
 * yoksa ana dünyanın {@code getWorldFolder()}'ı kullanılıyor — 26.1
 * öncesinde ana dünyanın klasörü seviye klasörünün TA KENDİSİDİR.
 *
 * <p>{@code World#getWorldPath()} KULLANILMAZ: 1.21.6 paper-api'de YOK
 * (1.21.8'de var), yani 1.20.5–1.21.6 sunucularında
 * {@code NoSuchMethodError} verirdi; 26.x'te ise seviye klasörünü değil
 * boyut klasörünü döndürür — yukarıdaki hatanın kaynağı tam olarak buydu.
 * {@code getWorldFolder()} ise ölçülen her sürümde vardır.
 *
 * <p><b>{@code Server#getWorldContainer()} KULLANILMAZ.</b> O metot
 * {@code @ApiStatus.Obsolete} işaretlidir ve seviye klasörünün EBEVEYNİNİ
 * döndürür — yani bir dizin YUKARIDA. Sonuç sinsidir: klasör vardır ama
 * içinde .dat yoktur, {@link #read} null döner ve her aktarım "oyuncu kaydı
 * bulunamadı" ile iptal olur. Ağ hatası gibi görünen bir YOL hatası.
 *
 * <h2>KARIŞIK KÜME (Fabric + Paper) UYARISI — SESSİZCE GEÇİLMEDİ</h2>
 *
 * <p>Paper/CraftBukkit, AYNI .dat dosyasına vanilla/Fabric'in hiç yazmadığı
 * iki alt bileşik yazar ({@code CraftPlayer#setExtraData}):
 * <ul>
 *   <li>{@code bukkit}: newExp, newTotalExp, newLevel, expToDrop, keepLevel,
 *       firstPlayed, lastPlayed, lastKnownName</li>
 *   <li>{@code Paper}: LastLogin, LastSeen</li>
 * </ul>
 * ve bunları geri okurken varsayılanlı okuyucular kullanır
 * ({@code getLongOr}/{@code getIntOr}/{@code getBooleanOr}).
 *
 * <p>Üç yön, üç sonuç:
 * <ul>
 *   <li><b>Paper -&gt; Paper: kayıpsız.</b> İki bileşik de taşıdığımız
 *       dosyanın İÇİNDEDİR; ayrıca bir şey yapmaya gerek yok.</li>
 *   <li><b>Paper -&gt; Fabric: zararsız.</b> NBT okuyucular tanımadıkları
 *       etiketleri yok sayar — yukarıdaki "sürüm dayanıklılığı" argümanının
 *       ta kendisi.</li>
 *   <li><b>Fabric -&gt; Paper: KAYIPLI.</b> Gelen dosyada bu bileşikler
 *       olmadığı için varsayılanlar devreye girer: firstPlayed/lastPlayed 0'a,
 *       newExp/newTotalExp/newLevel/expToDrop 0'a, keepLevel false'a döner.
 *       Pratik zarar, yanlış ilk/son oynama zaman damgalarıdır; keepLevel ve
 *       newExp yalnızca ÖLÜM anında iş görür.</li>
 * </ul>
 *
 * <p><b>Verilen karar: dosya OLDUĞU GİBİ taşınır, bileşikler UYDURULMAZ.</b>
 * Alternatif, o on alanı LinkProtocol başlığına yan kanal olarak eklemek
 * olurdu; bu, telin Fabric ile bayt uyumunu bozar (Fabric eş o alanları
 * yazmaz, okumaz) ve modun "şemayı hiç yorumlama" ilkesini deler — yani
 * bir sonraki Minecraft sürümünde alan adı değiştiğinde sessizce bozulacak
 * tek yer burası olurdu. Bunun yerine işletim kuralı şudur: <b>küme ya
 * tamamen Fabric ya tamamen Paper olsun</b>; karışık geçiş yapılacaksa tüm
 * düğümler AYNI ANDA yükseltilsin. Kayıp, kabul ediliyorsa da bilinerek
 * kabul edilmiş olur.
 */
public final class PlayerData {

    private PlayerData() {
    }

    /**
     * {@code Server#getLevelDirectory()} — yalnızca 26.1 ve sonrasında var,
     * yoksa null. Bkz. sınıf açıklaması.
     *
     * <p>Sınıf yüklenirken BİR KEZ aranır: aktarım başına yansıma araması
     * hem gereksiz hem de her seferinde aynı cevabı verir.
     */
    private static final Method LEVEL_DIRECTORY = findLevelDirectory();

    private static Method findLevelDirectory() {
        try {
            Method m = Server.class.getMethod("getLevelDirectory");
            // Dönüş tipi de denetleniyor: aynı adla başka tipte bir metot
            // gelirse aşağıdaki (Path) dönüşümü aktarım anında patlardı.
            return Path.class.isAssignableFrom(m.getReturnType()) ? m : null;
        } catch (NoSuchMethodException e) {
            return null; // 1.20.5 .. 1.21.11: beklenen durum
        }
    }

    /**
     * Seviye klasörü ({@code level.dat}'ın bulunduğu klasör).
     *
     * <p>Fabric sürümü {@code MinecraftServer}'ı parametre olarak alıyordu;
     * Bukkit'te sunucu zaten küresel olarak erişilebilir ({@code Bukkit}),
     * bu yüzden parametreye gerek yok.
     *
     * <p><b>26.1 öncesinde neden {@code getWorlds().get(0)}?</b> Bukkit
     * boyutları kardeş klasörlere ayırır (world, world_nether,
     * world_the_end) ve {@code playerdata/} YALNIZCA ana seviye klasörünün
     * altındadır. Ana dünya, sunucunun ilk yüklediği dünyadır ve liste
     * yükleme sırasını korur.
     *
     * @throws IllegalStateException dünya listesi boşsa. Bu, yalnızca
     *     eklenti dünyalar yüklenmeden çalıştırılırsa olur
     *     ({@code plugin.yml} {@code load: POSTWORLD} varsayılanı bunu
     *     engeller). Sessizce yanlış bir yol uydurmaktansa yüksek sesle
     *     durmak doğrudur: yanlış yol, her aktarımın "oyuncu kaydı
     *     bulunamadı" ile iptal olması demektir ve sebebi görünmez.
     */
    static Path levelDirectory() {
        if (LEVEL_DIRECTORY != null) {
            try {
                return (Path) LEVEL_DIRECTORY.invoke(Bukkit.getServer());
            } catch (ReflectiveOperationException | ClassCastException e) {
                throw new IllegalStateException(
                        "seviye klasörü sunucudan alınamadı: " + e, e);
            }
        }
        List<World> worlds = Bukkit.getWorlds();
        if (worlds.isEmpty()) {
            throw new IllegalStateException(
                    "dünya henüz yüklenmedi; oyuncu verisi klasörü belirlenemiyor");
        }
        return worlds.get(0).getWorldFolder().toPath();
    }

    /**
     * Oyuncu kayıt klasörü: 26.1+ {@code players/data}, öncesi
     * {@code playerdata}.
     *
     * <p><b>Önce DİSKE bakılır, sürüme değil.</b> İki klasörü de sunucu
     * açılışta kendisi oluşturur (ölçüldü: hiç oyuncu girmeden ikisi de
     * yerinde). Yani var olan klasör, sunucunun GERÇEKTEN okuduğu
     * klasördür. Yeni düzen önce denenir: yükseltmede sunucu eski klasörü
     * zaten kaldırıyor, ama bir yedekten geri kopyalanmış eski bir
     * {@code playerdata/} yanlış yere yazmamıza yol açmamalı.
     *
     * <p>İkisi de yoksa (biri çalışırken silmişse) API'ye göre karar verilir:
     * {@code getLevelDirectory()} varsa sunucu 26.1+ düzenindedir.
     * {@code HandoffService}/{@code LinkServer} gerekirse klasörü
     * oluşturur.
     */
    public static Path directory() {
        Path level = levelDirectory();
        Path modern = level.resolve("players").resolve("data");
        if (Files.isDirectory(modern)) {
            return modern;
        }
        Path legacy = level.resolve("playerdata");
        if (Files.isDirectory(legacy)) {
            return legacy;
        }
        return LEVEL_DIRECTORY != null ? modern : legacy;
    }

    /** Bir oyuncunun kayıt dosyası. */
    public static Path file(UUID uuid) {
        return directory().resolve(uuid + ".dat");
    }

    /**
     * Kayıt dosyasını okur.
     *
     * <p>Dosya yoksa null döner: hiç oynamamış bir oyuncunun verisi
     * olmayabilir ve bu bir hata değildir — hedef düğüm onu yeni oyuncu
     * gibi karşılar.
     *
     * <p><b>İş parçacığı:</b> dosya okuma engelleyicidir, ama bu çağrı
     * bilerek OYUN İŞ PARÇACIĞINDA yapılır ve hemen öncesinde
     * {@code player.saveData()} gelir (bkz. {@code HandoffService#begin}).
     * Araya bir tik girerse okuduğumuz dosya, kaydettiğimiz dosya olmayabilir
     * — sıralamanın bozulmaması, duraklamadan daha önemlidir. Asıl
     * engelleyici iş (sekiz saniyeye kadar süren soket) zaten havuza
     * devredilir.
     *
     * <p><b>Duraklamanın gerçek üst sınırı, "birkaç on kilobayt" DEĞİL.</b>
     * Burada bir zamanlar öyle yazıyordu; tipik bir .dat için doğru ama
     * kodun GARANTİ ETTİĞİ şey o değil. Tek sınır aşağıdaki
     * {@link LinkProtocol#MAX_BODY} denetimidir: 8 MiB. Envanteri ağır bir
     * oyuncunun birkaç yüz kilobaytlık dosyası, MCOS'un hedeflediği donanımda
     * (SD karttan çalışan tek kartlı bilgisayar) ana iş parçacığında yüz
     * milisaniyelerle ölçülen bir duraklama demektir.
     *
     * <p>Buna rağmen okuma ana iş parçacığında KALIYOR, çünkü alternatifi
     * daha kötü: okumayı havuza taşımak, {@code saveData()} ile okuma
     * arasına tik sokar ve gönderilen dosyanın kaydedilen dosya olduğu
     * garantisini kaybettirir — yani envanter kaybı riski. Değiş tokuş
     * bilinerek bu yönde yapıldı; sınırı daraltmak gerekirse doğru yer
     * MAX_BODY değil, handoff'a özel ayrı ve daha küçük bir tavandır
     * (protokolün genel tavanını değiştirmek Fabric eşiyle uyumu bozar).
     */
    public static byte[] read(UUID uuid) throws IOException {
        Path p = file(uuid);
        if (!Files.isRegularFile(p)) {
            return null;
        }
        byte[] data = Files.readAllBytes(p);
        if (data.length == 0 || data.length > LinkProtocol.MAX_BODY) {
            // Boş dosya, yarıda kesilmiş bir kaydın izidir; göndermek
            // oyuncunun envanterini SİLER. Göndermemek, onu yerinde
            // tutmaktan daha kötü değildir.
            throw new IOException("oyuncu kaydı geçersiz boyutta: " + data.length);
        }
        return data;
    }
}
