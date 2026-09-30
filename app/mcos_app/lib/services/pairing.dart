import '../models/connection.dart';
import 'errors.dart';
import 'pair_uri.dart';
import 'rpc_client.dart';

/// Bir MCOS'a bağlanma ve jetonu doğrulama akışı.
///
/// ════════════════════════════════════════════════════════════════════════════
/// NEDEN EKRANDAN AYRI
/// ════════════════════════════════════════════════════════════════════════════
///
/// Bu akış eskiden ConnectScreen'in içindeydi ve yalnızca telefonda, elle
/// denenebiliyordu. Burada Flutter'a bağımlı hiçbir şey yok: aynı kod
/// test/e2e_mcosd_test.dart içinde GERÇEK bir mcosd'ye karşı çalıştırılıyor.
///
/// ── Sıra hayati ──────────────────────────────────────────────────────────
///  1. /health (JETONSUZ): adres doğru mu, orada MCOS var mı. QR birden çok
///     adres taşıyorsa ilk yanıt veren seçilir.
///  2. Sertifika parmak izi: QR'dan beklenen varsa KARŞILAŞTIRILIR. Tutmazsa
///     jeton HİÇ gönderilmez — jeton makinenin tam denetimini veriyor.
///  3. ping (jetonla): jeton doğru mu.
///  4. config.get: panelin tema rengi (uygulama aynı vurguyu kullanır).
class Pairing {
  Pairing._();

  /// Bağlanır, doğrular ve kaydedilmeye hazır bağlantıyı döndürür.
  ///
  /// [expectedFingerprint]: QR'dan gelen parmak izi; verilirse ilk
  /// bağlantıda bile sertifika doğrulanır.
  /// [pinnedFingerprint]: düzenlenen kayıtlı bağlantının sabitlenmiş parmak
  /// izi; değiştiyse kullanıcı uyarılır.
  ///
  /// Hata durumunda mesajı doğrudan gösterilebilir bir [RpcException] atar.
  static Future<Connection> connect({
    required String id,
    required String label,
    required List<String> hosts,
    required int port,
    required String token,
    String? expectedFingerprint,
    String? pinnedFingerprint,
    int sshPort = 22,
    String sshUser = 'root',
    void Function(String message)? onProgress,
  }) async {
    final tok = cleanToken(token);
    if (hosts.isEmpty) throw RpcException('Adres gerekli.');
    if (tok.isEmpty) throw RpcException('Jeton gerekli.');

    // ── 1. Adres ─────────────────────────────────────────────────────────
    // Birden çok aday varsa her birine kısa süre tanınıyor: QR'daki NAT
    // adresi (10.0.2.15) gibi ulaşılamayan bir aday, kullanıcıyı uzun süre
    // bekletmemeli.
    final perHost = hosts.length > 1
        ? const Duration(seconds: 4)
        : const Duration(seconds: 8);
    HealthInfo? health;
    String? seen;
    String? okHost;
    final failures = <String>[];
    for (final h in hosts) {
      onProgress?.call('$h:$port deneniyor…');
      final probe = RpcClient(
        Connection(id: id, label: label, host: h, port: port, token: ''),
        connectTimeout: perHost,
      );
      try {
        health = await probe.health();
        seen = probe.lastSeenFingerprint ?? health.fingerprint;
        okHost = h;
        break;
      } catch (e) {
        failures.add(friendlyError(e, host: h, port: port));
      } finally {
        probe.close();
      }
    }
    if (health == null || okHost == null) {
      if (failures.length == 1) throw RpcException(failures.first);
      throw RpcException(
        'Adreslerin hiçbirine ulaşılamadı (${hosts.join(', ')}).\n\n'
        '${failures.last}',
      );
    }

    // ── 2. Kimlik ────────────────────────────────────────────────────────
    final exp = normalizeFingerprint(expectedFingerprint);
    if (exp.isNotEmpty && !sameFingerprint(seen, exp)) {
      throw RpcException(
        'Bu adresteki sunucu, QR kodundaki MCOS DEĞİL — sertifika parmak izi '
        'tutmuyor. Güvenlik için jeton gönderilmedi.\n\n'
        'Paneldeki QR\'ı yeniden okutun. Sürerse ağınızda araya giren biri '
        'olabilir.',
      );
    }
    // QR parmak izi TUTTUYSA eski sabitleme yok sayılıyor: QR bant dışı
    // (ekran → kamera) geldiği için eski kayıttan daha güçlü bir güven
    // çapası. Aksi hâlde MCOS yeniden kurulduktan sonra "Düzenle → QR kodu
    // tara" diyen kullanıcı, tam da istenen şeyi yaptığı hâlde "kimlik
    // değişti, QR ile yeniden ekleyin" hatasına takılıyordu.
    final pin = normalizeFingerprint(pinnedFingerprint);
    if (exp.isEmpty && pin.isNotEmpty && !sameFingerprint(seen, pin)) {
      throw RpcException(
        'Sunucunun kimliği DEĞİŞTİ.\n\nBeklenen:\n$pinnedFingerprint\n\n'
        'Gelen:\n$seen\n\n'
        'MCOS yeniden kurulduysa bu normaldir; bağlantıyı silip QR ile '
        'yeniden ekleyin. Kurulmadıysa ağınızda araya giren biri olabilir.',
      );
    }

    final name = health.name.isEmpty ? 'MCOS' : health.name;
    onProgress?.call('$name bulundu (MCOS ${health.version}) — jeton '
        'doğrulanıyor…');

    // ── 3-4. Jeton ve tema ───────────────────────────────────────────────
    final conn = Connection(
      id: id,
      label: label.trim().isEmpty ? name : label.trim(),
      host: okHost,
      port: port,
      token: tok,
      fingerprint: seen,
      sshPort: sshPort,
      sshUser: sshUser,
    );
    final verify = RpcClient(conn);
    String? theme;
    try {
      await verify.call('ping', null, const Duration(seconds: 15));
      try {
        final cfg = await verify.callMap(
            'config.get', null, const Duration(seconds: 15),);
        theme = themeFromConfig(cfg);
      } catch (_) {
        // Tema bir süs: alınamazsa bağlantı yine kurulmuş sayılır.
      }
    } catch (e) {
      throw RpcException(friendlyError(e, host: okHost, port: port));
    } finally {
      verify.close();
    }
    return conn.copyWith(theme: theme);
  }
}

/// config.get yanıtından panelin tema adını çıkarır.
///
/// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
/// Daemon ayarları bir zarfın İÇİNDE döndürüyor: {"config": {"theme": …}}
/// (gerçek mcosd'den ölçüldü). Eski kod `cfg['theme']` okuyordu; alan hep
/// null geliyor ve telefon panelin vurgu rengini hiç almıyordu.
String? themeFromConfig(Map<String, dynamic> cfg) {
  final inner = cfg['config'];
  final src = inner is Map<String, dynamic> ? inner : cfg;
  final t = src['theme'];
  return t is String && t.isNotEmpty ? t : null;
}
