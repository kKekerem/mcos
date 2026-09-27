// Uygulamanın bağlantı akışı — GERÇEK bir mcosd'ye karşı.
//
// ════════════════════════════════════════════════════════════════════════════
// NASIL ÇALIŞTIRILIR
// ════════════════════════════════════════════════════════════════════════════
//
// Ortam değişkenleri verilmezse ATLANIR (CI'da ve telefonsuz makinede de
// `flutter test` yeşil kalsın). Elle:
//
//   mcosd --data-root /tmp/x --config /tmp/x/config.json \
//         --listen tcp://127.0.0.1:47211 > /tmp/x/mcosd.log 2>&1 &
//   # remote.enable {"port":47223} → token ve fingerprint'i al
//   MCOS_E2E_PORT=47223 MCOS_E2E_TOKEN=… MCOS_E2E_FP=… \
//   MCOS_E2E_IPC_PORT=47211 MCOS_E2E_LOG=/tmp/x/mcosd.log \
//     flutter test test/e2e_mcosd_test.dart
//
// Test, telefondaki ConnectScreen'in çağırdığı Pairing.connect'in AYNISINI
// çalıştırır; ardından panonun/sunucu listesinin/konsolun yaptığı çağrıları
// yapıp yanıtları uygulamanın kendi modelleriyle çözer.

import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:mcos_app/models/connection.dart';
import 'package:mcos_app/models/console.dart';
import 'package:mcos_app/models/server.dart';
import 'package:mcos_app/models/system_status.dart';
import 'package:mcos_app/services/errors.dart';
import 'package:mcos_app/services/pairing.dart';
import 'package:mcos_app/services/rpc_client.dart';

final env = Platform.environment;
final port = int.tryParse(env['MCOS_E2E_PORT'] ?? '');
final token = env['MCOS_E2E_TOKEN'] ?? '';
final fp = env['MCOS_E2E_FP'] ?? '';
final ipcPort = int.tryParse(env['MCOS_E2E_IPC_PORT'] ?? '');
final logPath = env['MCOS_E2E_LOG'];
const host = '127.0.0.1';

int unauthorizedLogLines() {
  if (logPath == null) return -1;
  return File(logPath!)
      .readAsLinesSync()
      .where((l) => l.contains('yetkisiz istek'))
      .length;
}

Future<Connection> pair({
  List<String> hosts = const [host],
  String? tok,
  String? expected,
}) =>
    Pairing.connect(
      id: 'e2e',
      label: '',
      hosts: hosts,
      port: port!,
      token: tok ?? token,
      expectedFingerprint: expected ?? fp,
    );

void main() {
  final skip = (port == null || token.isEmpty || fp.isEmpty)
      ? 'MCOS_E2E_PORT/TOKEN/FP verilmedi — gerçek mcosd yok'
      : null;

  test('QR akışı: ulaşılamayan adres atlanıyor, parmak izi doğrulanıyor', () async {
    // 192.0.2.1 (RFC 5737 TEST-NET): hiçbir zaman yanıt vermez — QR'daki
    // VirtualBox NAT adresinin (10.0.2.15) telefondaki karşılığı.
    final c = await pair(hosts: ['192.0.2.1', host]);
    expect(c.host, host);
    expect(c.label, isNotEmpty); // /health'teki makine adı
    expect(c.theme, isNotNull, reason: 'config.get zarfından tema okunmalı');
    expect(c.fingerprint, contains(':'));
  }, skip: skip, timeout: const Timeout(Duration(seconds: 60)),);

  test('panelde göründüğü gibi BOŞLUKLU yazılan jeton kabul ediliyor', () async {
    final spaced = '${token.substring(0, 16)} ${token.substring(16)}';
    final c = await pair(tok: spaced);
    expect(c.token, token);
  }, skip: skip,);

  test('yanlış jeton: Türkçe "Jeton kabul edilmedi"', () async {
    await expectLater(
      pair(tok: '00000000000000000000000000000000'),
      throwsA(isA<RpcException>()
          .having((e) => e.message, 'mesaj', contains('Jeton kabul edilmedi')),),
    );
  }, skip: skip,);

  test('QR parmak izi tutmazsa jeton HİÇ gönderilmiyor', () async {
    final before = unauthorizedLogLines();
    await expectLater(
      // Yanlış jeton + yanlış parmak izi: jeton gönderilseydi daemon
      // günlüğüne "yetkisiz istek" düşerdi.
      pair(tok: 'ffffffffffffffffffffffffffffffff', expected: 'ab' * 32),
      throwsA(isA<RpcException>()
          .having((e) => e.message, 'mesaj', contains('QR kodundaki MCOS')),),
    );
    if (before >= 0) {
      await Future<void>.delayed(const Duration(milliseconds: 300));
      expect(unauthorizedLogLines(), before,
          reason: 'jeton sertifika doğrulanmadan gönderildi',);
    }
  }, skip: skip,);

  test('kapalı port: "bağlantıyı reddetti" (ham SocketException değil)', () async {
    // Az önce açıp kapattığımız bir port: kesinlikle dinleyen yok.
    final s = await ServerSocket.bind(host, 0);
    final closed = s.port;
    await s.close();
    await expectLater(
      Pairing.connect(
          id: 'e2e', label: '', hosts: const [host], port: closed, token: token,),
      throwsA(isA<RpcException>()
          .having((e) => e.message, 'mesaj', contains('reddetti')),),
    );
  }, skip: skip,);

  test('yanlış port (mcosd\'nin TLS\'siz IPC portu): anlaşılır mesaj', () async {
    await expectLater(
      Pairing.connect(
          id: 'e2e', label: '', hosts: const [host], port: ipcPort!, token: token,),
      throwsA(isA<RpcException>().having(
        (e) => e.message,
        'mesaj',
        allOf(contains('2223'), isNot(contains('Exception'))),
      ),),
    );
  }, skip: skip ?? (ipcPort == null ? 'MCOS_E2E_IPC_PORT yok' : null),);

  test('pano, sunucu listesi ve konsol gerçek yanıtlarla çözülüyor', () async {
    final c = await pair();
    final rpc = RpcClient(c);
    try {
      final st = SystemStatus.fromJson(await rpc.callMap('system.status'));
      expect(st.version, isNotEmpty);
      expect(st.cpuCores, greaterThan(0));
      expect(st.memTotalBytes, greaterThan(0));
      expect(st.addresses, isNotEmpty,
          reason: 'pano adres kartı net.localIP/nics[].ipv4 okumalı',);

      final list = GameServer.listFrom(
          (await rpc.callMap('server.list'))['servers'],);
      for (final s in list) {
        expect(s.id, isNotEmpty);
        expect(s.stateLabel, isNot(contains('null')));
        final ch = ConsoleChunk.fromJson(
            await rpc.callMap('server.console', {'id': s.id, 'cursor': 0}), 0,);
        expect(ch.cursor, greaterThanOrEqualTo(ch.lines.length));
        // ignore: avoid_print
        print('konsol ${s.name}: ${ch.lines.length} satır, imleç ${ch.cursor}');
      }

      await expectLater(
        rpc.call('boyle.bir.yontem.yok'),
        throwsA(isA<RpcException>()
            .having((e) => e.message, 'mesaj', contains('desteklemiyor')),),
      );
      try {
        await rpc.call('server.command', {'id': 'yok', 'command': 'list'});
      } catch (e) {
        // Daemon'un kendi mesajı; ne olursa olsun ham tür adı olmamalı.
        expect(friendlyError(e), isNot(contains('Exception')));
      }
    } finally {
      rpc.close();
    }
  }, skip: skip,);
}
