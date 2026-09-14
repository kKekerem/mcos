import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import '../models/connection.dart';

/// Kayıtlı bağlantıların kalıcı deposu.
///
/// ════════════════════════════════════════════════════════════════════════════
/// NEDEN ŞİFRELİ DEPO
/// ════════════════════════════════════════════════════════════════════════════
///
/// Burada saklanan jeton, MCOS'un TAM DENETİMİNİ verir: sunucu silme, dosya
/// yazma, makineyi kapatma. Aynı dosyada SSH parolası da olabilir.
///
/// shared_preferences bunları düz XML olarak yazar; köklenmiş bir telefonda
/// ya da yedekten okuyan bir uygulamada açıkça görünür. flutter_secure_storage
/// Android'de EncryptedSharedPreferences (Keystore destekli) kullanır.
class Store {
  Store({FlutterSecureStorage? storage})
      : _s = storage ??
            const FlutterSecureStorage(
              // encryptedSharedPreferences: eski Android sürümlerinde de
              // Keystore destekli şifreleme kullanılsın.
              aOptions: AndroidOptions(encryptedSharedPreferences: true),
            );

  final FlutterSecureStorage _s;

  static const _keyConnections = 'connections';
  static const _keyActive = 'active_connection';
  static const _keySshPrefix = 'ssh_password_';

  Future<List<Connection>> connections() async {
    final raw = await _s.read(key: _keyConnections);
    return Connection.decodeList(raw);
  }

  Future<void> saveConnections(List<Connection> list) async {
    await _s.write(key: _keyConnections, value: Connection.encodeList(list));
  }

  /// Bir bağlantıyı ekler ya da günceller.
  Future<List<Connection>> upsert(Connection c) async {
    final list = await connections();
    final i = list.indexWhere((e) => e.id == c.id);
    if (i >= 0) {
      list[i] = c;
    } else {
      list.add(c);
    }
    await saveConnections(list);
    return list;
  }

  Future<List<Connection>> remove(String id) async {
    final list = await connections();
    list.removeWhere((e) => e.id == id);
    await saveConnections(list);
    // SSH parolası da gitsin: bağlantı silindiyse parolayı tutmak, geride
    // sahipsiz bir sır bırakmak olurdu.
    await _s.delete(key: _keySshPrefix + id);
    final active = await activeId();
    if (active == id) {
      await setActiveId(list.isEmpty ? null : list.first.id);
    }
    return list;
  }

  Future<String?> activeId() => _s.read(key: _keyActive);

  Future<void> setActiveId(String? id) async {
    if (id == null) {
      await _s.delete(key: _keyActive);
    } else {
      await _s.write(key: _keyActive, value: id);
    }
  }

  // ── SSH parolası ────────────────────────────────────────────────────────
  //
  // İSTEĞE BAĞLI olarak saklanır: kullanıcı her bağlanışta yazmak
  // istemeyebilir. Saklamamayı seçerse hiçbir yere yazılmaz.

  Future<String?> sshPassword(String connectionId) =>
      _s.read(key: _keySshPrefix + connectionId);

  Future<void> setSshPassword(String connectionId, String? password) async {
    final key = _keySshPrefix + connectionId;
    if (password == null || password.isEmpty) {
      await _s.delete(key: key);
    } else {
      await _s.write(key: key, value: password);
    }
  }
}
