import 'package:flutter/material.dart';

/// MCOS panelinin renkleri.
///
/// ════════════════════════════════════════════════════════════════════════════
/// NEDEN BİREBİR AYNI DEĞERLER
/// ════════════════════════════════════════════════════════════════════════════
///
/// Kullanıcının isteği "tasarımı benzer olsun" idi. "Benzer" yaklaşık demek
/// değil: telefon ile ekran yan yana durduğunda iki ayrı ürün gibi
/// görünmemeli. Bu yüzden değerler tahmin edilmedi, panelin kaynağından
/// (internal/fbui/theme.go) alındı.
///
/// Orada bir renk değişirse burası da değişmeli; bu dosyanın başındaki
/// yorum, o eşleşmenin bilinçli olduğunu söylüyor.
class Palette {
  const Palette._();

  // ── Zeminler ──────────────────────────────────────────────────────────
  /// Uygulamanın en arka zemini.
  static const bg = Color(0xFF0F1216);

  /// Kart ve panel zemini.
  static const surface = Color(0xFF161B21);

  /// Öne çıkan yüzey (seçili satır, açılır pencere).
  static const raised = Color(0xFF1E252D);

  // ── Çizgiler ──────────────────────────────────────────────────────────
  static const border = Color(0xFF39485A);
  static const divider = Color(0xFF26303B);

  // ── Yazı ──────────────────────────────────────────────────────────────
  static const text = Color(0xFFDCE3EA);
  static const textDim = Color(0xFF8D9AA8);
  static const textFaint = Color(0xFF606D7B);

  /// Vurgu renginin ÜZERİNE yazılan metin (koyu).
  static const textOn = Color(0xFF07120F);

  // ── Durum ─────────────────────────────────────────────────────────────
  /// Varsayılan vurgu: graphite-teal.
  static const accent = Color(0xFF23A99C);
  static const ok = Color(0xFF46A758);
  static const warn = Color(0xFFD9A21B);
  static const error = Color(0xFFE5484D);

  /// Panelde seçilebilen tema vurguları.
  ///
  /// Uygulama, bağlandığı MCOS'un temasını okuyup aynı vurguyu kullanır:
  /// kullanıcı panelde moru seçtiyse telefonda da mor görür.
  static const accents = <String, Color>{
    'graphite-teal': Color(0xFF23A99C),
    'noir-purple': Color(0xFF8B5CF6),
    'anthracite-orange': Color(0xFFE07A3F),
    'anthracite-green': Color(0xFF46A758),
    'crimson-night': Color(0xFFD64550),
    'amber-graphite': Color(0xFFD9A21B),
  };

  /// Tema adından vurgu rengi; bilinmeyen ad varsayılana düşer.
  static Color accentFor(String? theme) => accents[theme] ?? accent;
}
