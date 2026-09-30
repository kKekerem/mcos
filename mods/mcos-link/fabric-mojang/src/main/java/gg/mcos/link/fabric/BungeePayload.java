package gg.mcos.link.fabric;

import gg.mcos.link.ProxyMessage;
import io.netty.buffer.ByteBuf;
import net.fabricmc.fabric.api.networking.v1.PayloadTypeRegistry;
import net.minecraft.network.codec.StreamCodec;
import net.minecraft.network.protocol.common.custom.CustomPacketPayload;
import net.minecraft.resources.Identifier;

/**
 * "bungeecord:main" kanalının ham baytlı yükü (26.x, resmî adlar).
 *
 * <p>Neden: oyuncuyu Velocity vekilinin arkasındaki başka bir sunucuya
 * SESSİZCE geçirmek (kullanıcının isteği: "DonutSMP gibi, hepsi tek IP'den").
 * Vekil bu kanaldaki "Connect" mesajını yakalar; istemci onu hiç görmez.
 *
 * <p>Yarn tarafındaki {@code BungeePayload} ile aynı gerekçe: vanilya,
 * tanımadığı bir kanalın gövdesini boş yazar; Fabric API'ye kaydedilmiş bir
 * kodek olmadan vekile boş bir mesaj giderdi. 26.x'te Fabric API da resmî
 * adlara geçti: kayıt {@code clientboundPlay()} (Yarn'daki playS2C).
 */
record BungeePayload(byte[] data) implements CustomPacketPayload {

    static final CustomPacketPayload.Type<BungeePayload> TYPE =
            new CustomPacketPayload.Type<>(Identifier.parse(ProxyMessage.CHANNEL));

    /** Gövde olduğu gibi yazılır; önüne uzunluk eklemek mesajı bozardı. */
    static final StreamCodec<ByteBuf, BungeePayload> CODEC = StreamCodec.of(
            (buf, payload) -> buf.writeBytes(payload.data),
            buf -> {
                byte[] b = new byte[buf.readableBytes()];
                buf.readBytes(b);
                return new BungeePayload(b);
            });

    /** Sunucudan istemci yönüne kayıt; giriş noktasında bir kez çağrılır. */
    static void register() {
        PayloadTypeRegistry.clientboundPlay().register(TYPE, CODEC);
    }

    @Override
    public CustomPacketPayload.Type<? extends CustomPacketPayload> type() {
        return TYPE;
    }
}
