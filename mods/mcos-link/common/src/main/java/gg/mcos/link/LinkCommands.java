package gg.mcos.link;

import com.mojang.brigadier.CommandDispatcher;
import com.mojang.brigadier.arguments.StringArgumentType;
import com.mojang.brigadier.builder.LiteralArgumentBuilder;
import com.mojang.brigadier.builder.RequiredArgumentBuilder;

/**
 * {@code /mcoslink} komutları.
 *
 * <p><b>Neden komut gerekli?</b> Ortak dünya, görünmeyen bir altyapıdır.
 * Bir şey çalışmadığında ("neden aktarılmıyorum?") sunucu sahibinin
 * bakabileceği bir yer olmalı. Bu komutlar tam olarak o yerdir: topoloji
 * doğru mu, düğümler çevrimiçi mi, ben hangi dilimdeyim.
 *
 * <p><b>Neden sürümden bağımsız?</b> Komut ağacı doğrudan Brigadier ile
 * kurulur ({@code CommandManager.literal}/{@code Commands.literal} de
 * yalnızca {@link LiteralArgumentBuilder#literal} çağırır). Sürüme göre
 * değişen yetki denetimi ve yanıt yazma {@link CommandHost}'tadır; böylece
 * beş alt komutun hepsi 1.20.5'ten 26.x'e kadar tek kopyadır.
 */
public final class LinkCommands {

    private LinkCommands() {
    }

    /**
     * Komutları kaydeder.
     *
     * <p>Servisler DEĞER olarak değil {@link LinkCore} üzerinden, komut
     * çalıştığı anda okunur.
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
    public static <S> void register(CommandDispatcher<S> d, CommandHost<S> host,
                                    LinkCore core) {
        Commands<S> c = new Commands<>(host, core);

        d.register(LiteralArgumentBuilder.<S>literal("mcoslink")
                .then(LiteralArgumentBuilder.<S>literal("status")
                        .executes(ctx -> c.withBoth(ctx.getSource(), c::status)))
                .then(LiteralArgumentBuilder.<S>literal("map")
                        .executes(ctx -> c.with(ctx.getSource(), c::map)))
                .then(LiteralArgumentBuilder.<S>literal("reload")
                        // Yenileme yalnızca operatöre: topoloji sorgusu
                        // daemon'a yük bindirir ve herkesin tetiklemesi
                        // gereksizdir. Denetimin kendisi sürüme göre
                        // değiştiği için bağdaştırıcıdadır
                        // (CommandHost.gamemaster).
                        .requires(host.gamemaster())
                        .executes(ctx -> c.with(ctx.getSource(), c::reload)))
                .then(LiteralArgumentBuilder.<S>literal("where")
                        .then(RequiredArgumentBuilder.<S, String>argument(
                                        "oyuncu", StringArgumentType.word())
                                .executes(ctx -> c.with(ctx.getSource(),
                                        (src, co) -> c.where(src, co,
                                                StringArgumentType.getString(ctx, "oyuncu"))))))
                .then(LiteralArgumentBuilder.<S>literal("send")
                        // Elle aktarım da operatöre: bir oyuncuyu başka bir
                        // düğüme göndermek dünyayı değiştirir.
                        .requires(host.gamemaster())
                        .then(RequiredArgumentBuilder.<S, String>argument(
                                        "oyuncu", StringArgumentType.word())
                                .then(RequiredArgumentBuilder.<S, String>argument(
                                                "dugum", StringArgumentType.word())
                                        .executes(ctx -> c.withBoth(ctx.getSource(),
                                                (src, co, h) -> c.send(src, co, h,
                                                        StringArgumentType.getString(ctx, "oyuncu"),
                                                        StringArgumentType.getString(ctx, "dugum")))))))
                // Alt komut verilmezse durum göster: en sık istenen şey odur.
                .executes(ctx -> c.withBoth(ctx.getSource(), c::status)));
    }

    /**
     * Alt komutların gövdeleri.
     *
     * <p>Ayrı bir örnek sınıfı, çünkü her gövde hem {@code S} tür
     * parametresine hem de {@link CommandHost}'a ihtiyaç duyar; statik
     * metotlarda ikisini her çağrıda ayrı ayrı taşımak gerekirdi.
     */
    private static final class Commands<S> {

        /** Yalnızca koordinatör isteyen alt komutlar için. */
        interface WithCoordinator<S> {
            int run(S src, Coordinator coordinator);
        }

        /** Koordinatör ve aktarım servisi isteyen alt komutlar için. */
        interface WithBoth<S> {
            int run(S src, Coordinator coordinator, HandoffService handoff);
        }

        private final CommandHost<S> host;
        private final LinkCore core;

        Commands(CommandHost<S> host, LinkCore core) {
            this.host = host;
            this.core = core;
        }

        private void say(S src, Msg msg) {
            host.reply(src, msg, false);
        }

        int with(S src, WithCoordinator<S> body) {
            Coordinator c = core.coordinator();
            if (c == null || core.game() == null) {
                return notReady(src);
            }
            return body.run(src, c);
        }

        int withBoth(S src, WithBoth<S> body) {
            Coordinator c = core.coordinator();
            HandoffService h = core.handoff();
            if (c == null || h == null || core.game() == null) {
                return notReady(src);
            }
            return body.run(src, c, h);
        }

        /**
         * Servisler henüz kurulmamışken verilen yanıt.
         *
         * <p>Pratikte yalnızca komut, sunucu tam açılmadan çalıştırılabilirse
         * görülür (örneğin açılış betiğinden). Sessizce hiçbir şey yapmak
         * yerine SEBEBİ söylüyoruz: modun var olduğu ama henüz hazır olmadığı
         * bilgisi, "komut yok" hatasından çok daha kullanışlıdır.
         */
        private int notReady(S src) {
            say(src, Msg.of("Ortak dünya henüz başlatılmadı — sunucu açılışını bekleyin",
                    Msg.Color.GRAY));
            return 0;
        }

        int status(S src, Coordinator coordinator, HandoffService handoff) {
            Topology t = coordinator.topology();

            say(src, Msg.of("── MCOS Link ──", Msg.Color.AQUA));

            if (!t.enabled) {
                say(src, Msg.of("Ortak dünya KAPALI"
                        + (t.note.isEmpty() ? "" : " — " + t.note), Msg.Color.GRAY));
                say(src, Msg.of("MCOS panelinde: MCOS Paylaşım → Ortak dünya",
                        Msg.Color.DARK_GRAY));
                return 1;
            }

            say(src, Msg.of("Bu düğüm: ", Msg.Color.GRAY).then(t.self, Msg.Color.WHITE));

            Topology.Area mine = t.selfArea();
            if (mine != null) {
                say(src, Msg.of("Bölgem: ", Msg.Color.GRAY)
                        .then(mine.describe(), Msg.Color.WHITE));
            }
            say(src, Msg.of("Zorluk: " + t.difficulty
                    + "   ·   Aktarım: " + handoff.handoffs(), Msg.Color.DARK_GRAY));

            for (Topology.Node n : t.nodes) {
                String mark = n.self ? "► " : "  ";
                say(src, Msg.of(mark + n.name + "  ", Msg.Color.WHITE)
                        .then(n.host + ":" + n.mcPort + "  ", Msg.Color.DARK_GRAY)
                        .then(n.online ? "çevrimiçi" : "ÇEVRİMDIŞI",
                                n.online ? Msg.Color.GREEN : Msg.Color.RED));
            }
            return 1;
        }

        /**
         * Dilimleri metin haritası olarak çizer.
         *
         * <p>Sayılarla anlatmak ("mcos-1: x&lt;0, mcos-2: x&gt;=0") üç
         * düğümden sonra okunmaz olur. Basit bir çubuk, kimin nerede olduğunu
         * bir bakışta gösterir.
         */
        int map(S src, Coordinator coordinator) {
            Topology t = coordinator.topology();
            if (!t.enabled || t.areas.isEmpty()) {
                say(src, Msg.of("Ortak dünya kapalı", Msg.Color.GRAY));
                return 1;
            }

            say(src, Msg.of("── Dünya haritası (X ekseni) ──", Msg.Color.AQUA));

            for (Topology.Area a : t.areas) {
                boolean self = a.node.equals(t.self);
                say(src, Msg.of((self ? "████" : "▒▒▒▒") + " " + a.node,
                                self ? Msg.Color.GREEN : Msg.Color.GRAY)
                        .then("   " + a.describe(), Msg.Color.DARK_GRAY));
            }

            // Oyuncuya kendi konumunu da söyle: harita ancak "ben neredeyim"
            // sorusunu yanıtlayınca işe yarar.
            GamePlayer p = host.playerOf(src);
            if (p != null) {
                int cx = p.blockX() >> 4;
                say(src, Msg.of("Şu an: x=" + p.blockX() + "  →  " + t.ownerOf(cx),
                        Msg.Color.WHITE));
            }
            return 1;
        }

        int reload(S src, Coordinator coordinator) {
            Topology t = coordinator.refreshNow();
            host.reply(src, Msg.of("Topoloji yenilendi: "
                    + (t.enabled ? t.nodes.size() + " düğüm" : "kapalı"),
                    Msg.Color.AQUA), true);
            return 1;
        }

        int where(S src, Coordinator coordinator, String name) {
            Topology t = coordinator.topology();
            GamePlayer p = core.game().player(name);
            if (p == null) {
                // Oyuncu BİZDE yok demek, yok demek DEĞİLDİR: başka bir düğümde
                // olabilir. Bunu söylemek, kullanıcıyı yanlış sonuca varmaktan
                // kurtarır.
                say(src, Msg.of(name + " bu düğümde değil — başka bir cihazda olabilir",
                        Msg.Color.GRAY));
                return 0;
            }
            int cx = p.blockX() >> 4;
            String owner = t.enabled ? t.ownerOf(cx) : t.self;
            say(src, Msg.of(name + ": x=" + p.blockX() + ", z=" + p.blockZ()
                    + "  →  " + owner, Msg.Color.WHITE));
            return 1;
        }

        int send(S src, Coordinator coordinator, HandoffService handoff,
                 String playerName, String nodeName) {
            Topology t = coordinator.topology();
            if (!t.enabled) {
                say(src, Msg.of("Ortak dünya kapalı", Msg.Color.RED));
                return 0;
            }
            GamePlayer p = core.game().player(playerName);
            if (p == null) {
                say(src, Msg.of(playerName + " bu düğümde değil", Msg.Color.RED));
                return 0;
            }
            Topology.Node target = t.node(nodeName);
            if (target == null || target.self) {
                say(src, Msg.of("Geçersiz hedef düğüm: " + nodeName, Msg.Color.RED));
                return 0;
            }
            handoff.begin(t, p, target);
            host.reply(src, Msg.of(playerName + " → " + nodeName, Msg.Color.AQUA), true);
            return 1;
        }
    }
}
