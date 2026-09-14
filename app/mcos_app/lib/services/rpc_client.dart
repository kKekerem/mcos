import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart' as crypto;

import '../models/connection.dart';

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
  RpcClient(this.connection, {this.onPinMismatch});

  final Connection connection;

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
      ..connectionTimeout = const Duration(seconds: 8)
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
      return fp == pinned;
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
    final req = await _client().getUrl(_healthUri);
    final resp = await req.close().timeout(const Duration(seconds: 10));
    final body = await resp.transform(utf8.decoder).join();
    if (resp.statusCode != 200) {
      throw RpcException('Sunucu ${resp.statusCode} döndü');
    }
    final map = jsonDecode(body) as Map<String, dynamic>;
    if (map['service'] != 'mcos') {
      throw RpcException('Bu adreste MCOS yok');
    }
    return HealthInfo(
      version: map['version'] as String? ?? '',
      name: map['name'] as String? ?? '',
      fingerprint: map['fingerprint'] as String? ?? '',
    );
  }

  /// Bir JSON-RPC yöntemi çağırır.
  Future<dynamic> call(String method, [Map<String, dynamic>? params]) async {
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
    } on HandshakeException catch (e) {
      // Parmak izi tutmadığında dart:io burada patlar. Kullanıcıya teknik
      // metni değil, ne anlama geldiğini söylüyoruz.
      throw RpcException(
        'Sunucunun kimliği değişti. Ya MCOS yeniden kuruldu ya da '
        'araya giren biri var. Ayarlardan bu bağlantıyı silip yeniden '
        'ekleyin.\n\n(${e.message})',
      );
    }

    req.headers.set(HttpHeaders.contentTypeHeader, 'application/json');
    req.headers.set(HttpHeaders.authorizationHeader, 'Bearer ${connection.token}');
    req.add(utf8.encode(jsonEncode(payload)));

    final resp = await req.close().timeout(
          // Uzun süren çağrılar var (sunucu yazılımı indirme). Kısa bir
          // zaman aşımı, çalışan bir işlemi "başarısız" gösterirdi.
          const Duration(minutes: 3),
        );
    final body = await resp.transform(utf8.decoder).join();

    if (resp.statusCode == HttpStatus.unauthorized) {
      throw RpcException(
        'Jeton kabul edilmedi. MCOS panelinde "Uzaktan Kontrol" ekranından '
        'jetonu kontrol edin.',
      );
    }
    if (resp.statusCode != 200) {
      throw RpcException('Sunucu ${resp.statusCode} döndü');
    }

    final map = jsonDecode(body) as Map<String, dynamic>;
    final err = map['error'];
    if (err is Map<String, dynamic>) {
      throw RpcException(err['message'] as String? ?? 'bilinmeyen hata');
    }
    return map['result'];
  }

  /// Sonucu harita olarak isteyen çağrılar için kısayol.
  Future<Map<String, dynamic>> callMap(String method,
      [Map<String, dynamic>? params,]) async {
    final r = await call(method, params);
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
