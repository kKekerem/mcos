// Hiçbir hata yolu kullanıcıya ham istisna metni, "null" ya da "undefined"
// göstermemeli.
//
// Kullanıcının şikâyeti: "bağlanmaya çalışınca undefined mi ne falan o tarz
// hatalar veriyor". Ekranlar yakaladıkları her şeyi e.toString() ile
// basıyordu; aşağıdaki istisnaların hepsi gerçek telefonda karşılaşılan
// türler (yanlış port, kapalı makine, NAT arkasında sanal makine, bozuk
// yanıt…).

import 'dart:async';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:mcos_app/services/errors.dart';
import 'package:mcos_app/services/pair_uri.dart';
import 'package:mcos_app/services/rpc_client.dart';

Object typeErrorOf() {
  try {
    const Object? v = null;
    // Eski kodun yaptığı türden bir dönüşüm: alan yoksa patlar.
    // ignore: unnecessary_cast
    (v as dynamic) as String;
  } catch (e) {
    return e;
  }
  throw StateError('TypeError üretilemedi');
}

void main() {
  final cases = <String, Object>{
    'reddedildi': const SocketException('Connection refused',
        osError: OSError('Connection refused', 111),),
    'zaman aşımı': const SocketException('Connection timed out',
        osError: OSError('Connection timed out', 110),),
    'ulaşılamıyor': const SocketException('No route to host',
        osError: OSError('No route to host', 113),),
    'ad çözülemedi': const SocketException('Failed host lookup: \'mcos.local\''),
    'tls': const HandshakeException('CERTIFICATE_VERIFY_FAILED'),
    'zaman aşımı future': TimeoutException('x', const Duration(minutes: 3)),
    'http kesildi': const HttpException(
        'Connection closed before full header was received',),
    'bozuk json': const FormatException('Unexpected character'),
    'tür hatası': typeErrorOf(),
    'bilinmeyen': StateError('iç durum'),
  };

  for (final entry in cases.entries) {
    test('${entry.key}: Türkçe ve ham metinsiz', () {
      final m = friendlyError(entry.value, host: '192.168.1.20', port: 2223);
      expect(m, isNotEmpty);
      // Ham istisna metni HİÇBİR biçimde sızmamalı (ör. "Bad state: …").
      expect(m, isNot(contains(entry.value.toString())), reason: entry.key);
      for (final raw in [
        'null', 'undefined', 'Exception', 'Error', 'errno', 'subtype',
        'OS Error', 'Instance of',
      ]) {
        expect(m, isNot(contains(raw)), reason: '${entry.key}: "$m"');
      }
    });
  }

  test('ağ hatalarında adres söyleniyor', () {
    final m = friendlyError(cases['reddedildi']!, host: '192.168.1.20', port: 2223);
    expect(m, contains('192.168.1.20:2223'));
  });

  test('yanıt biçimi hatası sürüm uyumsuzluğunu söylüyor', () {
    expect(friendlyError(typeErrorOf()), contains('sürüm'));
  });

  test('zaman aşımı VirtualBox NAT ipucunu veriyor', () {
    expect(friendlyError(cases['zaman aşımı']!), contains('Köprü'));
  });

  test('kendi hata türlerimiz olduğu gibi geçiyor', () {
    expect(friendlyError(RpcException('Jeton kabul edilmedi.')),
        'Jeton kabul edilmedi.',);
    expect(friendlyError(const PairFormatException('Adres gerekli.')),
        'Adres gerekli.',);
  });
}
