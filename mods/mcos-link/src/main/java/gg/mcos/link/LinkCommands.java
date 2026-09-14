package gg.mcos.link;

import com.mojang.brigadier.CommandDispatcher;
import com.mojang.brigadier.arguments.StringArgumentType;
import net.minecraft.server.command.CommandManager;
import net.minecraft.server.command.ServerCommandSource;
import net.minecraft.server.network.ServerPlayerEntity;
import net.minecraft.text.Text;
import net.minecraft.util.Formatting;

import java.util.function.Supplier;

/**
 * {@code /mcoslink} komutları.
 *
 * <p><b>Neden komut gerekli?</b> Ortak dünya, görünmeyen bir altyapıdır.
 * Bir şey çalışmadığında ("neden aktarılmıyorum?") sunucu sahibinin
 * bakabileceği bir yer olmalı. Bu komutlar tam olarak o yerdir: topoloji
 * doğru mu, düğümler çevrimiçi mi, ben hangi dilimdeyim.
 */
public final class LinkCommands {

    private LinkCommands() {
    }

    /**
     * Komutları kaydeder.
     *
     * <p>Servisler DEĞER olarak değil {@link Supplier} olarak alınır.
     *
     * <p><b>Yakalanan gerçek hata:</b> Fabric,
     * {@code CommandRegistrationCallback}'i {@code CommandManager}
     * kurucusundan tetikler; bu, {@code MinecraftServer.loadWorld()}
     * sırasında, yani {@code SERVER_STARTED}'dan ÖNCE olur. Servisler
     * {@code SERVER_STARTED}'da kurulduğu için çağrı anında ikisi de
     * null'dı ve çağıran taraftaki "null ise kaydetme" koşulu yüzünden
     * {@code /mcoslink} NORMAL HİÇBİR AÇILIŞTA kaydedilmiyordu — beş
     * alt komut da "Unknown or incomplete command" veriyor, ancak elle
     * {@code /reload} çalıştırılırsa ortaya çıkıyordu. Kayıt artık koşulsuz;
     * servisler komut ÇALIŞTIĞI anda okunuyor, o anda ikisi de hazır.
     */
    public static void register(CommandDispatcher<ServerCommandSource> d,
                                Supplier<Coordinator> coordinator,
                                Supplier<HandoffService> handoff) {

        d.register(CommandManager.literal("mcoslink")
                .then(CommandManager.literal("status")
                        .executes(ctx -> withBoth(ctx.getSource(), coordinator, handoff,
                                LinkCommands::status)))
                .then(CommandManager.literal("map")
                        .executes(ctx -> with(ctx.getSource(), coordinator,
                                LinkCommands::map)))
                .then(CommandManager.literal("reload")
                        // Yenileme yalnızca operatöre: topoloji sorgusu
                        // daemon'a yük bindirir ve herkesin tetiklemesi
                        // gereksizdir.
                        //
                        // ── API değişikliği (1.21.11) ───────────────────
                        // Burada `s -> s.hasPermissionLevel(2)` vardı ve
                        // derleme "cannot find symbol: hasPermissionLevel"
                        // ile kırılıyordu. Minecraft sayısal yetki
                        // seviyesini bıraktı; ServerCommandSource artık
                        // PermissionSource uyguluyor ve denetim
                        // PermissionCheck sabitleriyle yapılıyor.
                        // (Kanıt: minecraft-merged-1.21.11 jar'ında javap
                        // ile ServerCommandSource'ta hasPermissionLevel YOK,
                        // yerine getPermissions(): PermissionPredicate var.)
                        //
                        // Eski seviye 2'nin karşılığı GAMEMASTERS_CHECK'tir
                        // (vanilla: 1=MODERATORS, 2=GAMEMASTERS,
                        // 3=ADMINS, 4=OWNERS). requirePermissionLevel bir
                        // PermissionSourcePredicate döndürür ve o da
                        // Predicate<T> olduğu için .requires'a doğrudan
                        // verilebilir.
                        .requires(CommandManager.requirePermissionLevel(
                                CommandManager.GAMEMASTERS_CHECK))
                        .executes(ctx -> with(ctx.getSource(), coordinator,
                                LinkCommands::reload)))
                .then(CommandManager.literal("where")
                        .then(CommandManager.argument("oyuncu", StringArgumentType.word())
                                .executes(ctx -> with(ctx.getSource(), coordinator,
                                        (src, c) -> where(src, c,
                                                StringArgumentType.getString(ctx, "oyuncu"))))))
                .then(CommandManager.literal("send")
                        // Elle aktarım da operatöre: bir oyuncuyu başka bir
                        // düğüme göndermek dünyayı değiştirir. Yetki
                        // denetiminin gerekçesi için yukarıdaki "reload"
                        // açıklamasına bakın.
                        .requires(CommandManager.requirePermissionLevel(
                                CommandManager.GAMEMASTERS_CHECK))
                        .then(CommandManager.argument("oyuncu", StringArgumentType.word())
                                .then(CommandManager.argument("dugum", StringArgumentType.word())
                                        .executes(ctx -> withBoth(ctx.getSource(),
                                                coordinator, handoff,
                                                (src, c, h) -> send(src, c, h,
                                                        StringArgumentType.getString(ctx, "oyuncu"),
                                                        StringArgumentType.getString(ctx, "dugum")))))))
                // Alt komut verilmezse durum göster: en sık istenen şey odur.
                .executes(ctx -> withBoth(ctx.getSource(), coordinator, handoff,
                        LinkCommands::status)));
    }

    /** Yalnızca koordinatör isteyen alt komutlar için. */
    private interface WithCoordinator {
        int run(ServerCommandSource src, Coordinator coordinator);
    }

    /** Koordinatör ve aktarım servisi isteyen alt komutlar için. */
    private interface WithBoth {
        int run(ServerCommandSource src, Coordinator coordinator,
                HandoffService handoff);
    }

    private static int with(ServerCommandSource src,
                            Supplier<Coordinator> coordinator,
                            WithCoordinator body) {
        Coordinator c = coordinator.get();
        if (c == null) {
            return notReady(src);
        }
        return body.run(src, c);
    }

    private static int withBoth(ServerCommandSource src,
                                Supplier<Coordinator> coordinator,
                                Supplier<HandoffService> handoff,
                                WithBoth body) {
        Coordinator c = coordinator.get();
        HandoffService h = handoff.get();
        if (c == null || h == null) {
            return notReady(src);
        }
        return body.run(src, c, h);
    }

    /**
     * Servisler henüz kurulmamışken verilen yanıt.
     *
     * <p>Pratikte yalnızca komut, sunucu tam açılmadan çalıştırılabilirse
     * görülür (örneğin açılış betiğinden). Sessizce hiçbir şey yapmak yerine
     * SEBEBİ söylüyoruz: modun var olduğu ama henüz hazır olmadığı bilgisi,
     * "komut yok" hatasından çok daha kullanışlıdır.
     */
    private static int notReady(ServerCommandSource src) {
        src.sendFeedback(() -> Text.literal(
                        "Ortak dünya henüz başlatılmadı — sunucu açılışını bekleyin")
                .formatted(Formatting.GRAY), false);
        return 0;
    }

    private static int status(ServerCommandSource src, Coordinator coordinator,
                              HandoffService handoff) {
        Topology t = coordinator.topology();

        src.sendFeedback(() -> Text.literal("── MCOS Link ──")
                .formatted(Formatting.AQUA), false);

        if (!t.enabled) {
            src.sendFeedback(() -> Text.literal("Ortak dünya KAPALI"
                            + (t.note.isEmpty() ? "" : " — " + t.note))
                    .formatted(Formatting.GRAY), false);
            src.sendFeedback(() -> Text.literal(
                            "MCOS panelinde: MCOS Paylaşım → Ortak dünya")
                    .formatted(Formatting.DARK_GRAY), false);
            return 1;
        }

        src.sendFeedback(() -> Text.literal("Bu düğüm: ")
                .formatted(Formatting.GRAY)
                .append(Text.literal(t.self).formatted(Formatting.WHITE)), false);

        Topology.Area mine = t.selfArea();
        if (mine != null) {
            src.sendFeedback(() -> Text.literal("Bölgem: ")
                    .formatted(Formatting.GRAY)
                    .append(Text.literal(mine.describe()).formatted(Formatting.WHITE)),
                    false);
        }
        src.sendFeedback(() -> Text.literal("Zorluk: " + t.difficulty
                + "   ·   Aktarım: " + handoff.handoffs())
                .formatted(Formatting.DARK_GRAY), false);

        for (Topology.Node n : t.nodes) {
            String mark = n.self ? "► " : "  ";
            Formatting col = n.online ? Formatting.GREEN : Formatting.RED;
            String state = n.online ? "çevrimiçi" : "ÇEVRİMDIŞI";
            src.sendFeedback(() -> Text.literal(mark + n.name + "  ")
                    .formatted(Formatting.WHITE)
                    .append(Text.literal(n.host + ":" + n.mcPort + "  ")
                            .formatted(Formatting.DARK_GRAY))
                    .append(Text.literal(state).formatted(col)), false);
        }
        return 1;
    }

    /**
     * Dilimleri metin haritası olarak çizer.
     *
     * <p>Sayılarla anlatmak ("mcos-1: x&lt;0, mcos-2: x&gt;=0") üç düğümden
     * sonra okunmaz olur. Basit bir çubuk, kimin nerede olduğunu bir bakışta
     * gösterir.
     */
    private static int map(ServerCommandSource src, Coordinator coordinator) {
        Topology t = coordinator.topology();
        if (!t.enabled || t.areas.isEmpty()) {
            src.sendFeedback(() -> Text.literal("Ortak dünya kapalı")
                    .formatted(Formatting.GRAY), false);
            return 1;
        }

        src.sendFeedback(() -> Text.literal("── Dünya haritası (X ekseni) ──")
                .formatted(Formatting.AQUA), false);

        for (Topology.Area a : t.areas) {
            boolean self = a.node.equals(t.self);
            Formatting col = self ? Formatting.GREEN : Formatting.GRAY;
            String bar = self ? "████" : "▒▒▒▒";
            src.sendFeedback(() -> Text.literal(bar + " " + a.node)
                    .formatted(col)
                    .append(Text.literal("   " + a.describe())
                            .formatted(Formatting.DARK_GRAY)), false);
        }

        // Oyuncuya kendi konumunu da söyle: harita ancak "ben neredeyim"
        // sorusunu yanıtlayınca işe yarar.
        if (src.getEntity() instanceof ServerPlayerEntity p) {
            int cx = p.getBlockX() >> 4;
            String owner = t.ownerOf(cx);
            src.sendFeedback(() -> Text.literal("Şu an: x=" + p.getBlockX()
                            + "  →  " + owner)
                    .formatted(Formatting.WHITE), false);
        }
        return 1;
    }

    private static int reload(ServerCommandSource src, Coordinator coordinator) {
        Topology t = coordinator.refreshNow();
        src.sendFeedback(() -> Text.literal("Topoloji yenilendi: "
                        + (t.enabled ? t.nodes.size() + " düğüm" : "kapalı"))
                .formatted(Formatting.AQUA), true);
        return 1;
    }

    private static int where(ServerCommandSource src, Coordinator coordinator,
                             String name) {
        Topology t = coordinator.topology();
        ServerPlayerEntity p = src.getServer().getPlayerManager().getPlayer(name);
        if (p == null) {
            // Oyuncu BİZDE yok demek, yok demek DEĞİLDİR: başka bir düğümde
            // olabilir. Bunu söylemek, kullanıcıyı yanlış sonuca varmaktan
            // kurtarır.
            src.sendFeedback(() -> Text.literal(name
                            + " bu düğümde değil — başka bir cihazda olabilir")
                    .formatted(Formatting.GRAY), false);
            return 0;
        }
        int cx = p.getBlockX() >> 4;
        String owner = t.enabled ? t.ownerOf(cx) : t.self;
        src.sendFeedback(() -> Text.literal(name + ": x=" + p.getBlockX()
                        + ", z=" + p.getBlockZ() + "  →  " + owner)
                .formatted(Formatting.WHITE), false);
        return 1;
    }

    private static int send(ServerCommandSource src, Coordinator coordinator,
                            HandoffService handoff, String playerName,
                            String nodeName) {
        Topology t = coordinator.topology();
        if (!t.enabled) {
            src.sendFeedback(() -> Text.literal("Ortak dünya kapalı")
                    .formatted(Formatting.RED), false);
            return 0;
        }
        ServerPlayerEntity p = src.getServer().getPlayerManager().getPlayer(playerName);
        if (p == null) {
            src.sendFeedback(() -> Text.literal(playerName + " bu düğümde değil")
                    .formatted(Formatting.RED), false);
            return 0;
        }
        Topology.Node target = t.node(nodeName);
        if (target == null || target.self) {
            src.sendFeedback(() -> Text.literal("Geçersiz hedef düğüm: " + nodeName)
                    .formatted(Formatting.RED), false);
            return 0;
        }
        handoff.begin(t, p, target);
        src.sendFeedback(() -> Text.literal(playerName + " → " + nodeName)
                .formatted(Formatting.AQUA), true);
        return 1;
    }
}
