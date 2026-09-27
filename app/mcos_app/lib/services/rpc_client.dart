import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart' as crypto;

import '../models/connection.dart';
import 'pair_uri.dart';
import 'panel_text.dart';

/// MCOS'un uzaktan kontrol köprüsüne konuşan istemci.
///
/// ════════════════════════════════════════════════════════════════════════════
/// GÜVENLİK: NEDEN SERTİFİKA SABİTLEME
/// ════════════════════════════════════════════════════════════════════════════
///
/// Sunucu KENDİNDEN İMZALI bir sertifika kullanıyor (ev cihazının alan adı
/// yok). Böyle bir sertifikayı doğrulamanın iki yolu var:
///
///   1. "Hepsini kabul et" — şifreleme olur ama kimlik doğrulaması olmaz.
///      Araya giren biri kendi sertifikasını sunar, jetonu alır, sunucunun
///      tam denetimini ele geçirir. KABUL EDİLEMEZ.
///
///   2. SABİTLEME (pinning) — ilk bağlantıda sertifikanın parmak izini
///      kaydet, sonraki her bağlantıda aynı mı diye bak. Araya giren biri
///      kendi sertifikasını sunmak zorunda kalır ve parmak izi TUTMAZ.
///
/// İkincisi uygulanıyor. Kullanıcı parmak izini MCOS panelinde de görebilir
/// ve ilk bağlantıda karşılaştırabilir.
class RpcClient {
  RpcClient(
    this.connection, {
    this.onPinMismatch,
    this.connectTimeout = const Duration(seconds: 8),
  });

  final Connection connection;

  /// TCP bağlantısı için bekleme süresi. QR'daki birden çok adres sırayla
  /// denenirken kısa tutuluyor: ulaşılamayan her adres için 8 saniye beklemek,
  /// üç adreste kullanıcıyı yarım dakika bekletirdi.
  final Duration connectTimeout;

  /// Parmak izi değiştiğinde çağrılır. Dönen değer true ise yeni parmak izi
  /// kabul edilir (kullanıcı "evet, sunucuyu yeniden kurdum" dedi).
  ///
  /// null ise değişiklik HER ZAMAN reddedilir — sessizce kabul etmek,
  /// sabitlemenin tamamen anlamsız olması demekti.
  final Future<bool> Function(String oldFp, String newFp)? onPinMismatch;

  HttpClient? _http;
  int _nextId = 1;

  /// Sunucudan en son görülen parmak izi (bağlantı kurulduktan sonra dolu).
  String? lastSeenFingerprint;

  Uri get _rpcUri =>
      Uri.parse('https://${connection.host}:${connection.port}/rpc');

  Uri get _healthUri =>
      Uri.parse('https://${connection.host}:${connection.port}/health');

  HttpClient _client() {
    final existing = _http;
    if (existing != null) return existing;

    final c = HttpClient()
      // Zaman aşımı: ev ağında ulaşılamayan bir adres, kullanıcıyı
      // sonsuza dek dönen bir çarkla baş başa bırakmamalı.
      ..connectionTimeout = connectTimeout
      ..idleTimeout = const Duration(seconds: 30);

    // ── Sabitleme ─────────────────────────────────────────────────────
    // badCertificateCallback YALNIZCA sertifika zinciri doğrulanamadığında
    // çağrılır — kendinden imzalı bir sertifikada bu her zaman olur.
    // Burada "true" dönmek doğrulamayı atlar, bu yüzden parmak izini
    // KENDİMİZ denetliyoruz.
    c.badCertificateCallback = (X509Certificate cert, String host, int port) {
      final fp = fingerprintOf(cert);
      lastSeenFingerprint = fp;

      final pinned = connection.fingerprint;
      if (pinned == null || pinned.isEmpty) {
        // İlk bağlantı: henüz sabitlenmiş bir şey yok. Kabul ediyoruz ve
        // çağıran katman bunu kaydediyor (TOFU — ilk kullanımda güven).
        return true;
      }
      // Sabit süreli karşılaştırmaya gerek yok: parmak izi gizli değil,
      // sunucu onu /health üzerinden zaten herkese söylüyor.
      //
      // BİÇİMDEN BAĞIMSIZ karşılaştırma: QR'dan gelen parmak izi iki
      // noktasız ve küçük harf ("a679f0…"), burada hesaplanan ise
      // "A6:79:F0:…". Düz == ile QR'la eşleşen her bağlantı "kimlik
      // değişti" diye reddedilirdi.
      return sameFingerprint(fp, pinned);
    };

    _http = c;
    return c;
  }

  /// Sertifikanın SHA-256 parmak izi, panelde gösterilen biçimde.
  static String fingerprintOf(X509Certificate cert) {
    final digest = crypto.sha256.convert(cert.der);
    return digest.bytes
        .map((b) => b.toRadixString(16).padLeft(2, '0').toUpperCase())
        .join(':');
  }

  /// Bağlantıyı sınar ve sunucunun kimlik bilgisini döndürür.
  ///
  /// Jeton GEREKTİRMEZ: kullanıcı adresi doğru yazıp yazmadığını, jetonu
  /// girmeden önce görebilmeli.
  Future<HealthInfo> health() async {
    final HttpClientRequest req;
    try {
      req = await _client().getUrl(_healthUri);
    } on HandshakeException {
      throw _handshakeFailure();
    }
    final resp = await req.close().timeout(const Duration(seconds: 10));
    final body = await resp.transform(utf8.decoder).join();
    if (resp.statusCode != 200) {
      throw RpcException(
        'Bu adreste MCOS uzaktan kontrolü yok (sunucu ${resp.statusCode} '
        'döndü). Port olarak panelde yazanı girin (varsayılan 2223).',
      );
    }
    final map = _decodeObject(body);
    if (map == null || map['service'] != 'mcos') {
      throw RpcException(
        'Bu adreste MCOS yok. Adresi ve portu MCOS panelindeki '
        '$panelRemotePath ekranından kontrol edin.',
      );
    }
    return HealthInfo(
      version: _str(map['version']),
      name: _str(map['name']),
      fingerprint: _str(map['fingerprint']),
    );
  }

  /// TLS el sıkışması başarısız olduğunda NEDENİ ayırt eder.
  ///
  /// ── Düzeltilen hata (e2e testi yakaladı) ────────────────────────────────
  /// İlk sürüm her HandshakeException'ı "sunucunun kimliği değişti" diye
  /// raporluyordu. Oysa en sık sebep YANLIŞ PORT: TLS konuşmayan bir porta
  /// (ör. mcosd'nin 2222 eşleştirme ya da IPC portu) bağlanınca da aynı
  /// istisna geliyor. Kimlik değişikliği yalnızca sertifika GÖRÜLDÜYSE ve
  /// sabitlenmiş parmak iziyle tutmadıysa söylenir; gerisi genel TLS
  /// mesajına (errors.dart) bırakılır.
  Object _handshakeFailure() {
    final pinned = connection.fingerprint;
    final seen = lastSeenFingerprint;
    if (pinned != null &&
        pinned.isNotEmpty &&
        seen != null &&
        !sameFingerprint(seen, pinned)) {
      return _identityChanged();
    }
    return const HandshakeException('TLS');
  }

  RpcException _identityChanged() => RpcException(
        'Sunucunun kimliği değişti. Ya MCOS yeniden kuruldu ya da araya '
        'giren biri var. Ayarlardan bu bağlantıyı silip QR ile yeniden '
        'ekleyin.',
      );

  /// JSON gövdesini nesne olarak çözer; nesne değilse ya da bozuksa null.
  ///
  /// Eski kod `jsonDecode(body) as Map<String, dynamic>` yapıyordu: gövde
  /// bir HTML hata sayfası ya da boşsa kullanıcı ham "FormatException" /
  /// "type 'Null' is not a subtype…" metnini görüyordu.
  static Map<String, dynamic>? _decodeObject(String body) {
    try {
      final v = jsonDecode(body);
      return v is Map<String, dynamic> ? v : null;
    } on FormatException {
      return null;
    }
  }

  static String _str(Object? v) => v is String ? v : '';

  /// Bir JSON-RPC yöntemi çağırır.
  ///
  /// [timeout] verilmezse 3 dakika (uzun işlemler için). Bağlantı sınaması
  /// gibi hızlı yanıt beklenen çağrılar kısa bir süre verir.
  Future<dynamic> call(String method,
      [Map<String, dynamic>? params, Duration? timeout,]) async {
    final id = _nextId++;
    final payload = <String, dynamic>{
      'jsonrpc': '2.0',
      'id': id,
      'method': method,
      if (params != null) 'params': params,
    };

    HttpClientRequest req;
    try {
      req = await _client().postUrl(_rpcUri);
    } on HandshakeException {
      // Parmak izi tutmadığında dart:io burada patlar. Kullanıcıya teknik
      // metni değil, ne anlama geldiğini söylüyoruz.
      throw _handshakeFailure();
    }

    req.headers.set(HttpHeaders.contentTypeHeader, 'application/json');
    // cleanToken: eski kayıtlarda panelden boşluklu yazılmış jeton durabilir
    // (bkz. pair_uri.dart cleanToken); onları da kurtarıyoruz.
    req.headers.set(
        HttpHeaders.authorizationHeader, 'Bearer ${cleanToken(connection.token)}',);
    req.add(utf8.encode(jsonEncode(payload)));

    final resp = await req.close().timeout(
          // Uzun süren çağrılar var (sunucu yazılımı indirme). Kısa bir
          // zaman aşımı, çalışan bir işlemi "başarısız" gösterirdi.
          timeout ?? const Duration(minutes: 3),
        );
    final body = await resp.transform(utf8.decoder).join();

    if (resp.statusCode == HttpStatus.unauthorized) {
      throw RpcException(
        'Jeton kabul edilmedi. MCOS panelinde $panelRemotePath ekranından '
        'jetonu kontrol edin ya da QR ile yeniden eşleştirin.',
      );
    }
    if (resp.statusCode != 200) {
      throw RpcException(
        'MCOS isteği kabul etmedi (HTTP ${resp.statusCode}). Uygulama ile '
        'MCOS sürümleri uyumsuz olabilir.',
      );
    }

    final map = _decodeObject(body);
    if (map == null) {
      throw RpcException('MCOS\'tan anlaşılmayan bir yanıt geldi.');
    }
    final err = map['error'];
    if (err is Map<String, dynamic>) {
      throw RpcException(rpcErrorMessage(method, err));
    }
    return map['result'];
  }

  /// JSON-RPC hata nesnesini kullanıcı cümlesine çevirir.
  ///
  /// -32601 (yöntem yok) ayrı ele alınıyor: daemon "method not found: x"
  /// diyor, bu İngilizce ve kullanıcıya ne yapacağını söylemiyor. Sebep
  /// hemen her zaman MCOS'un uygulamadan eski olması.
  static String rpcErrorMessage(String method, Map<String, dynamic> err) {
    final code = err['code'];
    if (code == -32601) {
      return 'Bu MCOS sürümü bu işlemi desteklemiyor ($method). MCOS\'u '
          'güncelleyin.';
    }
    final msg = err['message'];
    if (msg is String && msg.trim().isNotEmpty) return msg.trim();
    return 'MCOS işlemi yapamadı ($method).';
  }

  /// Sonucu harita olarak isteyen çağrılar için kısayol.
  Future<Map<String, dynamic>> callMap(String method,
      [Map<String, dynamic>? params, Duration? timeout,]) async {
    final r = await call(method, params, timeout);
    if (r is Map<String, dynamic>) return r;
    return <String, dynamic>{};
  }

  void close() {
    _http?.close(force: true);
    _http = null;
  }
}

/// /health yanıtı.
class HealthInfo {
  const HealthInfo({
    required this.version,
    required this.name,
    required this.fingerprint,
  });

  final String version;
  final String name;
  final String fingerprint;
}

/// Kullanıcıya gösterilebilir bir hata.
///
/// Dart'ın kendi istisnaları teknik metinler taşır ("SocketException: OS
/// Error: Connection refused, errno = 111"). Kullanıcı bunu okuyamaz; bu
/// sınıf, anlaşılır cümleyi taşımak için var.
class RpcException implements Exception {
  RpcException(this.message);
  final String message;

  @override
  String toString() => message;
}
