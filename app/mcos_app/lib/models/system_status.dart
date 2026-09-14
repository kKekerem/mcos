/// MCOS makinesinin anlık durumu (system.status yanıtı).
class SystemStatus {
  const SystemStatus({
    required this.systemName,
    required this.version,
    required this.uptimeSec,
    required this.tier,
    required this.cpuModel,
    required this.cpuCores,
    required this.cpuUsagePct,
    required this.memTotalBytes,
    required this.memUsedBytes,
    required this.memUsagePct,
    required this.internet,
    required this.addresses,
  });

  final String systemName;
  final String version;
  final int uptimeSec;
  final String tier;

  final String cpuModel;
  final int cpuCores;
  final double cpuUsagePct;

  final int memTotalBytes;
  final int memUsedBytes;
  final double memUsagePct;

  final bool internet;
  final List<String> addresses;

  /// Çalışma süresini "3g 4s 12d" gibi okunur bir metne çevirir.
  String get uptimeLabel {
    final d = Duration(seconds: uptimeSec);
    final days = d.inDays;
    final hours = d.inHours % 24;
    final mins = d.inMinutes % 60;
    if (days > 0) return '${days}g ${hours}s';
    if (hours > 0) return '${hours}s ${mins}d';
    return '${mins}d';
  }

  static SystemStatus fromJson(Map<String, dynamic> j) {
    final cpu = (j['cpu'] as Map<String, dynamic>?) ?? const {};
    final mem = (j['memory'] as Map<String, dynamic>?) ?? const {};
    final net = (j['net'] as Map<String, dynamic>?) ?? const {};

    // Adresler farklı biçimlerde gelebilir; hepsini metne çeviriyoruz.
    final addrs = <String>[];
    final rawAddrs = net['addresses'];
    if (rawAddrs is List) {
      for (final a in rawAddrs) {
        if (a is String && a.isNotEmpty) addrs.add(a);
      }
    }

    return SystemStatus(
      systemName: j['systemName'] as String? ?? 'MCOS',
      version: j['version'] as String? ?? '',
      uptimeSec: (j['uptimeSec'] as num?)?.toInt() ?? 0,
      tier: j['tier'] as String? ?? '',
      cpuModel: cpu['model'] as String? ?? '',
      cpuCores: (cpu['cores'] as num?)?.toInt() ?? 0,
      cpuUsagePct: (cpu['usagePct'] as num?)?.toDouble() ?? 0,
      memTotalBytes: (mem['totalBytes'] as num?)?.toInt() ?? 0,
      memUsedBytes: (mem['usedBytes'] as num?)?.toInt() ?? 0,
      memUsagePct: (mem['usagePct'] as num?)?.toDouble() ?? 0,
      internet: net['internet'] as bool? ?? false,
      addresses: addrs,
    );
  }
}

/// Bayt sayısını okunur bir metne çevirir.
String formatBytes(int bytes) {
  if (bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  var value = bytes.toDouble();
  var unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  // GB ve üstünde bir ondalık: "15.5 GB" okunur, "15.48 GB" gürültü.
  return value >= 100 || unit == 0
      ? '${value.round()} ${units[unit]}'
      : '${value.toStringAsFixed(1)} ${units[unit]}';
}
