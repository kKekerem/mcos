package gg.mcos.link.fabric;

import gg.mcos.link.LinkCommands;
import gg.mcos.link.LinkCore;
import gg.mcos.link.Log;
import net.fabricmc.api.DedicatedServerModInitializer;
import net.fabricmc.fabric.api.command.v2.CommandRegistrationCallback;
import net.fabricmc.fabric.api.event.lifecycle.v1.ServerLifecycleEvents;
import net.fabricmc.fabric.api.event.lifecycle.v1.ServerTickEvents;
import net.fabricmc.fabric.api.message.v1.ServerMessageEvents;
import net.fabricmc.fabric.api.networking.v1.ServerPlayConnectionEvents;
import net.fabricmc.loader.api.FabricLoader;

/**
 * MCOS Link — giriş noktası (26.x: eşlemesiz, Mojang'ın resmî adları).
 *
 * <p>Yarn bağdaştırıcısındaki {@code McosLink}'in birebir karşılığıdır;
 * yalnızca olay parametrelerinin türleri resmî adlarla yazılır
 * ({@code ServerPlayer}, {@code PlayerChatMessage}, {@code CommandSourceStack}).
 * Karar veren her satır {@code common/} altındadır.
 *
 * <p><b>Neden ayrı jar?</b> 26.1'den beri Minecraft karartılmamış
 * (obfuscation yok) olarak yayımlanıyor ve Fabric ara adları (intermediary)
 * bıraktı: 26.x modları doğrudan resmî adlara bağlanır. Yarn/intermediary ile
 * derlenmiş bir jar 26.x'te sınıfları bulamaz; bu yüzden 26.x için ayrı,
 * yeniden eşlenmeyen bir yapı gerekir.
 */
public final class McosLink implements DedicatedServerModInitializer {

    private final LinkCore core = new LinkCore();

    @Override
    public void onInitializeServer() {
        // Vekil (Velocity) üzerinden sessiz geçiş için "bungeecord:main"
        // yükü. Kayıt BAŞTA yapılır: ilk oyuncu bağlanmadan kodek hazır
        // olmalı, yoksa ilk geçişte vekile boş bir mesaj giderdi.
        BungeePayload.register();
        ServerLifecycleEvents.SERVER_STARTED.register(server ->
                core.started(new MojangGame(server)));
        ServerLifecycleEvents.SERVER_STOPPING.register(server -> core.stopping());
        ServerTickEvents.END_SERVER_TICK.register(server -> core.tick());

        ServerMessageEvents.CHAT_MESSAGE.register((message, sender, params) ->
                core.chat(MojangPlayer.nameOf(sender), message.decoratedContent().getString()));

        ServerPlayConnectionEvents.JOIN.register((handler, sender, server) ->
                core.joined(handler.getPlayer().getUUID(),
                        MojangPlayer.nameOf(handler.getPlayer())));

        // Kayıt KOŞULSUZ: Fabric bu geri çağrıyı SERVER_STARTED'dan önce
        // tetikler (gerekçe LinkCommands.register'da).
        CommandRegistrationCallback.EVENT.register((dispatcher, registry, env) ->
                LinkCommands.register(dispatcher, MojangCommands.INSTANCE, core));

        Log.info("yüklendi (" + describe() + ") — MCOS daemon'una bağlanılacak");
    }

    /** "mod sürümü, Minecraft sürümü" — gerekçe Yarn tarafındaki describe'da. */
    static String describe() {
        FabricLoader l = FabricLoader.getInstance();
        String mod = l.getModContainer("mcos-link")
                .map(c -> c.getMetadata().getVersion().getFriendlyString())
                .orElse("?");
        String mc = l.getModContainer("minecraft")
                .map(c -> c.getMetadata().getVersion().getFriendlyString())
                .orElse("?");
        return mod + ", Minecraft " + mc;
    }
}
