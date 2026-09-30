package gg.mcos.link.velocity;

import com.google.inject.Inject;
import com.velocitypowered.api.event.Subscribe;
import com.velocitypowered.api.event.player.KickedFromServerEvent;
import com.velocitypowered.api.event.player.PlayerChooseInitialServerEvent;
import com.velocitypowered.api.event.proxy.ProxyInitializeEvent;
import com.velocitypowered.api.event.proxy.ProxyPingEvent;
import com.velocitypowered.api.event.proxy.ProxyShutdownEvent;
import com.velocitypowered.api.plugin.Plugin;
import com.velocitypowered.api.proxy.ProxyServer;
import com.velocitypowered.api.proxy.server.RegisteredServer;
import com.velocitypowered.api.scheduler.ScheduledTask;
import java.util.Optional;
import java.util.concurrent.TimeUnit;
import net.kyori.adventure.text.Component;
import org.slf4j.Logger;

/**
 * MCOS Link — Velocity tarafı.
 *
 * <p><b>Neden var.</b> Kullanıcının isteği: "velocity eklentisini de yap
 * herkes düşmesin". Ortak dünyanın tek adresi kurucudaki Velocity'dir; arka
 * uç listesi eskiden velocity.toml'a yazılıyordu ve Velocity onu canlı
 * okumadığı için liste her değiştiğinde (yeni PC eşleşti, bir PC'nin IP'si
 * ya da portu değişti, yerel kopya açıldı/kapandı) MCOS proxy'yi YENİDEN
 * BAŞLATIYORDU — bağlı herkes düşüyordu. Bu eklenti listeyi MCOS'un
 * koordinatöründen okur ve çalışan proxy'ye kendisi ekler/çıkarır.
 *
 * <p>BungeeCord "Connect &lt;arka uç&gt;" iletileri (ortak dünya modunun
 * sınır geçişi) Velocity'nin kendi işidir; ad o anda aranır, yani burada
 * sonradan kaydedilen sunuculara da çalışır. Bu eklenti ona dokunmaz.
 */
@Plugin(
        id = "mcos-link",
        name = "MCOS Link",
        version = "1.0.0",
        description = "MCOS ortak dünya: arka uç listesini proxy'yi yeniden başlatmadan günceller",
        authors = {"MCOS"})
public final class McosLinkVelocity {

    /**
     * Yoklama aralığı. 3 sn: yeni bir PC en geç bu kadar sonra geçiş hedefi
     * olur; istek 127.0.0.1'e ve birkaç yüz bayttır, bedeli yok.
     */
    private static final long POLL_SECONDS = 3;

    private final ProxyServer proxy;
    private final Logger log;
    private final BackendSync sync;
    private ScheduledTask task;

    @Inject
    public McosLinkVelocity(ProxyServer proxy, Logger log) {
        this.proxy = proxy;
        this.log = log;
        this.sync = new BackendSync(proxy, log, System.getenv("MCOS_LINK_COORDINATOR"));
    }

    @Subscribe
    public void onInit(ProxyInitializeEvent e) {
        // Gecikme yok: velocity.toml yalnızca kurucunun sunucusunu taşır,
        // diğer arka uçlar ilk turda hemen eklensin.
        task = proxy.getScheduler().buildTask(this, sync::poll)
                .repeat(POLL_SECONDS, TimeUnit.SECONDS)
                .schedule();
        log.info("MCOS Link: arka uç listesi {} adresinden {} sn'de bir okunuyor",
                sync.url(), POLL_SECONDS);
    }

    @Subscribe
    public void onShutdown(ProxyShutdownEvent e) {
        if (task != null) {
            task.cancel();
        }
    }

    /** Oyuncu her zaman önce kurucunun sunucusuna ("try") girer. */
    @Subscribe
    public void onChooseInitial(PlayerChooseInitialServerEvent e) {
        tryServer().ifPresent(e::setInitialServer);
    }

    /**
     * Arka uç düşünce oyuncu proxy'den ATILMASIN, kurucunun sunucusuna
     * dönsün.
     *
     * <p>Yalnızca oyuncu o sunucuda OYNARKEN (bağlanma denemesi değilken):
     * geçiş denemesi başarısız olduysa oyuncu hâlâ eski sunucusundadır;
     * onu "try"a taşımak gereksiz bir atlama olurdu (Velocity'nin varsayılanı
     * zaten bildirip yerinde bırakmak). Listeden çıkarılmış bir sunucu
     * kapanınca da buraya düşülür.
     */
    @Subscribe
    public void onKicked(KickedFromServerEvent e) {
        if (e.kickedDuringServerConnect()) {
            return;
        }
        Optional<RegisteredServer> t = tryServer();
        if (t.isEmpty() || sameServer(t.get(), e.getServer())) {
            // "try"ın kendisi düştüyse gidecek güvenli yer yok; Velocity'nin
            // kendi kararı (failover) geçerli.
            return;
        }
        Component why = e.getServerKickReason()
                .orElse(Component.text("Bağlı olduğun bölüm kapandı; ana sunucuya döndün."));
        e.setResult(KickedFromServerEvent.RedirectPlayer.create(t.get(), why));
    }

    /**
     * Sunucu listesindeki üst sınır: velocity.toml'da arka uç sayısına göre
     * yazılsaydı her yeni PC dosyayı değiştirir ve yine yeniden başlatma
     * gerekirdi; değeri MCOS verir (/link/proxy maxPlayers).
     */
    @Subscribe
    public void onPing(ProxyPingEvent e) {
        int max = sync.maxPlayers();
        if (max > 0) {
            e.setPing(e.getPing().asBuilder().maximumPlayers(max).build());
        }
    }

    private Optional<RegisteredServer> tryServer() {
        String name = sync.tryName();
        if (name == null || name.isEmpty()) {
            return Optional.empty();
        }
        return proxy.getServer(name);
    }

    private static boolean sameServer(RegisteredServer a, RegisteredServer b) {
        return b != null && a.getServerInfo().getName().equalsIgnoreCase(b.getServerInfo().getName());
    }
}
