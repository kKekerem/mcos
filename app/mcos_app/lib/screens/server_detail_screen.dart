import 'dart:async';

import 'package:flutter/material.dart';

import '../models/server.dart';
import '../theme/palette.dart';
import '../widgets/mcos_logo.dart';

/// Tek bir sunucunun ayrıntısı: canlı konsol ve komut satırı.
///
/// ── Neden konsol imleçle okunuyor ───────────────────────────────────────────
/// Daemon konsolu bir imleç (cursor) ile sunuyor: "şu satırdan sonrasını ver".
/// Her yoklamada tüm günlüğü indirmek, uzun süre çalışan bir sunucuda
/// megabaytlarca veri demek olurdu. İmleçle yalnızca YENİ satırlar geliyor.
class ServerDetailScreen extends StatefulWidget {
  const ServerDetailScreen({
    super.key,
    required this.server,
    required this.onAction,
    required this.query,
  });

  final GameServer server;

  final Future<void> Function(String method,
      {Map<String, dynamic>? params, String? okMessage,}) onAction;

  final Future<Map<String, dynamic>> Function(String method,
      [Map<String, dynamic>? params,]) query;

  @override
  State<ServerDetailScreen> createState() => _ServerDetailScreenState();
}

class _ServerDetailScreenState extends State<ServerDetailScreen> {
  final _lines = <String>[];
  final _scroll = ScrollController();
  final _command = TextEditingController();

  Timer? _poll;
  int _cursor = 0;
  bool _busy = false;
  String? _error;

  /// Konsolda tutulan en fazla satır.
  ///
  /// Sınırsız biriktirmek, uzun açık kalan bir ekranda telefonun belleğini
  /// tüketir. 2000 satır, sorun ayıklamak için fazlasıyla yeter.
  static const _maxLines = 2000;

  @override
  void initState() {
    super.initState();
    _tick();
    _poll = Timer.periodic(const Duration(seconds: 2), (_) => _tick());
  }

  @override
  void dispose() {
    _poll?.cancel();
    _scroll.dispose();
    _command.dispose();
    super.dispose();
  }

  Future<void> _tick() async {
    if (_busy || !mounted) return;
    _busy = true;
    try {
      final r = await widget.query('server.console', {
        'id': widget.server.id,
        'cursor': _cursor,
      });
      final entries = r['entries'];
      final next = (r['cursor'] as num?)?.toInt() ?? _cursor;

      if (entries is List && entries.isNotEmpty) {
        final fresh = <String>[];
        for (final e in entries) {
          if (e is Map<String, dynamic>) {
            final msg = e['message'] ?? e['text'] ?? e['line'];
            if (msg is String) fresh.add(msg);
          } else if (e is String) {
            fresh.add(e);
          }
        }
        if (fresh.isNotEmpty && mounted) {
          setState(() {
            _lines.addAll(fresh);
            if (_lines.length > _maxLines) {
              _lines.removeRange(0, _lines.length - _maxLines);
            }
            _error = null;
          });
          _scrollToEnd();
        }
      }
      _cursor = next;
      if (mounted && _error != null) setState(() => _error = null);
    } catch (e) {
      if (mounted) setState(() => _error = e.toString());
    } finally {
      _busy = false;
    }
  }

  void _scrollToEnd() {
    // Bir kare sonra: setState'in düzeni henüz yeniden hesaplamadığı anda
    // maxScrollExtent eski değeri verir ve en alta inmeyiz.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!_scroll.hasClients) return;
      _scroll.animateTo(
        _scroll.position.maxScrollExtent,
        duration: const Duration(milliseconds: 180),
        curve: Curves.easeOut,
      );
    });
  }

  Future<void> _send() async {
    final cmd = _command.text.trim();
    if (cmd.isEmpty) return;
    _command.clear();
    // Gönderdiğimiz komutu hemen göster: sunucu onu yankılamayabilir ve
    // kullanıcı komutun gidip gitmediğini bilmeli.
    setState(() => _lines.add('> $cmd'));
    _scrollToEnd();
    await widget.onAction('server.command',
        params: {'id': widget.server.id, 'command': cmd},);
  }

  @override
  Widget build(BuildContext context) {
    final s = widget.server;
    return Scaffold(
      appBar: AppBar(
        title: Row(
          children: [
            StatusDot(state: s.state, size: 9),
            const SizedBox(width: 10),
            Expanded(
              child: Text(s.name, overflow: TextOverflow.ellipsis),
            ),
          ],
        ),
        actions: [
          PopupMenuButton<String>(
            color: Palette.raised,
            icon: const Icon(Icons.more_vert),
            onSelected: (v) => widget.onAction(
              'server.$v',
              params: {'id': s.id},
              okMessage: switch (v) {
                'start' => '${s.name} başlatılıyor',
                'stop' => '${s.name} durduruluyor',
                'restart' => '${s.name} yeniden başlatılıyor',
                _ => null,
              },
            ),
            itemBuilder: (_) => const [
              PopupMenuItem(value: 'start', child: Text('Başlat')),
              PopupMenuItem(value: 'stop', child: Text('Durdur')),
              PopupMenuItem(value: 'restart', child: Text('Yeniden başlat')),
            ],
          ),
        ],
      ),
      body: SafeArea(
        child: Column(
          children: [
            Container(
              width: double.infinity,
              padding: const EdgeInsets.fromLTRB(16, 10, 16, 10),
              color: Palette.surface,
              child: Text(
                '${s.software} ${s.mcVersion} · port ${s.port} · '
                '${s.ramMB} MB · ${s.stateLabel}',
                style: const TextStyle(color: Palette.textDim, fontSize: 12),
              ),
            ),
            if (_error != null)
              Container(
                width: double.infinity,
                padding: const EdgeInsets.all(10),
                color: Palette.error.withValues(alpha: 0.12),
                child: Text(
                  _error!,
                  style: const TextStyle(color: Palette.error, fontSize: 12),
                ),
              ),
            Expanded(
              child: Container(
                width: double.infinity,
                color: Palette.bg,
                child: _lines.isEmpty
                    ? const Center(
                        child: Text(
                          'Konsol boş',
                          style: TextStyle(color: Palette.textFaint),
                        ),
                      )
                    : ListView.builder(
                        controller: _scroll,
                        padding: const EdgeInsets.all(12),
                        itemCount: _lines.length,
                        itemBuilder: (_, i) => Padding(
                          padding: const EdgeInsets.only(bottom: 2),
                          child: SelectableText(
                            _lines[i],
                            style: TextStyle(
                              // monospace: Minecraft günlüğü sütun hizalı
                              // yazar; orantılı bir yazı tipi onu bozar.
                              fontFamily: 'monospace',
                              fontSize: 12,
                              height: 1.35,
                              color: _lineColor(_lines[i]),
                            ),
                          ),
                        ),
                      ),
              ),
            ),
            Container(
              padding: const EdgeInsets.fromLTRB(12, 10, 12, 12),
              color: Palette.surface,
              child: Row(
                children: [
                  Expanded(
                    child: TextField(
                      controller: _command,
                      autocorrect: false,
                      enableSuggestions: false,
                      textInputAction: TextInputAction.send,
                      onSubmitted: (_) => _send(),
                      style: const TextStyle(
                          fontFamily: 'monospace', fontSize: 13,),
                      decoration: const InputDecoration(
                        hintText: 'komut  (örn: say merhaba)',
                        isDense: true,
                        contentPadding: EdgeInsets.symmetric(
                            horizontal: 12, vertical: 12,),
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                  IconButton.filled(
                    onPressed: s.isRunning ? _send : null,
                    icon: const Icon(Icons.send, size: 18),
                    tooltip: s.isRunning
                        ? 'Gönder'
                        : 'Sunucu çalışmıyor',
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  /// Günlük satırının önem rengi.
  ///
  /// Minecraft günlükleri "[HH:mm:ss] [Server thread/WARN]: ..." biçiminde.
  /// Seviyeyi renklendirmek, uzun bir akışta hatayı gözle bulmayı sağlar.
  static Color _lineColor(String line) {
    if (line.startsWith('> ')) return Palette.accent;
    final upper = line.toUpperCase();
    if (upper.contains('/ERROR') || upper.contains('[MCOS HATA]')) {
      return Palette.error;
    }
    if (upper.contains('/WARN')) return Palette.warn;
    return Palette.text;
  }
}
