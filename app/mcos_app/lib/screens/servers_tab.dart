import 'package:flutter/material.dart';

import '../models/server.dart';
import '../theme/palette.dart';
import '../widgets/mcos_logo.dart';
import 'server_detail_screen.dart';

/// Sunucu listesi: başlat, durdur, ayrıntıya git.
class ServersTab extends StatelessWidget {
  const ServersTab({
    super.key,
    required this.servers,
    required this.loading,
    required this.onRefresh,
    required this.onAction,
    required this.query,
  });

  final List<GameServer> servers;
  final bool loading;
  final Future<void> Function() onRefresh;

  final Future<void> Function(String method,
      {Map<String, dynamic>? params, String? okMessage,}) onAction;

  final Future<Map<String, dynamic>> Function(String method,
      [Map<String, dynamic>? params,]) query;

  @override
  Widget build(BuildContext context) {
    return RefreshIndicator(
      onRefresh: onRefresh,
      color: Theme.of(context).colorScheme.primary,
      backgroundColor: Palette.surface,
      child: servers.isEmpty
          ? ListView(
              // ListView: boşken de aşağı çekip yenilenebilsin. Column
              // olsaydı RefreshIndicator çalışmazdı.
              children: [
                SizedBox(height: MediaQuery.of(context).size.height * 0.25),
                Center(
                  child: Column(
                    children: [
                      Icon(
                        loading ? Icons.hourglass_empty : Icons.dns_outlined,
                        size: 48,
                        color: Palette.textFaint,
                      ),
                      const SizedBox(height: 12),
                      Text(
                        loading ? 'Yükleniyor…' : 'Henüz sunucu yok',
                        style: const TextStyle(color: Palette.textDim),
                      ),
                      if (!loading) ...[
                        const SizedBox(height: 6),
                        const Text(
                          'MCOS panelinden yeni sunucu oluşturabilirsiniz.',
                          textAlign: TextAlign.center,
                          style: TextStyle(
                              color: Palette.textFaint, fontSize: 12,),
                        ),
                      ],
                    ],
                  ),
                ),
              ],
            )
          : ListView.separated(
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
              itemCount: servers.length,
              separatorBuilder: (_, __) => const SizedBox(height: 12),
              itemBuilder: (context, i) => _ServerTile(
                server: servers[i],
                onAction: onAction,
                onOpen: () => Navigator.of(context).push(
                  MaterialPageRoute(
                    builder: (_) => ServerDetailScreen(
                      server: servers[i],
                      onAction: onAction,
                      query: query,
                    ),
                  ),
                ),
              ),
            ),
    );
  }
}

class _ServerTile extends StatelessWidget {
  const _ServerTile({
    required this.server,
    required this.onAction,
    required this.onOpen,
  });

  final GameServer server;
  final VoidCallback onOpen;
  final Future<void> Function(String method,
      {Map<String, dynamic>? params, String? okMessage,}) onAction;

  @override
  Widget build(BuildContext context) {
    final s = server;
    return Material(
      color: Palette.surface,
      borderRadius: BorderRadius.circular(12),
      child: InkWell(
        onTap: onOpen,
        borderRadius: BorderRadius.circular(12),
        child: Container(
          padding: const EdgeInsets.all(16),
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(12),
            border: Border.all(color: Palette.divider),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  StatusDot(state: s.state),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Text(
                      s.name,
                      style: const TextStyle(
                        color: Palette.text,
                        fontSize: 17,
                        fontWeight: FontWeight.w600,
                      ),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  Text(
                    s.stateLabel,
                    style: TextStyle(
                      color: s.isRunning
                          ? Palette.ok
                          : s.isError
                              ? Palette.error
                              : Palette.textDim,
                      fontSize: 13,
                      fontWeight: FontWeight.w500,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 6),
              Text(
                '${s.software} ${s.mcVersion} · port ${s.port}'
                '${s.isRunning ? ' · ${s.players}/${s.maxPlayers} oyuncu' : ''}',
                style: const TextStyle(color: Palette.textDim, fontSize: 12),
                overflow: TextOverflow.ellipsis,
              ),
              const SizedBox(height: 14),
              Row(
                children: [
                  Expanded(
                    child: s.isRunning
                        ? OutlinedButton.icon(
                            // Meşgulken düğmeyi kapat: başlatılırken bir kez
                            // daha "başlat" demek, daemon'a çelişen iki
                            // komut göndermek olurdu.
                            onPressed: s.isBusy
                                ? null
                                : () => onAction('server.stop',
                                    params: {'id': s.id},
                                    okMessage: '${s.name} durduruluyor',),
                            icon: const Icon(Icons.stop, size: 18),
                            label: const Text('Durdur'),
                            style: OutlinedButton.styleFrom(
                              minimumSize: const Size.fromHeight(42),
                              foregroundColor: Palette.text,
                            ),
                          )
                        : FilledButton.icon(
                            onPressed: s.isBusy
                                ? null
                                : () => onAction('server.start',
                                    params: {'id': s.id},
                                    okMessage: '${s.name} başlatılıyor',),
                            icon: const Icon(Icons.play_arrow, size: 18),
                            label: const Text('Başlat'),
                            style: FilledButton.styleFrom(
                              minimumSize: const Size.fromHeight(42),
                            ),
                          ),
                  ),
                  const SizedBox(width: 10),
                  OutlinedButton(
                    onPressed: onOpen,
                    style: OutlinedButton.styleFrom(
                      minimumSize: const Size(52, 42),
                      padding: EdgeInsets.zero,
                    ),
                    child: const Icon(Icons.chevron_right, size: 20),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}
