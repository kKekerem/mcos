package gg.mcos.link.fabric;

import gg.mcos.link.GamePlayer;
import gg.mcos.link.Msg;
import net.minecraft.network.protocol.common.ClientboundTransferPacket;
import net.minecraft.server.level.ServerPlayer;

import java.util.UUID;

/** {@link GamePlayer}'ın 26.x uygulaması (resmî adlar). */
final class MojangPlayer implements GamePlayer {

    private final ServerPlayer player;

    MojangPlayer(ServerPlayer player) {
        this.player = player;
    }

    static MojangPlayer wrap(ServerPlayer p) {
        return p == null ? null : new MojangPlayer(p);
    }

    /**
     * Oyuncu adı — Yarn tarafındaki {@code YarnPlayer.nameOf} ile aynı
     * gerekçe: varlık adı, oyuncuda profil adının ta kendisidir ve
     * {@code GameProfile}'ın erişimci biçimine bağlı değildir.
     */
    static String nameOf(ServerPlayer p) {
        return p.getName().getString();
    }

    @Override
    public UUID uuid() {
        return player.getUUID();
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
        // İkinci parametre "overlay": true → eylem çubuğu (Yarn'daki
        // sendMessage(text, true) ile aynı paket).
        player.sendSystemMessage(Texts.of(msg), true);
    }

    @Override
    public void chat(Msg msg) {
        player.sendSystemMessage(Texts.of(msg), false);
    }

    @Override
    public void transfer(String host, int port) {
        // Bu paketten sonra istemci bizden kopar; başka bir şey göndermenin
        // anlamı yok.
        player.connection.send(new ClientboundTransferPacket(host, port));
    }
}
