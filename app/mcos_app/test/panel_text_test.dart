// Uygulamanın "panelde şuraya gidin" metinleri panelin GERÇEK menüsüyle
// aynı mı? Panelin Go kaynağından okunarak sınanıyor.
//
// Neden: uygulama "Sol menü → Uzaktan Kontrol" diyordu, panelde öyle bir yer
// yok (uzaktan kontrol Ayarlar'ın içinde). Kullanıcı QR'ı bulamadı. Bu test
// o tür bir kaymayı yakalar: panelde bir ad değişirse burası düşer.
//
// Uygulama depodan ayrı derlenirse (Go kaynağı yoksa) atlanır.

import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:mcos_app/services/panel_text.dart';

const _panel = '../../internal/fbpanel';

String _go(String name) => File('$_panel/$name').readAsStringSync();

void main() {
  final skip = Directory(_panel).existsSync()
      ? null
      : 'panel kaynağı yok ($_panel) — depo dışında derleniyor';

  test('sol menüde "Ayarlar" bölümü var', () {
    expect(_go('app.go'),
        matches(RegExp(r'SecSettings:\s*"' + RegExp.escape(panelSettings) + '"')),);
  }, skip: skip,);

  test('Ayarlar içinde uzaktan kontrol ve SSH satırları var', () {
    final s = _go('screen_settings.go');
    expect(s,
        matches(RegExp(r'setRemote:\s*\{"' + RegExp.escape(panelRemoteRow) + '"')),);
    expect(s,
        matches(RegExp(r'setSSH:\s*\{"' + RegExp.escape(panelSshRow) + '"')),);
  }, skip: skip,);

  test('uzaktan kontrol penceresindeki düğme adları panelle aynı', () {
    final s = _go('screen_remote.go');
    for (final label in [panelRemoteEnable, panelQrAction, panelSshEnable]) {
      expect(s, contains('Label: "$label"'), reason: label);
    }
  }, skip: skip,);

  test('uygulamada panelde olmayan eski yol kalmadı', () {
    // "Sol menü → Uzaktan Kontrol" panelde yok; hiçbir ekran bunu yazmamalı.
    final eski = <String>[];
    for (final f in Directory('lib').listSync(recursive: true)) {
      if (f is! File || !f.path.endsWith('.dart')) continue;
      // panel_text.dart eski yolu BİLEREK anıyor: düzeltilen hatayı
      // belgeleyen açıklamada. Ekrana giden metin orada değil, sabitlerde.
      if (f.path.endsWith('panel_text.dart')) continue;
      final t = f.readAsStringSync();
      if (t.contains('Sol menü') || t.contains('Uzaktan Kontrol →')) {
        eski.add(f.path);
      }
    }
    expect(eski, isEmpty);
  },);
}
