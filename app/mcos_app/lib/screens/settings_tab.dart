import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../models/connection.dart';
import '../services/store.dart';
import '../theme/palette.dart';
import 'connect_screen.dart';

/// Bağlantı ayarları, SSH kurulumu ve güç düğmeleri.
class SettingsTab extends StatefulWidget {
  const SettingsTab({
    super.key,
    required this.connection,
    required this.store,
    required this.query,
    required this.onAction,
    required this.onConnectionsChanged,
    required this.onThemeChanged,
  });

  final Connection connection;
  final Store store;

  final Future<Map<String, dynamic>> Function(String method,
      [Map<String, dynamic>? params,]) query;

  final Future<void> Function(String method,
      {Map<String, dynamic>? params, String? okMessage,}) onAction;

  final Future<void> Function() onConnectionsChanged;
  final void Function(String? theme) onThemeChanged;

  @override
  State<SettingsTab> createState() => _SettingsTabState();
}

class _SettingsTabState extends State<SettingsTab> {
  Map<String, dynamic>? _ssh;
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _loadSsh();
  }

  Future<void> _loadSsh() async {
    try {
      final r = await widget.query('ssh.status');
      if (mounted) setState(() => _ssh = r);
    } catch (_) {
      // SSH durumu alınamazsa ekranın kalanı yine çalışmalı.
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _toggleSsh(bool on) async {
    await widget.onAction(
      on ? 'ssh.enable' : 'ssh.disable',
      okMessage: on ? 'SSH açıldı' : 'SSH kapatıldı',
    );
    await _loadSsh();
  }

  Future<void> _setSshPassword() async {
    final controller = TextEditingController();
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('SSH parolası'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Text(
              'Bu parola ile "root" kullanıcısı olarak bağlanacaksınız. '
              'En az 8 karakter.',
              style: TextStyle(color: Palette.textDim, fontSize: 13),
            ),
            const SizedBox(height: 16),
            TextField(
              controller: controller,
              obscureText: true,
              autofocus: true,
              decoration: const InputDecoration(labelText: 'Yeni parola'),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Vazgeç'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Kaydet'),
          ),
        ],
      ),
    );
    if (ok != true) return;

    await widget.onAction(
      'ssh.password',
      params: {'password': controller.text},
      okMessage: 'SSH parolası ayarlandı',
    );
    await _loadSsh();
  }

  Future<void> _confirmPower(String action, String label) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(label),
        content: Text(
          action == 'poweroff'
              ? 'Makine kapanacak. Yeniden açmak için fiziksel olarak '
                  'güç düğmesine basmanız gerekir.'
              : 'Makine yeniden başlatılacak. Sunucular kısa süre '
                  'erişilemez olacak.',
          style: const TextStyle(color: Palette.textDim),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Vazgeç'),
          ),
          // Yıkıcı eylem: düğme KIRMIZI ve odak vazgeçmede değil, ama
          // kullanıcı bilerek dokunmak zorunda.
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Palette.error),
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(label),
          ),
        ],
      ),
    );
    if (ok != true) return;
    await widget.onAction('system.power', params: {'action': action});
  }

  @override
  Widget build(BuildContext context) {
    final c = widget.connection;
    final ssh = _ssh;
    final sshAvailable = ssh?['available'] as bool? ?? false;
    final sshEnabled = ssh?['enabled'] as bool? ?? false;
    final sshRunning = ssh?['running'] as bool? ?? false;
    final sshPasswordSet = ssh?['passwordSet'] as bool? ?? false;
    final sshPort = (ssh?['port'] as num?)?.toInt() ?? 22;

    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
      children: [
        _section('BAĞLANTI'),
        _card([
          _row('Ad', c.label.isEmpty ? '—' : c.label),
          _row('Adres', '${c.host}:${c.port}'),
          if (c.fingerprint != null)
            _row(
              'Sertifika',
              // İlk 23 karakter: kullanıcı panelle karşılaştırırken bu kadarı
              // yeter, tamamı ekranı doldururdu.
              '${c.fingerprint!.substring(0, 23)}…',
              mono: true,
              onCopy: () => _copy(c.fingerprint!, 'Parmak izi kopyalandı'),
            ),
          _row('Jeton', '••••••••',
              onCopy: () => _copy(c.token, 'Jeton kopyalandı'),),
        ]),
        const SizedBox(height: 10),
        Row(
          children: [
            Expanded(
              child: OutlinedButton.icon(
                onPressed: () async {
                  final updated = await Navigator.of(context).push<Connection>(
                    MaterialPageRoute(
                      builder: (_) =>
                          ConnectScreen(store: widget.store, existing: c),
                    ),
                  );
                  if (updated != null) {
                    widget.onThemeChanged(updated.theme);
                    await widget.onConnectionsChanged();
                  }
                },
                icon: const Icon(Icons.edit_outlined, size: 18),
                label: const Text('Düzenle'),
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: OutlinedButton.icon(
                onPressed: () async {
                  await widget.store.remove(c.id);
                  await widget.onConnectionsChanged();
                },
                icon: const Icon(Icons.delete_outline, size: 18),
                label: const Text('Sil'),
                style: OutlinedButton.styleFrom(
                  foregroundColor: Palette.error,
                  side: BorderSide(color: Palette.error.withValues(alpha: 0.6)),
                ),
              ),
            ),
          ],
        ),

        const SizedBox(height: 24),
        _section('SSH'),
        if (_loading)
          const Padding(
            padding: EdgeInsets.all(24),
            child: Center(child: CircularProgressIndicator()),
          )
        else if (!sshAvailable)
          _card([
            const Padding(
              padding: EdgeInsets.all(14),
              child: Text(
                'Bu MCOS imajında SSH sunucusu yok.',
                style: TextStyle(color: Palette.textDim, fontSize: 13),
              ),
            ),
          ])
        else
          _card([
            SwitchListTile(
              value: sshEnabled,
              onChanged: _toggleSsh,
              title: const Text('SSH sunucusunu aç',
                  style: TextStyle(color: Palette.text),),
              subtitle: Text(
                sshRunning
                    ? 'çalışıyor · port $sshPort'
                    : sshEnabled
                        ? 'açık ama çalışmıyor'
                        : 'kapalı',
                style: TextStyle(
                  color: sshRunning ? Palette.ok : Palette.textDim,
                  fontSize: 12,
                ),
              ),
            ),
            const Divider(height: 1, color: Palette.divider),
            ListTile(
              title: const Text('SSH parolası',
                  style: TextStyle(color: Palette.text),),
              subtitle: Text(
                sshPasswordSet ? 'kurulu' : 'kurulu değil — giriş yapılamaz',
                style: TextStyle(
                  color: sshPasswordSet ? Palette.ok : Palette.warn,
                  fontSize: 12,
                ),
              ),
              trailing: const Icon(Icons.chevron_right),
              onTap: _setSshPassword,
            ),
          ]),

        const SizedBox(height: 24),
        _section('GÜÇ'),
        _card([
          ListTile(
            leading: const Icon(Icons.restart_alt, color: Palette.warn),
            title: const Text('Yeniden başlat',
                style: TextStyle(color: Palette.text),),
            onTap: () => _confirmPower('reboot', 'Yeniden başlat'),
          ),
          const Divider(height: 1, color: Palette.divider),
          ListTile(
            leading: const Icon(Icons.power_settings_new, color: Palette.error),
            title: const Text('Kapat', style: TextStyle(color: Palette.text)),
            onTap: () => _confirmPower('poweroff', 'Kapat'),
          ),
        ]),
      ],
    );
  }

  void _copy(String text, String message) {
    Clipboard.setData(ClipboardData(text: text));
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  Widget _section(String title) => Padding(
        padding: const EdgeInsets.only(bottom: 8, left: 4),
        child: Text(
          title,
          style: const TextStyle(
            color: Palette.textFaint,
            fontSize: 11,
            letterSpacing: 1.2,
            fontWeight: FontWeight.w600,
          ),
        ),
      );

  Widget _card(List<Widget> children) => Container(
        decoration: BoxDecoration(
          color: Palette.surface,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: Palette.divider),
        ),
        child: Column(children: children),
      );

  Widget _row(String label, String value,
      {bool mono = false, VoidCallback? onCopy,}) {
    return ListTile(
      dense: true,
      title: Text(label,
          style: const TextStyle(color: Palette.textDim, fontSize: 13),),
      subtitle: Text(
        value,
        style: TextStyle(
          color: Palette.text,
          fontSize: 13,
          fontFamily: mono ? 'monospace' : null,
        ),
      ),
      trailing: onCopy == null
          ? null
          : IconButton(
              icon: const Icon(Icons.copy, size: 18),
              onPressed: onCopy,
              tooltip: 'Kopyala',
            ),
    );
  }
}
