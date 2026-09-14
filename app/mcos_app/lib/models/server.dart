/// Bir Minecraft sunucusunun telefonda gösterilen hâli.
///
/// ── Neden daemon'un tüm alanları yok ────────────────────────────────────────
/// Sunucu nesnesi otuzdan fazla alan taşıyor (JVM bayrakları, yedek planı,
/// dünya ayarları). Telefon ekranında bunların çoğu görünmez ve hepsini
/// modellemek, sunucu tarafında bir alan değiştiğinde uygulamanın kırılması
/// demekti. Burada yalnızca GÖSTERİLEN alanlar var; gerisi yok sayılıyor.
class GameServer {
  const GameServer({
    required this.id,
    required this.name,
    required this.state,
    required this.software,
    required this.mcVersion,
    required this.port,
    required this.players,
    required this.maxPlayers,
    required this.ramMB,
  });

  final String id;
  final String name;

  /// "running" | "stopped" | "starting" | "stopping" | "error" | "installing"
  final String state;

  final String software;
  final String mcVersion;
  final int port;
  final int players;
  final int maxPlayers;
  final int ramMB;

  bool get isRunning => state == 'running';
  bool get isBusy => state == 'starting' || state == 'stopping' || state == 'installing';
  bool get isError => state == 'error';

  /// Kullanıcıya gösterilecek durum metni.
  String get stateLabel => switch (state) {
        'running' => 'Çalışıyor',
        'stopped' => 'Durdu',
        'starting' => 'Başlatılıyor',
        'stopping' => 'Durduruluyor',
        'installing' => 'Kuruluyor',
        'error' => 'Hata',
        _ => state,
      };

  static GameServer fromJson(Map<String, dynamic> j) => GameServer(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? 'adsız',
        state: j['state'] as String? ?? 'stopped',
        software: j['software'] as String? ?? '',
        mcVersion: j['mcVersion'] as String? ?? '',
        port: (j['port'] as num?)?.toInt() ?? 0,
        players: (j['players'] as num?)?.toInt() ?? 0,
        maxPlayers: (j['maxPlayers'] as num?)?.toInt() ?? 0,
        ramMB: (j['ramMB'] as num?)?.toInt() ?? 0,
      );

  static List<GameServer> listFrom(dynamic raw) {
    if (raw is! List) return const [];
    return raw
        .whereType<Map<String, dynamic>>()
        .map(GameServer.fromJson)
        .toList();
  }
}
