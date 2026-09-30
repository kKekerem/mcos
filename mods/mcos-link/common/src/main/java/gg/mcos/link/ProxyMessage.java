package gg.mcos.link;

import java.io.ByteArrayOutputStream;
import java.io.DataOutputStream;
import java.io.IOException;

/**
 * Velocity vekil sunucusuna giden BungeeCord uyumlu eklenti mesajı.
 *
 * <p>Neden: kullanıcı "modern sunucular (DonutSMP) oyuncuyu yeni sunucuya
 * aktarmıyor, sessizce geçiriyor; hepsi tek bir IP'den çıkıyor" dedi.
 * DonutSMP, Velocity vekilinin arkasındaki Paper sunucularıdır ve sunucular
 * arası geçişi vekil yapar. Arka sunucunun vekile "bu oyuncuyu şu sunucuya
 * geçir" demesinin yerleşik yolu, BungeeCord'dan kalan "Connect" mesajıdır;
 * Velocity onu {@code bungee-plugin-message-channel = true} iken anlar ve
 * oyuncunun istemci bağlantısını KOPARMADAN arka sunucuyu değiştirir.
 *
 * <p>Bu yüzden istemcide yine hiçbir mod gerekmez: mesaj istemciye hiç
 * ulaşmaz, vekil onu yolda yakalayıp tüketir.
 */
public final class ProxyMessage {

    /**
     * Kanalın 1.13+ adı. Velocity yalnızca bunu (ve eski "BungeeCord"
     * adını) tanır; Paper'ın Bukkit API'si "BungeeCord" yazılınca kendisi
     * bu ada çevirir.
     */
    public static final String CHANNEL = "bungeecord:main";

    private ProxyMessage() {
    }

    /**
     * "Connect" mesajının gövdesi: writeUTF("Connect") + writeUTF(sunucu).
     *
     * <p>DataOutputStream.writeUTF biçimi (2 baytlık uzunluk + değiştirilmiş
     * UTF-8) ŞARTTIR: Velocity gövdeyi Guava'nın ByteArrayDataInput.readUTF'i
     * ile okur ve o da tam olarak bu biçimi bekler. Başka bir kodlama,
     * vekilin mesajı sessizce yok sayması demektir.
     */
    public static byte[] connect(String backend) {
        ByteArrayOutputStream buf = new ByteArrayOutputStream(16 + backend.length());
        try (DataOutputStream out = new DataOutputStream(buf)) {
            out.writeUTF("Connect");
            out.writeUTF(backend);
        } catch (IOException e) {
            // Bellekteki bir akışa yazmak G/Ç hatası veremez; buraya düşmek
            // ancak 64 KB'yi aşan bir ad demektir (writeUTF sınırı).
            throw new IllegalArgumentException("vekil sunucu adı yazılamadı: " + backend, e);
        }
        return buf.toByteArray();
    }
}
