// Ekranda görünen sürüm, pubspec.yaml'daki sürümle aynı olmalı: kullanıcı
// hangi APK'yı kurduğunu bu satırdan anlıyor.
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:mcos_app/services/app_version.dart';

void main() {
  test('appVersion pubspec ile aynı', () {
    final pub = File('pubspec.yaml').readAsStringSync();
    final m = RegExp(r'^version:\s*([0-9.]+)', multiLine: true).firstMatch(pub);
    expect(m, isNotNull);
    expect(appVersion, m!.group(1));
  });
}
