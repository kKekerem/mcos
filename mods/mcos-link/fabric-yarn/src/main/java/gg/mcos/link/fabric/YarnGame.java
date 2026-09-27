package gg.mcos.link.fabric;

import gg.mcos.link.Game;
import gg.mcos.link.GamePlayer;
import gg.mcos.link.Msg;
import net.minecraft.network.packet.s2c.play.PlayerListHeaderS2CPacket;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.network.ServerPlayerEntity;
import net.minecraft.util.WorldSavePath;

import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;

/** {@link Game}'in Yarn uygulaması (1.20.5–1.21.11). */
final class YarnGame implements Game {

    private final MinecraftServer server;

    YarnGame(MinecraftServer server) {
        this.server = server;
    }

    @Override
    public void execute(Runnable task) {
        server.execute(task);
    }

    @Override
    public List<GamePlayer> players() {
        List<ServerPlayerEntity> list = server.getPlayerManager().getPlayerList();
        List<GamePlayer> out = new ArrayList<>(list.size());
        for (ServerPlayerEntity p : list) {
            out.add(new YarnPlayer(p));
        }
        return out;
    }

    @Override
    public GamePlayer player(UUID id) {
        return YarnPlayer.wrap(server.getPlayerManager().getPlayer(id));
    }

    @Override
    public GamePlayer player(String name) {
        return YarnPlayer.wrap(server.getPlayerManager().getPlayer(name));
    }

    @Override
    public int playerCount() {
        return server.getPlayerManager().getCurrentPlayerCount();
    }

    @Override
    public void saveAllPlayers() {
        // Aktarımın doğruluğu için zorunlu: kayıt olmadan gönderilecek bir
        // .dat yok.
        server.getPlayerManager().saveAllPlayerData();
    }

    @Override
    public Path playerDataDir() {
        return server.getSavePath(WorldSavePath.PLAYERDATA);
    }

    @Override
    public void broadcast(Msg msg) {
        server.getPlayerManager().broadcast(Texts.of(msg), false);
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
        server.getPlayerManager().sendToAll(
                new PlayerListHeaderS2CPacket(Texts.of(header), Texts.of(footer)));
    }
}
