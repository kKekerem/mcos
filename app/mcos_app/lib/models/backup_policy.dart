/// Otomatik yedek planı (backup.policy / backup.setPolicy yanıtı).
///
/// Kullanıcının isteği: "yedek alma saat/gün aralığını da ayarlayalım".
///
/// ── Neden özet ve "sonraki" daemon'dan ─────────────────────────────────────
/// "Her gün 04:00 · son 5 kopya" metni ve bir sonraki yedeğin zamanı
/// daemon'un KENDİ zamanlayıcı kuralıyla hesaplanıp gelir. Uygulama aynı
/// kuralı ikinci kez yazsaydı, telefonda yazan saat ile yedeğin gerçekten
/// alındığı saat zamanla ayrışırdı (saat dilimi, kaçırılan yedek, yaz saati).
/// Burada yalnızca GÖSTERİM var: bugün/yarın ve seçimlerin ön işaretlenmesi.
library;

/// Panelle AYNI seçenekler (model.BackupHourChoices vb.).
const backupHourChoices = [1, 2, 3, 4, 6, 8, 12];
const backupDayChoices = [1, 2, 3, 7];
const backupKeepChoices = [3, 5, 10, 20, 0];

/// Sunucu kaydındaki plan.
class BackupPolicy {
  const BackupPolicy({
    required this.auto,
    required this.schedule,
    required this.keep,
  });

  final bool auto;

  /// "6h", "1d", "2d@04:00" (bkz. model.ParseBackupSchedule).
  final String schedule;

  /// Saklanacak kopya; 0 = sınırsız.
  final int keep;

  static BackupPolicy fromJson(Object? raw) {
    final j = raw is Map<String, dynamic> ? raw : const <String, dynamic>{};
    return BackupPolicy(
      auto: j['auto'] == true,
      schedule: j['schedule'] as String? ?? '',
      keep: (j['keep'] as num?)?.toInt() ?? 0,
    );
  }
}

/// Planın seçim ekranındaki hâli: saat ya da gün aralığı, isteğe bağlı saat.
class BackupPlan {
  const BackupPlan.hours(this.hours)
      : days = 0,
        atMinute = null;

  const BackupPlan.days(this.days, {this.atMinute}) : hours = 0;

  /// > 0 ise saat aralığı.
  final int hours;

  /// > 0 ise gün aralığı.
  final int days;

  /// Gece yarısından beri dakika; null = saat sabit değil.
  final int? atMinute;

  bool get isDays => days > 0;

  /// Daemon'un kanonik biçimi ("6h", "1d@04:00"). Doğrulama daemon'da:
  /// geçersiz bir değer Türkçe bir hatayla döner ve ekranda gösterilir.
  String get schedule {
    if (!isDays) return '${hours}h';
    final at = atMinute;
    return at == null ? '${days}d' : '${days}d@${clock(at)}';
  }

  /// Kayıtlı planı çözer. Seçeneklerde olmayan biçimler ("90m") null döner:
  /// ekran o zaman hiçbir seçimi "şu anki" diye işaretlemez.
  static BackupPlan? parse(String s) {
    final t = s.trim().toLowerCase();
    final h = RegExp(r'^(\d+)h$').firstMatch(t);
    if (h != null) return BackupPlan.hours(int.parse(h.group(1)!));
    final d = RegExp(r'^(\d+)d(?:@(\d{1,2}):(\d{2}))?$').firstMatch(t);
    if (d == null) return null;
    final days = int.parse(d.group(1)!);
    if (d.group(2) == null) return BackupPlan.days(days);
    return BackupPlan.days(
      days,
      atMinute: int.parse(d.group(2)!) * 60 + int.parse(d.group(3)!),
    );
  }

  static String clock(int minute) =>
      '${(minute ~/ 60).toString().padLeft(2, '0')}:'
      '${(minute % 60).toString().padLeft(2, '0')}';
}

/// backup.policy yanıtı.
class BackupPolicyInfo {
  const BackupPolicyInfo({
    required this.policy,
    required this.summary,
    this.next,
    this.due = false,
    this.reason = '',
    this.running = false,
    this.problem = '',
  });

  final BackupPolicy policy;

  /// "Her 6 saatte bir · son 5 kopya" ya da "Kapalı".
  final String summary;

  /// RFC 3339, DAEMON'un saat dilimi ofsetiyle ("2026-09-28T04:00:00+03:00").
  final String? next;
  final bool due;
  final String reason;
  final bool running;
  final String problem;

  static BackupPolicyInfo fromJson(Map<String, dynamic> j) => BackupPolicyInfo(
        policy: BackupPolicy.fromJson(j['policy']),
        summary: j['summary'] as String? ?? '',
        next: j['next'] as String?,
        due: j['due'] == true,
        reason: j['reason'] as String? ?? '',
        running: j['running'] == true,
        problem: j['problem'] as String? ?? '',
      );

  /// "bugün 16:00", "yarın 04:00", "2 Ekim Cuma 03:30", "şimdi — …".
  /// null = gösterilecek bir şey yok (plan kapalı).
  ///
  /// Saat DAEMON'un diliminde yazılır, telefonunkinde değil: "her gün 04:00"
  /// planı cihazın saatiyle 04:00'tür. Telefon başka bir dilimdeyken (yurt
  /// dışında) 04:00'ü 22:00 diye göstermek kullanıcıyı yanıltırdı.
  String? nextLabel(DateTime now) {
    if (!policy.auto) return null;
    if (problem.isNotEmpty) return 'bekliyor — $problem';
    if (running) return 'şu an alınıyor…';
    final raw = next;
    if (raw == null) return null;
    final DateTime at;
    try {
      at = DateTime.parse(raw);
    } on FormatException {
      return null;
    }
    // Yanıt eskidiyse (ekran uzun süre açık kaldı) geçmiş bir saati "bugün
    // 04:00" diye yazmak yedeğin kaçtığını sandırır.
    if (due || !now.isBefore(at)) {
      return due && reason.isNotEmpty ? 'şimdi — $reason' : 'şimdi';
    }
    final offset = _offsetOf(raw);
    final wall = at.toUtc().add(offset);
    final today = now.toUtc().add(offset);
    final clock = BackupPlan.clock(wall.hour * 60 + wall.minute);
    if (_sameDay(wall, today)) return 'bugün $clock';
    final tomorrow = DateTime.utc(today.year, today.month, today.day + 1);
    if (_sameDay(wall, tomorrow)) return 'yarın $clock';
    final month = _months[wall.month - 1];
    if (wall.year != today.year) {
      return '${wall.day} $month ${wall.year} $clock';
    }
    return '${wall.day} $month ${_weekdays[wall.weekday - 1]} $clock';
  }

  static bool _sameDay(DateTime a, DateTime b) =>
      a.year == b.year && a.month == b.month && a.day == b.day;

  /// RFC 3339 dizgisinin sonundaki ofset ("+03:00", "Z").
  static Duration _offsetOf(String raw) {
    final m = RegExp(r'([+-])(\d{2}):(\d{2})$').firstMatch(raw);
    if (m == null) return Duration.zero;
    final d = Duration(
      hours: int.parse(m.group(2)!),
      minutes: int.parse(m.group(3)!),
    );
    return m.group(1) == '-' ? -d : d;
  }

  static const _months = [
    'Ocak', 'Şubat', 'Mart', 'Nisan', 'Mayıs', 'Haziran', //
    'Temmuz', 'Ağustos', 'Eylül', 'Ekim', 'Kasım', 'Aralık',
  ];

  // DateTime.weekday: 1 = Pazartesi.
  static const _weekdays = [
    'Pazartesi',
    'Salı',
    'Çarşamba',
    'Perşembe',
    'Cuma',
    'Cumartesi',
    'Pazar',
  ];
}

/// Seçenek etiketleri (panelle aynı sözcükler).
String backupHoursLabel(int h) => h == 1 ? 'Her saat' : 'Her $h saatte bir';

String backupDaysLabel(int d) => switch (d) {
      1 => 'Her gün',
      7 => 'Her hafta',
      _ => 'Her $d günde bir',
    };

String backupKeepLabel(int k) => k <= 0 ? 'Sınırsız' : 'Son $k';
