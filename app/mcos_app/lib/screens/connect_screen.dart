import 'dart:io';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../models/connection.dart';
import '../services/rpc_client.dart';
import '../services/store.dart';
import '../theme/palette.dart';
import '../widgets/mcos_logo.dart';

/// Yeni bir MCOS bağlantısı ekleme ekranı.
///
/// ── Akış ────────────────────────────────────────────────────────────────────
/// 1. Kullanıcı IP ve jetonu girer (ikisi de MCOS panelinde yazılıdır),
/// 2. "Bağlan" önce /health çağırır: adres doğru mu, orada MCOS var mı,
/// 3. sertifikanın parmak izi gösterilir ve KAYDEDİLİR (sabitleme),
/// 4. sonra jetonla gerçek bir çağrı yapılır: jeton doğru mu,
/// 5. her ikisi de geçerse bağlantı kaydedilir.
///
/// Neden iki aşama: "adres yanlış" ile "jeton yanlış" bambaşka sorunlardır ve
/// kullanıcı hangisi olduğunu bilmeli. Tek bir "bağlanamadı" mesajı, insanı
/// yanlış yerde arattırır.
class ConnectScreen extends StatefulWidget {
  const ConnectScreen({super.key, required this.store, this.existing});

  final Store store;

  /// Düzenleme kipinde dolu; yeni bağlantıda null.
  final Connection? existing;

  @override
  State<ConnectScreen> createState() => _ConnectScreenState();
}

class _ConnectScreenState extends State<ConnectScreen> {
  final _formKey = GlobalKey<FormState>();
  late final TextEditingController _label;
  late final TextEditingController _host;
  late final TextEditingController _port;
  late final TextEditingController _token;

  bool _busy = false;
  String? _error;
  String? _info;

  @override
  void initState() {
    super.initState();
    final e = widget.existing;
    _label = TextEditingController(text: e?.label ?? '');
    _host = TextEditingController(text: e?.host ?? '');
    _port = TextEditingController(text: (e?.port ?? 2223).toString());
    _token = TextEditingController(text: e?.token ?? '');
  }

  @override
  void dispose() {
    _label.dispose();
    _host.dispose();
    _port.dispose();
    _token.dispose();
    super.dispose();
  }

  Future<void> _connect() async {
    if (!(_formKey.currentState?.validate() ?? false)) return;

    setState(() {
      _busy = true;
      _error = null;
      _info = null;
    });

    final draft = Connection(
      id: widget.existing?.id ?? _newId(),
      label: _label.text.trim(),
      host: _host.text.trim(),
      port: int.parse(_port.text.trim()),
      token: _token.text.trim(),
      // Düzenleme kipinde eski parmak izini KORUYORUZ: adres değişmediyse
      // sertifika da değişmemeli ve değiştiyse kullanıcı uyarılmalı.
      fingerprint: widget.existing?.fingerprint,
    );

    final client = RpcClient(draft);
    try {
      // 1. Adres doğru mu?
      final health = await client.health();
      setState(() => _info = '${health.name} bulundu (MCOS ${health.version})');

      // 2. Sertifika: ilk kez görüyorsak sabitle.
      final seen = client.lastSeenFingerprint ?? health.fingerprint;
      final pinned = draft.fingerprint;
      if (pinned != null && pinned.isNotEmpty && pinned != seen) {
        throw RpcException(
          'Sunucunun kimliği DEĞİŞTİ.\n\nBeklenen:\n$pinned\n\nGelen:\n$seen\n\n'
          'MCOS yeniden kurulduysa bu normaldir; bağlantıyı silip yeniden '
          'ekleyin. Kurulmadıysa ağınızda araya giren biri olabilir.',
        );
      }

      // 3. Jeton doğru mu? En ucuz kimlikli çağrı.
      final withPin = draft.copyWith(fingerprint: seen);
      final verify = RpcClient(withPin);
      String? theme;
      try {
        await verify.call('ping');
        // Tema: telefon, panelle aynı vurgu rengini kullansın.
        final cfg = await verify.callMap('config.get');
        theme = cfg['theme'] as String?;
      } finally {
        verify.close();
      }

      final saved = withPin.copyWith(
        theme: theme,
        label: withPin.label.isEmpty ? health.name : withPin.label,
      );
      await widget.store.upsert(saved);
      await widget.store.setActiveId(saved.id);

      if (!mounted) return;
      Navigator.of(context).pop(saved);
    } on RpcException catch (e) {
      setState(() => _error = e.message);
    } on SocketException catch (e) {
      // En sık hata bu: yanlış IP ya da MCOS kapalı. Teknik metni
      // göstermek yerine ne yapılacağını söylüyoruz.
      setState(() => _error =
          'Bağlanılamadı: ${_host.text.trim()}:${_port.text.trim()}\n\n'
          'MCOS açık mı ve telefon aynı ağda mı? Panelde '
          '"Uzaktan Kontrol" açık olmalı.\n\n(${e.osError?.message ?? e.message})',);
    } on HandshakeException {
      setState(() => _error =
          'Güvenli bağlantı kurulamadı. Bu portta MCOS yoksa ya da '
          'sertifika değiştiyse böyle olur.',);
    } catch (e) {
      setState(() => _error = 'Beklenmeyen hata: $e');
    } finally {
      client.close();
      if (mounted) setState(() => _busy = false);
    }
  }

  static String _newId() {
    final r = Random.secure();
    return List.generate(8, (_) => r.nextInt(256).toRadixString(16).padLeft(2, '0'))
        .join();
  }

  @override
  Widget build(BuildContext context) {
    final editing = widget.existing != null;
    return Scaffold(
      appBar: AppBar(title: Text(editing ? 'Bağlantıyı Düzenle' : 'MCOS Ekle')),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(20, 8, 20, 32),
          child: Form(
            key: _formKey,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                if (!editing) ...[
                  const SizedBox(height: 12),
                  const Center(child: McosLogo(size: 64)),
                  const SizedBox(height: 20),
                  const Text(
                    'MCOS panelinde:  Sol menü → Uzaktan Kontrol',
                    textAlign: TextAlign.center,
                    style: TextStyle(color: Palette.textDim),
                  ),
                  const SizedBox(height: 24),
                ],
                TextFormField(
                  controller: _host,
                  autocorrect: false,
                  keyboardType: TextInputType.url,
                  textInputAction: TextInputAction.next,
                  decoration: const InputDecoration(
                    labelText: 'IP adresi',
                    hintText: '192.168.1.20',
                    prefixIcon: Icon(Icons.dns_outlined),
                  ),
                  validator: (v) {
                    final s = v?.trim() ?? '';
                    if (s.isEmpty) return 'Adres gerekli';
                    if (s.contains(' ')) return 'Adres boşluk içeremez';
                    return null;
                  },
                ),
                const SizedBox(height: 14),
                TextFormField(
                  controller: _port,
                  keyboardType: TextInputType.number,
                  inputFormatters: [FilteringTextInputFormatter.digitsOnly],
                  textInputAction: TextInputAction.next,
                  decoration: const InputDecoration(
                    labelText: 'Port',
                    prefixIcon: Icon(Icons.settings_ethernet),
                    helperText: 'MCOS varsayılanı: 2223',
                  ),
                  validator: (v) {
                    final n = int.tryParse(v?.trim() ?? '');
                    if (n == null || n < 1 || n > 65535) {
                      return '1–65535 arası bir sayı';
                    }
                    return null;
                  },
                ),
                const SizedBox(height: 14),
                TextFormField(
                  controller: _token,
                  autocorrect: false,
                  maxLines: 2,
                  minLines: 1,
                  decoration: const InputDecoration(
                    labelText: 'Jeton',
                    hintText: 'panelde yazan uzun kod',
                    prefixIcon: Icon(Icons.key_outlined),
                  ),
                  validator: (v) {
                    final s = v?.trim() ?? '';
                    if (s.isEmpty) return 'Jeton gerekli';
                    if (s.length < 8) return 'Jeton eksik görünüyor';
                    return null;
                  },
                ),
                const SizedBox(height: 14),
                TextFormField(
                  controller: _label,
                  textInputAction: TextInputAction.done,
                  decoration: const InputDecoration(
                    labelText: 'Ad (isteğe bağlı)',
                    hintText: 'Salon PC',
                    prefixIcon: Icon(Icons.label_outline),
                  ),
                ),
                const SizedBox(height: 24),
                FilledButton.icon(
                  onPressed: _busy ? null : _connect,
                  icon: _busy
                      ? const SizedBox(
                          width: 18,
                          height: 18,
                          child: CircularProgressIndicator(
                            strokeWidth: 2,
                            color: Palette.textOn,
                          ),
                        )
                      : const Icon(Icons.link),
                  label: Text(_busy ? 'Bağlanılıyor…' : 'Bağlan'),
                ),
                if (_info != null) ...[
                  const SizedBox(height: 16),
                  _Banner(text: _info!, color: Palette.ok, icon: Icons.check_circle_outline),
                ],
                if (_error != null) ...[
                  const SizedBox(height: 16),
                  _Banner(text: _error!, color: Palette.error, icon: Icons.error_outline),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _Banner extends StatelessWidget {
  const _Banner({required this.text, required this.color, required this.icon});

  final String text;
  final Color color;
  final IconData icon;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.10),
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: color.withValues(alpha: 0.5)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, color: color, size: 20),
          const SizedBox(width: 10),
          Expanded(
            child: SelectableText(
              text,
              style: const TextStyle(color: Palette.text, fontSize: 13),
            ),
          ),
        ],
      ),
    );
  }
}
