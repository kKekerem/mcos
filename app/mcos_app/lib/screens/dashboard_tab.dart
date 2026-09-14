import 'package:flutter/material.dart';

import '../models/connection.dart';
import '../models/server.dart';
import '../models/system_status.dart';
import '../theme/palette.dart';
import '../widgets/mcos_logo.dart';
import '../widgets/stat_card.dart';

/// Makinenin genel durumu.
///
/// Panelin "Sistem Durumu" ekranının telefon karşılığı: aynı bilgiler, aynı
/// sıra, ama dokunmatik için büyütülmüş.
class DashboardTab extends StatelessWidget {
  const DashboardTab({
    super.key,
    required this.status,
    required this.servers,
    required this.error,
    required this.loading,
    required this.connection,
    required this.onRefresh,
  });

  final SystemStatus? status;
  final List<GameServer> servers;
  final String? error;
  final bool loading;
  final Connection connection;
  final Future<void> Function() onRefresh;

  @override
  Widget build(BuildContext context) {
    final accent = Theme.of(context).colorScheme.primary;
    final running = servers.where((s) => s.isRunning).length;
    final players = servers.fold<int>(0, (n, s) => n + s.players);

    return RefreshIndicator(
      onRefresh: onRefresh,
      color: accent,
      backgroundColor: Palette.surface,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        children: [
          Row(
            children: [
              McosLogo(size: 34, color: accent),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      status?.systemName ?? connection.label,
                      style: const TextStyle(
                        color: Palette.text,
                        fontSize: 20,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    Text(
                      '${connection.host}:${connection.port}',
                      style: const TextStyle(
                          color: Palette.textDim, fontSize: 13,),
                    ),
                  ],
                ),
              ),
              if (status != null)
                _Pill(text: 'MCOS ${status!.version}', color: accent),
            ],
          ),
          const SizedBox(height: 20),

          if (error != null) ...[
            _ErrorCard(message: error!),
            const SizedBox(height: 16),
          ],

          if (loading && status == null)
            const Padding(
              padding: EdgeInsets.symmetric(vertical: 48),
              child: Center(child: CircularProgressIndicator()),
            ),

          if (status != null) ...[
            Row(
              children: [
                Expanded(
                  child: StatCard(
                    label: 'Sunucu',
                    value: '$running / ${servers.length}',
                    sub: 'çalışıyor',
                    icon: Icons.dns_outlined,
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: StatCard(
                    label: 'Oyuncu',
                    value: '$players',
                    sub: 'çevrimiçi',
                    icon: Icons.people_outline,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 12),
            StatCard(
              label: 'İşlemci',
              value: '%${status!.cpuUsagePct.toStringAsFixed(0)}',
              sub: '${status!.cpuCores} çekirdek · ${status!.cpuModel}',
              icon: Icons.memory_outlined,
              progress: status!.cpuUsagePct / 100,
            ),
            const SizedBox(height: 12),
            StatCard(
              label: 'Bellek',
              value: '%${status!.memUsagePct.toStringAsFixed(0)}',
              sub: '${formatBytes(status!.memUsedBytes)} / '
                  '${formatBytes(status!.memTotalBytes)}',
              icon: Icons.sd_card_outlined,
              progress: status!.memUsagePct / 100,
            ),
            const SizedBox(height: 12),
            StatCard(
              label: 'Çalışma süresi',
              value: status!.uptimeLabel,
              sub: status!.internet ? 'internet var' : 'internet yok',
              icon: Icons.schedule_outlined,
              subColor: status!.internet ? Palette.ok : Palette.warn,
            ),

            if (status!.addresses.isNotEmpty) ...[
              const SizedBox(height: 20),
              const Text(
                'AĞ ADRESLERİ',
                style: TextStyle(
                  color: Palette.textFaint,
                  fontSize: 11,
                  letterSpacing: 1.2,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 8),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  for (final a in status!.addresses)
                    _Pill(text: a, color: Palette.textDim),
                ],
              ),
            ],
          ],
        ],
      ),
    );
  }
}

class _Pill extends StatelessWidget {
  const _Pill({required this.text, required this.color});

  final String text;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: color.withValues(alpha: 0.35)),
      ),
      child: Text(
        text,
        style: TextStyle(color: color, fontSize: 12, fontWeight: FontWeight.w500),
      ),
    );
  }
}

class _ErrorCard extends StatelessWidget {
  const _ErrorCard({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: Palette.error.withValues(alpha: 0.10),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: Palette.error.withValues(alpha: 0.5)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Icon(Icons.cloud_off, color: Palette.error, size: 20),
          const SizedBox(width: 10),
          Expanded(
            child: Text(
              message,
              style: const TextStyle(color: Palette.text, fontSize: 13),
            ),
          ),
        ],
      ),
    );
  }
}
