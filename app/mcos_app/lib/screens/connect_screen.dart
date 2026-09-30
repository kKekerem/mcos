import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../models/connection.dart';
import '../services/errors.dart';
import '../services/pair_uri.dart';
import '../services/pairing.dart';
import '../services/store.dart';
import '../theme/palette.dart';
import '../widgets/mcos_logo.dart';
import 'qr_scan_screen.dart';
import '../services/panel_text.dart';

/// Yeni bir MCOS bağlantısı ekleme ekranı.
///
/// ── Akış ────────────────────────────────────────────────────────────────────
/// İki yol var, ikisi de aynı doğrulamadan geçer (services/pairing.dart):
///
///  * QR ile (önerilen): panelde Ayarlar → Uzaktan kontrol → "QR ile bağlan". QR adres
///    adaylarını, portu, jetonu VE sertifika parmak izini taşır; telefon
///    jetonu göndermeden önce karşı tarafın kimliğini doğrular.
///  * Elle: IP, port ve jeton panelden okunup yazılır. Adres alanına
///    "IP:port", "https://…" ya da panelden kopyalanmış mcos:// kodu da
///    yapıştırılabilir.
///
/// Neden iki aşama: "adres yanlış" ile "jeton yanlış" bambaşka sorunlardır ve
/// kullanıcı hangisi olduğunu bilmeli. Tek bir "bağlanamadı" mesajı, insanı
/// yanlış yerde arattırır.
class ConnectScreen extends StatefulWidget {
  const ConnectScreen({
    super.key,
    required this.store,
    this.existing,
    this.startWithQr = false,
  });

  final Store store;

  /// Düzenleme kipinde dolu; yeni bağlantıda null.
  final Connection? existing;

  /// Ekran açılır açılmaz tarayıcıyı başlat. Karşılama ekranındaki ve
  /// Ayarlar'daki "QR kodu tara" düğmeleri bunu kullanıyor: kullanıcı QR
  /// istediğini zaten söyledi, onu bir form ekranından geçirip ikinci kez
  /// düğme aratmak "QR kod yeri yok" şikâyetinin ta kendisiydi.
  final bool startWithQr;

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

  /// QR'dan gelen ek bilgiler: diğer adres adayları ve beklenen parmak izi.
  /// Kullanıcı adres alanını elle değiştirirse geçersiz sayılır.
  PairInfo? _pair;

  @override
  void initState() {
    super.initState();
    final e = widget.existing;
    _label = TextEditingController(text: e?.label ?? '');
    _host = TextEditingController(text: e?.host ?? '');
    _port = TextEditingController(text: (e?.port ?? 2223).toString());
    _token = TextEditingController(text: e?.token ?? '');
    if (widget.startWithQr) {
      // İlk kare çizildikten sonra: Navigator.push initState içinde
      // çağrılamaz (bu ekranın rotası henüz yerleşmedi).
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _scanQr();
      });
    }
  }

  @override
  void dispose() {
    _label.dispose();
    _host.dispose();
    _port.dispose();
    _token.dispose();
    super.dispose();
  }

  /// Tarayıcıyı açar; okunan kodla alanları doldurup hemen bağlanır.
  Future<void> _scanQr() async {
    final info = await Navigator.of(context).push<PairInfo>(
      MaterialPageRoute(builder: (_) => const QrScanScreen()),
    );
    if (info == null || !mounted) return;
    _applyPair(info);
    await _connect();
  }

  void _applyPair(PairInfo info) {
    setState(() {
      _pair = info;
      _host.text = info.hosts.first;
      _port.text = info.port.toString();
      _token.text = info.token;
      _error = null;
      _info = null;
    });
  }

  Future<void> _connect() async {
    // Adres alanına mcos:// kodu yapıştırıldıysa QR okutulmuş gibi davran:
    // kamerası çalışmayan kullanıcı kodu başka bir yoldan kopyalayabilir.
    final pasted = _host.text.trim();
    if (looksLikePairUri(pasted)) {
      try {
        _applyPair(parsePairUri(pasted));
      } on PairFormatException catch (e) {
        setState(() => _error = e.message);
        return;
      }
    }
    if (!(_formKey.currentState?.validate() ?? false)) return;

    setState(() {
      _busy = true;
      _error = null;
      _info = null;
    });

    try {
      final input = parseHostInput(_host.text);
      // Adresle birlikte yazılmış port (192.168.1.20:2223) port alanından
      // önce gelir: kullanıcı onu bilerek yazdı.
      final port = input.port ?? int.parse(_port.text.trim());

      // QR bilgisi yalnızca adres hâlâ QR'daki adreslerden biriyse geçerli.
      final pair = _pair;
      final fromQr = pair != null &&
          pair.hosts.contains(input.host) &&
          pair.port == port;
      final hosts = fromQr
          ? [input.host, ...pair.hosts.where((h) => h != input.host)]
          : [input.host];

      final saved = await Pairing.connect(
        id: widget.existing?.id ?? _newId(),
        label: _label.text,
        hosts: hosts,
        port: port,
        token: _token.text,
        expectedFingerprint: fromQr ? pair.fingerprint : null,
        // Düzenleme kipinde eski parmak izini KORUYORUZ: sertifika
        // değişmemeli, değiştiyse kullanıcı uyarılmalı.
        pinnedFingerprint: widget.existing?.fingerprint,
        sshPort: widget.existing?.sshPort ?? 22,
        sshUser: widget.existing?.sshUser ?? 'root',
        onProgress: (m) {
          if (mounted) setState(() => _info = m);
        },
      );

      await widget.store.upsert(saved);
      await widget.store.setActiveId(saved.id);

      if (!mounted) return;
      Navigator.of(context).pop(saved);
    } catch (e) {
      // friendlyError: hiçbir yol ham istisna metni göstermiyor.
      if (mounted) {
        setState(() {
          _info = null;
          _error = friendlyError(e, host: _host.text.trim());
        });
      }
    } finally {
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
                    'MCOS panelinde:  $panelRemotePath',
                    textAlign: TextAlign.center,
                    style: TextStyle(color: Palette.textDim),
                  ),
                  const SizedBox(height: 20),
                ],
                // QR EN ÜSTTE ve BÜYÜK: elle 32 haneli jeton yazmak istisna
                // olmalı. Küçük bir düğme formun arasında kayboluyordu.
                FilledButton.icon(
                  onPressed: _busy ? null : _scanQr,
                  icon: const Icon(Icons.qr_code_scanner, size: 28),
                  label: const Text('QR kodu tara'),
                  style: FilledButton.styleFrom(
                    minimumSize: const Size.fromHeight(60),
                    textStyle: const TextStyle(
                        fontSize: 17, fontWeight: FontWeight.w600,),
                  ),
                ),
                const SizedBox(height: 8),
                const Text(
                  'Panelde: $panelQrPath',
                  textAlign: TextAlign.center,
                  style: TextStyle(color: Palette.textFaint, fontSize: 12),
                ),
                const SizedBox(height: 18),
                const Row(
                  children: [
                    Expanded(child: Divider()),
                    Padding(
                      padding: EdgeInsets.symmetric(horizontal: 10),
                      child: Text('ya da elle girin',
                          style: TextStyle(color: Palette.textDim, fontSize: 12),),
                    ),
                    Expanded(child: Divider()),
                  ],
                ),
                const SizedBox(height: 14),
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
                    try {
                      parseHostInput(v ?? '');
                      return null;
                    } on PairFormatException catch (e) {
                      return e.message;
                    }
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
                    // Boşluklar sayılmaz: panel jetonu iki grup hâlinde,
                    // aralarında boşlukla gösteriyor.
                    final s = cleanToken(v ?? '');
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
                            color: Palette.text,
                          ),
                        )
                      : const Icon(Icons.link),
                  label: Text(_busy ? 'Bağlanılıyor…' : 'Bağlan'),
                  style: FilledButton.styleFrom(
                    backgroundColor: Palette.raised,
                    foregroundColor: Palette.text,
                  ),
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
