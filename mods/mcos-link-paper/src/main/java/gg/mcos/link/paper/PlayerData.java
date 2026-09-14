package gg.mcos.link.paper;

import org.bukkit.Bukkit;
import org.bukkit.World;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.UUID;

/**
 * Oyuncu kayıt dosyasına erişim.
 *
 * <p><b>Tasarım kararı.</b> Aktarımda oyuncu durumunu ELLE serileştirmiyoruz
 * (her eşya, her efekt, her NBT etiketi). Bunun yerine Minecraft'ın kendi
 * kayıt dosyasını ({@code playerdata/&lt;uuid&gt;.dat}) olduğu gibi taşıyoruz.
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
 * <h2>PAPER PORTU: TEK GERÇEK DEĞİŞİKLİK</h2>
 *
 * <p>Fabric {@code server.getSavePath(WorldSavePath.PLAYERDATA)} çağırıyordu.
 * Bukkit karşılığı {@code World#getWorldPath()}'tir:
 *
 * <pre>
 *   Path playerdata = Bukkit.getWorlds().get(0).getWorldPath().resolve("playerdata");
 * </pre>
 *
 * <p><b>{@code Server#getLevelDirectory()} DİYE BİR METOT YOK.</b> Bu dosya
 * bir zamanlar onu çağırıyordu ve derleme şu hatayla kırılıyordu:
 *
 * <pre>
 *   symbol:   method getLevelDirectory()
 *   location: interface Server
 * </pre>
 *
 * <p>{@code paper-api-1.21.11-R0.1-SNAPSHOT.jar} üzerinde {@code javap} ile
 * bakıldığında {@code org.bukkit.Server}'ın dizin döndüren metotları
 * yalnızca şunlardır: {@code getPluginsFolder()}, {@code getUpdateFolderFile()}
 * ve {@code getWorldContainer()}. {@code org.bukkit.World} ise
 * {@code getWorldFolder()} (File) ve {@code getWorldPath()} (Path) sunar.
 * Kullanılan {@code getWorldPath()}'tir — tip dönüşümü gerektirmeyen tek
 * seçenek.
 *
 * <p><b>{@code Server#getWorldContainer()} KULLANILMAZ.</b> O metot
 * {@code @ApiStatus.Obsolete} işaretlidir ve seviye klasörünün EBEVEYNİNİ
 * döndürür — yani bir dizin YUKARIDA. Sonuç sinsidir: klasör vardır ama
 * içinde .dat yoktur, {@link #read} null döner ve her aktarım "oyuncu kaydı
 * bulunamadı" ile iptal olur. Ağ hatası gibi görünen bir YOL hatası.
 *
 * <p>Bukkit boyutları kardeş klasörlere ayırır (world, world_nether,
 * world_the_end) ama {@code playerdata/} YALNIZCA ana seviye klasörünün
 * altındadır; bu eklentinin dilimlemesi zaten tek dünyada X ekseni üzerinden
 * olduğu için bunun bir etkisi yok.
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
     * {@code <dünya>/playerdata} klasörü.
     *
     * <p>Fabric sürümü {@code MinecraftServer}'ı parametre olarak alıyordu;
     * Bukkit'te sunucu zaten küresel olarak erişilebilir ({@code Bukkit}),
     * bu yüzden parametreye gerek yok.
     *
     * <p><b>Neden {@code getWorlds().get(0)}?</b> Bukkit boyutları kardeş
     * klasörlere ayırır (world, world_nether, world_the_end) ama
     * {@code playerdata/} YALNIZCA ana seviye klasörünün altındadır. Ana
     * dünya, sunucunun ilk yüklediği dünyadır ve liste yükleme sırasını
     * korur; ayrıca bu eklentinin dilimlemesi zaten tek dünyada X ekseni
     * üzerinden yapıldığı için başka bir boyut söz konusu değil.
     *
     * @throws IllegalStateException dünya listesi boşsa. Bu, yalnızca
     *     eklenti dünyalar yüklenmeden çalıştırılırsa olur
     *     ({@code plugin.yml} {@code load: POSTWORLD} varsayılanı bunu
     *     engeller). Sessizce yanlış bir yol uydurmaktansa yüksek sesle
     *     durmak doğrudur: yanlış yol, her aktarımın "oyuncu kaydı
     *     bulunamadı" ile iptal olması demektir ve sebebi görünmez.
     */
    public static Path directory() {
        List<World> worlds = Bukkit.getWorlds();
        if (worlds.isEmpty()) {
            throw new IllegalStateException(
                    "dünya henüz yüklenmedi; oyuncu verisi klasörü belirlenemiyor");
        }
        return worlds.get(0).getWorldPath().resolve("playerdata");
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
