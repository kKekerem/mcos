import 'dart:convert';

/// Kayıtlı bir MCOS bağlantısı.
///
/// Kullanıcı birden çok MCOS'a bağlanabilir (evdeki, arkadaşındaki) ve her
/// biri ayrı adres, jeton ve parmak izi taşır.
class Connection {
  const Connection({
    required this.id,
    required this.label,
    required this.host,
    required this.port,
    required this.token,
    this.fingerprint,
    this.theme,
    this.sshPort = 22,
    this.sshUser = 'root',
  });

  /// Kalıcı kimlik: kullanıcı adı veya adresi değiştirse de kayıt aynı kalır.
  final String id;

  /// Kullanıcının verdiği ad ("Salon PC").
  final String label;

  final String host;
  final int port;

  /// Uzaktan kontrol jetonu. ŞİFRELİ depoda saklanır (bkz. store.dart).
  final String token;

  /// Sabitlenmiş sertifika parmak izi. İlk bağlantıda doldurulur.
  final String? fingerprint;

  /// Sunucunun tema adı; uygulama aynı vurgu rengini kullanır.
  final String? theme;

  final int sshPort;
  final String sshUser;

  Connection copyWith({
    String? label,
    String? host,
    int? port,
    String? token,
    String? fingerprint,
    String? theme,
    int? sshPort,
    String? sshUser,
  }) {
    return Connection(
      id: id,
      label: label ?? this.label,
      host: host ?? this.host,
      port: port ?? this.port,
      token: token ?? this.token,
      fingerprint: fingerprint ?? this.fingerprint,
      theme: theme ?? this.theme,
      sshPort: sshPort ?? this.sshPort,
      sshUser: sshUser ?? this.sshUser,
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'label': label,
        'host': host,
        'port': port,
        'token': token,
        if (fingerprint != null) 'fingerprint': fingerprint,
        if (theme != null) 'theme': theme,
        'sshPort': sshPort,
        'sshUser': sshUser,
      };

  static Connection fromJson(Map<String, dynamic> j) => Connection(
        id: j['id'] as String,
        label: j['label'] as String? ?? '',
        host: j['host'] as String? ?? '',
        // Sayılar JSON'dan int ya da double gelebilir; ikisini de kabul et.
        port: (j['port'] as num?)?.toInt() ?? 2223,
        token: j['token'] as String? ?? '',
        fingerprint: j['fingerprint'] as String?,
        theme: j['theme'] as String?,
        sshPort: (j['sshPort'] as num?)?.toInt() ?? 22,
        sshUser: j['sshUser'] as String? ?? 'root',
      );

  static String encodeList(List<Connection> list) =>
      jsonEncode(list.map((c) => c.toJson()).toList());

  static List<Connection> decodeList(String? raw) {
    if (raw == null || raw.isEmpty) return const [];
    try {
      final data = jsonDecode(raw);
      if (data is! List) return const [];
      return data
          .whereType<Map<String, dynamic>>()
          .map(Connection.fromJson)
          .toList();
    } catch (_) {
      // Bozuk bir kayıt uygulamayı açılmaz hâle getirmemeli: kullanıcı
      // bağlantıyı yeniden ekler, ama uygulama açılır.
      return const [];
    }
  }
}
