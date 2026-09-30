package gg.mcos.link.fabric;

import gg.mcos.link.ProxyMessage;
import io.netty.buffer.ByteBuf;
import net.fabricmc.fabric.api.networking.v1.PayloadTypeRegistry;
import net.minecraft.network.codec.PacketCodec;
import net.minecraft.network.packet.CustomPayload;
import net.minecraft.util.Identifier;

/**
 * "bungeecord:main" kanalının ham baytlı yükü (Yarn adları, 1.20.5–1.21.11).
 *
 * <p>Neden: oyuncuyu Velocity vekilinin arkasındaki başka bir sunucuya
 * SESSİZCE geçirmek (kullanıcının isteği: "DonutSMP gibi, hepsi tek IP'den").
 * Vekil bu kanaldaki "Connect" mesajını yakalar; istemci onu hiç görmez.
 *
 * <p><b>Neden Fabric API'nin yük kaydı?</b> 1.20.5'ten beri vanilya, tanımadığı
 * bir kanalın yükünü yazarken GÖVDEYİ BOŞ bırakır (bilinmeyen kanal "atılmış
 * yük" sayılır). Kayıtlı bir kodek olmadan vekile yalnızca kanal adı giderdi
 * ve Velocity boş bir mesajı sessizce yok sayardı.
 *
 * <p><b>Neden {@code Identifier.tryParse}?</b> Bu kaynak 1.20.5–1.21.10'un
 * hepsine TEK jar olarak gidiyor. {@code new Identifier(ns, yol)} 1.21'de
 * kaldırıldı, {@code Identifier.of(ns, yol)} ise 1.20.5'te başka bir metodun
 * adıydı; tryParse(String) iki uçta da aynı ara adla (method_12829) duruyor.
 */
record BungeePayload(byte[] data) implements CustomPayload {

    static final CustomPayload.Id<BungeePayload> ID =
            new CustomPayload.Id<>(Identifier.tryParse(ProxyMessage.CHANNEL));

    /**
     * Gövde olduğu gibi yazılır: vekil, kanalın içeriğini bir DataInput gibi
     * okur; önüne bir uzunluk eklemek mesajı bozardı. Çözücü yalnızca
     * simetri için var (mod yalnızca sunucuda, bu yük sunucuya hiç gelmez).
     */
    static final PacketCodec<ByteBuf, BungeePayload> CODEC = PacketCodec.of(
            (payload, buf) -> buf.writeBytes(payload.data),
            buf -> {
                byte[] b = new byte[buf.readableBytes()];
                buf.readBytes(b);
                return new BungeePayload(b);
            });

    /** Sunucudan istemci yönüne (S2C) kayıt; giriş noktasında bir kez çağrılır. */
    static void register() {
        PayloadTypeRegistry.playS2C().register(ID, CODEC);
    }

    @Override
    public CustomPayload.Id<? extends CustomPayload> getId() {
        return ID;
    }
}
