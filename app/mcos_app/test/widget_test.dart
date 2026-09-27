// MCOS uygulamasının açılış testi.
//
// ── Neden bu dosya yeniden yazıldı ──────────────────────────────────────────
// Burada Flutter'ın şablon testi duruyordu: `MyApp` adlı bir sınıfı ve bir
// sayaç düğmesini arıyordu. MCOS'un kök widget'ı `McosApp`, sayaç ise hiç yok.
// Sonuç: `flutter analyze` tek bir hatayla kırmızı yanıyordu
// ("The name 'MyApp' isn't a class") ve gerçek bir sorun varsa o hatanın
// arasında kaybolacaktı.
//
// Bu test bilerek küçük: uygulamanın ÇİZİLEBİLDİĞİNİ doğrular. Daha fazlası
// (bağlantı akışı, RPC) gerçek bir MCOS kutusu ister ve birim testine sığmaz.

import 'package:flutter_test/flutter_test.dart';

import 'package:mcos_app/main.dart';

void main() {
  testWidgets('uygulama açılıyor ve karşılama ekranı çiziliyor',
      (WidgetTester tester) async {
    await tester.pumpWidget(const McosApp());

    // İlk kare: açılış (splash) ekranı. Zamanlayıcıları ilerletmiyoruz —
    // pumpAndSettle burada sonsuza kadar bekler, çünkü açılış animasyonu
    // süreklidir.
    await tester.pump();

    expect(tester.takeException(), isNull);
  });
}
