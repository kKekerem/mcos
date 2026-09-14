package gg.mcos.link.paper;

import org.bukkit.Bukkit;
import org.bukkit.Location;
import org.bukkit.command.Command;
import org.bukkit.command.CommandExecutor;
import org.bukkit.command.CommandSender;
import org.bukkit.entity.Player;
import org.bukkit.plugin.Plugin;

import java.util.Locale;
import java.util.function.Supplier;

/**
 * {@code /mcoslink} komutları.
 *
 * <p><b>Neden komut gerekli?</b> Ortak dünya, görünmeyen bir altyapıdır.
 * Bir şey çalışmadığında ("neden aktarılmıyorum?") sunucu sahibinin
 * bakabileceği bir yer olmalı. Bu komutlar tam olarak o yerdir: topoloji
 * doğru mu, düğümler çevrimiçi mi, ben hangi dilimdeyim.
 *
 * <h2>PAPER PORTU: BRIGADIER GİTTİ</h2>
 *
 * <p>Fabric, komutları {@code CommandDispatcher} üzerinden kaydediyordu.
 * Paper'da komut {@code plugin.yml} içinde TANIMLIDIR ve
 * {@code getCommand("mcoslink").setExecutor(...)} ile bu sınıfa bağlanır;
 * alt komut ayrıştırması {@code args} dizisi üzerinden elle yapılır.
 *
 * <p><b>Yetki eşlemesi.</b> Fabric yalnızca {@code reload} ve {@code send}
 * için {@code hasPermissionLevel(2)} istiyordu; {@code status}, {@code map}
 * ve {@code where} HERKESE açıktı. Bu yüzden {@code plugin.yml}'deki komut
 * bloğundan {@code permission:} satırı KALDIRILDI: komut düzeyindeki bir
 * yetki, yürütücü daha çalışmadan sıradan oyuncuyu geri çevirir ve üç açık
 * alt komut da kaybolurdu. Yetki denetimi aşağıda, yalnızca o iki alt
 * komutta yapılıyor.
 *
 * <p><b>Servisler neden {@link Supplier}?</b> Fabric'te bu bir zorunluluktu
 * (kayıt, servisler kurulmadan ÖNCE tetikleniyordu). Paper'da bu tehlike
 * yok — {@code onEnable} önce servisleri kurar, sonra yürütücüyü bağlar —
 * ama savunmacı null denetimini ve GRİ mesajını KORUYORUZ: komut,
 * eklentinin kapanışı sırasında ya da kısmen başarısız bir açılıştan sonra
 * da çalıştırılabilir. "Komut yok" hatası yerine SEBEBİ söylemek her zaman
 * daha kullanışlıdır.
 */
public final class LinkCommands implements CommandExecutor {

    private static final String USAGE = "/mcoslink <status|map|reload|where|send>";

    /**
     * Ana iş parçacığına dönmek için gerekli.
     *
     * <p>Yalnızca {@link #reload} kullanır: topoloji yenilemesi arka planda
     * yapılır ve sonucu oyuncuya yazmak için ana iş parçacığına dönmek
     * zorunludur.
     */
    private final Plugin plugin;
    private final Supplier<Coordinator> coordinator;
    private final Supplier<HandoffService> handoff;

    public LinkCommands(Plugin plugin, Supplier<Coordinator> coordinator,
                        Supplier<HandoffService> handoff) {
        this.plugin = plugin;
        this.coordinator = coordinator;
        this.handoff = handoff;
    }

    /**
     * ANA İŞ PARÇACIĞI: Bukkit, oyuncu ve konsol komutlarını oyun iş
     * parçacığında dağıtır. {@code handoff.begin()} ve her
     * {@code sendMessage} bu yüzden doğrudan çağrılabilir.
     *
     * <p>Her zaman {@code true} döner: kullanım satırını Bukkit'in
     * bastırmasındansa kendimiz yazıyoruz, böylece mesaj da Türkçe ve
     * renkli kalıyor.
     */
    @Override
    public boolean onCommand(CommandSender sender, Command command,
                             String label, String[] args) {
        // Alt komut verilmezse durum göster: en sık istenen şey odur.
        String sub = args.length == 0
                ? "status" : args[0].toLowerCase(Locale.ROOT);

        switch (sub) {
            case "status" -> status(sender);
            case "map" -> map(sender);
            case "reload" -> {
                // Yenileme yalnızca operatöre: topoloji sorgusu daemon'a yük
                // bindirir ve herkesin tetiklemesi gereksizdir.
                if (admin(sender)) {
                    reload(sender);
                }
            }
            case "where" -> {
                if (args.length < 2) {
                    usage(sender, "/mcoslink where <oyuncu>");
                } else {
                    where(sender, args[1]);
                }
            }
            case "send" -> {
                // Elle aktarım da yalnızca operatöre: bir oyuncuyu başka bir
                // makineye göndermek, onun kayıt dosyasını taşımak demektir.
                if (admin(sender)) {
                    if (args.length < 3) {
                        usage(sender, "/mcoslink send <oyuncu> <dugum>");
                    } else {
                        send(sender, args[1], args[2]);
                    }
                }
            }
            default -> usage(sender, USAGE);
        }
        return true;
    }

    /** Yetkili mi? Değilse sebebini söyler. */
    private static boolean admin(CommandSender sender) {
        if (sender.hasPermission("mcoslink.admin")) {
            return true;
        }
        sender.sendMessage(Fmt.RED + "Bu komut için yetkiniz yok");
        return false;
    }

    private static void usage(CommandSender sender, String line) {
        sender.sendMessage(Fmt.GRAY + "Kullanım: " + line);
    }

    /**
     * Servisler henüz kurulmamışken verilen yanıt.
     *
     * <p>Sessizce hiçbir şey yapmak yerine SEBEBİ söylüyoruz: eklentinin var
     * olduğu ama henüz hazır olmadığı bilgisi, "komut yok" hatasından çok
     * daha kullanışlıdır.
     */
    private static void notReady(CommandSender sender) {
        sender.sendMessage(Fmt.GRAY
                + "Ortak dünya henüz başlatılmadı — sunucu açılışını bekleyin");
    }

    private void status(CommandSender sender) {
        Coordinator c = coordinator.get();
        HandoffService h = handoff.get();
        if (c == null || h == null) {
            notReady(sender);
            return;
        }
        Topology t = c.topology();

        sender.sendMessage(Fmt.AQUA + "── MCOS Link ──");

        if (!t.enabled) {
            sender.sendMessage(Fmt.GRAY + "Ortak dünya KAPALI"
                    + (t.note.isEmpty() ? "" : " — " + t.note));
            sender.sendMessage(Fmt.DARK_GRAY
                    + "MCOS panelinde: MCOS Paylaşım → Ortak dünya");
            return;
        }

        sender.sendMessage(Fmt.GRAY + "Bu düğüm: " + Fmt.WHITE + t.self);

        Topology.Area mine = t.selfArea();
        if (mine != null) {
            sender.sendMessage(Fmt.GRAY + "Bölgem: " + Fmt.WHITE + mine.describe());
        }
        sender.sendMessage(Fmt.DARK_GRAY + "Zorluk: " + t.difficulty
                + "   ·   Aktarım: " + h.handoffs());

        for (Topology.Node n : t.nodes) {
            String mark = n.self ? "► " : "  ";
            String col = n.online ? Fmt.GREEN : Fmt.RED;
            String state = n.online ? "çevrimiçi" : "ÇEVRİMDIŞI";
            sender.sendMessage(Fmt.WHITE + mark + n.name + "  "
                    + Fmt.DARK_GRAY + n.host + ":" + n.mcPort + "  "
                    + col + state);
        }
    }

    /**
     * Dilimleri metin haritası olarak çizer.
     *
     * <p>Sayılarla anlatmak ("mcos-1: x&lt;0, mcos-2: x&gt;=0") üç düğümden
     * sonra okunmaz olur. Basit bir çubuk, kimin nerede olduğunu bir bakışta
     * gösterir.
     */
    private void map(CommandSender sender) {
        Coordinator c = coordinator.get();
        if (c == null) {
            notReady(sender);
            return;
        }
        Topology t = c.topology();
        if (!t.enabled || t.areas.isEmpty()) {
            sender.sendMessage(Fmt.GRAY + "Ortak dünya kapalı");
            return;
        }

        sender.sendMessage(Fmt.AQUA + "── Dünya haritası (X ekseni) ──");

        for (Topology.Area a : t.areas) {
            boolean self = a.node.equals(t.self);
            String col = self ? Fmt.GREEN : Fmt.GRAY;
            String bar = self ? "████" : "▒▒▒▒";
            sender.sendMessage(col + bar + " " + a.node
                    + Fmt.DARK_GRAY + "   " + a.describe());
        }

        // Oyuncuya kendi konumunu da söyle: harita ancak "ben neredeyim"
        // sorusunu yanıtlayınca işe yarar.
        if (sender instanceof Player p) {
            Location loc = p.getLocation();
            int cx = loc.getBlockX() >> 4;
            sender.sendMessage(Fmt.WHITE + "Şu an: x=" + loc.getBlockX()
                    + "  →  " + t.ownerOf(cx));
        }
    }

    /**
     * Topolojiyi elle yeniler.
     *
     * <p><b>Yakalanan gerçek hata: bu komut sunucuyu donduruyordu.</b> Eski
     * hâli {@code Coordinator.refreshNow()} çağırıyordu ve o metot HTTP
     * GET'i ÇAĞIRANIN iş parçacığında yapıyordu. Bukkit komutları ANA İŞ
     * PARÇACIĞINDA dağıtır; yani {@code /mcoslink reload}, oyun döngüsünün
     * üzerinde 2 sn bağlanma + 2 sn okuma = en kötü durumda ~4 saniyelik
     * engelleyici bir istekti — tam da bir operatörün bu komutu yazacağı
     * durumda, yani daemon yanıt vermezken. Ad çözümlemesi hiç
     * sınırlanmadığı için ({@code setConnectTimeout} DNS'i kapsamaz)
     * {@code MCOS_LINK_COORDINATOR} bir ad taşıyorsa donma SINIRSIZDI ve
     * Paper'ın gözcüsü sunucuyu öldürürdü.
     *
     * <p>Yeni akış üç adım: (1) ana iş parçacığında "yenileniyor" de ve
     * HEMEN dön, (2) ağ işi koordinatör iş parçacığında yapılsın, (3) sonuç
     * için ana iş parçacığına geri sıçra. Komut artık hiçbir noktada
     * engellemiyor.
     */
    private void reload(CommandSender sender) {
        Coordinator c = coordinator.get();
        if (c == null) {
            notReady(sender);
            return;
        }
        // ANA İŞ PARÇACIĞI. Anında geri bildirim: yanıt bir tik sonra
        // geleceği için komutun "yutulduğu" izlenimi doğmamalı.
        sender.sendMessage(Fmt.GRAY + "Topoloji yenileniyor…");
        // Fabric'te bu geri bildirim broadcastToOps=true ile veriliyordu;
        // Bukkit'te doğrudan karşılığı yok. Denetim izini korumak için
        // günlüğe yazıyoruz: kimin ne zaman yenilediği kaybolmasın. İSTEĞİ
        // yazıyoruz, sonucu değil — istek kesindir, sonuç değil. Aynı
        // gerekçeyle "send" de isteği yazar.
        Log.info(sender.getName() + " topoloji yenilemesi istedi");

        String who = sender.getName();
        boolean isPlayer = sender instanceof Player;

        c.refreshAsync(t -> {
            // ── KOORDİNATÖR İŞ PARÇACIĞI: Bukkit'e DOKUNULAMAZ ────────────
            try {
                Bukkit.getScheduler().runTask(plugin, () -> {
                    // ANA İŞ PARÇACIĞI.
                    //
                    // Oyuncu bu arada çıkmış olabilir; elimizdeki
                    // CommandSender o zaman bayat olur. Çevrimiçi listesinden
                    // YENİDEN buluyoruz — konsol/komut bloğu gibi oyuncu
                    // olmayan göndericiler her zaman geçerlidir ve olduğu
                    // gibi kullanılır.
                    CommandSender out = sender;
                    if (isPlayer) {
                        out = byName(who);
                        if (out == null) {
                            return;
                        }
                    }
                    out.sendMessage(Fmt.AQUA + "Topoloji yenilendi: "
                            + (t.enabled ? t.nodes.size() + " düğüm" : "kapalı"));
                });
            } catch (Exception e) {
                // Eklenti kapanıyorsa runTask fırlatır; sonucu yazacak bir
                // yer de kalmamıştır.
                Log.debug("yenileme sonucu ana iş parçacığına verilemedi: " + e);
            }
        });
    }

    private void where(CommandSender sender, String name) {
        Coordinator c = coordinator.get();
        if (c == null) {
            notReady(sender);
            return;
        }
        Topology t = c.topology();
        Player p = byName(name);
        if (p == null) {
            // Oyuncu BİZDE yok demek, yok demek DEĞİLDİR: başka bir düğümde
            // olabilir. Bunu söylemek, kullanıcıyı yanlış sonuca varmaktan
            // kurtarır.
            sender.sendMessage(Fmt.GRAY + name
                    + " bu düğümde değil — başka bir cihazda olabilir");
            return;
        }
        Location loc = p.getLocation();
        int cx = loc.getBlockX() >> 4;
        String owner = t.enabled ? t.ownerOf(cx) : t.self;
        sender.sendMessage(Fmt.WHITE + name + ": x=" + loc.getBlockX()
                + ", z=" + loc.getBlockZ() + "  →  " + owner);
    }

    /**
     * Elle aktarım.
     *
     * <p>{@code check()}'in aksine hedefin ÇEVRİMİÇİ olup olmadığına
     * BAKMAZ: yönetici bilerek deniyorsa, denemenin nasıl başarısız olduğunu
     * görmesi teşhis için değerlidir. Sonuç, kırmızı "…geçilemedi" eylem
     * çubuğudur.
     */
    private void send(CommandSender sender, String playerName, String nodeName) {
        Coordinator c = coordinator.get();
        HandoffService h = handoff.get();
        if (c == null || h == null) {
            notReady(sender);
            return;
        }
        Topology t = c.topology();
        if (!t.enabled) {
            sender.sendMessage(Fmt.RED + "Ortak dünya kapalı");
            return;
        }
        Player p = byName(playerName);
        if (p == null) {
            sender.sendMessage(Fmt.RED + playerName + " bu düğümde değil");
            return;
        }
        Topology.Node target = t.node(nodeName);
        if (target == null || target.self) {
            sender.sendMessage(Fmt.RED + "Geçersiz hedef düğüm: " + nodeName);
            return;
        }
        // ANA İŞ PARÇACIĞI: begin() ilk yarısını burada yapar (saveData,
        // eylem çubuğu) ve ağ işini kendi havuzuna devreder.
        h.begin(t, p, target);
        sender.sendMessage(Fmt.AQUA + playerName + " → " + nodeName);
        Log.info(sender.getName() + " elle aktarım istedi: "
                + playerName + " -> " + nodeName);
    }

    /**
     * Çevrimiçi oyuncuyu TAM adıyla bulur; yoksa null.
     *
     * <p>Çevrimiçi listesini geziyoruz — imzası doğrulanmış API listesinde
     * bulunan tek erişim yolu budur.
     */
    private static Player byName(String name) {
        for (Player p : Bukkit.getOnlinePlayers()) {
            if (p.getName().equals(name)) {
                return p;
            }
        }
        return null;
    }
}
