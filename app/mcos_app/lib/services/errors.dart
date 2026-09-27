import 'dart:async';
import 'dart:io';

import 'pair_uri.dart';
import 'rpc_client.dart';
import 'panel_text.dart';

/// Herhangi bir hatayı kullanıcıya gösterilebilir TÜRKÇE bir cümleye çevirir.
///
/// ════════════════════════════════════════════════════════════════════════════
/// NEDEN TEK BİR YERDE
/// ════════════════════════════════════════════════════════════════════════════
///
/// Kullanıcı "bağlanmaya çalışınca undefined mi ne falan o tarz hatalar
/// veriyor" dedi. Telefonun ekranını göremedik; ama kodda bu tarifin
/// birebir karşılığı vardı: ekranlar yakaladıkları her şeyi `e.toString()`
/// ile basıyordu ve Dart'ın kendi istisnaları teknik ve İngilizce:
///
///   SocketException: Connection timed out (OS Error: …, errno = 110)
///   HttpException: Connection closed before full header was received
///   type 'Null' is not a subtype of type 'String' in type cast
///   TimeoutException after 0:03:00.000000: Future not completed
///
/// Her ekran kendi eşlemesini yapınca bazıları unutuluyordu (konsol, SSH,
/// pano yenileme). Artık hepsi buradan geçiyor ve hiçbir yol ham bir tür adı,
/// "null" ya da "undefined" göstermiyor (test/errors_test.dart).
///
/// [host]/[port] verilirse ağ hatalarında adres de söylenir: kullanıcı
/// yanlış adresi yazdığını ancak böyle fark eder.
String friendlyError(Object e, {String? host, int? port}) {
  final where = host == null
      ? 'MCOS'
      : (port == null ? host : '$host:$port');

  if (e is RpcException) return e.message;
  if (e is PairFormatException) return e.message;

  if (e is SocketException) {
    final code = e.osError?.errorCode;
    final msg = '${e.message} ${e.osError?.message ?? ''}'.toLowerCase();
    // errno değerleri Linux/Android'e göre: 111 ECONNREFUSED, 110 ETIMEDOUT,
    // 113 EHOSTUNREACH, 101 ENETUNREACH, 7 EAI_NODATA (ad çözülemedi).
    if (code == 111 || msg.contains('refused')) {
      return '$where bağlantıyı reddetti.\n\n'
          'Makine açık ama uzaktan kontrol dinlemiyor olabilir. MCOS '
          'panelinde: $panelRemotePath → "$panelRemoteEnable". Portun da '
          'panelde yazanla aynı olduğundan emin olun (varsayılan 2223).';
    }
    if (code == 110 || msg.contains('timed out')) {
      return '$where yanıt vermiyor (zaman aşımı).\n\n'
          'Telefon ile MCOS aynı ağda mı? Sanal makinedeyseniz (VirtualBox) '
          'ağ bağdaştırıcısı "NAT" iken telefon ulaşamaz; "Köprü" (Bridged) '
          'yapın.';
    }
    if (code == 113 || code == 101 || msg.contains('unreachable')) {
      return '$where adresine ulaşılamıyor.\n\n'
          'Telefonun Wi-Fi\'ye bağlı olduğundan ve adresin doğru yazıldığından '
          'emin olun.';
    }
    if (msg.contains('host lookup') || msg.contains('no address')) {
      return '"${host ?? where}" adı çözülemedi. Panelde yazan IP adresini '
          '(ör. 192.168.1.20) girin.';
    }
    return '$where adresine bağlanılamadı. Telefon ile MCOS aynı ağda mı?';
  }

  if (e is HandshakeException || e is TlsException) {
    return 'Güvenli bağlantı kurulamadı ($where).\n\n'
        'Bu portta MCOS yoksa ya da MCOS yeniden kurulup kimliği değiştiyse '
        'böyle olur. Port olarak panelde yazanı (varsayılan 2223) girin.';
  }

  if (e is TimeoutException) {
    return '$where zamanında yanıt vermedi. Ağ yavaş ya da makine meşgul '
        'olabilir; birazdan yeniden deneyin.';
  }

  if (e is HttpException) {
    return '$where bağlantıyı yarıda kesti. Portun uzaktan kontrol portu '
        'olduğundan emin olun (varsayılan 2223; 2222 eşleştirme portudur).';
  }

  if (e is FormatException) {
    return 'Sunucudan anlaşılmayan bir yanıt geldi. Bu adreste MCOS '
        'çalıştığından emin olun.';
  }

  if (e is TypeError) {
    // Yanıtın biçimi beklediğimiz gibi değil: çoğunlukla uygulama ile MCOS
    // sürümleri farklıdır. Ham tür metni kullanıcıya hiçbir şey anlatmaz.
    return 'MCOS\'tan beklenmeyen biçimde bir yanıt geldi. Uygulama ile MCOS '
        'sürümleri uyumsuz olabilir; ikisini de güncelleyin.';
  }

  return 'Beklenmeyen bir hata oluştu. Yeniden deneyin; sürerse uygulamayı '
      'kapatıp açın.';
}
