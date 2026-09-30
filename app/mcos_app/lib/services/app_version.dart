/// Uygulamanın kullanıcıya gösterilen sürümü.
///
/// ── Neden ekranda ────────────────────────────────────────────────────────────
/// dist/ altında birden çok APK birikti (1.0.1'de QR tarayıcı hiç yoktu).
/// Kullanıcı "QR kod yeri yok" dediğinde hangi APK'yı kurduğunu bilmenin
/// tek yolu, sürümün uygulamada görünmesi. package_info_plus eklemek yerine
/// sabit: bir bağımlılık daha kırılabilecek bir yüzey daha demek.
/// test/app_version_test.dart bu değeri pubspec.yaml'daki sürümle
/// karşılaştırır; biri güncellenip öteki unutulursa test düşer.
library;

const appVersion = '1.0.3';
