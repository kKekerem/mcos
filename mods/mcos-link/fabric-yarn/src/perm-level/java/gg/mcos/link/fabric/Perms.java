package gg.mcos.link.fabric;

import net.minecraft.server.command.ServerCommandSource;

import java.util.function.Predicate;

/**
 * Operatör denetimi — sayısal yetki seviyeli sürümler.
 *
 * <p>Minecraft 1.21.10'a kadar yetki bir sayıdır (1=moderatör,
 * 2=gamemaster, 3=admin, 4=sahip) ve {@code hasPermissionLevel(int)} ile
 * denetlenir. 1.21.11 bu yöntemi KALDIRDI; o sürümün karşılığı
 * {@code perm-check/} altındadır.
 */
final class Perms {

    private Perms() {
    }

    static Predicate<ServerCommandSource> gamemaster() {
        return s -> s.hasPermissionLevel(2);
    }
}
