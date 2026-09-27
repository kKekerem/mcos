// Otomatik yedek planı — GERÇEK mcosd yanıtlarıyla.
//
// JSON'lar elle yazılmadı: daemon'un backup.setPolicy işleyicisinin
// (internal/daemon/handlers_backup_policy.go) Europe/Istanbul diliminde
// döndürdüğü yanıtlar json.Marshal ile alınıp kopyalandı (2026-09-27).

import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:mcos_app/models/backup_policy.dart';

const realDaily =
    '''{"serverId":"srv_yedek","policy":{"auto":true,"schedule":"1d@04:00","keep":5},"summary":"Her gün 04:00 · son 5 kopya","next":"2026-09-28T04:00:00+03:00","last":"2026-09-27T07:01:03+03:00","timezone":"Europe/Istanbul"}''';

const realHourly =
    '''{"serverId":"srv_yedek","policy":{"auto":true,"schedule":"6h","keep":10},"summary":"Her 6 saatte bir · son 10 kopya","next":"2026-09-27T13:01:03+03:00","last":"2026-09-27T07:01:03+03:00","timezone":"Europe/Istanbul"}''';

const realOff =
    '''{"serverId":"srv_yedek","policy":{"auto":false,"schedule":"6h","keep":10},"summary":"Kapalı","timezone":"Europe/Istanbul"}''';

BackupPolicyInfo info(String s) =>
    BackupPolicyInfo.fromJson(jsonDecode(s) as Map<String, dynamic>);

void main() {
  // 27 Eylül 2026 12:00 İstanbul = 09:00 UTC.
  final now = DateTime.utc(2026, 9, 27, 9);

  test('backup.policy: plan, özet ve sonraki yedek', () {
    final d = info(realDaily);
    expect(d.policy.auto, isTrue);
    expect(d.policy.schedule, '1d@04:00');
    expect(d.policy.keep, 5);
    expect(d.summary, 'Her gün 04:00 · son 5 kopya');
    expect(d.nextLabel(now), 'yarın 04:00');
    expect(info(realHourly).nextLabel(now), 'bugün 13:01');
    // Kapalı planda "sonraki" yok ama plan SAKLANIR (yeniden açınca gelir).
    final off = info(realOff);
    expect(off.nextLabel(now), isNull);
    expect(off.policy.schedule, '6h');
    expect(off.summary, 'Kapalı');
  });

  test('sonraki: saat CİHAZIN diliminde, telefonunkinde değil', () {
    // Telefon New York'ta olsa da (UTC-4) cihazın 04:00'ü "04:00" yazmalı;
    // İstanbul'da 28'i 02:00 iken (UTC 27'si 23:00) 04:00 "bugün"dür.
    final lateNight = DateTime.utc(2026, 9, 27, 23);
    expect(info(realDaily).nextLabel(lateNight), 'bugün 04:00');
  });

  test('sonraki: kaçırılan, süren, sorunlu ve eskimiş yanıt', () {
    Map<String, dynamic> base() =>
        jsonDecode(realDaily) as Map<String, dynamic>;
    final due = base()
      ..['next'] = '2026-09-27T04:00:00+03:00'
      ..['due'] = true
      ..['reason'] = 'planlanan yedek kaçırıldı (27.09 04:00)';
    expect(
      BackupPolicyInfo.fromJson(due).nextLabel(now),
      'şimdi — planlanan yedek kaçırıldı (27.09 04:00)',
    );
    final stale = base()..['next'] = '2026-09-27T11:00:00+03:00';
    expect(BackupPolicyInfo.fromJson(stale).nextLabel(now), 'şimdi');
    final running = base()..['running'] = true;
    expect(
        BackupPolicyInfo.fromJson(running).nextLabel(now), 'şu an alınıyor…',);
    final problem = base()
      ..remove('next')
      ..['problem'] =
          'sistem saati yanlış (2010-01-01) — NTP eşitlemesi bekleniyor';
    expect(
      BackupPolicyInfo.fromJson(problem).nextLabel(now),
      'bekliyor — sistem saati yanlış (2010-01-01) — NTP eşitlemesi bekleniyor',
    );
    final later = base()..['next'] = '2026-10-02T03:30:00+03:00';
    expect(
        BackupPolicyInfo.fromJson(later).nextLabel(now), '2 Ekim Cuma 03:30',);
  });

  test('plan dizgisi: eski "6h" ve yeni biçimler gidip geliyor', () {
    for (final s in [
      '6h',
      '12h',
      '1d',
      '7d',
      '1d@04:00',
      '2d@23:59',
      '7d@03:30',
    ]) {
      expect(BackupPlan.parse(s)?.schedule, s, reason: s);
    }
    expect(BackupPlan.parse('1D@4:00')?.schedule, '1d@04:00');
    expect(BackupPlan.parse('90m'), isNull);
    expect(BackupPlan.parse(''), isNull);
    expect(const BackupPlan.days(1, atMinute: 210).schedule, '1d@03:30');
    expect(const BackupPlan.hours(6).schedule, '6h');
  });

  test('etiketler panelle aynı sözcükler', () {
    expect(backupHoursLabel(1), 'Her saat');
    expect(backupHoursLabel(6), 'Her 6 saatte bir');
    expect(backupDaysLabel(1), 'Her gün');
    expect(backupDaysLabel(3), 'Her 3 günde bir');
    expect(backupDaysLabel(7), 'Her hafta');
    expect(backupKeepLabel(0), 'Sınırsız');
    expect(backupKeepLabel(5), 'Son 5');
  });
}
