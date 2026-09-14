import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'models/connection.dart';
import 'screens/connect_screen.dart';
import 'screens/home_screen.dart';
import 'services/store.dart';
import 'theme/app_theme.dart';
import 'theme/palette.dart';
import 'widgets/mcos_logo.dart';

void main() {
  // Flutter bağlayıcısı: SystemChrome'u runApp'ten önce çağırmak için şart.
  WidgetsFlutterBinding.ensureInitialized();

  // Durum çubuğunu koyu zeminle uyumlu yap: açık ikonlar, saydam zemin.
  // Yoksa üstte beyaz bir şerit kalır ve uygulama "yarım" görünür.
  SystemChrome.setSystemUIOverlayStyle(const SystemUiOverlayStyle(
    statusBarColor: Colors.transparent,
    statusBarIconBrightness: Brightness.light,
    systemNavigationBarColor: Palette.surface,
    systemNavigationBarIconBrightness: Brightness.light,
  ),);

  runApp(const McosApp());
}

class McosApp extends StatefulWidget {
  const McosApp({super.key});

  @override
  State<McosApp> createState() => _McosAppState();
}

class _McosAppState extends State<McosApp> {
  final _store = Store();

  List<Connection> _connections = const [];
  Connection? _active;
  bool _loading = true;
  Color _accent = Palette.accent;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final list = await _store.connections();
    final activeId = await _store.activeId();

    Connection? active;
    if (list.isNotEmpty) {
      active = list.firstWhere(
        (c) => c.id == activeId,
        // Kayıtlı etkin bağlantı silinmişse ilkine düş: kullanıcı boş bir
        // ekranla karşılaşmasın.
        orElse: () => list.first,
      );
    }

    if (!mounted) return;
    setState(() {
      _connections = list;
      _active = active;
      _accent = Palette.accentFor(active?.theme);
      _loading = false;
    });
  }

  void _setAccent(String? theme) {
    final c = Palette.accentFor(theme);
    if (c != _accent) setState(() => _accent = c);
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'MCOS',
      debugShowCheckedModeBanner: false,
      theme: AppTheme.build(_accent),
      home: _loading
          ? const _Splash()
          : _active == null
              ? _Welcome(store: _store, onAdded: _load)
              : HomeScreen(
                  // key: bağlantı değişince HomeScreen'in durumu sıfırlansın.
                  key: ValueKey(_active!.id),
                  connection: _active!,
                  store: _store,
                  onConnectionsChanged: _load,
                  onThemeChanged: _setAccent,
                ),
      // Birden çok bağlantı varsa hızlı geçiş için sürükleme menüsü yerine
      // Ayarlar ekranını kullanıyoruz; tek bir MCOS'u olan kullanıcı (çoğu)
      // fazladan bir katmanla uğraşmasın.
      builder: (context, child) => _ConnectionSwitcher(
        connections: _connections,
        active: _active,
        onPick: (c) async {
          await _store.setActiveId(c.id);
          await _load();
        },
        child: child ?? const SizedBox.shrink(),
      ),
    );
  }
}

/// Açılış ekranı: depo okunurken kısa bir an görünür.
class _Splash extends StatelessWidget {
  const _Splash();

  @override
  Widget build(BuildContext context) {
    return const Scaffold(
      body: Center(child: McosLogo(size: 72)),
    );
  }
}

/// Hiç bağlantı yokken görünen karşılama ekranı.
class _Welcome extends StatelessWidget {
  const _Welcome({required this.store, required this.onAdded});

  final Store store;
  final Future<void> Function() onAdded;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: Padding(
            padding: const EdgeInsets.all(32),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const McosLogo(size: 88),
                const SizedBox(height: 28),
                const Text(
                  'MCOS',
                  style: TextStyle(
                    color: Palette.text,
                    fontSize: 30,
                    fontWeight: FontWeight.w700,
                    letterSpacing: 2,
                  ),
                ),
                const SizedBox(height: 10),
                const Text(
                  'Minecraft sunucunuzu telefondan yönetin.',
                  textAlign: TextAlign.center,
                  style: TextStyle(color: Palette.textDim, fontSize: 14),
                ),
                const SizedBox(height: 40),
                FilledButton.icon(
                  onPressed: () async {
                    final added = await Navigator.of(context).push<Connection>(
                      MaterialPageRoute(
                        builder: (_) => ConnectScreen(store: store),
                      ),
                    );
                    if (added != null) await onAdded();
                  },
                  icon: const Icon(Icons.add),
                  label: const Text('MCOS ekle'),
                ),
                const SizedBox(height: 20),
                const Text(
                  'MCOS panelinde:  Sol menü → Uzaktan Kontrol',
                  textAlign: TextAlign.center,
                  style: TextStyle(color: Palette.textFaint, fontSize: 12),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// Birden çok MCOS varsa, üstten aşağı kaydırarak geçiş yapmayı sağlar.
///
/// ── Neden görünmez bir katman ───────────────────────────────────────────────
/// Çoğu kullanıcının tek bir MCOS'u var; onlara kalıcı bir "bağlantı seç"
/// çubuğu göstermek boş yer kaplardı. Bu katman yalnızca birden fazla
/// bağlantı varken bir düğme çizer.
class _ConnectionSwitcher extends StatelessWidget {
  const _ConnectionSwitcher({
    required this.connections,
    required this.active,
    required this.onPick,
    required this.child,
  });

  final List<Connection> connections;
  final Connection? active;
  final Future<void> Function(Connection) onPick;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    if (connections.length < 2 || active == null) return child;

    return Stack(
      children: [
        child,
        Positioned(
          right: 12,
          bottom: 86,
          child: FloatingActionButton.small(
            heroTag: 'switcher',
            backgroundColor: Palette.raised,
            foregroundColor: Palette.text,
            onPressed: () => _show(context),
            child: const Icon(Icons.swap_horiz, size: 20),
          ),
        ),
      ],
    );
  }

  void _show(BuildContext context) {
    showModalBottomSheet<void>(
      context: context,
      backgroundColor: Palette.surface,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Padding(
              padding: EdgeInsets.all(16),
              child: Text(
                'MCOS seç',
                style: TextStyle(
                    color: Palette.text, fontWeight: FontWeight.w600,),
              ),
            ),
            for (final c in connections)
              ListTile(
                leading: Icon(
                  c.id == active?.id
                      ? Icons.radio_button_checked
                      : Icons.radio_button_unchecked,
                  color: c.id == active?.id
                      ? Theme.of(ctx).colorScheme.primary
                      : Palette.textFaint,
                ),
                title: Text(c.label.isEmpty ? c.host : c.label,
                    style: const TextStyle(color: Palette.text),),
                subtitle: Text('${c.host}:${c.port}',
                    style: const TextStyle(
                        color: Palette.textDim, fontSize: 12,),),
                onTap: () {
                  Navigator.pop(ctx);
                  onPick(c);
                },
              ),
            const SizedBox(height: 8),
          ],
        ),
      ),
    );
  }
}
