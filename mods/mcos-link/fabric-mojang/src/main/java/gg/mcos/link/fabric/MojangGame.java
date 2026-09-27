package gg.mcos.link.fabric;

import gg.mcos.link.Game;
import gg.mcos.link.GamePlayer;
import gg.mcos.link.Msg;
import net.minecraft.network.protocol.game.ClientboundTabListPacket;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.world.level.storage.LevelResource;

import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;

/** {@link Game}'in 26.x uygulaması (resmî adlar). */
final class MojangGame implements Game {

    private final MinecraftServer server;

    MojangGame(MinecraftServer server) {
        this.server = server;
    }

    @Override
    public void execute(Runnable task) {
        server.execute(task);
    }

    @Override
    public List<GamePlayer> players() {
        List<ServerPlayer> list = server.getPlayerList().getPlayers();
        List<GamePlayer> out = new ArrayList<>(list.size());
        for (ServerPlayer p : list) {
            out.add(new MojangPlayer(p));
        }
        return out;
    }

    @Override
    public GamePlayer player(UUID id) {
        return MojangPlayer.wrap(server.getPlayerList().getPlayer(id));
    }

    @Override
    public GamePlayer player(String name) {
        return MojangPlayer.wrap(server.getPlayerList().getPlayerByName(name));
    }

    @Override
    public int playerCount() {
        return server.getPlayerList().getPlayerCount();
    }

    @Override
    public void saveAllPlayers() {
        // Aktarımın doğruluğu için zorunlu: kayıt olmadan gönderilecek bir
        // .dat yok.
        server.getPlayerList().saveAll();
    }

    /**
     * Oyuncu kayıtlarının klasörü.
     *
     * <p><b>Dikkat:</b> 26.x'te bu klasör {@code playerdata/} DEĞİL,
     * {@code players/data/}'dır (26.1 ve 26.3 sunucu jar'larında
     * {@code LevelResource} sabitleri javap ile okundu). Yolu elle yazmak
     * yerine oyunun kendi sabitini kullandığımız için aktarım iki düzende de
     * doğru dosyayı okur ve yazar.
     */
    @Override
    public Path playerDataDir() {
        return server.getWorldPath(LevelResource.PLAYER_DATA_DIR);
    }

    @Override
    public void broadcast(Msg msg) {
        server.getPlayerList().broadcastSystemMessage(Texts.of(msg), false);
    }

    @Override
    public void setDifficulty(Difficulty d) {
        server.setDifficulty(switch (d) {
            case PEACEFUL -> net.minecraft.world.Difficulty.PEACEFUL;
            case EASY -> net.minecraft.world.Difficulty.EASY;
            case NORMAL -> net.minecraft.world.Difficulty.NORMAL;
            case HARD -> net.minecraft.world.Difficulty.HARD;
        }, true);
    }

    @Override
    public void sendTabList(Msg header, Msg footer) {
        server.getPlayerList().broadcastAll(
                new ClientboundTabListPacket(Texts.of(header), Texts.of(footer)));
    }
}
