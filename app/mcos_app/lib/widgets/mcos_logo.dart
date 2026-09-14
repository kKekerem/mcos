import 'package:flutter/material.dart';

import '../theme/palette.dart';

/// MCOS işareti.
///
/// ── Neden çizilmiş, resim değil ─────────────────────────────────────────────
/// Bir PNG, her ekran yoğunluğu için ayrı boyut ister ve tema rengini takip
/// edemez. Bu işaret vurgu renginden çizildiği için kullanıcı panelde temayı
/// değiştirdiğinde telefonda da değişir.
///
/// Şekil, panelin açılış ekranındaki blok yığınını yansıtır: üç küp.
class McosLogo extends StatelessWidget {
  const McosLogo({super.key, this.size = 48, this.color});

  final double size;
  final Color? color;

  @override
  Widget build(BuildContext context) {
    final c = color ?? Theme.of(context).colorScheme.primary;
    return SizedBox(
      width: size,
      height: size,
      child: CustomPaint(painter: _LogoPainter(c)),
    );
  }
}

class _LogoPainter extends CustomPainter {
  const _LogoPainter(this.color);

  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    final unit = size.width / 10;
    final r = Radius.circular(unit * 0.6);

    // Üç blok: ikisi altta, biri üstte ortada. Basit ve ölçekte okunur.
    final blocks = <(double, double, double)>[
      (1.0, 5.0, 0.55), // sol alt  (x, y, opaklık)
      (5.4, 5.0, 0.80), // sağ alt
      (3.2, 0.8, 1.00), // üst
    ];

    for (final (x, y, alpha) in blocks) {
      final paint = Paint()
        ..color = color.withValues(alpha: alpha)
        ..style = PaintingStyle.fill;
      canvas.drawRRect(
        RRect.fromRectAndRadius(
          Rect.fromLTWH(x * unit, y * unit, unit * 3.6, unit * 3.6),
          r,
        ),
        paint,
      );
    }
  }

  @override
  bool shouldRepaint(covariant _LogoPainter old) => old.color != color;
}

/// Durum noktası: çalışıyor / durdu / hata.
class StatusDot extends StatelessWidget {
  const StatusDot({super.key, required this.state, this.size = 10});

  /// GameServer.state ile aynı değerler.
  final String state;
  final double size;

  Color get _color => switch (state) {
        'running' => Palette.ok,
        'error' => Palette.error,
        'starting' || 'stopping' || 'installing' => Palette.warn,
        _ => Palette.textFaint,
      };

  @override
  Widget build(BuildContext context) {
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        color: _color,
        shape: BoxShape.circle,
        // Hafif bir hâle: küçük bir noktayı ekranda bulmayı kolaylaştırır.
        boxShadow: [
          BoxShadow(color: _color.withValues(alpha: 0.45), blurRadius: 6),
        ],
      ),
    );
  }
}
