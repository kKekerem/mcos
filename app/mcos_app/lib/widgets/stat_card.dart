import 'package:flutter/material.dart';

import '../theme/palette.dart';

/// Tek bir ölçümü gösteren kart.
///
/// Panelin gösterge kutularının telefon karşılığı: aynı düzen (küçük etiket,
/// büyük değer, altında ayrıntı), dokunmatik için büyütülmüş.
class StatCard extends StatelessWidget {
  const StatCard({
    super.key,
    required this.label,
    required this.value,
    this.sub,
    this.icon,
    this.progress,
    this.subColor,
  });

  final String label;
  final String value;
  final String? sub;
  final IconData? icon;

  /// 0..1 arası; verilirse altında bir çubuk çizilir.
  final double? progress;

  final Color? subColor;

  /// Doluluk oranına göre renk: yüksek kullanım uyarı rengine kayar.
  ///
  /// Kullanıcı sayıyı okumadan da "bellek dolmuş" diyebilmeli.
  Color _barColor(Color accent) {
    final p = progress ?? 0;
    if (p >= 0.90) return Palette.error;
    if (p >= 0.75) return Palette.warn;
    return accent;
  }

  @override
  Widget build(BuildContext context) {
    final accent = Theme.of(context).colorScheme.primary;
    final p = progress;

    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Palette.surface,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: Palette.divider),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              if (icon != null) ...[
                Icon(icon, size: 16, color: Palette.textDim),
                const SizedBox(width: 6),
              ],
              Expanded(
                child: Text(
                  label.toUpperCase(),
                  style: const TextStyle(
                    color: Palette.textFaint,
                    fontSize: 11,
                    letterSpacing: 1.1,
                    fontWeight: FontWeight.w600,
                  ),
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Text(
            value,
            style: const TextStyle(
              color: Palette.text,
              fontSize: 26,
              fontWeight: FontWeight.w600,
              height: 1.1,
            ),
          ),
          if (p != null) ...[
            const SizedBox(height: 10),
            ClipRRect(
              borderRadius: BorderRadius.circular(4),
              child: LinearProgressIndicator(
                // clamp: sunucu %101 bildirirse çubuk taşmasın.
                value: p.clamp(0.0, 1.0),
                minHeight: 6,
                backgroundColor: Palette.raised,
                valueColor: AlwaysStoppedAnimation(_barColor(accent)),
              ),
            ),
          ],
          if (sub != null) ...[
            const SizedBox(height: 8),
            Text(
              sub!,
              style: TextStyle(
                color: subColor ?? Palette.textDim,
                fontSize: 12,
              ),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
          ],
        ],
      ),
    );
  }
}
