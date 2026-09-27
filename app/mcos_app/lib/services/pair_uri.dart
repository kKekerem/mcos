/// MCOS panelinin "QR ile bağlan" kodunun ve elle girilen adresin çözümü.
///
/// ════════════════════════════════════════════════════════════════════════════
/// QR'IN İÇİ (Go karşılığı: internal/remote/pairuri.go — iki taraf AYNI kuralı
/// uygulamalı, testler aynı örnek URI'yi iki tarafta da çözüyor)
/// ════════════════════════════════════════════════════════════════════════════
///
///   mcos://pair?v=1&h=<adres>[&h=<adres2>...]&p=<port>&t=<jeton>&f=<parmakizi>
///
///   v  biçim sürümü (yalnızca 1 tanınır)
///   h  adres; BİRDEN ÇOK olabilir, öncelik sırasıyla (panel yerel ağ
///      adresini öne, VirtualBox NAT'ını sona koyar). Telefon sırayla dener.
///   p  uzaktan kontrol portu
///   t  jeton
///   f  sertifikanın SHA-256 parmak izi; iki nokta YOK, küçük harf
///
/// Parmak izi QR'da olduğu için telefon, jetonu GÖNDERMEDEN ÖNCE karşı
/// tarafın gerçekten o MCOS olduğunu doğrulayabiliyor (bant dışı sabitleme).
library;

import 'panel_text.dart';

/// QR'dan çıkan eşleşme bilgisi.
class PairInfo {
  const PairInfo({
    required this.hosts,
    required this.port,
    required this.token,
    this.fingerprint,
  });

  /// Denenecek adresler, öncelik sırasıyla. En az bir tane var.
  final List<String> hosts;
  final int port;
  final String token;

  /// normalizeFingerprint biçiminde; QR parmak izi taşımıyorsa null.
  final String? fingerprint;
}

/// Kullanıcıya gösterilebilir çözümleme hatası (Türkçe, teknik değil).
class PairFormatException implements Exception {
  const PairFormatException(this.message);
  final String message;

  @override
  String toString() => message;
}

/// Metnin bir MCOS eşleşme kodu gibi görünüp görünmediği.
///
/// Tarayıcı ekranında her okunan QR'ı hata diye göstermemek için: kamera bir
/// ürün barkodu ya da web adresi yakalarsa sessizce aramaya devam ediyoruz.
bool looksLikePairUri(String raw) =>
    raw.trim().toLowerCase().startsWith('mcos://');

/// mcos://pair?... metnini çözer.
///
/// Hata durumunda [PairFormatException] fırlatır; mesaj doğrudan ekrana
/// basılabilir.
PairInfo parsePairUri(String raw) {
  final Uri u;
  try {
    u = Uri.parse(raw.trim());
  } on FormatException {
    throw const PairFormatException(
        'Bu QR kodu okunamadı. MCOS panelindeki kodu yeniden okutun.',);
  }
  if (u.scheme.toLowerCase() != 'mcos' || u.host.toLowerCase() != 'pair') {
    throw const PairFormatException(
        'Bu bir MCOS eşleşme kodu değil. Panelde: $panelQrPath.',);
  }
  final q = u.queryParametersAll;
  String first(String k) {
    final v = q[k];
    return (v == null || v.isEmpty) ? '' : v.first.trim();
  }

  if (first('v') != '1') {
    throw const PairFormatException(
        'Bu eşleşme kodu daha yeni bir MCOS sürümünden. Uygulamayı '
        'güncelleyin.');
  }
  final hosts = <String>[
    for (final h in q['h'] ?? const <String>[])
      if (h.trim().isNotEmpty) h.trim(),
  ];
  final token = cleanToken(first('t'));
  if (hosts.isEmpty || token.isEmpty) {
    throw const PairFormatException(
        'Eşleşme kodunda adres ya da jeton eksik. Panelde uzaktan kontrolün '
        'açık olduğundan emin olup kodu yeniden okutun.');
  }
  final port = int.tryParse(first('p'));
  if (port == null || port < 1 || port > 65535) {
    throw const PairFormatException('Eşleşme kodunda geçerli bir port yok.');
  }
  final fp = normalizeFingerprint(first('f'));
  return PairInfo(
    hosts: hosts,
    port: port,
    token: token,
    fingerprint: fp.isEmpty ? null : fp,
  );
}

/// Parmak izinden ayraçları atar ve küçük harfe çevirir.
///
/// Aynı parmak izi üç biçimde dolaşıyor: panelde "A3:88:0F…", QR'da çıplak
/// "a3880f…", telefonda hesaplanan "A3:88:0F…". Karşılaştırma TEK biçimde
/// yapılmazsa doğrulama boşuna "kimlik değişti" der ve kullanıcı güvenlik
/// uyarısını görmezden gelmeyi öğrenir.
String normalizeFingerprint(String? s) {
  if (s == null) return '';
  final b = StringBuffer();
  for (final r in s.toLowerCase().runes) {
    final c = String.fromCharCode(r);
    if ('0123456789abcdef'.contains(c)) b.write(c);
  }
  return b.toString();
}

/// İki parmak izi aynı sertifikayı mı gösteriyor (biçimden bağımsız).
bool sameFingerprint(String? a, String? b) {
  final na = normalizeFingerprint(a);
  final nb = normalizeFingerprint(b);
  return na.isNotEmpty && na == nb;
}

/// Jetondaki TÜM boşlukları atar.
///
/// ── Düzeltilen gerçek hata ──────────────────────────────────────────────────
/// Panel 32 haneli jetonu okunaklı olsun diye 16'lık iki grup hâlinde,
/// ARASINDA BOŞLUKLA gösteriyor ("76d3d64f7febb116 d27838771485dfd9").
/// Kullanıcı gördüğünü yazınca eski kod yalnızca baştaki/sondaki boşluğu
/// kırpıyordu; ortadaki boşluk jetonun parçası sayılıyor ve MCOS "yetkisiz"
/// diyordu. Jeton onaltılık; içinde boşluk OLAMAZ.
String cleanToken(String raw) => raw.replaceAll(RegExp(r'\s+'), '');

/// Elle girilen adres alanının çözümü.
class HostInput {
  const HostInput(this.host, this.port);
  final String host;

  /// Kullanıcı adresle birlikte port da yazdıysa o port; yoksa null.
  final int? port;
}

/// "192.168.1.20", "192.168.1.20:2223", "https://192.168.1.20:2223/" ya da
/// "[fe80::1]:2223" biçimlerini kabul eder.
///
/// ── Neden ─────────────────────────────────────────────────────────────────
/// Adresi portuyla ("192.168.1.20:2223") ya da tarayıcıdan kopyalanmış
/// hâliyle ("https://…") yazmak çok doğal. Eski kod alanı olduğu gibi
/// "https://$host:$port/rpc" içine koyuyordu; "192.168.1.20:2223" girilince
/// adres "https://192.168.1.20:2223:2223/health" oluyor ve kullanıcı dart'ın
/// ham "Beklenmeyen hata: FormatException: Invalid port" metnini görüyordu
/// (test/pair_uri_test.dart bunu ölçüyor).
///
/// Geçersizse [PairFormatException] fırlatır.
HostInput parseHostInput(String raw) {
  var s = raw.trim();
  if (s.isEmpty) throw const PairFormatException('Adres gerekli.');
  final scheme = RegExp(r'^[a-zA-Z][a-zA-Z0-9+.-]*://');
  if (scheme.hasMatch(s)) s = s.replaceFirst(scheme, '');
  // Yol, sorgu: yalnızca adres:port kısmı gerekli.
  final slash = s.indexOf('/');
  if (slash >= 0) s = s.substring(0, slash);
  if (s.isEmpty || s.contains(' ')) {
    throw const PairFormatException('Adres boşluk içeremez.');
  }

  String host = s;
  int? port;
  if (s.startsWith('[')) {
    // IPv6: [adres]:port
    final end = s.indexOf(']');
    if (end < 0) throw const PairFormatException('IPv6 adresi eksik: "]" yok.');
    host = s.substring(1, end);
    final rest = s.substring(end + 1);
    if (rest.startsWith(':')) port = _port(rest.substring(1));
  } else if (':'.allMatches(s).length == 1) {
    final i = s.indexOf(':');
    host = s.substring(0, i);
    port = _port(s.substring(i + 1));
  }
  if (host.isEmpty) throw const PairFormatException('Adres gerekli.');
  return HostInput(host, port);
}

int _port(String s) {
  final n = int.tryParse(s);
  if (n == null || n < 1 || n > 65535) {
    throw const PairFormatException('Adresteki port 1–65535 arası olmalı.');
  }
  return n;
}
