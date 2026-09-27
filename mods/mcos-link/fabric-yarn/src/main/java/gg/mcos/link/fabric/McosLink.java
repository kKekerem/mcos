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
 * MCOS Link — giriş noktası (Yarn eşlemeli sürümler: 1.20.5–1.21.11).
 *
 * <p>Birden çok MCOS cihazının AYNI dünyayı çalıştırmasını sağlar: dünya X
 * ekseninde dilimlere bölünür, her cihaz kendi dilimini simüle eder ve
 * oyuncu sınırı geçtiğinde envanteriyle birlikte kesintisiz aktarılır.
 *
 * <p><b>Yalnızca sunucu tarafı.</b> İstemcide hiçbir mod gerekmez; aktarım,
 * Minecraft'ın 1.20.5 ile eklediği kendi transfer paketiyle yapılır.
 *
 * <p>Bu sınıf yalnızca Fabric olaylarını {@link LinkCore}'a iletir. Karar
 * veren her satır {@code common/} altındadır ve bütün sürümlerde aynıdır;
 * burada kalan tek şey, olay parametrelerinin (Yarn adlı Minecraft
 * türleri) sürümden bağımsız değerlere çevrilmesidir.
 */
public final class McosLink implements DedicatedServerModInitializer {

    private final LinkCore core = new LinkCore();

    @Override
    public void onInitializeServer() {
        ServerLifecycleEvents.SERVER_STARTED.register(server ->
                core.started(new YarnGame(server)));
        ServerLifecycleEvents.SERVER_STOPPING.register(server -> core.stopping());
        ServerTickEvents.END_SERVER_TICK.register(server -> core.tick());

        ServerMessageEvents.CHAT_MESSAGE.register((message, sender, params) ->
                core.chat(YarnPlayer.nameOf(sender), message.getContent().getString()));

        ServerPlayConnectionEvents.JOIN.register((handler, sender, server) ->
                core.joined(handler.getPlayer().getUuid(),
                        YarnPlayer.nameOf(handler.getPlayer())));

        // Kayıt KOŞULSUZ: Fabric bu geri çağrıyı SERVER_STARTED'dan önce
        // tetikler (gerekçe LinkCommands.register'da).
        CommandRegistrationCallback.EVENT.register((dispatcher, registry, env) ->
                LinkCommands.register(dispatcher, YarnCommands.INSTANCE, core));

        Log.info("yüklendi (" + describe() + ") — MCOS daemon'una bağlanılacak");
    }

    /**
     * "mod sürümü, Minecraft sürümü" — günlükteki ilk satır için.
     *
     * <p>Aynı mod artık birden çok jar olarak geliyor (her Minecraft sürüm
     * grubu için bir tane). Sorun bildiren bir kullanıcının günlüğünde HANGİ
     * jar'ın HANGİ oyun sürümüne yüklendiği ilk satırda yazmalı; aksi halde
     * "yanlış jar mı kuruldu" sorusu ancak dosya adlarına bakılarak
     * yanıtlanabilir.
     */
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
