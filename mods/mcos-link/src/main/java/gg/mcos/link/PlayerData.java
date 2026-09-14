package gg.mcos.link;

import net.minecraft.server.MinecraftServer;
import net.minecraft.util.WorldSavePath;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
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
 */
public final class PlayerData {

    private PlayerData() {
    }

    /** {@code <dünya>/playerdata} klasörü. */
    public static Path directory(MinecraftServer server) {
        return server.getSavePath(WorldSavePath.PLAYERDATA);
    }

    /** Bir oyuncunun kayıt dosyası. */
    public static Path file(MinecraftServer server, UUID uuid) {
        return directory(server).resolve(uuid + ".dat");
    }

    /**
     * Kayıt dosyasını okur.
     *
     * <p>Dosya yoksa null döner: hiç oynamamış bir oyuncunun verisi
     * olmayabilir ve bu bir hata değildir — hedef düğüm onu yeni oyuncu
     * gibi karşılar.
     */
    public static byte[] read(MinecraftServer server, UUID uuid) throws IOException {
        Path p = file(server, uuid);
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
