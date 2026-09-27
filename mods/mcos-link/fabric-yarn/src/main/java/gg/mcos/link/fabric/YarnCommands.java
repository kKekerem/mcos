package gg.mcos.link.fabric;

import gg.mcos.link.CommandHost;
import gg.mcos.link.GamePlayer;
import gg.mcos.link.Msg;
import net.minecraft.server.command.ServerCommandSource;
import net.minecraft.server.network.ServerPlayerEntity;

import java.util.function.Predicate;

/** {@link CommandHost}'un Yarn uygulaması. */
final class YarnCommands implements CommandHost<ServerCommandSource> {

    static final YarnCommands INSTANCE = new YarnCommands();

    private YarnCommands() {
    }

    @Override
    public void reply(ServerCommandSource source, Msg msg, boolean toOps) {
        source.sendFeedback(() -> Texts.of(msg), toOps);
    }

    @Override
    public Predicate<ServerCommandSource> gamemaster() {
        // Tek sürüme özgü satır: ayrı dosyada, çünkü 1.21.11'de API değişti.
        return Perms.gamemaster();
    }

    @Override
    public GamePlayer playerOf(ServerCommandSource source) {
        return source.getEntity() instanceof ServerPlayerEntity p ? new YarnPlayer(p) : null;
    }
}
