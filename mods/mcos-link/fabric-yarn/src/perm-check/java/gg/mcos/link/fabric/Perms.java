package gg.mcos.link.fabric;

import net.minecraft.server.command.CommandManager;
import net.minecraft.server.command.ServerCommandSource;

import java.util.function.Predicate;

/**
 * Operatör denetimi — 1.21.11'in izin sistemi.
 *
 * <p>── API değişikliği (1.21.11) ──────────────────────────────────────
 * Burada önce {@code s -> s.hasPermissionLevel(2)} vardı ve 1.21.11'e
 * karşı derleme "cannot find symbol: hasPermissionLevel" ile kırılıyordu.
 * Minecraft sayısal yetki seviyesini bıraktı; {@code ServerCommandSource}
 * artık {@code PermissionSource} uyguluyor ve denetim {@code PermissionCheck}
 * sabitleriyle yapılıyor. (Kanıt: minecraft-merged-1.21.11 jar'ında javap
 * ile {@code ServerCommandSource}'ta {@code hasPermissionLevel} YOK, yerine
 * {@code getPermissions(): PermissionPredicate} var.)
 *
 * <p>Eski seviye 2'nin karşılığı {@code GAMEMASTERS_CHECK}'tir (vanilla:
 * 1=MODERATORS, 2=GAMEMASTERS, 3=ADMINS, 4=OWNERS).
 *
 * <p><b>Neden ayrı dosya?</b> Bu alan 1.21.11'den önce YOKTUR. Mod eskiden
 * yalnızca 1.21.11 için derlenip "&gt;=1.20.5" diye ilan ediliyordu ve
 * 1.21.1 sunucusunu açılışta {@code NoSuchFieldError} ile düşürüyordu.
 */
final class Perms {

    private Perms() {
    }

    static Predicate<ServerCommandSource> gamemaster() {
        return CommandManager.requirePermissionLevel(CommandManager.GAMEMASTERS_CHECK);
    }
}
