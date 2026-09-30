package gg.mcos.link.fabric;

import gg.mcos.link.GamePlayer;
import gg.mcos.link.Msg;
import gg.mcos.link.ProxyMessage;
import net.fabricmc.fabric.api.networking.v1.ServerPlayNetworking;
import net.minecraft.network.packet.s2c.common.ServerTransferS2CPacket;
import net.minecraft.server.network.ServerPlayerEntity;

import java.util.UUID;

/** {@link GamePlayer}'ın Yarn uygulaması. */
final class YarnPlayer implements GamePlayer {

    private final ServerPlayerEntity player;

    YarnPlayer(ServerPlayerEntity player) {
        this.player = player;
    }

    static YarnPlayer wrap(ServerPlayerEntity p) {
        return p == null ? null : new YarnPlayer(p);
    }

    /**
     * Oyuncu adı.
     *
     * <p><b>Neden {@code getGameProfile().name()} DEĞİL?</b> Eski kod onu
     * kullanıyordu ve yalnızca 1.21.9+ ile derlenebiliyordu: authlib 7
     * {@code GameProfile}'ı kayıt sınıfına çevirdi, 1.21.8 ve öncesinde
     * erişimci {@code getName()}'dir. {@code getName()} (varlık adı, oyuncuda
     * profil adının ta kendisi) 1.20.5'ten 1.21.11'e kadar AYNI ara adla
     * durur; bu sayede bu satır tek bir jar'ı sürüm grubuna bölmez.
     */
    static String nameOf(ServerPlayerEntity p) {
        return p.getName().getString();
    }

    @Override
    public UUID uuid() {
        return player.getUuid();
    }

    @Override
    public String name() {
        return nameOf(player);
    }

    @Override
    public int blockX() {
        return player.getBlockX();
    }

    @Override
    public int blockZ() {
        return player.getBlockZ();
    }

    @Override
    public void actionBar(Msg msg) {
        player.sendMessage(Texts.of(msg), true);
    }

    @Override
    public void chat(Msg msg) {
        player.sendMessage(Texts.of(msg), false);
    }

    @Override
    public void transfer(String host, int port) {
        // Bu paketten sonra istemci bizden kopar; başka bir şey göndermenin
        // anlamı yok.
        player.networkHandler.sendPacket(new ServerTransferS2CPacket(host, port));
    }

    @Override
    public void proxyConnect(String backend) {
        // Kanal kaydı BungeePayload'da; mesaj istemciye değil, yoldaki
        // Velocity vekiline gider ve o oyuncuyu bağlantıyı koparmadan geçirir.
        ServerPlayNetworking.send(player, new BungeePayload(ProxyMessage.connect(backend)));
    }
}
