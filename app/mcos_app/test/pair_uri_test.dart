// Eşleşme QR'ı ve elle girilen adresin çözümü.
//
// Buradaki örnek URI UYDURMA DEĞİL: MCOS panelinin QR penceresi Go testinde
// (internal/fbpanel/screen_remote_qr_test.go) çizildi, ekran görüntüsü
// bağımsız bir çözücüyle (zxing-cpp) okundu ve çıkan metin buraya kondu.
// Yani telefonun kamerası paneli okuduğunda eline geçecek metnin aynısı.

import 'package:flutter_test/flutter_test.dart';
import 'package:mcos_app/services/pair_uri.dart';

const panelQr = 'mcos://pair?f=a679f0babf4cf1b57ab558b2acb8b78f7d9b9e288686b6d7a31602d3ff4ab217'
    '&h=192.168.1.42&h=10.0.2.15&p=2223&t=76d3d64f7febb116d27838771485dfd9&v=1';

// Panelin METİN olarak gösterdiği parmak izi (iki noktalı, büyük harf).
const panelFp = 'A6:79:F0:BA:BF:4C:F1:B5:7A:B5:58:B2:AC:B8:B7:8F:'
    '7D:9B:9E:28:86:86:B6:D7:A3:16:02:D3:FF:4A:B2:17';

void main() {
  group('QR', () {
    test('panelin gerçek QR metni çözülüyor', () {
      final p = parsePairUri(panelQr);
      expect(p.hosts, ['192.168.1.42', '10.0.2.15']);
      expect(p.port, 2223);
      expect(p.token, '76d3d64f7febb116d27838771485dfd9');
      expect(sameFingerprint(p.fingerprint, panelFp), isTrue);
    });

    test('tek adresli (eski panel) kod da çözülüyor', () {
      final p = parsePairUri('mcos://pair?h=10.1.2.3&p=2223&t=abcdef0123&v=1');
      expect(p.hosts, ['10.1.2.3']);
      expect(p.fingerprint, isNull);
    });

    test('bozuk kodlar Türkçe ve anlaşılır bir mesajla reddediliyor', () {
      for (final bad in [
        'https://pair?v=1&h=a&p=1&t=b', // yanlış şema
        'mcos://pair?v=9&h=a&p=1&t=b', // yanlış sürüm
        'mcos://pair?v=1&h=a&p=1', // jeton yok
        'mcos://pair?v=1&p=1&t=b', // adres yok
        'mcos://pair?v=1&h=a&t=b', // port yok
        'mcos://pair?v=1&h=a&p=0&t=b', // port sıfır
        'mcos://pair?v=1&h=a&p=70000&t=b', // port büyük
      ]) {
        expect(
          () => parsePairUri(bad),
          throwsA(isA<PairFormatException>().having(
            (e) => e.message,
            'mesaj',
            allOf(isNotEmpty, isNot(contains('null')),
                isNot(contains('Exception')),),
          ),),
          reason: bad,
        );
      }
    });

    test('yalnızca mcos:// kodları eşleşme kodu sayılıyor', () {
      expect(looksLikePairUri(panelQr), isTrue);
      expect(looksLikePairUri('  MCOS://pair?x'), isTrue);
      expect(looksLikePairUri('https://example.com'), isFalse);
      expect(looksLikePairUri('8690000000001'), isFalse);
    });
  });

  group('parmak izi', () {
    test('üç biçim aynı sertifikayı gösteriyor', () {
      const bare = 'a679f0babf4cf1b57ab558b2acb8b78f7d9b9e288686b6d7a31602d3ff4ab217';
      expect(normalizeFingerprint(panelFp), bare);
      expect(sameFingerprint(panelFp, bare), isTrue);
      expect(sameFingerprint(panelFp.toLowerCase(), bare.toUpperCase()), isTrue);
    });

    test('boş parmak izi hiçbir şeyle eşleşmiyor', () {
      expect(sameFingerprint('', ''), isFalse);
      expect(sameFingerprint(null, panelFp), isFalse);
    });
  });

  group('jeton', () {
    // Panel jetonu "76d3d64f7febb116 d27838771485dfd9" diye, ortada boşlukla
    // gösteriyor. Eski kod yalnızca uçları kırpıyordu ve MCOS "yetkisiz"
    // diyordu (test/e2e_mcosd_test.dart bunu gerçek mcosd'de ölçüyor).
    test('panelin boşluklu gösterimi temizleniyor', () {
      expect(cleanToken('76d3d64f7febb116 d27838771485dfd9'),
          '76d3d64f7febb116d27838771485dfd9',);
      expect(cleanToken(' 76d3 d64f\n7feb\tb116 '), '76d3d64f7febb116');
    });
  });

  group('adres alanı', () {
    test('yaygın yazımların hepsi kabul ediliyor', () {
      HostInput p(String s) => parseHostInput(s);
      expect(p('192.168.1.20').host, '192.168.1.20');
      expect(p('192.168.1.20').port, isNull);
      expect(p('192.168.1.20:2223').host, '192.168.1.20');
      expect(p('192.168.1.20:2223').port, 2223);
      expect(p('https://192.168.1.20:2223/').host, '192.168.1.20');
      expect(p('https://192.168.1.20:2223/').port, 2223);
      expect(p(' mcos.local ').host, 'mcos.local');
      expect(p('[fe80::1]:2223').host, 'fe80::1');
      expect(p('[fe80::1]:2223').port, 2223);
      expect(p('fe80::1').host, 'fe80::1'); // çıplak IPv6: port yok
    });

    test('geçersiz yazımlar Türkçe mesajla reddediliyor', () {
      for (final bad in ['', '   ', '192.168.1.20:99999', '192.168.1.20:abc', 'a b']) {
        expect(() => parseHostInput(bad), throwsA(isA<PairFormatException>()),
            reason: bad,);
      }
    });

    // ESKİ HATANIN ÖLÇÜMÜ: eski kod adres alanını olduğu gibi URI'ye
    // koyuyordu. "IP:port" yazan kullanıcı ham FormatException görüyordu.
    test('eski yol: "IP:port" girişi ham FormatException üretiyordu', () {
      const host = '192.168.1.20:2223';
      expect(() => Uri.parse('https://$host:2223/health'),
          throwsFormatException,);
      // Yeni yol aynı girişten geçerli bir adres çıkarıyor.
      final h = parseHostInput(host);
      expect(Uri.parse('https://${h.host}:${h.port}/health').port, 2223);
    });
  });
}
