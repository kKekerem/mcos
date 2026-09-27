// Model çözümleme — GERÇEK mcosd yanıtlarıyla.
//
// Aşağıdaki JSON'lar elle yazılmadı: host'ta scratch veri köküyle çalışan
// mcosd'nin uzaktan kontrol köprüsüne (HTTPS /rpc) curl ile sorulup
// kopyalandı (2026-09-26, mcosd 1.0.1). Eski modeller bu yanıtlarda
// konsolu (lines ↔ entries), panonun adresini (net.localIP ↔ net.addresses)
// ve temayı (config.theme ↔ theme) hiç bulamıyordu.

import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:mcos_app/models/console.dart';
import 'package:mcos_app/models/server.dart';
import 'package:mcos_app/models/system_status.dart';
import 'package:mcos_app/services/pairing.dart';
import 'package:mcos_app/services/rpc_client.dart';

const realStatus = '''{"systemName":"mcos-1","version":"1.0.1","uptimeSec":15,"tier":"medium","detectedTier":"medium","panel":"mcos-panel","pollIntervalMs":2000,"cpu":{"model":"13th Gen Intel(R) Core(TM) i7-13700HX","cores":12,"threads":24,"usagePct":0,"mhz":2304,"coreMHz":[2304,2304]},"memory":{"totalBytes":16650874880,"availableBytes":14122467328,"usedBytes":2528407552,"usagePct":15.184833050646285},"disks":[{"mount":"/","totalBytes":1081101176832,"usedBytes":157528666112,"freeBytes":923572510720,"usagePct":14.571130758881736}],"gpus":null,"net":{"nics":[{"name":"eth0","mac":"00:15:5d:5c:58:a7","ipv4":"172.24.3.241","up":true,"kind":"wired","driver":"hv_netvsc","link":true}],"localIP":"172.24.3.241","internet":true,"hostname":"DESKTOP-7KDDCQ4"},"javaVersions":[],"serversTotal":0,"serversUp":0,"wan":"stopped","peersOnline":0,"activeTasks":0,"usb":{"present":false,"partitions":0},"clockSynced":true,"turboOn":false}''';

const realServerListEmpty = '{"servers":null}';

const realServerList = '''{"servers":[{"id":"srv_a82f70bc","name":"Deneme","software":"paper","mcVersion":"1.21.1","javaMajor":21,"allowOldVersions":false,"ramMB":1024,"port":25565,"priority":"normal","fullPerf":false,"jvmFlags":"aikar","maxPlayers":20,"gamemode":"survival","difficulty":"easy","onlineMode":true,"pvp":true,"link":{"mode":""},"autostart":false,"restartOnCrash":true,"supportsPlugins":true,"supportsMods":false,"backup":{"auto":false,"schedule":"","keep":0},"wan":{"enabled":false},"createdAt":"2026-09-26T13:55:44.116652726+03:00","updatedAt":"2026-09-26T13:55:44.116652726+03:00","state":"stopped"}]}''';

const realConsole = '''{"lines":[{"t":1790420224640,"text":"Downloading mojang_1.21.1.jar"},{"t":1790420228702,"text":"Applying patches"},{"t":1790420231353,"text":"Starting org.bukkit.craftbukkit.Main"}],"cursor":8}''';

const realConfigGet = '''{"config":{"version":"1.0.1","theme":"graphite-teal","tier":{"mode":"auto"},"autostartServers":true,"remote":{"enabled":true,"port":47223,"token":"x"},"ssh":{},"vnc":{},"setupComplete":false}}''';

Map<String, dynamic> j(String s) => jsonDecode(s) as Map<String, dynamic>;

void main() {
  test('system.status: adresler net.localIP ve nics[].ipv4\'ten geliyor', () {
    final s = SystemStatus.fromJson(j(realStatus));
    expect(s.systemName, 'mcos-1');
    expect(s.cpuCores, 12);
    expect(s.memTotalBytes, 16650874880);
    expect(s.internet, isTrue);
    expect(s.addresses, ['172.24.3.241']); // tekrar yok
  });

  test('system.status: alanlar yoksa ya da türü farklıysa çökmüyor', () {
    final s = SystemStatus.fromJson(j('{"cpu":null,"memory":[],"net":"x"}'));
    expect(s.systemName, 'MCOS');
    expect(s.addresses, isEmpty);
  });

  test('server.list: null liste boş sayılıyor', () {
    expect(GameServer.listFrom(j(realServerListEmpty)['servers']), isEmpty);
  });

  test('server.list: gerçek sunucu nesnesi çözülüyor', () {
    final list = GameServer.listFrom(j(realServerList)['servers']);
    expect(list, hasLength(1));
    expect(list.first.name, 'Deneme');
    expect(list.first.state, 'stopped');
    expect(list.first.stateLabel, 'Durdu');
    expect(list.first.players, 0); // daemon "players"ı omitempty ile atlıyor
    expect(list.first.ramMB, 1024);
  });

  test('server.console: gerçek "lines/text" yanıtı okunuyor', () {
    final c = ConsoleChunk.fromJson(j(realConsole), 0);
    expect(c.lines, [
      'Downloading mojang_1.21.1.jar',
      'Applying patches',
      'Starting org.bukkit.craftbukkit.Main',
    ]);
    expect(c.cursor, 8);
  });

  test('server.console: boş/eksik yanıt imleci korur', () {
    final c = ConsoleChunk.fromJson(j('{"lines":[]}'), 5);
    expect(c.lines, isEmpty);
    expect(c.cursor, 5);
  });

  test('config.get: tema zarfın içinden okunuyor', () {
    expect(themeFromConfig(j(realConfigGet)), 'graphite-teal');
    expect(themeFromConfig(j('{"theme":"ocean"}')), 'ocean');
    expect(themeFromConfig(j('{"config":{}}')), isNull);
  });

  test('JSON-RPC hataları: "method not found" Türkçeleşiyor', () {
    // Gerçek mcosd: {"code":-32601,"message":"method not found: x"}
    final m = RpcClient.rpcErrorMessage(
        'server.foo', {'code': -32601, 'message': 'method not found: server.foo'},);
    expect(m, contains('desteklemiyor'));
    expect(m, isNot(contains('method not found')));
    // Daemon'un kendi Türkçe mesajı olduğu gibi geçiyor.
    expect(
        RpcClient.rpcErrorMessage('system.power',
            {'code': -32602, 'message': 'action poweroff|reboot olmalı'},),
        'action poweroff|reboot olmalı',);
    // Mesajsız hata: "null" değil, anlamlı bir cümle.
    final empty = RpcClient.rpcErrorMessage('ping', {'code': -1, 'message': null});
    expect(empty, isNot(contains('null')));
    expect(empty, isNotEmpty);
  });
}
