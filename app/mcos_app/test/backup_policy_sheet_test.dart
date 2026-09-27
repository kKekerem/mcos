// Otomatik yedek planı sayfası: dokunulan seçimler daemon'a DOĞRU
// parametrelerle gitmeli. Sahte sorgu işlevi gönderileni kaydeder ve gerçek
// daemon'un yanıt biçimini döndürür (bkz. backup_policy_test.dart).

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mcos_app/models/backup_policy.dart';
import 'package:mcos_app/theme/app_theme.dart';
import 'package:mcos_app/theme/palette.dart';
import 'package:mcos_app/widgets/backup_policy_tile.dart';

class _FakeDaemon {
  final calls = <Map<String, dynamic>>[];

  Future<Map<String, dynamic>> query(
    String method, [
    Map<String, dynamic>? params,
  ]) async {
    calls.add({'method': method, ...?params});
    return {
      'serverId': params?['serverId'],
      'policy': {
        'auto': params?['auto'] ?? false,
        'schedule': params?['schedule'] ?? '6h',
        'keep': params?['keep'] ?? 5,
      },
      'summary': 'özet',
    };
  }
}

Future<void> _open(
    WidgetTester tester, _FakeDaemon f, BackupPolicy initial,) async {
  // Uzun sayfa: varsayılan 800x600 test yüzeyinde Kaydet görünmez kalırdı.
  tester.view.physicalSize = const Size(1080, 2400);
  tester.view.devicePixelRatio = 2.5;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    MaterialApp(
      theme: AppTheme.build(Palette.accent),
      home: Scaffold(
        body: BackupPolicySheet(
            serverId: 'srv_a', initial: initial, query: f.query,),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('gün aralığı + 04:00 + 10 kopya kaydediliyor', (tester) async {
    final f = _FakeDaemon();
    await _open(
        tester, f, const BackupPolicy(auto: false, schedule: '', keep: 0),);
    await tester.tap(find.text('Otomatik yedek'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Gün aralığı'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Her gün'));
    await tester.pumpAndSettle();
    // Günün saati önerilen 04:00 ile açık gelir.
    expect(find.text('Saat: 04:00'), findsOneWidget);
    await tester.tap(find.text('Son 10'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Kaydet'));
    await tester.pumpAndSettle();
    expect(f.calls, [
      {
        'method': 'backup.setPolicy',
        'serverId': 'srv_a',
        'auto': true,
        'schedule': '1d@04:00',
        'keep': 10,
      },
    ]);
  });

  testWidgets('kayıtlı plan ön seçili; saat sabit değil seçilebiliyor',
      (tester) async {
    final f = _FakeDaemon();
    await _open(tester, f,
        const BackupPolicy(auto: true, schedule: '7d@03:30', keep: 20),);
    expect(find.text('Saat: 03:30'), findsOneWidget);
    final chip =
        tester.widget<ChoiceChip>(find.widgetWithText(ChoiceChip, 'Her hafta'));
    expect(chip.selected, isTrue);
    await tester.tap(find.text('Belirli bir saatte'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Kaydet'));
    await tester.pumpAndSettle();
    expect(f.calls.single['schedule'], '7d');
    expect(f.calls.single['keep'], 20);
  });

  testWidgets('kapatmak planı göndermiyor (daemon saklıyor)', (tester) async {
    final f = _FakeDaemon();
    await _open(
        tester, f, const BackupPolicy(auto: true, schedule: '6h', keep: 5),);
    await tester.tap(find.text('Otomatik yedek'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Kaydet'));
    await tester.pumpAndSettle();
    expect(f.calls, [
      {'method': 'backup.setPolicy', 'serverId': 'srv_a', 'auto': false},
    ]);
  });
}
