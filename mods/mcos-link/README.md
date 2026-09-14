# MCOS Link

Birden çok MCOS cihazının **aynı dünyayı** çalıştırmasını sağlayan Fabric modu.

```
        mcos-lab                                   mcos-oda
  ┌───────────────────────┐                 ┌───────────────────────┐
  │  dünyanın sol yarısı  │   oyuncu →      │  dünyanın sağ yarısı  │
  │       x < 0           │  ═══════════►   │       x ≥ 0           │
  └───────────────────────┘   aktarım       └───────────────────────┘
```

---

## Ne yapar

| Özellik | Durum |
|---|---|
| Dünyayı X ekseninde dilimlere böler, her dilimi bir cihaz simüle eder | ✅ |
| Oyuncu sınırı geçince **kesintisiz aktarım** (envanter, can, XP, konum korunur) | ✅ |
| Sınıra yaklaşınca uyarı (action bar) | ✅ |
| Tüm cihazlarda **ortak sohbet** ve **ortak oyuncu listesi** | ✅ |
| Zorluk tüm düğümlerde otomatik eşitlenir | ✅ |
| Sınırsız cihaz (2, 3, 10…) | ✅ |
| `/mcoslink` komutlarıyla durum, harita, elle aktarım | ✅ |
| Komşu dilimdeki **yapıların** karşı taraftan görünmesi | ❌ (bkz. Sınırlar) |

---

## Nasıl çalışır

### 1. Topoloji

Mod, aynı makinedeki MCOS daemon'una sorar:

```
GET http://127.0.0.1:27892/link/topology
```

Yanıt: düğüm listesi (ad, adres, Minecraft portu), dilim sınırları, zorluk.
Beş saniyede bir yenilenir; MCOS'ta yeni bir cihaz eşleştirildiğinde mod bunu
kendiliğinden görür — sunucuyu yeniden başlatmak gerekmez.

### 2. Sahiplik

Dünya X ekseninde dilimlere bölünür. İki cihazda sınır tam olarak `x = 0`'dır.
Üç cihazda iki sınır oluşur ve orta cihaz doğuş noktasını içerir.

Sahiplik **bitişiktir** — karo/hash tabanlı değil. Karoya göre hash'lemek,
oyuncunun her 16 blokta bir başka makineye atlamasına yol açardı.

### 3. Aktarım

Oyuncu kendi dilimini **2 chunk aşınca** (32 blok gecikme, sınırda ileri-geri
gidip gelmeyi engeller):

1. Sunucu oyuncunun verisini diske yazar (`playerdata/<uuid>.dat`),
2. o dosyayı hedef cihazın link portuna (27893) gönderir,
3. istemciye Minecraft'ın kendi **transfer paketini** yollar.

İstemci dünya ekranından hiç çıkmadan öbür sunucuya bağlanır ve vanilla, az
önce gönderilen `.dat` dosyasını yükler: envanter, can, XP, konum, efektler —
hepsi yerinde.

> **Neden `.dat` dosyası?** Oyuncu durumunu elle serileştirmek (her eşya,
> her efekt, her NBT etiketi) hem uzun hem de Minecraft sürümüne bağımlıdır.
> Oyunun KENDİ kayıt biçimini taşımak, sürüm değişse bile çalışır ve hiçbir
> eşya kaybolmaz.

### 4. Tek sunucu hissi

Sohbet, katılma/ayrılma bildirimleri ve oyuncu listesi bütün düğümler arasında
yayılır. Oyuncular tek bir sunucuda gibi konuşur ve birbirini listede görür.

---

## Sınırlar (dürüstçe)

**Komşu dilimdeki yapılar karşı taraftan görünmez.** Her cihaz kendi diliminin
blok verisini tutar; aynı tohumdan aynı arazi üretilir ama komşunun inşa ettiği
ev, sınırın diğer tarafından bakıldığında görünmez. Oyuncu sınırı geçtiğinde
aktarılır ve yapıyı **yerinde** görür.

Bunu çözmek için dünya verisinin gerçek zamanlı çoğaltılması gerekir
(dağıtık chunk deposu). MCOS Link bunu **iddia etmiyor**; iddia ettiği şey
tek bir dünyada, birden çok makinede, kesintisiz oynanabilirlik.

Diğer sınırlar:

- Redstone, eşya taşıyıcıları ve varlıklar (entity) **sınırı geçemez**.
  Sınırın üstüne kurulan bir demiryolu, karşı tarafta sürmez.
- Sınırın iki yanında ayrı hava/hava durumu döngüleri işler.
- İki cihazda da **aynı sürüm ve aynı mod listesi** olmalıdır.

---

## Derleme

```bash
cd mods/mcos-link
./gradlew build
```

Sonuç: `build/libs/mcos-link-<sürüm>.jar`

MCOS'un ana `Makefile`'ı bunu `make mod` ile çağırır ve jar'ı
`dist/mods/mcos-link.jar` altına koyar; daemon oradan alıp sunucunun `mods/`
klasörüne kurar.

Gradle'ın **bir kez** internet erişimi gerekir (Fabric Loom + Minecraft
eşlemeleri). Sonrasında çevrimdışı derlenebilir.

---

## Komutlar

| Komut | İş |
|---|---|
| `/mcoslink status` | Topolojiyi, kendi dilimini ve düğümleri gösterir |
| `/mcoslink map` | Dilim sınırlarını metin haritası olarak çizer |
| `/mcoslink where <oyuncu>` | Oyuncunun hangi düğümde olduğunu söyler |
| `/mcoslink send <oyuncu> <düğüm>` | Oyuncuyu elle aktarır (operatör) |
| `/mcoslink reload` | Topolojiyi hemen yeniler |

---

## Yapılandırma

Mod **kendi başına yapılandırma dosyası tutmaz**: her şeyi MCOS daemon'undan
alır. Bu bilerek böyle — iki ayrı yerde tutulan aynı ayar, er ya da geç
birbirinden ayrılır ve hata ancak oyuncu sınırı geçtiğinde ortaya çıkar.

Tek istisna, daemon'a ulaşılamadığında kullanılan ortam değişkenleridir:

| Değişken | Varsayılan |
|---|---|
| `MCOS_LINK_COORDINATOR` | `http://127.0.0.1:27892` |
| `MCOS_LINK_PORT` | `27893` |
| `MCOS_LINK_DEBUG` | `0` |
