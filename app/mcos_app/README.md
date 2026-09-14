# MCOS — Android uygulaması

Minecraft sunucunuzu telefondan yönetin: durum, sunucu başlat/durdur, canlı
konsol ve SSH terminali.

---

## ⚠ Bu kod DERLENMEDİ

Açık olmak gerekiyor: bu uygulama yazıldı ama **hiç derlenmedi ve
çalıştırılmadı**, çünkü geliştirme makinesinde Flutter SDK kurulu değil.

Yapılan doğrulama, yapabildiğim kadarıyla şu:

| Denetim | Durum |
|---|---|
| Dengeli parantez/süslü parantez (17 dosya) | geçti |
| Göreli `import`ların var olan dosyaya işaret etmesi | geçti |
| Kullanılmayan içe aktarma | yok |
| Aynı dosyada ikiz sınıf | yok |
| Bağlandığı sunucu API'si (`/health`, `/rpc`) | **gerçek daemon'a karşı sınandı** |

Yani sunucu tarafı çalışıyor ve sınandı; telefon tarafı yazıldı ama
**ilk `flutter run`'da derleme hatası çıkması olağan.** Beklenen tipik
hatalar: paket sürümlerinin API'lerinde ufak farklar (`xterm`, `dartssh2`).

Derlemeden önce:

```bash
cd app/mcos_app
flutter pub get
flutter analyze          # önce bunu çalıştırın — hataları burada görürsünüz
flutter run              # telefon bağlıyken
```

Android iskeleti (`android/`) elle yazıldı. Flutter sürümünüzle uyuşmazsa
en güvenlisi onu yeniden ürettirmek — `lib/` ve `pubspec.yaml` korunur:

```bash
cd app/mcos_app
flutter create . --platforms=android --project-name mcos_app --org gg.mcos
```

---

## Nasıl bağlanılır

1. **MCOS panelinde:** Sol menü → Ayarlar → **Uzaktan kontrol** → "Uzaktan
   kontrolü aç".
   Ekranda adres, port ve **jeton** görünür.
2. **Telefonda:** uygulamayı açın → "MCOS ekle" → adresi, portu (2223) ve
   jetonu girin → **Bağlan**.

İlk bağlantıda uygulama sunucunun sertifika parmak izini kaydeder. Sonraki
bağlantılarda parmak izi değişirse bağlantı **reddedilir** — araya giren
birine karşı koruma budur. MCOS'u yeniden kurduysanız bağlantıyı silip
yeniden ekleyin.

### SSH

Panelde Ayarlar → **SSH** → önce bir parola koyun, sonra "SSH'ı aç".
Uygulamadaki SSH sekmesinden aynı parolayla bağlanabilirsiniz.

---

## Klasör yapısı

```
app/mcos_app/
├── pubspec.yaml              bağımlılıklar (4 paket, fazlası yok)
├── analysis_options.yaml     sıkı statik çözümleme
├── android/                  Android iskeleti (elle yazıldı)
│   └── app/src/main/
│       ├── AndroidManifest.xml   yalnızca INTERNET izni
│       ├── kotlin/gg/mcos/app/   MainActivity (boş, her şey Dart'ta)
│       └── res/                  vektör simge + koyu açılış ekranı
└── lib/
    ├── main.dart             uygulama kökü, bağlantı seçimi
    ├── theme/
    │   ├── palette.dart      MCOS panelinin BİREBİR renkleri
    │   └── app_theme.dart    Material teması
    ├── models/
    │   ├── connection.dart   kayıtlı bir MCOS (adres + jeton + parmak izi)
    │   ├── server.dart       Minecraft sunucusu
    │   └── system_status.dart  CPU/bellek/çalışma süresi
    ├── services/
    │   ├── rpc_client.dart   HTTPS + jeton + SERTİFİKA SABİTLEME
    │   └── store.dart        şifreli depo (jeton düz metin saklanmaz)
    ├── screens/
    │   ├── connect_screen.dart    IP + jeton gir
    │   ├── home_screen.dart       dört sekme, tek yoklama döngüsü
    │   ├── dashboard_tab.dart     sistem durumu
    │   ├── servers_tab.dart       sunucu listesi, başlat/durdur
    │   ├── server_detail_screen.dart  canlı konsol + komut
    │   ├── ssh_tab.dart           gerçek terminal (xterm + dartssh2)
    │   └── settings_tab.dart      bağlantı, SSH, güç
    └── widgets/
        ├── mcos_logo.dart    çizilmiş logo + durum noktası
        └── stat_card.dart    ölçüm kartı
```

---

## Güvenlik

| Konu | Nasıl |
|---|---|
| Taşıma | TLS zorunlu. Düz HTTP isteği servis edilmez. |
| Sunucunun kimliği | Sertifika parmak izi sabitlenir (TOFU). Değişirse bağlantı reddedilir. |
| Jeton | 128 bit rastgele. Her istekte `Authorization: Bearer`. Sabit süreli karşılaştırma. |
| Telefonda saklama | `flutter_secure_storage` → Android'de EncryptedSharedPreferences. |
| İzinler | Yalnızca `INTERNET` ve `ACCESS_NETWORK_STATE`. Konum/kamera/depolama yok. |
| Kaba kuvvet | Sunucu tarafında IP başına yavaşlatma. |

**Bilinen sınır:** sertifika kendinden imzalıdır, yani ilk bağlantıda
"güven" kararı kullanıcıya aittir (parmak izini panelle karşılaştırarak).
Tünel üzerinden internete açarsanız bu kararın önemi artar.

---

## Sunucu tarafı API

Uygulama, MCOS'un JSON-RPC yüzeyinin AYNISINI kullanır — ayrı bir API yok.
Yani panelde çalışan her şey telefonda da çalışır.

```
GET  /health          kimliksiz. {"service":"mcos","version":…,"fingerprint":…}
POST /rpc             Authorization: Bearer <jeton>
                      gövde: {"jsonrpc":"2.0","id":1,"method":"server.list"}
```

Komut satırından denemek için:

```bash
curl -k https://192.168.1.20:2223/health
curl -k -X POST https://192.168.1.20:2223/rpc \
  -H "Authorization: Bearer <jeton>" \
  -d '{"jsonrpc":"2.0","id":1,"method":"system.status"}'
```
