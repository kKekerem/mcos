package gg.mcos.link.fabric;

import gg.mcos.link.CommandHost;
import gg.mcos.link.GamePlayer;
import gg.mcos.link.Msg;
import net.minecraft.commands.CommandSourceStack;
import net.minecraft.commands.Commands;
import net.minecraft.server.level.ServerPlayer;

import java.util.function.Predicate;

/** {@link CommandHost}'un 26.x uygulaması (resmî adlar). */
final class MojangCommands implements CommandHost<CommandSourceStack> {

    static final MojangCommands INSTANCE = new MojangCommands();

    private MojangCommands() {
    }

    @Override
    public void reply(CommandSourceStack source, Msg msg, boolean toOps) {
        source.sendSuccess(() -> Texts.of(msg), toOps);
    }

    /**
     * Eski "yetki seviyesi 2".
     *
     * <p>26.x, 1.21.11'in izin sistemini sürdürüyor; yalnızca adlar resmî:
     * Yarn'daki {@code CommandManager.requirePermissionLevel(GAMEMASTERS_CHECK)}
     * burada {@code Commands.hasPermission(LEVEL_GAMEMASTERS)}'tır.
     */
    @Override
    public Predicate<CommandSourceStack> gamemaster() {
        return Commands.hasPermission(Commands.LEVEL_GAMEMASTERS);
    }

    @Override
    public GamePlayer playerOf(CommandSourceStack source) {
        return source.getEntity() instanceof ServerPlayer p ? new MojangPlayer(p) : null;
    }
}
