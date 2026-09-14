import 'dart:async';

import 'package:flutter/material.dart';

import '../models/connection.dart';
import '../models/server.dart';
import '../models/system_status.dart';
import '../services/rpc_client.dart';
import '../services/store.dart';
import '../theme/palette.dart';
import 'dashboard_tab.dart';
import 'servers_tab.dart';
import 'settings_tab.dart';
import 'ssh_tab.dart';

/// Bağlı bir MCOS'un ana ekranı: dört sekme.
///
/// ── Neden veri burada toplanıyor ────────────────────────────────────────────
/// Gösterge paneli ve sunucu listesi AYNI iki RPC'yi kullanıyor. Her sekme
/// kendi başına sorsaydı, sekme değiştirmek her seferinde yeni bir istek
/// demek olurdu ve telefon tek bir bağlantı üzerinden sırayla konuştuğu için
/// bu gözle görülür bir gecikme yaratırdı.
///
/// Tek bir yoklama döngüsü var; sekmeler yalnızca çiziyor.
class HomeScreen extends StatefulWidget {
  const HomeScreen({
    super.key,
    required this.connection,
    required this.store,
    required this.onConnectionsChanged,
    required this.onThemeChanged,
  });

  final Connection connection;
  final Store store;

  /// Bağlantı silindiğinde/değiştiğinde kök widget'ı haberdar eder.
  final Future<void> Function() onConnectionsChanged;

  /// Sunucunun teması değiştiğinde uygulamanın vurgu rengini günceller.
  final void Function(String? theme) onThemeChanged;

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  late RpcClient _client;
  Timer? _poll;

  int _tab = 0;

  SystemStatus? _status;
  List<GameServer> _servers = const [];
  String? _error;
  bool _firstLoadDone = false;

  /// Aynı anda iki yoklama olmasın: RPC bağlantısı tek ve sıralı.
  bool _polling = false;

  @override
  void initState() {
    super.initState();
    _client = RpcClient(widget.connection);
    _refresh();
    // 3 saniye: durum panelde de ~2 saniyede bir yenileniyor. Telefonda
    // biraz daha seyrek, çünkü mobil veri ve pil söz konusu.
    _poll = Timer.periodic(const Duration(seconds: 3), (_) => _refresh());
  }

  @override
  void didUpdateWidget(covariant HomeScreen old) {
    super.didUpdateWidget(old);
    if (old.connection.id != widget.connection.id ||
        old.connection.host != widget.connection.host ||
        old.connection.token != widget.connection.token) {
      _client.close();
      _client = RpcClient(widget.connection);
      setState(() {
        _status = null;
        _servers = const [];
        _firstLoadDone = false;
      });
      _refresh();
    }
  }

  @override
  void dispose() {
    _poll?.cancel();
    _client.close();
    super.dispose();
  }

  Future<void> _refresh() async {
    if (_polling || !mounted) return;
    _polling = true;
    try {
      final status = await _client.callMap('system.status');
      final servers = await _client.callMap('server.list');
      if (!mounted) return;
      setState(() {
        _status = SystemStatus.fromJson(status);
        _servers = GameServer.listFrom(servers['servers']);
        _error = null;
        _firstLoadDone = true;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e is RpcException ? e.message : e.toString();
        _firstLoadDone = true;
      });
    } finally {
      _polling = false;
    }
  }

  /// Bir eylemi çalıştırır, sonucu bildirir ve listeyi tazeler.
  Future<void> runAction(
    String method, {
    Map<String, dynamic>? params,
    String? okMessage,
  }) async {
    try {
      await _client.call(method, params);
      if (okMessage != null) _toast(okMessage, Palette.ok);
      await _refresh();
    } catch (e) {
      _toast(e is RpcException ? e.message : e.toString(), Palette.error);
    }
  }

  /// Sonucu doğrudan isteyen çağrılar (konsol okuma gibi).
  Future<Map<String, dynamic>> query(String method,
      [Map<String, dynamic>? params,]) {
    return _client.callMap(method, params);
  }

  void _toast(String message, Color color) {
    if (!mounted) return;
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(
        SnackBar(
          content: Text(message),
          backgroundColor: Palette.raised,
          duration: Duration(seconds: color == Palette.error ? 6 : 3),
        ),
      );
  }

  @override
  Widget build(BuildContext context) {
    final tabs = [
      DashboardTab(
        status: _status,
        servers: _servers,
        error: _error,
        loading: !_firstLoadDone,
        connection: widget.connection,
        onRefresh: _refresh,
      ),
      ServersTab(
        servers: _servers,
        loading: !_firstLoadDone,
        onRefresh: _refresh,
        onAction: runAction,
        query: query,
      ),
      SshTab(connection: widget.connection, store: widget.store),
      SettingsTab(
        connection: widget.connection,
        store: widget.store,
        query: query,
        onAction: runAction,
        onConnectionsChanged: widget.onConnectionsChanged,
        onThemeChanged: widget.onThemeChanged,
      ),
    ];

    return Scaffold(
      body: SafeArea(child: tabs[_tab]),
      bottomNavigationBar: NavigationBar(
        selectedIndex: _tab,
        onDestinationSelected: (i) => setState(() => _tab = i),
        destinations: const [
          NavigationDestination(
            icon: Icon(Icons.speed_outlined),
            selectedIcon: Icon(Icons.speed),
            label: 'Durum',
          ),
          NavigationDestination(
            icon: Icon(Icons.dns_outlined),
            selectedIcon: Icon(Icons.dns),
            label: 'Sunucular',
          ),
          NavigationDestination(
            icon: Icon(Icons.terminal_outlined),
            selectedIcon: Icon(Icons.terminal),
            label: 'SSH',
          ),
          NavigationDestination(
            icon: Icon(Icons.settings_outlined),
            selectedIcon: Icon(Icons.settings),
            label: 'Ayarlar',
          ),
        ],
      ),
    );
  }
}
