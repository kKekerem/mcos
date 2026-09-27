import 'dart:async';

import 'package:flutter/material.dart';

import '../models/backup_policy.dart';
import '../services/errors.dart';
import '../theme/palette.dart';

/// Sorgu işlevi: HomeScreen.query ile aynı imza.
typedef BackupQuery = Future<Map<String, dynamic>> Function(
  String method, [
  Map<String, dynamic>? params,
]);

/// Sunucu ayrıntısındaki "Otomatik yedek" şeridi; dokununca plan açılır.
///
/// Panelin Yedekler sekmesiyle aynı bilgi: plan ve bir sonraki yedek.
/// Telefon, cihazın başında olmayan kullanıcının tek denetim yolu; planı
/// yalnızca panelden değiştirilebilir kılmak o kullanıcıyı yedeksiz bırakırdı.
class BackupPolicyTile extends StatefulWidget {
  const BackupPolicyTile({
    super.key,
    required this.serverId,
    required this.query,
  });

  final String serverId;
  final BackupQuery query;

  @override
  State<BackupPolicyTile> createState() => _BackupPolicyTileState();
}

class _BackupPolicyTileState extends State<BackupPolicyTile> {
  BackupPolicyInfo? _info;
  String? _error;
  Timer? _poll;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
    // Dakikada bir: yedek alınınca "sonraki" değişir. Daha sık sormak, konsol
    // yoklamasının yanında mobil veriye boşuna yük olurdu.
    _poll = Timer.periodic(const Duration(minutes: 1), (_) => _load());
  }

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final r = await widget.query(
        'backup.policy',
        {'serverId': widget.serverId},
      );
      if (!mounted) return;
      setState(() {
        _info = BackupPolicyInfo.fromJson(r);
        _error = null;
      });
    } catch (e) {
      if (mounted) setState(() => _error = friendlyError(e));
    }
  }

  Future<void> _edit() async {
    final info = _info;
    if (info == null) return;
    final saved = await showModalBottomSheet<BackupPolicyInfo>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Palette.surface,
      builder: (_) => BackupPolicySheet(
        serverId: widget.serverId,
        initial: info.policy,
        query: widget.query,
      ),
    );
    if (saved != null && mounted) setState(() => _info = saved);
  }

  @override
  Widget build(BuildContext context) {
    final info = _info;
    final next = info?.nextLabel(DateTime.now());
    final String line;
    if (info != null) {
      line = next == null ? info.summary : '${info.summary}\nSonraki: $next';
    } else {
      line = _error ?? 'Yükleniyor…';
    }
    return Material(
      color: Palette.surface,
      child: InkWell(
        onTap: info == null ? null : _edit,
        child: Container(
          width: double.infinity,
          padding: const EdgeInsets.fromLTRB(16, 8, 12, 8),
          decoration: const BoxDecoration(
            border: Border(top: BorderSide(color: Palette.divider)),
          ),
          child: Row(
            children: [
              const Icon(Icons.backup_outlined,
                  size: 18, color: Palette.textDim,),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  'Otomatik yedek: $line',
                  style: TextStyle(
                    color: _error != null && info == null
                        ? Palette.textFaint
                        : Palette.text,
                    fontSize: 12,
                    height: 1.35,
                  ),
                ),
              ),
              if (info != null)
                const Icon(Icons.edit_outlined,
                    size: 18, color: Palette.accent,),
            ],
          ),
        ),
      ),
    );
  }
}

/// Planı düzenleme sayfası: kapalı / saat / gün (+ günün saati) / kopya.
class BackupPolicySheet extends StatefulWidget {
  const BackupPolicySheet({
    super.key,
    required this.serverId,
    required this.initial,
    required this.query,
  });

  final String serverId;
  final BackupPolicy initial;
  final BackupQuery query;

  @override
  State<BackupPolicySheet> createState() => _BackupPolicySheetState();
}

class _BackupPolicySheetState extends State<BackupPolicySheet> {
  late bool _auto;
  late bool _byDays;
  late int _hours;
  late int _days;
  int? _atMinute;
  late int _keep;
  bool _saving = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    final p = widget.initial;
    final plan = BackupPlan.parse(p.schedule);
    final stored = p.schedule.trim().isNotEmpty;
    _auto = p.auto;
    _byDays = plan?.isDays ?? false;
    // Seçeneklerde olmayan kayıtlı değer (CLI ile "5h") önerilene döner;
    // kullanıcı Kaydet'e basmadıkça daemon'daki plan değişmez.
    _hours =
        plan != null && backupHourChoices.contains(plan.hours) ? plan.hours : 6;
    _days =
        plan != null && backupDayChoices.contains(plan.days) ? plan.days : 1;
    // Günün saati hiç seçilmemişse 04:00 önerilir: yedek sırasında dünya
    // sıkıştırılır ve sunucu yavaşlar, gecenin en sessiz saati en az hissedilir.
    _atMinute = plan != null && plan.isDays ? plan.atMinute : 4 * 60;
    _keep = stored ? p.keep : 5;
  }

  Future<void> _pickTime() async {
    final cur = _atMinute ?? 4 * 60;
    final t = await showTimePicker(
      context: context,
      initialTime: TimeOfDay(hour: cur ~/ 60, minute: cur % 60),
      helpText: 'Yedek saati (MCOS cihazının saatiyle)',
      builder: (ctx, child) => MediaQuery(
        data: MediaQuery.of(ctx).copyWith(alwaysUse24HourFormat: true),
        child: child!,
      ),
    );
    if (t != null && mounted) {
      setState(() => _atMinute = t.hour * 60 + t.minute);
    }
  }

  Future<void> _save() async {
    setState(() {
      _saving = true;
      _error = null;
    });
    final plan = _byDays
        ? BackupPlan.days(_days, atMinute: _atMinute)
        : BackupPlan.hours(_hours);
    try {
      final r = await widget.query('backup.setPolicy', {
        'serverId': widget.serverId,
        'auto': _auto,
        // Kapatırken plan gönderilmez: daemon saklar, yeniden açan kullanıcı
        // eski seçimini bulur.
        if (_auto) 'schedule': plan.schedule,
        if (_auto) 'keep': _keep,
      });
      if (!mounted) return;
      Navigator.of(context).pop(BackupPolicyInfo.fromJson(r));
    } catch (e) {
      if (mounted) {
        setState(() {
          _saving = false;
          _error = friendlyError(e);
        });
      }
    }
  }

  Widget _chips<T>(
    List<T> values,
    T selected,
    String Function(T) label,
    void Function(T) onPick,
  ) {
    return Wrap(
      spacing: 8,
      runSpacing: 4,
      children: [
        for (final v in values)
          ChoiceChip(
            label: Text(label(v)),
            selected: v == selected,
            onSelected: _saving ? null : (_) => setState(() => onPick(v)),
          ),
      ],
    );
  }

  static const _caption = TextStyle(color: Palette.textDim, fontSize: 12);

  @override
  Widget build(BuildContext context) {
    final at = _atMinute;
    return SafeArea(
      child: SingleChildScrollView(
        padding: EdgeInsets.fromLTRB(
          20,
          16,
          20,
          16 + MediaQuery.of(context).viewInsets.bottom,
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('Otomatik yedek'),
              subtitle: Text(
                _auto ? 'Açık' : 'Kapalı — otomatik yedek alınmaz',
                style: _caption,
              ),
              value: _auto,
              onChanged: _saving ? null : (v) => setState(() => _auto = v),
            ),
            if (_auto) ...[
              const SizedBox(height: 8),
              SegmentedButton<bool>(
                segments: const [
                  ButtonSegment(value: false, label: Text('Saat aralığı')),
                  ButtonSegment(value: true, label: Text('Gün aralığı')),
                ],
                selected: {_byDays},
                onSelectionChanged:
                    _saving ? null : (s) => setState(() => _byDays = s.first),
              ),
              const SizedBox(height: 12),
              if (!_byDays) ...[
                _chips(
                  backupHourChoices,
                  _hours,
                  backupHoursLabel,
                  (v) => _hours = v,
                ),
                const SizedBox(height: 4),
                const Text(
                  'Süre son yedekten itibaren sayılır.',
                  style: _caption,
                ),
              ] else ...[
                _chips(
                  backupDayChoices,
                  _days,
                  backupDaysLabel,
                  (v) => _days = v,
                ),
                const SizedBox(height: 8),
                SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('Belirli bir saatte'),
                  subtitle: Text(
                    at == null
                        ? 'Saat sabit değil: son yedekten ${_days * 24} saat sonra'
                        : 'Cihaz o saatte kapalıysa yedek açılınca hemen alınır.',
                    style: _caption,
                  ),
                  value: at != null,
                  onChanged: _saving
                      ? null
                      : (v) => setState(() => _atMinute = v ? 4 * 60 : null),
                ),
                if (at != null)
                  OutlinedButton.icon(
                    onPressed: _saving ? null : _pickTime,
                    icon: const Icon(Icons.schedule, size: 18),
                    label: Text('Saat: ${BackupPlan.clock(at)}'),
                  ),
              ],
              const SizedBox(height: 16),
              const Text('Saklanacak kopya'),
              const SizedBox(height: 6),
              _chips(
                backupKeepChoices,
                _keep,
                backupKeepLabel,
                (v) => _keep = v,
              ),
              const SizedBox(height: 4),
              const Text(
                'Sayı aşılınca en eski yedek silinir (elle alınanlar dahil).',
                style: _caption,
              ),
            ],
            if (_error != null) ...[
              const SizedBox(height: 12),
              Text(
                _error!,
                style: const TextStyle(color: Palette.error, fontSize: 12),
              ),
            ],
            const SizedBox(height: 16),
            SizedBox(
              width: double.infinity,
              child: FilledButton(
                onPressed: _saving ? null : _save,
                child: Text(_saving ? 'Kaydediliyor…' : 'Kaydet'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
