// Bağlantı ekranı: QR yolu görünüyor mu, elle giriş hâlâ çalışıyor mu,
// hata ekranında ham metin var mı.
//
// Kullanıcı "Android uygulamasında QR kod yeri yok" dedi. Bu test ekranın
// en üstünde "QR kodu tara" düğmesinin durduğunu, düğmenin tarayıcıyı
// açtığını ve kamera açılamazsa Türkçe bir açıklama göründüğünü doğrular.
// (Testte gerçek kamera yok: eklentinin yerel kanalı taklit ediliyor ve
// kamera izni REDDEDİLMİŞ bir telefon canlandırılıyor.)

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mcos_app/screens/connect_screen.dart';
import 'package:mcos_app/screens/qr_scan_screen.dart';
import 'package:mcos_app/services/store.dart';

const panelQr = 'mcos://pair?f=a679f0babf4cf1b57ab558b2acb8b78f7d9b9e288686b6d7a31602d3ff4ab217'
    '&h=127.0.0.1&h=10.0.2.15&p=2223&t=76d3d64f7febb116d27838771485dfd9&v=1';

Widget app() => MaterialApp(home: ConnectScreen(store: Store()));

void main() {
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));

  testWidgets('QR düğmesi en üstte ve elle giriş alanları duruyor', (t) async {
    await t.pumpWidget(app());
    final qr = find.widgetWithText(FilledButton, 'QR kodu tara');
    expect(qr, findsOneWidget);
    expect(find.text('ya da elle girin'), findsOneWidget);
    expect(find.widgetWithText(TextFormField, 'IP adresi'), findsOneWidget);
    expect(find.widgetWithText(TextFormField, 'Jeton'), findsOneWidget);
    // QR düğmesi adres alanının ÜSTÜNDE.
    expect(t.getTopLeft(qr).dy,
        lessThan(t.getTopLeft(find.widgetWithText(TextFormField, 'IP adresi')).dy),);
  });

  testWidgets('QR düğmesi tarayıcıyı açıyor; kamera izni yoksa Türkçe açıklama',
      (t) async {
    // Telefonda kamera iznini REDDEDEN kullanıcıyı taklit ediyoruz:
    // eklentinin yerel tarafı "izin durumu: belirsiz", "izin isteği: hayır"
    // yanıtlarını veriyor.
    const channel = MethodChannel('dev.steenbakker.mobile_scanner/scanner/method');
    t.binding.defaultBinaryMessenger.setMockMethodCallHandler(channel, (call) async {
      switch (call.method) {
        case 'state':
          return 0;
        case 'request':
          return false;
      }
      return null;
    });
    addTearDown(() =>
        t.binding.defaultBinaryMessenger.setMockMethodCallHandler(channel, null),);
    await t.pumpWidget(app());
    await t.tap(find.widgetWithText(FilledButton, 'QR kodu tara'));
    await t.pump();
    await t.pump(const Duration(milliseconds: 500));
    expect(find.byType(QrScanScreen), findsOneWidget);
    // İzin reddedildi → hata yolu. Mesaj Türkçe, geri dönüş düğmesi var.
    await t.pump(const Duration(seconds: 1));
    final texts = t
        .widgetList<Text>(find.byType(Text))
        .map((w) => w.data ?? '')
        .join('\n');
    expect(texts, contains('Kamera izni verilmedi'));
    expect(texts, isNot(contains('Exception')));
    expect(texts, isNot(contains('null')));
    expect(texts, isNot(contains('permissionDenied')));
    expect(find.text('Elle gir'), findsOneWidget,
        reason: 'kamera açılamayınca geri dönüş düğmesi görünmeli',);
    await t.tap(find.text('Elle gir'));
    await t.pump();
    await t.pump(const Duration(seconds: 1));
    expect(find.byType(QrScanScreen), findsNothing);
    expect(find.byType(ConnectScreen), findsOneWidget);
  });

  testWidgets('adres alanına yapıştırılan mcos:// kodu alanları dolduruyor',
      (t) async {
    await t.pumpWidget(app());
    await t.enterText(find.widgetWithText(TextFormField, 'IP adresi'), panelQr);
    final connect = find.widgetWithText(FilledButton, 'Bağlan');
    await t.ensureVisible(connect);
    await t.pump();
    await t.tap(connect);
    await t.pump();
    final fields = t
        .widgetList<EditableText>(find.byType(EditableText))
        .map((e) => e.controller.text)
        .toList();
    expect(fields, contains('127.0.0.1'));
    expect(fields, contains('2223'));
    expect(fields, contains('76d3d64f7febb116d27838771485dfd9'));
    // Bağlantı denemesi bitene kadar bekle (testte ağ taklit ediliyor ve
    // her isteğe 400 dönüyor). Çıkan hata Türkçe ve ham metinsiz olmalı.
    await t.runAsync(() => Future<void>.delayed(const Duration(seconds: 2)));
    await t.pump();
    final texts = t
        .widgetList<SelectableText>(find.byType(SelectableText))
        .map((w) => w.data ?? '')
        .join('\n');
    expect(texts, isNotEmpty, reason: 'hata bandı görünmedi');
    for (final raw in ['Exception', 'null', 'undefined', 'Instance of']) {
      expect(texts, isNot(contains(raw)));
    }
  });

  testWidgets('startWithQr: ekran açılınca tarayıcı hemen açılıyor', (t) async {
    // Karşılama ve Ayarlar'daki "QR kodu tara" düğmeleri bu yolu kullanıyor;
    // kullanıcı formda ikinci bir düğme aramamalı.
    const channel = MethodChannel('dev.steenbakker.mobile_scanner/scanner/method');
    t.binding.defaultBinaryMessenger.setMockMethodCallHandler(channel, (call) async {
      switch (call.method) {
        case 'state':
          return 0;
        case 'request':
          return false;
      }
      return null;
    });
    addTearDown(() =>
        t.binding.defaultBinaryMessenger.setMockMethodCallHandler(channel, null),);
    await t.pumpWidget(
        MaterialApp(home: ConnectScreen(store: Store(), startWithQr: true)),);
    await t.pump();
    await t.pump(const Duration(milliseconds: 500));
    expect(find.byType(QrScanScreen), findsOneWidget);
  });
}
