import 'dart:async';
import 'dart:convert';

import 'package:dartssh2/dartssh2.dart';
import 'package:flutter/material.dart';
import 'package:xterm/xterm.dart';

import '../models/connection.dart';
import '../services/store.dart';
import '../theme/palette.dart';

/// MCOS makinesine SSH ile bağlanan gerçek bir terminal.
///
/// ════════════════════════════════════════════════════════════════════════════
/// NEDEN GERÇEK BİR TERMİNAL
/// ════════════════════════════════════════════════════════════════════════════
///
/// "Komut yaz, çıktıyı göster" biçiminde basit bir kutu yapılabilirdi, ama o
/// kutuda `top`, `nano`, `htop` ya da renkli hiçbir çıktı çalışmazdı: bunlar
/// imleci hareket ettiren kaçış dizileri kullanır. Kullanıcının isteği
/// "ssh ile bağlanıp kontrol etme" idi — yarım bir kabuk bunu karşılamaz.
///
/// xterm paketi kaçış dizilerini yorumluyor, dartssh2 de saf Dart bir SSH
/// istemcisi (yerel kod derlemesi gerekmez).
class SshTab extends StatefulWidget {
  const SshTab({super.key, required this.connection, required this.store});

  final Connection connection;
  final Store store;

  @override
  State<SshTab> createState() => _SshTabState();
}

class _SshTabState extends State<SshTab> {
  final _terminal = Terminal(maxLines: 4000);
  final _password = TextEditingController();
  bool _remember = true;

  SSHClient? _client;
  SSHSession? _session;

  bool _connecting = false;
  bool _connected = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _loadSavedPassword();
  }

  Future<void> _loadSavedPassword() async {
    final saved = await widget.store.sshPassword(widget.connection.id);
    if (saved != null && mounted) {
      setState(() => _password.text = saved);
    }
  }

  @override
  void dispose() {
    _disconnect();
    _password.dispose();
    super.dispose();
  }

  Future<void> _connect() async {
    if (_connecting) return;
    setState(() {
      _connecting = true;
      _error = null;
    });

    final c = widget.connection;
    final password = _password.text;

    try {
      final socket = await SSHSocket.connect(
        c.host,
        c.sshPort,
        timeout: const Duration(seconds: 10),
      );

      final client = SSHClient(
        socket,
        username: c.sshUser,
        onPasswordRequest: () => password,
      );

      final session = await client.shell(
        pty: SSHPtyConfig(
          width: _terminal.viewWidth,
          height: _terminal.viewHeight,
          // xterm-256color: renkli çıktı (ls --color, htop) çalışsın.
          type: 'xterm-256color',
        ),
      );

      // Terminalden SSH'a: kullanıcının tuşları.
      _terminal.onOutput = (data) {
        session.write(utf8.encode(data));
      };
      // Terminal boyutu değişince (klavye açılınca) uzak tarafa bildir;
      // yoksa `top` gibi programlar yanlış boyutta çizer.
      _terminal.onResize = (w, h, pw, ph) {
        session.resizeTerminal(w, h, pw, ph);
      };

      // SSH'tan terminale: sunucunun çıktısı.
      //
      // allowMalformed: ikili bir çıktı (yanlışlıkla `cat` edilen bir dosya)
      // UTF-8 çözücüsünü patlatıp bağlantıyı düşürmemeli.
      session.stdout.listen(
        (data) => _terminal.write(utf8.decode(data, allowMalformed: true)),
        onError: (Object e) => _terminal.write('\r\n[okuma hatası: $e]\r\n'),
      );
      session.stderr.listen(
        (data) => _terminal.write(utf8.decode(data, allowMalformed: true)),
      );

      // Oturum kapandığında arayüzü güncelle.
      unawaited(session.done.then((_) {
        if (!mounted) return;
        setState(() {
          _connected = false;
          _session = null;
        });
        _terminal.write('\r\n\x1b[33m[oturum kapandı]\x1b[0m\r\n');
      }),);

      if (_remember) {
        await widget.store.setSshPassword(c.id, password);
      } else {
        await widget.store.setSshPassword(c.id, null);
      }

      if (!mounted) {
        client.close();
        return;
      }
      setState(() {
        _client = client;
        _session = session;
        _connected = true;
        _connecting = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _connecting = false;
        _error = _friendlyError(e);
      });
    }
  }

  /// SSH hatalarını kullanıcının anlayacağı cümleye çevirir.
  static String _friendlyError(Object e) {
    final s = e.toString();
    if (s.contains('All authentication methods failed') ||
        s.contains('auth')) {
      return 'Parola kabul edilmedi.\n\nMCOS panelinde Ayarlar → SSH '
          'ekranından bir parola koyduğunuzdan emin olun.';
    }
    if (s.contains('Connection refused')) {
      return 'Bağlantı reddedildi.\n\nMCOS panelinde SSH açık mı? '
          'Ayarlar → SSH → "SSH sunucusunu aç".';
    }
    if (s.contains('timed out') || s.contains('TimeoutException')) {
      return 'Zaman aşımı. Telefon MCOS ile aynı ağda mı?';
    }
    return 'Bağlanılamadı: $s';
  }

  void _disconnect() {
    _session?.close();
    _client?.close();
    _session = null;
    _client = null;
    if (mounted) setState(() => _connected = false);
  }

  @override
  Widget build(BuildContext context) {
    final accent = Theme.of(context).colorScheme.primary;

    if (!_connected) {
      return _buildLogin(accent);
    }

    return Column(
      children: [
        Container(
          color: Palette.surface,
          padding: const EdgeInsets.fromLTRB(16, 10, 8, 10),
          child: Row(
            children: [
              Icon(Icons.terminal, size: 18, color: accent),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  '${widget.connection.sshUser}@${widget.connection.host}',
                  style: const TextStyle(
                      color: Palette.text, fontWeight: FontWeight.w600,),
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              TextButton.icon(
                onPressed: _disconnect,
                icon: const Icon(Icons.logout, size: 16),
                label: const Text('Kapat'),
              ),
            ],
          ),
        ),
        Expanded(
          child: TerminalView(
            _terminal,
            theme: _terminalTheme,
            // Klavye otomatik açılmasın: kullanıcı önce çıktıyı okumak
            // isteyebilir ve klavye ekranın yarısını kaplar.
            autofocus: false,
            backgroundOpacity: 1,
          ),
        ),
      ],
    );
  }

  Widget _buildLogin(Color accent) {
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 32, 20, 32),
      children: [
        Icon(Icons.terminal, size: 56, color: accent),
        const SizedBox(height: 16),
        const Text(
          'SSH ile bağlan',
          textAlign: TextAlign.center,
          style: TextStyle(
            color: Palette.text,
            fontSize: 20,
            fontWeight: FontWeight.w600,
          ),
        ),
        const SizedBox(height: 8),
        Text(
          '${widget.connection.sshUser}@${widget.connection.host}:'
          '${widget.connection.sshPort}',
          textAlign: TextAlign.center,
          style: const TextStyle(color: Palette.textDim, fontSize: 13),
        ),
        const SizedBox(height: 28),
        TextField(
          controller: _password,
          obscureText: true,
          autocorrect: false,
          enableSuggestions: false,
          onSubmitted: (_) => _connect(),
          decoration: const InputDecoration(
            labelText: 'SSH parolası',
            prefixIcon: Icon(Icons.lock_outline),
          ),
        ),
        const SizedBox(height: 8),
        SwitchListTile(
          value: _remember,
          onChanged: (v) => setState(() => _remember = v),
          title: const Text('Parolayı bu cihazda sakla',
              style: TextStyle(color: Palette.text, fontSize: 14),),
          subtitle: const Text('Şifreli depoda tutulur',
              style: TextStyle(color: Palette.textFaint, fontSize: 12),),
          contentPadding: EdgeInsets.zero,
        ),
        const SizedBox(height: 16),
        FilledButton.icon(
          onPressed: _connecting ? null : _connect,
          icon: _connecting
              ? const SizedBox(
                  width: 18,
                  height: 18,
                  child: CircularProgressIndicator(
                      strokeWidth: 2, color: Palette.textOn,),
                )
              : const Icon(Icons.login),
          label: Text(_connecting ? 'Bağlanılıyor…' : 'Bağlan'),
        ),
        if (_error != null) ...[
          const SizedBox(height: 20),
          Container(
            padding: const EdgeInsets.all(14),
            decoration: BoxDecoration(
              color: Palette.error.withValues(alpha: 0.10),
              borderRadius: BorderRadius.circular(8),
              border: Border.all(color: Palette.error.withValues(alpha: 0.5)),
            ),
            child: Text(
              _error!,
              style: const TextStyle(color: Palette.text, fontSize: 13),
            ),
          ),
        ],
      ],
    );
  }

  /// Terminal renkleri: paletle aynı aile.
  static const _terminalTheme = TerminalTheme(
    cursor: Palette.accent,
    selection: Color(0x4023A99C),
    foreground: Palette.text,
    background: Palette.bg,
    black: Color(0xFF0F1216),
    red: Palette.error,
    green: Palette.ok,
    yellow: Palette.warn,
    blue: Color(0xFF4C8EDA),
    magenta: Color(0xFF8B5CF6),
    cyan: Palette.accent,
    white: Palette.text,
    brightBlack: Palette.textFaint,
    brightRed: Color(0xFFFF6369),
    brightGreen: Color(0xFF5DC66E),
    brightYellow: Color(0xFFF0BD3A),
    brightBlue: Color(0xFF6BA6E8),
    brightMagenta: Color(0xFFA78BFA),
    brightCyan: Color(0xFF3FC7B8),
    brightWhite: Color(0xFFFFFFFF),
    searchHitBackground: Palette.warn,
    searchHitBackgroundCurrent: Palette.accent,
    searchHitForeground: Palette.textOn,
  );
}
