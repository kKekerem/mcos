/// server.console yanıtı: yeni satırlar ve ilerletilmiş imleç.
///
/// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
/// Gerçek mcosd'nin yanıtı (ölçüldü):
///
///   {"lines": [{"t": 1790420224640, "text": "Applying patches"}, …],
///    "cursor": 8}
///
/// Eski kod `entries` alanını ve `message`/`line` anahtarlarını arıyordu —
/// ikisi de daemon'da hiç yok. Sonuç: telefonda sunucu konsolu HER ZAMAN boş
/// kalıyordu, oysa imleç ilerlediği için satırlar bir daha da gelmiyordu.
/// Eski adlar geriye dönük uyum için hâlâ okunuyor.
class ConsoleChunk {
  const ConsoleChunk(this.lines, this.cursor);

  final List<String> lines;
  final int cursor;

  static ConsoleChunk fromJson(Map<String, dynamic> j, int previousCursor) {
    final raw = j['lines'] ?? j['entries'];
    final out = <String>[];
    if (raw is List) {
      for (final e in raw) {
        if (e is Map<String, dynamic>) {
          final msg = e['text'] ?? e['message'] ?? e['line'];
          if (msg is String) out.add(msg);
        } else if (e is String) {
          out.add(e);
        }
      }
    }
    final c = j['cursor'];
    return ConsoleChunk(out, c is num ? c.toInt() : previousCursor);
  }
}
