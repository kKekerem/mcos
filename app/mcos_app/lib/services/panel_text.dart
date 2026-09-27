/// MCOS panelindeki menü adları: uygulamanın kullanıcıya "panelde şuraya
/// gidin" dediği HER yer bu sabitlerden okur.
///
/// ════════════════════════════════════════════════════════════════════════════
/// DÜZELTİLEN GERÇEK HATA
/// ════════════════════════════════════════════════════════════════════════════
///
/// Uygulama karşılama ve "MCOS ekle" ekranlarında "MCOS panelinde: Sol menü →
/// Uzaktan Kontrol" yazıyordu. Panelin sol menüsünde öyle bir bölüm YOK
/// (internal/fbpanel/app.go: Sistem Durumu, Sunucular, … Güç, Ayarlar);
/// uzaktan kontrol Ayarlar'ın içinde bir satır (screen_settings.go
/// setRemote). Kullanıcı QR'ı panelde arayıp bulamıyordu ("QR kod yeri
/// yok"). SSH ipucu da panelde olmayan "SSH sunucusunu aç" düğmesini
/// tarif ediyordu (paneldeki: "SSH'ı aç").
///
/// Metinler dağınık dururken biri panelde değişince uygulamadaki kopyası
/// eskiyordu. test/panel_text_test.dart bu adları panelin Go kaynağıyla
/// karşılaştırır: panelde bir ad değişirse test düşer.
library;

/// Panelin sol menüsündeki bölüm.
const panelSettings = 'Ayarlar';

/// Ayarlar'daki uzaktan kontrol satırı.
const panelRemoteRow = 'Uzaktan kontrol';

/// Uzaktan kontrol penceresindeki düğmeler.
const panelRemoteEnable = 'Uzaktan kontrolü aç';
const panelQrAction = 'QR ile bağlan (telefonla okut)';

/// Ayarlar'daki SSH satırı ve düğmesi.
const panelSshRow = 'SSH';
const panelSshEnable = "SSH'ı aç";

/// "Ayarlar → Uzaktan kontrol": kullanıcıya gösterilen yol.
const panelRemotePath = '$panelSettings → $panelRemoteRow';

/// QR'a giden tam yol.
const panelQrPath = '$panelRemotePath → "$panelQrAction"';
