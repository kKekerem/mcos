# GECIS.md — MCOS Proje Devir Dosyası

> Bu dosya, projeyi devralan yeni bir yapay zekâ ajanı içindir. Amacı, önceki
> oturumdaki hafızanın tamamını aktarmaktır: ne yapıldı, neden yapıldı, ne
> yarım kaldı, hangi kod ne işe yarıyor, araştırma çıktıları nerede.
>
> **Tarih:** 2026-09-11 · **Dal:** `ensonlarındanbiri` · **Depo:** `~/mcos` (WSL Ubuntu)

---

# BÖLÜM A — HAM AJAN HAFIZASI

> Bu bölüm, önceki oturumun bağlam penceresinde duran **ham gerçeklerdir**:
> birebir metinler, ölçülmüş sayılar, çalışan komut kalıpları ve yanıltıcı
> tuzaklar. Hiçbiri tahmin değil, hepsi doğrulandı. Aşağıdaki bölümler bunların
> yorumudur; **çelişki olursa bu bölüm doğrudur.**

## A.1 Ortam — birebir

```
Depo (WSL içinden):       /home/kkekerem/mcos
Depo (Windows'tan):       \\wsl.localhost\ubuntu\home\kkekerem\mcos
Platform:                 Ajan Windows 11'de, depo WSL Ubuntu'da
Go:                       go1.26.3 linux/amd64 (WSL içinde)
Dal:                      ensonlarındanbiri
Kullanıcı e-posta:        hakanakgul@klu.edu.tr
Hedef donanım:            Gigabyte Z390 D (Intel, LGA1151) — kernel panic bu makinede çekildi
Buildroot:                2024.02
Çekirdek:                 6.6.32
```

**Depoda bir otomatik yükleyici bot var**: `git log` içinde
`Auto-commit via GitHub Auto Uploader` commit'leri görünüyor (`d6b5624`,
`50d8259`). Çalışmalarını habersiz commit'leyebilir; `git status` beklenmedik
görünürse sebebi bu olabilir.

Depoda zaten bir `DENETIM.md` var (önceki bir denetim raporu).

## A.2 ÇALIŞAN komut kalıpları (bunları kopyala)

```bash
# Go komutları — WSL içinden çalıştırılmalı
wsl.exe -d ubuntu -- bash -lc 'cd ~/mcos && go test ./cmd/... ./internal/... ./panel/...'

# Kabuk testleri
wsl.exe -d ubuntu -- bash -lc 'cd ~/mcos && make test-boot'

# Arayüzü donanımsız görmek — EN KULLANIŞLI KOMUT
wsl.exe -d ubuntu -- bash -lc 'cd ~/mcos && go build -o /tmp/p ./cmd/mcos-panel-fb && /tmp/p --screenshot /tmp/ekran.png --connect /yok.sock'

# Widget mockup'ları
wsl.exe -d ubuntu -- bash -lc 'cd ~/mcos && MCOS_UI_OUT=/tmp/ui go test ./internal/fbui/ -run TestMockup'

# PNG'yi Windows tarafına alıp bakmak
cp //wsl.localhost/ubuntu/tmp/ui/*.png "<scratchpad>/"
```

## A.3 TUZAKLAR — beni defalarca yanılttılar

1. **`wsl.exe ... bash -lc '...'` içinde tek tırnak (`'`) KULLANMA.**
   Türkçe kesme işareti bile (`mcos-launch'in`, `Go'da`) komutu kırıyor:
   `syntax error near unexpected token`. Çözüm: çift tırnaklı dış kabuk veya
   `Edit`/`Write` aracını kullan.

2. **`$DEGISKEN` ve `$(komut)` yeniyor.** `$T`, `$M`, `$OFF`, `$(stat …)`
   sessizce boşa çıktı ve bir kez **yanıltıcı sonuç** üretti (host yolları
   hedef rootfs yoluymuş gibi raporlandı). Değişken kullanacaksan betik
   dosyasına yaz, sonra çalıştır.

3. **`//wsl.localhost/...` üzerinden `Edit`/`Write` çalıştırılabilir biti
   düşürüyor.** `mcos-install` düzenlendikten sonra `test-install-logic.sh`
   "calistirilabilir = hayir" diye patladı. Kabuk betiği düzenledikten sonra
   **daima `chmod +x`**.

4. **`SendUserFile` UNC yolu kabul etmiyor.** Dosyayı önce scratchpad'e
   kopyala, oradan gönder.

5. **`pip` WSL'de yok.** fontTools ile font kapsama ölçmeye çalıştım,
   çalışmadı; karar Go testinden gelen kanıtla verildi.

6. **`go build ./...` KULLANMA.** ISO derlendikten sonra
   `os/buildroot/output/build/*/gcc/testsuite` altında binlerce geçersiz `.go`
   oluşuyor (ör. `bug257.go`, 20 000 satır). Makefile'daki
   `GO_PKGS := ./cmd/... ./internal/... ./panel/...` kullanılmalı.

7. **Depoda çöp dosyalar var** (test kalıntısı): `BOOTX64.EFI`, `core.img`,
   `test.txt`, `test_lipgloss.go`, `check_legacy.py`, kökte `mcos-panel-fb`,
   ve bozuk adlı bir dosya: `" kopyalanıyor...dd if=\\ of=\\ bs=4M status=progress…"`.
   Temizlemeden önce kullanıcıya sor.

## A.4 ÖLÇÜLEN SAYILAR (tahmin değil)

```
Bulanıklık, 1920x1080, yarıçap 12:
    tam çözünürlükte     176 ms      ← kabul edilemez
    küçültme yoluyla      50 ms      ← şu anki, test bütçesi 60 ms

Font hücresi:
    1920x1080 @ 19px  ->  hücre 12x24, 45 satır hedefi
    1920x1080 @ 16px  ->  hücre 10x20, taban 15 -> 192 sütun x 54 satır

GRUB imajları (bu makinede üretilen):
    BOOTX64.EFI          765 952 bayt
    core.img             164 898 bayt = 323 sektör (MBR boşluğu sınırı 2047)
    boot.img kernel_sector alanı (ofset 92) = 1   -> core.img LBA 1'e dd edilir
    core.img blok listesi (ofset 0x1F4) = başlangıç LBA 2, uzunluk 321 sektör

initramfs:                ~122 MB (tamamı RAM'e açılıyor)

Testler:                  92 kabuk kontrolü + ~60 Go testi, hepsi geçiyor
Yeni Go kodu:             ~8000 satır
```

## A.5 GÖMÜLÜ FONTTA OLMAYAN GLİFLER (kanıtlanmış)

FiraCode Nerd Font Regular şunları **içermiyor**:

```
▸ ◂ ▴ ▾ ◈ ◍ ★ ⚑ ⚙ ⚠ ✗
```

Eski panel bunları kullanıyordu → fbterm yedek fonta düşüyor → yedek fontun
**farklı ilerleme genişliği tüm satırı kaydırıyor**. Kullanıcının şikâyet
ettiği hizalama bozukluğunun bir sebebi buydu.

**Bu yüzden sözleşme:** font YALNIZCA metin çizer. Sözleşme metni
`internal/fbfont/font_test.go` içinde yazılı. Yeni bir ikon gerekirse
**fontta arama — `fbdraw` ile çiz.**

## A.6 ESKİ PANELİN BÖLÜM LİSTESİ — birebir (`panel/sidebar.go:25-36`)

Kullanıcı *"düzen aynı kalacak"* dedi. Sıra ve metin budur:

```go
0  "Sistem Durumu"
1  "Sunucular"
2  "USB Bellek"        // yalnızca status.USB.Present iken görünür
3  "Yazılım"
4  "Performans"
5  "Donanım"
6  "Ağ"
7  "Tünel (Serveo)"    // -> yeni panelde "Tünel (playit)" oldu
8  "MCOS Paylaşım"
9  "Ayarlar"
```

Yeni panelde ayrıca **"Güç"** bölümü eklendi (uyku kipi için).
Kurallar: `secUSB` USB yokken **tamamen gizlenir** (gri değil, yok);
`secWAN` `!status.WANOn` iken **gri** gösterilir.

## A.7 ESKİ PANELİN TUŞ ATAMALARI — birebir (`panel/app.go:375-475`)

Yeni panel bunları **aynen** korudu (tek yeni tuş: F12).

| Tuş | Etki |
|---|---|
| `q`, `ctrl+c` | çıkış |
| `g` | güç menüsü |
| `t` | turbo aç/kapa |
| `tab`, `shift+tab` | odak değiştir (kenar çubuğu ↔ içerik) |
| `up`/`k`, `down`/`j` | imleç |
| `enter`, `right`, `l` | seç / aç |
| `esc`, `left`, `h` | geri / kenar çubuğuna dön |
| `n` | yeni sunucu sihirbazı |
| `s`/`S` | sunucu başlat |
| `x`/`X` | sunucu durdur |
| `r`/`R` | yeniden başlat *(Donanım/Ağ bölümünde: yeniden tara)* |
| `e` | kablolu bağlan (Donanım/Ağ) |
| `w` | Wi-Fi tara (Donanım/Ağ) |
| `1`,`2`,`3`,`4` | Java 17/21/11/8 indir (Yazılım bölümünde) |
| `p` | USB kalıcı yap (Ayarlar) |
| `i` | kurulum sihirbazı (Ayarlar) |
| **`F12`** | **YENİ: eski panele dön** (çıkış kodu 64) |

**DİKKAT — eski paneldeki hata:** `case "left","h"` ve `case "right","l"," "`
metin alanından **önce** yakalanıyor. Bu yüzden "Düğüm adı" alanına `h`, `l`
ve boşluk yazılamıyor ("salon" → "saon"). Yeni panelde bu hata yok ama
**eski panelde hâlâ duruyor** (bkz. §7.6).

## A.8 TEMALAR — birebir (`panel/theme/theme.go:60-73`)

Sıra ve kimlikler değişmemeli; yükseltmede kullanıcının teması kaymasın.

```
graphite-teal      -> "Grafit Teal"        (varsayılan)
noir-purple        -> "Noir Mor"
anthracite-orange  -> "Antrasit Turuncu"
anthracite-green   -> "Antrasit Yeşil"
crimson-night      -> "Gece Kızılı"
amber-graphite     -> "Kehribar Grafit"
```

Yeni paletin ham değerleri (`internal/fbui/theme.go`):
```
Bg 0x0F1216 · Surface 0x161B21 · Raised 0x1E252D
Border 0x39485A · BorderFocus 0x23A99C · Divider 0x26303B
Text 0xDCE3EA · TextDim 0x8D9AA8 · TextFaint 0x606D7B · TextOn 0x07120F
Accent 0x23A99C · OK 0x46A758 · Warn 0xD9A21B · Error 0xE5484D
```
Vurgu renkleri: teal `0x23A99C`, mor `0x8B5CF6`, turuncu `0xE07A3F`,
yeşil `0x46A758`, kızıl `0xD64550`, kehribar `0xD9A21B`.
**Tema değişince YALNIZCA vurgu değişir** — OK/Warn/Error sabit kalır ki
okunabilirlik temadan bağımsız olsun.

## A.9 ÇEKİRDEK KOMUT SATIRI ve ÇÖZÜNÜRLÜK — birebir (`scripts/lib/display.sh`)

```sh
MCOS_GFXMODE='1920x1080x32,1920x1080,1680x1050,1600x900,1440x900,1366x768,1280x1024,1280x800,1280x720,1024x768,auto'

MCOS_CMDLINE_BASE='console=tty0 consoleblank=0 loglevel=4 fbcon=nodefer vt.global_cursor_default=0'

MCOS_CMDLINE_RECOVERY='console=tty0 nomodeset vga=normal loglevel=7'
```

**`root=` YOK ve OLMAMALI** — sistem initramfs'ten çalışıyor. `root=` eklemek
tam da kullanıcının fotoğrafını attığı VFS paniğini üretir.
*(Disk kökü mimarisine geçilirse bu kural değişir ve `test-boot-logic.sh`
de güncellenmelidir — şu an `root=` olmamasını şart koşuyor.)*

**`video=` ASLA KOYMA.** GRUB'un seçtiği modu ezer. Eski `mkiso.sh`'te
`video=1024x768` vardı; panelin düşük çözünürlükte açılmasının doğrudan
sebebi oydu.

GRUB prefix'leri (`post-build.sh`):
```
BIOS:  -p '/boot/grub'   + gömülü ön-yapılandırma:
           search --no-floppy --label --set=root MCOS-BOOT
           set prefix=($root)/boot/grub
UEFI:  -p /EFI/BOOT
```
Bölüm etiketleri: FAT boot = `MCOS-BOOT` (9 karakter, FAT sınırı 11),
ext4 veri = `MCOS-DATA`.

## A.10 KERNEL PANIC'İ ÜRETEN KOD — birebir (artık silindi)

Bu, `git show HEAD:.../mcos-install` içindeki suçlu bloktur. **Geri gelmemeli;
`scripts/test-boot-logic.sh` bunu arıyor.**

```sh
if [ ! -f /mnt/target/boot/EFI/BOOT/BOOTX64.EFI ] && [ -f /mnt/target/boot/bzImage ]; then
    log "EFISTUB fallback (Kernel EFI STUB) kopyalanıyor..."
    cp /mnt/target/boot/bzImage /mnt/target/boot/EFI/BOOT/BOOTX64.EFI
    echo "\\EFI\\BOOT\\BOOTX64.EFI $ROOT_CMDLINE" > /mnt/target/boot/startup.nsh
fi
```

Fotoğraftaki hata: `VFS: Cannot open root device "" or unknown-block(0,0)` →
`Kernel panic - not syncing`. Boş `""` = UEFI'nin komut satırı vermemesi.

## A.11 VERİ MODELİ — kolay yanılınan noktalar

```go
// internal/model/model.go:91-99  — DİKKAT
type ServerState string
const (
    StateStopped  = "stopped"
    StateStarting = "starting"
    StateRunning  = "running"
    StateStopping = "stopping"
    StateError    = "error"      // "crashed" veya "installing" YOK!
)

// Priority ZATEN VAR — turbo işinde buna bak
type Priority string
const (PriorityLow="low"; PriorityNormal="normal"; PriorityHigh="high")
```

`model.Config` içinde **hostname/PC adı alanı YOK** — OOBE'nin PC adını
kaydetmemesinin sebebi bu (bkz. §7.6, hata #4).

`SystemStatus` alanları: `SystemName, Version, Uptime, Tier, CPU, Memory,
Disks, GPUs, Net{NICs,LocalIP,Internet,Hostname}, JavaVersions, ServersTotal,
ServersUp, WAN, PeersOnline, USB{Present,Partitions}, TurboOn`.

## A.12 DAVRANIŞI KİLİTLEYEN TESTLER — isimleriyle

Bunları bozarsan bir davranışı da bozmuşsundur:

```
internal/fbdraw:
  TestStrokedRoundRectIsContinuous  köşelerde kopukluk olmamalı (KULLANICININ ASIL İSTEĞİ)
  TestCornersAreActuallyRounded     köşe pikseli boş, yay noktası dolu
  TestAlphaIsPremultiplied          saydam dolgu gerçekten saydam çizilmeli
  TestBlurLargeRegionIsFast         1080p bulanıklık ≤ 60 ms
  TestCrispLinesAreNotBlurred       ayırıcı çizgiler keskin

internal/fbfont:
  TestMonospaceAdvance              font sözleşmesi: yalnızca metin
  (geometrik set BİLEREK çıkarıldı — 11 glif yok)

internal/fbinput:
  TestTurkishCharactersSurvive      ığüşöçİĞÜŞÖÇ bozulmamalı  ← EN KRİTİK
  TestLettersAreNotSwallowed        h, l, boşluk yutulmamalı
  TestUnknownSequenceIsDropped      terminal yanıtı ekrana yazılmamalı
  TestSplitSequenceAcrossReads      bölünmüş dizi kaybolmamalı
  TestInputEventSize                amd64'te 24 bayt

scripts/:
  test-boot-logic.sh      EFISTUB geri gelmesin + prefix/etiket uyumu
  test-display-logic.sh   dört üreticide de aynı çözünürlük, video= yok
  test-panel-wiring.sh    yeni panel GERÇEKTEN bağlı mı + konsol geri veriliyor mu
```

`test-boot-logic.sh` ve `test-panel-wiring.sh`'in gerçekten hata yakaladığı
**kanıtlandı**: eski hatalı kod geçici olarak geri konup test kırmızıya
düşürüldü, sonra geri alındı.

## A.13 KULLANICININ ÇALIŞMA TARZI (gözlemlenen)

- Türkçe yazıyor, hızlı ve kısaltmalı. Yazım hatalarını düzeltme, **niyeti
  anla**.
- **Derlemeyi ve USB'ye yazmayı kendisi yapıyor.** Senden asla kurulum bekleme.
- Sonucu **görsel olarak** doğrulamak istiyor — PNG göndermek çok işe yarıyor.
  `--screenshot` kipi tam bu yüzden var.
- Kullanım limitine takılıyor. Uzun işe girmeden önce **"bu iş şu kadar
  sürer"** diye söyle; limit azalınca **durdurulabilir bir noktada bırak**.
- "Bitirmedin mi yoksa bir şey mi yanlış" diye sorduğunda **dürüst ol**.
  Bu oturumda cevap "bitirmedim" idi ve bunu açıkça söylemek doğru olandı.
- Örnek gösterdiği tasarımlar (opencode TUI fotoğrafları) **birebir kopyalanacak
  şey değil** — kendisi *"bu sana örnek ancak tamamını böyle yapmanı
  söylemiyorum"* dedi.

## A.14 HENÜZ HİÇ DOĞRULANMAMIŞ OLAN

**Yeni panel gerçek donanımda bir kez bile çalıştırılmadı.** Yalnızca
`--screenshot` kipinde doğrulandı. Kullanıcı bir sonraki derlemede deneyecek.
Riskli noktalar:

- Konsol grafik kipinden geri dönüş (3 yoldan garanti altına alındı ama sahada
  denenmedi). Bozulursa makine kör kalır — kullanıcıya **kör `reboot`** yazması
  söylenmeli.
- `/dev/tty` gerçek konsolda beklendiği gibi davranıyor mu.
- `/dev/input/event*` aygıtları mdev tarafından oluşturuluyor mu (fare
  uyandırma buna bağlı).
- `FBIOBLANK` muhtemelen **işe yaramayacak** (efifb/simpledrm'de donanım
  karartma yok) — kod bunu tespit edip ekranı boyayarak taklit ediyor.

Kullanıcının kurtarma yolu: **kurtarma menüsünde [5]** ile eski arayüze geç,
veya `MCOS_PANEL=legacy`.

---


---

## 0. ÖNCE BUNLARI OKU — Değişmez Kurallar

Bunlar kullanıcının açık talimatlarıdır. İhlal etme.

1. **ASLA BİR YERE KURULUM YAPMA.** Kullanıcının kelimeleri:
   *"ASLA VİR YERE KURULUM YAPMA BEN HALLEDECEM"*. Hiçbir diske yazma, `dd`
   çalıştırma, USB'ye imaj basma. Kullanıcı derlemeyi ve yazmayı kendisi yapar.
   Sen yalnızca kod yaz ve test et.

2. **GitHub token sızdı.** `git remote` URL'sinde düz metin bir Personal Access
   Token bulundu. O token **yanmıştır**, kullanılmamalıdır; kullanıcıya iptal
   etmesi söylendi. Bu yüzden PR açılmadı. Token'ı hiçbir yerde kullanma.

3. **Çalışma sırası:** kullanıcı şunu istedi — *"ilk olarak sistemi anla tüm
   mantığı, sonra bulduğun mantık, tasarım ne hatası varsa onları not et"*.
   Önce anla, sonra değiştir.

4. **Dil:** Kullanıcı Türkçe konuşuyor. Kod yorumları da Türkçe yazıldı
   (mevcut üsluba uy). Arayüz metinleri Türkçe.

5. **Platform tuzağı:** Depo WSL'de (`\\wsl.localhost\ubuntu\home\kkekerem\mcos`),
   ajan Windows'ta çalışıyor. Go komutları **WSL içinden** çalıştırılmalı:
   ```bash
   wsl.exe -d ubuntu -- bash -lc 'cd ~/mcos && go test ./internal/...'
   ```
   **Dikkat:** `wsl.exe ... bash -lc '...'` içinde tek tırnak (`'`) veya
   `$DEGISKEN` kullanma — kabuk bunları yiyor ve sessizce yanlış sonuç verir.
   Bu defalarca yanıltıcı sonuç üretti. Uzun betikler için dosyaya yaz, çalıştır.
   Ayrıca `//wsl.localhost/...` yolu üzerinden `Edit`/`Write` yapmak dosyanın
   **çalıştırılabilir bitini düşürüyor** — kabuk betiklerini düzenledikten sonra
   `chmod +x` yapmayı unutma (bu bir kez teste takıldı).

---

## 1. PROJE NEDİR?

**MCOS (Minecraft Server OS)** — tek amaçlı bir Linux dağıtımı. Bir PC'yi
açtığında doğrudan Minecraft sunucusu yöneten bir arayüz gelir. Klavye ile
kullanılır, masaüstü/X11 yoktur.

- **Temel:** Buildroot 2024.02, Linux çekirdeği 6.6.32, BusyBox
- **Uygulama katmanı:** Go 1.26 (`CGO_ENABLED=0`, linux/amd64 çapraz derleme)
- **Mimari:** `mcosd` (arka plan servisi, JSON-RPC) + panel (istemci)
- **Önyükleme:** Şu an **tamamen initramfs** (`BR2_TARGET_ROOTFS_CPIO=y`) —
  yani sistemin tamamı her açılışta RAM'e açılıyor. *(Bu, kullanıcının
  değiştirmemizi istediği şeylerden biri; bkz. §7.1)*

### Depo haritası (önemli olanlar)

```
~/mcos/
├── cmd/                        Çalıştırılabilir programlar
│   ├── mcosd/                  Arka plan servisi (sunucuları yönetir)
│   ├── mcosctl/                Komut satırı istemcisi
│   ├── mcos-detect/            Donanım tespiti
│   ├── mcos-panel/             ESKİ arayüz (Bubble Tea / terminal)
│   └── mcos-panel-fb/          YENİ arayüz (framebuffer) ← BU OTURUMDA YAZILDI
│
├── internal/
│   ├── model/                  Veri tipleri (Config, Server, SystemStatus…)
│   ├── ipc/                    JSON-RPC protokolü
│   ├── ipcclient/              Daemon istemcisi  ← BU OTURUMDA panel'den TAŞINDI
│   ├── daemon/                 mcosd iş mantığı, RPC işleyicileri
│   ├── server/                 Minecraft sunucu süreç yöneticisi
│   ├── java/                   JRE indirme/tespit, JVM bayrakları
│   ├── netcfg/                 Wi-Fi / kablolu ağ
│   ├── tunnel/                 Serveo tünelleri ← KALDIRILACAK (playit gelecek)
│   ├── cluster/                Çok makineli eşleşme
│   ├── files/                  Dosya/USB işlemleri
│   │
│   ├── fbdev/                  ← YENİ: /dev/fb0 mmap + ioctl
│   ├── fbfont/                 ← YENİ: gömülü TTF ile metin çizme
│   ├── fbdraw/                 ← YENİ: vektör çizim (yay, halka, bulanıklık)
│   ├── fbui/                   ← YENİ: widget'lar (panel, buton, radyo…)
│   ├── fbvt/                   ← YENİ: konsol sahipliği (grafik kipi)
│   ├── fbinput/                ← YENİ: tuş çözücü + fare etkinliği
│   └── fbpanel/                ← YENİ: ekranlar, tuş atamaları, döngü
│
├── panel/                      ESKİ Bubble Tea arayüzü (hâlâ çalışıyor, yedek)
│   ├── app.go                  Ana model, tuş yönlendirme, View()
│   ├── sidebar.go              Bölüm listesi
│   ├── view_setup.go           OOBE sihirbazı (10 adım) — MANTIK HATALARI VAR
│   ├── view_*.go               Diğer ekranlar
│   └── theme/                  Renk paleti (fbterm'in 16 yuva sınırı)
│
├── os/buildroot/external/
│   ├── configs/mcos_defconfig  Buildroot yapılandırması
│   ├── package/mcos/mcos.mk    Go ikililerini rootfs'e kurar
│   └── board/mcos/
│       ├── kernel.config       Çekirdek yapılandırması
│       ├── post-build.sh       GRUB imajları üretir, dosya kopyalar
│       ├── genimage.cfg        Disk imajı düzeni
│       └── rootfs-overlay/     Hedef sisteme kopyalanan dosyalar
│           ├── etc/init.d/S99mcos      Açılış betiği
│           └── usr/bin/
│               ├── mcos-launch         Paneli başlatır ← BU OTURUMDA DEĞİŞTİ
│               ├── mcos-install        Diske kurulum ← BU OTURUMDA DEĞİŞTİ
│               └── mcos-persist        USB kalıcılık
│
├── scripts/
│   ├── lib/display.sh          ← YENİ: çözünürlüğün TEK tanımı
│   ├── mkiso.sh                ISO üretir (DİKKAT: make iso bunu KULLANMIYOR!)
│   ├── mkusb.sh                USB disk imajı üretir
│   └── test-*.sh               Kabuk testleri (6 adet, bkz. §5)
│
├── docs/arastirma/             ← YENİ: alt ajanların araştırma çıktıları
│   ├── faz1-arastirma.json     boot/oobe/offline/ui (5 ajan)
│   └── faz2-arastirma.json     screens/turbo/playit/diskroot/sleep/offline (7 ajan)
│
└── Makefile                    Derleme hedefleri
```

---

## 2. KULLANICININ İSTEKLERİ (ham, kendi kelimeleriyle)

### 2.1 İlk büyük istek (3 fotoğraf ekli)

> *"bak. arayüz harflerle cizilmeyecek. tam tersi olacak tek parca yuvarlak
> olacak. kendin cizeceksin. tüm ekranlarda pencere kenarları, çizgiler,
> butonlar, ayırmak icin kullanılan cizgielr felan hersey tek parca köseleri
> yumusak çizgiler kullanacan. fotolara bak. bu sana örnek ancak tamamını böyle
> yapmanı söylemiyorum. sadece örnek... arayüzü adam akıllı yap. ek olarak
> çözünürlüğü yükselt, ekran ayarları ekle... bir recovery ekle internet yokken
> kurulum yaparsak mc sunucusu kurabilelim fabric 1.21.11 ve via version kurulu
> olsun... cok onemli bir hata daha buldum. aslında uefi icin buyuk ihtimalk
> kuruyor bootloaderi ama kernel panic veriyor. fotosu ekte... ne değiştirmen
> gerekiyorsa teknolojide değiştir. kurulum ekrnaını bisiler secme ekranını
> cizgileri felan hepsini yeniden kodla. kurulum ekranında bazı sacmalıklar var
> mantık hatası onları da düzelt"*

Fotoğraflar: ikisi `opencode` TUI'sinden **örnek** tasarım, biri gerçek
donanımda çekilmiş kernel panic (`VFS: Cannot open root device ""`,
Gigabyte Z390 D, çekirdek 6.6.32).

### 2.2 İkinci istek (ISO'yu derleyip denedikten sonra)

> *"bende 1 kere derkledim aldım isoyu ama hala aynı menüler felan acaba sen
> bitirmedin die mi yoksa bisi yanlıs mı. ek olarak fotolar da güzel ancak
> temaları koy bi de blur olsun hani wifi felan secerken parola girerken arkada
> olan sey dursun arkada ne varsa artık blur gelsin veya tamamen siyah olmasın
> hafif karartma olsun en kolay hangisiyse o olsun. arayüzün dizilişi aynı
> kalmalı sadece tasarım değişmelşi tek parca olmalı düzen aynı kalacak. yeni
> arayüzde bir kac sey eksik gibi görünüyo fotoda. altta biraz alan olacak
> oradan girdiğimiz yerşin kısayolu ve su an olan anlı ksey olacak mesela
> sunucuyu actıysak altta sunucu acılıyor basladı gibi uyarılkar olacak. dediğim
> gbii internet yokken calısacak bir sunucu entegre et. diske kurunca asla ama
> asla ramde calısmasın tutulsun diskte. güc menüsüne felan uyku modu ekle
> oradan arkada sunucular kapanacka ama ekran gidecek fare ile veya klavye ile
> hareket ettirince ekran gelecek. turbo mod gercekten ise yarasın. serveo
> seceneğini kaldır onun yerine playit seceneği ekle. playiti göm indir su an
> gömn os acılınca sunucuya girince o üstteki seceneklerden onun icin kurup
> playiit baslatabiliytoz ayarları kendisi yapmalı oto"*

**"Hâlâ aynı menüler" sorusunun cevabı:** Kullanıcı haklıydı ve bir hata
yoktu — **iş yarımdı**. Yeni çizim paketleri yazılmıştı ama hiçbir yerden
çağrılmıyordu; `mcos-launch` hâlâ eski paneli başlatıyordu. Bu bu oturumda
düzeltildi (bkz. §4.6).

---

## 3. BU OTURUMDA ÇÖZÜLEN GERÇEK HATALAR

Bunlar tahmin değil, kanıtlanmış hatalardır. Her biri için test yazıldı.

### 3.1 UEFI kernel panic (kullanıcının fotoğrafı) — ÇÖZÜLDÜ

**Belirti:** `VFS: Cannot open root device "" or unknown-block(0,0)` →
`Kernel panic - not syncing`.

**Sebep:** Eski `mcos-install`, GRUB bulamayınca "EFISTUB geri dönüşü" yapıp
**ham çekirdeği** `/EFI/BOOT/BOOTX64.EFI` olarak kopyalıyordu:
```sh
cp /mnt/target/boot/bzImage /mnt/target/boot/EFI/BOOT/BOOTX64.EFI
echo "\EFI\BOOT\BOOTX64.EFI $ROOT_CMDLINE" > /mnt/target/boot/startup.nsh
```
UEFI firmware'i bir EFI uygulamasını **komut satırı vermeden** çalıştırır.
`startup.nsh` dosyasını yalnızca UEFI Shell okur, normal firmware okumaz.
Sonuç: çekirdek boş cmdline ile açılır, `root=` göremez, panikler. Fotoğraftaki
boş `""` tam olarak bu imzadır.

**Düzeltme:** O yol tamamen kaldırıldı. `BOOTX64.EFI` artık yalnızca derleme
sırasında `grub-mkimage` ile üretilen GRUB ikilisidir.

### 3.2 UEFI imajı hiç üretilmeyebiliyordu — ÇÖZÜLDÜ

`post-build.sh` içinde `grub-mkimage -O x86_64-efi` çağrısı, BIOS bloğunun
**içinde** yuvalanmıştı. Derleme makinesinde yalnızca `grub-efi-amd64-bin`
kuruluysa (`/usr/lib/grub/i386-pc` yoksa) **hiçbiri** üretilmiyordu. Üstelik
`2>/dev/null` hatayı yutuyordu.

**Düzeltme:** BIOS ve UEFI bağımsız üretiliyor; hata çıktısı
`$BUILD_DIR/mcos-grub-mkimage.log` dosyasına yazılıyor.

### 3.3 BIOS GRUB prefix'i yanlış diske sabitlenmişti — ÇÖZÜLDÜ

`-p '(hd0,msdos1)/boot/grub'` — MCOS diski BIOS'ta ilk disk değilse (kullanıcının
dahili diski varsa USB genelde `hd1` olur) GRUB yanlış diskte `grub.cfg` arayıp
rescue kabuğuna düşerdi.

**Düzeltme:** Gömülü ön-yapılandırma bölümü **etiketten** buluyor:
```
search --no-floppy --label --set=root MCOS-BOOT
set prefix=($root)/boot/grub
```
Bulamazsa `core.img`'in okunduğu diske geri düşer.

### 3.4 Çözünürlük dört ayrı yerde farklıydı — ÇÖZÜLDÜ

| Dosya | Eski değer |
|---|---|
| `Makefile` (`iso:` hedefi) | `gfxmode=1024x768,800x600,auto` |
| `scripts/mkiso.sh` | `gfxmode=auto` **+ cmdline'da `video=1024x768`** |
| `scripts/mkusb.sh` | `gfxmode=auto` |
| `mcos-install` | `gfxmode=1024x768,800x600,auto` |

`video=1024x768` en kötüsüydü: GRUB 1080p seçse bile çekirdek bunu ezip
çözünürlüğü düşürüyordu. **Panelin düşük çözünürlükte açılmasının doğrudan
sebebi buydu.**

**KRİTİK BULGU:** `scripts/mkiso.sh` hiç kullanılmıyor! `make iso` kendi
`grub.cfg`'sini Makefile içinde satır içi üretiyor. Bunu araştırma ajanı buldu;
ben önce yanlış dosyayı düzeltmiştim.

**Düzeltme:** `scripts/lib/display.sh` tek kaynak oldu. Dördü de oradan okuyor.
Yeni liste 1080p ile başlıyor:
```
1920x1080x32,1920x1080,1680x1050,1600x900,1440x900,1366x768,1280x1024,1280x800,1280x720,1024x768,auto
```

### 3.5 `fbdraw.Alpha()` alfa ön çarpımı yapmıyordu — ÇÖZÜLDÜ

Go'da `color.RGBA` **alfa ön çarpımlıdır**. Yalnızca `A` alanını kısmak
geçersiz bir renk üretir (`R > A`) ve `draw.Over` bunu neredeyse tam opak
çizer. Belirti: "%20 saydam" olması gereken rozet **dolu sarı** görünüyordu.
Düzeltme: dört kanal da ölçekleniyor. Test: `TestAlphaIsPremultiplied`.

### 3.6 CSI ayrıştırıcı özel parametreleri tanımıyordu — ÇÖZÜLDÜ

`fbinput` tuş çözücüsü `ESC [ > 0;276;0c` gibi terminal yanıtlarını
çözemeyip kalanını **ekrana harf olarak yazacaktı**. Parametre aralığı
ECMA-48'e uygun hale getirildi (`0x30–0x3F`). Test: `TestUnknownSequenceIsDropped`.

### 3.7 Tam ekran bulanıklık 176 ms sürüyordu — ÇÖZÜLDÜ

Modal açılışında gözle görülür donma. Küçültme→bulanıklaştırma→büyütme yoluyla
**50 ms**'ye indi. Test: `TestBlurLargeRegionIsFast` (60 ms bütçesi).

### 3.8 F12 ile eski panele dönüş çalışmıyordu — ÇÖZÜLDÜ

Yeni panelden çıkınca `mcos-launch` yine yeni paneli açıyordu. Yeni arayüzde
bir sorun çıksa kullanıcı **kilitli kalırdı**. Düzeltme: F12 → çıkış kodu **64**
→ `mcos-launch` bunu "eski paneli aç" isteği olarak anlıyor ve seçimi
`/data/mcos/panel.conf`'a yazıyor. Panel hiç açılmazsa F12'ye basılamayacağı
için kurtarma menüsüne de **[5]** seçeneği eklendi (iki yöne geçiş).

### 3.9 Font kapsama sorunu — TASARIM DEĞİŞİKLİĞİYLE ÇÖZÜLDÜ

`fbfont` testi şunu ortaya çıkardı: gömülü FiraCode Nerd Font şu glifleri
**içermiyor**: `▸ ◂ ▴ ▾ ◈ ◍ ★ ⚑ ⚙ ⚠ ✗`. Eski panelde bunlar kullanılıyordu;
fbterm yedek fonta düşüyor, o fontun **farklı genişliği tüm satırı
kaydırıyordu**. Bu, kullanıcının şikâyet ettiği hizalama bozukluğunun bir
sebebiydi.

**Karar:** Testi yamamak yerine tasarım değişti — **font yalnızca METİN çizer**,
tüm çerçeve/ikon/işaret vektör şekil olarak çiziliyor. Sözleşme
`internal/fbfont/font_test.go` içinde yazılı.

---

## 4. BU OTURUMDA YAZILAN KOD — Her paket ne yapıyor?

Toplam ~8000 satır yeni Go kodu. Hepsi test edilmiş durumda.

### 4.1 `internal/fbdev` — Ekrana piksel yazma (önceden vardı)

- `device_linux.go`: `/dev/fb0`'ı `mmap` ile açar, `FBIOGET_VSCREENINFO`
  (0x4600) ve `FBIOGET_FSCREENINFO` (0x4602) ioctl'leriyle çözünürlük/derinlik
  öğrenir. `Flip(*image.RGBA)` tuvali ekrana basar.
- `canvas.go`: `SavePNG()` — ekran görüntüsü.
- `device_other.go`: Linux dışı için taslak (geliştirme makinesinde derlensin).

### 4.2 `internal/fbfont` — Metin çizme

- Yazı tipi **ikilinin içine gömülü** (`go:embed`), bu yüzden hedefte
  fontconfig/fbterm/font yolu gibi hiçbir bağımlılık yok.
- `Load(sizePx)` → `Face`. DPI 72 seçildi ki punto = piksel olsun.
- Hücre ölçüleri `GlyphAdvance('M')`'den **tam sayıya yuvarlanarak** türetilir
  (kesirli ilerleme sütunları kaydırırdı). `CellH = round(Ascent)+round(Descent)`,
  **satır aralığı yok** — kutu şekilleri dikeyde birleşsin diye.
- `Glyph(r)` asla nil dönmez, önbelleklidir.

### 4.3 `internal/fbdraw` — Vektör çizim (İŞİN KALBİ)

`golang.org/x/image/vector` rasterleştiricisi üzerine kurulu. Analitik kenar
yumuşatma, sıfır-olmayan sarım kuralı.

**En önemli fonksiyon — `StrokeRoundRect`:** dış konturu saat yönünde, iç
konturu **ters yönde**, **tek bir rasterleştirme işleminde** çizer. Dört kenarı
ayrı ayrı çizmek tam da köşelerde kopukluk üreten şeydir. Testler bunu kanıtlar:
- `TestStrokedRoundRectIsContinuous` — kutunun kapladığı **her** tarama
  satırında ve sütununda mürekkep olmalı.
- `TestCornersAreActuallyRounded` — kutunun tam köşe pikseli **boş**, 45°'lik
  yay noktası **dolu** olmalı.

Diğerleri: `FillRoundRect`, `HLine`/`VLine` (keskin, yumuşatılmamış — ayırıcı
çizgiler bulanık olmasın), `Line` (yumuşatılmış), `FillCircle`/`StrokeCircle`,
`FillPolygon`, `Blend`, `Alpha`.

`blur.go` — modal arkası:
- `Blur(dst, rect, radius)`: üç kez kutu bulanıklığı (Gauss'a yakın), kayan
  pencere toplamıyla O(piksel). Büyük bölgelerde otomatik olarak 4× küçültme
  yolunu kullanır (176 ms → 50 ms).
- `Dim(dst, rect, color, amount)`: hafif karartma (tamamen siyah değil).

### 4.4 `internal/fbui` — Widget'lar ve tema

- `theme.go`: **24 bit gerçek renk** paleti (fbterm'in 16 yuva sınırı gitti).
  `Metrics` tüm piksel ölçülerini **font hücresinden türetir** — çözünürlük
  değişince arayüz orantılı büyür.
  Temalar: `graphite-teal`, `noir-purple`, `anthracite-orange`,
  `anthracite-green`, `crimson-night`, `amber-graphite` — **eski panelle aynı
  isim ve sıra** (yükseltmede tema değişmesin diye).
- `widgets.go`: `Panel`, `Button` (odak halesi), `Radio` (çizilmiş halka+nokta),
  `Check` (yuvarlak kare + iki çizgi segmenti), `Chevron` (çokgen ok), `Row`,
  `Progress`, `StatusDot`, `Badge`, `Divider`, metin yardımcıları.
  **Eksik glif sessizce yutulmaz** — görünür kırmızı kutu çizilir.
- `chrome.go`: `Scrim` (bulanıklık+karartma), `ScrimCache` (modal açıkken her
  karede yeniden hesaplamasın), `Modal` (ortalanmış + gölgeli),
  `StatusBar` (solda canlı olay, sağda kısayollar), `Spinner` (çizilmiş dönen
  yay — karakter animasyonu değil, çünkü font onları sabit genişlikte
  çizmeyebilir ve satır kayar).

### 4.5 `internal/fbvt` — Konsol sahipliği (EN RİSKLİ PARÇA)

`/dev/fb0`'a yazmak tek başına yetmez: çekirdeğin framebuffer konsolu (fbcon)
aynı belleğe imleç ve mesaj yazmaya devam eder. `KD_GRAPHICS` kipi onu susturur.

**En kritik kural:** Grafik kipinde program çıkarsa veya çökerse **konsol
kullanılamaz kalır**. Bu yüzden `Restore()` üç yoldan garanti altında:
`defer`, `recover()`, ve `signal.Notify` (SIGINT/SIGTERM/SIGHUP/SIGQUIT).

Klavye için `K_UNICODE` seçildi, **`K_OFF` değil**: `K_OFF` çekirdeğin tuş
çevirisini kapatır ve Türkçe klavye düzenini (ı, ğ, ş, ö, ç, ü) elle uygulamak
gerekirdi. `K_UNICODE`'da çekirdek yüklü düzeni uygular, bize hazır UTF-8 verir.

`Blank(off)` — `FBIOBLANK` ioctl'i. **Dürüstlük notu:** efifb/simpledrm gibi
firmware framebuffer'larında donanım gerçekten uyutulamaz; o durumda `false`
döner ve ekran boyanarak taklit edilir.

### 4.6 `internal/fbinput` — Girdi

- `keys.go`: Ham bayt akışını tuş olaylarına çevirir. Türkçe UTF-8, ok tuşları,
  F tuşları (**hem Linux konsolu `ESC[[A` hem xterm `ESCOP` biçimi**),
  Home/End/PgUp/PgDn, Ctrl kombinasyonları, değiştiricili oklar.
  Yarım kalan diziler saklanır (okuma sınırı dizinin ortasına denk gelebilir).
  Yalnız ESC belirsizliği `Flush()` + zaman aşımıyla çözülür.
- `activity_linux.go`: `/dev/input/event*` okuyup **fare hareketini** yakalar
  (klavye zaten tty'den geliyor). Uyku kipinden uyandırma için.
  `inputEventSize = 24` (amd64'te `struct input_event` boyutu) — yanlış olursa
  olay akışı kayar, test bunu sabitliyor.

### 4.7 `internal/fbpanel` — Ekranlar ve döngü

- `app.go`: Durum (`App`), bölümler, olay akışı (`Emit`), uyku, tema uygulama.
  **Bölüm sırası eski panelle aynı**, iki fark: `Tünel (Serveo)` →
  `Tünel (playit)`, ve yeni `Güç` bölümü.
- `draw.go`: Düzen (kenar çubuğu | içerik / alt çubuk) ve ekranlar:
  Sistem Durumu, Sunucular, Güç, Ayarlar, Ağ, Performans, Donanım.
  Taşınmayan bölümler **sahte "yakında" ekranı göstermez** — dürüstçe
  "bu bölüm henüz taşınmadı, F12 ile eski panele dön" yazar.
- `keys.go`: Tuş atamaları **eski panelle birebir aynı** (`g` güç, `t` turbo,
  `n` yeni, `s`/`x`/`r` başlat/durdur/yeniden başlat, `tab` odak, ok tuşları,
  `q` çıkış). Yeni olan tek şey **F12** (eski panele dön).
- `run.go`: **Olay güdümlü** ana döngü — hiçbir şey değişmediyse çizim yapılmaz
  (üzerinde Minecraft sunucusu çalışan makinede boşuna CPU yakmasın).
  Uykudayken herhangi bir tuş **yalnızca uyandırır**, eylem tetiklemez.
- `demo.go`: `FillDemo()` — yalnızca ekran görüntüsü kipinde örnek veri.

### 4.8 `cmd/mcos-panel-fb` — Çalıştırılabilir

```bash
mcos-panel-fb --connect /run/mcosd.sock --fb /dev/fb0 --tty /dev/tty
mcos-panel-fb --screenshot cikti.png    # daemon'suz çalışır, örnek veriyle çizer
```
Yazı boyutu ekran yüksekliğinden otomatik hesaplanır (~45 satır hedefi).
Çıkış kodu **64 = kullanıcı F12 ile eski paneli istedi**.

### 4.9 `internal/ipcclient` — Paylaşılan daemon istemcisi

`panel/client.go`'nun gövdesi buraya taşındı. Sebep: iki arayüz de aynı
çağrıları kullanıyor; istemci `panel` paketinde kalsaydı framebuffer ikilisi
Bubble Tea ve Lip Gloss'u da bağlamak zorunda kalırdı (initramfs'te boşuna
megabaytlar). `panel/client.go` artık sadece `type Client = ipcclient.Client`
takma adı — mevcut ekran kodunun tek satırı bile değişmedi.

---

## 5. TESTLER — Ne neyi kanıtlıyor?

```bash
wsl.exe -d ubuntu -- bash -lc 'cd ~/mcos && go test ./cmd/... ./internal/... ./panel/...'
wsl.exe -d ubuntu -- bash -lc 'cd ~/mcos && make test-boot'
```

| Betik | Ne doğruluyor |
|---|---|
| `test-disksig.sh` | MBR disk imzası → PARTUUID dönüşümü |
| `test-install-logic.sh` | Kurulum betiğinin mantığı (72 kontrol) |
| `test-bootloader-embed.sh` | `boot.img` `kernel_sector`=1, `core.img` blok listesi |
| `test-boot-logic.sh` | **EFISTUB geri gelmesin**, GRUB prefix ↔ grub.cfg uyumu, etiket uzunluğu, cmdline'da `root=` yok |
| `test-display-logic.sh` | Çözünürlük dört yerde de aynı, `video=` yok, 1080p önce, `gfxpayload=keep` var |
| `test-panel-wiring.sh` | **Yeni panel gerçekten bağlı mı** — ikili derleniyor mu, rootfs'e kuruluyor mu, `mcos-launch` çağırıyor mu, eski panele dönüş yolu duruyor mu, konsol geri veriliyor mu |

**Toplam: 92 kabuk kontrolü + ~60 Go testi. Hepsi geçiyor.**

`test-boot-logic.sh` ve `test-panel-wiring.sh` **bu oturumdaki iki büyük
hatanın tekrarını** engellemek için yazıldı. İkisinin de gerçekten hata
yakaladığı kanıtlandı (eski kodu geçici olarak geri koyup test edildi).

---

## 6. ARAŞTIRMA ÇIKTILARI — NEREDE?

İki büyük paralel araştırma çalıştırıldı (Workflow aracı, 12 alt ajan toplam).
Sonuçlar **depoya kopyalandı** ki hesap değişince kaybolmasın:

### `docs/arastirma/faz1-arastirma.json` (5 ajan)
Alanlar: `boot`, `oobe`, `offline`, `ui` + sentez.
Okuma:
```bash
python3 -c "
import json,io
d=json.load(io.open('docs/arastirma/faz1-arastirma.json',encoding='utf-8'))
r=d['result']
if isinstance(r,str): r=json.loads(r)
for s in r['surveys']: print('==',s['key'],'==\n',s['summary'],'\n')
"
```

### `docs/arastirma/faz2-arastirma.json` (7 ajan)
Alanlar: `screens` (tüm eski panel ekranlarının satır satır envanteri),
`turbo`, `playit`, `diskroot`, `sleep`, `offline` + sentez (`result.plan`).

**Bu dosyalar çok değerli.** Özellikle:
- `screens` → eski panelin **her ekranının tam düzeni**, dosya:satır kanıtıyla.
  Kalan bölümleri yeni arayüze taşırken düzeni bozmadan yapmak için gerekli.
- `playit` → playit.gg ajanının indirme URL'leri, kimlik doğrulama akışı,
  yapılandırma biçimi, lisans durumu (web araştırması yapıldı).
- `diskroot` → initramfs → disk kökü geçişinin tam analizi ve daha az riskli
  alternatif.

Ayrıca oturum içi ham ajan kayıtları (silinmiş olabilir):
`C:\Users\Monster\.claude\projects\--wsl-localhost-ubuntu-home-kkekerem-mcos\6100d901-e73c-4d69-af69-57345478e96b\subagents\workflows\`

---

## 7. YARIM KALANLAR — Sıradaki işler

Kullanıcının öncelik sırasıyla.

### 7.1 Diske kurulunca RAM'de değil DİSKTE çalışsın — EN RİSKLİ

Kullanıcı: *"diske kurunca asla ama asla ramde calısmasın tutulsun diskte"*

**Mevcut durum:** `BR2_TARGET_ROOTFS_CPIO=y` — sistemin tamamı gzip'li cpio
olarak açılışta RAM'e açılıyor. initramfs ~122 MB.

**Gereken:** `switch_root` mimarisi — küçük bir initramfs diskteki ext4 kökü
bağlayıp ona geçsin. Değişmesi gerekenler: çekirdek cmdline, bir `/init`
betiği, `mcos-install`'ın disk kökünü doldurması, `S99mcos` ve `fstab`.

**TEHLİKE:** Yanlış yapılırsa tam da kullanıcının fotoğrafını attığı
`VFS: Cannot open root device` paniğine geri dönülür. `test-boot-logic.sh`
şu an cmdline'da `root=` OLMAMASINI şart koşuyor — disk kökü mimarisine
geçilirse **bu test de güncellenmelidir** (yoksa yanlış şeyi korur).

`faz2-arastirma.json` içindeki `diskroot` anketi daha az riskli bir alternatif
de öneriyor (squashfs kök + overlayfs üst katman disk üzerinde). Kullanıcıya
ikisini karşılaştırıp seçtirmek doğru olur.

### 7.2 Çevrimdışı Minecraft sunucusu (Fabric 1.21.11 + ViaVersion)

Kullanıcı: *"internet yokken kurulum yaparsak mc sunucusu kurabilelim fabric
1.21.11 ve via version kurulu olsun"*

**Araştırmanın bulduğu:** MCOS'ta **hiç çevrimdışı yol yok** — her sunucu türü,
her mod/eklenti ve JRE'nin kendisi kurulum anında HTTP ile indiriliyor. Ne
önbellek, ne gömülü yedek, ne "çevrimdışı kipi" bayrağı var.

**KRİTİK BULGU:** Mevcut `installViaVersion` yalnızca eklenti tabanlı türlerde
(Paper/Spigot) çalışıyor, **Fabric'i atlıyor**. Fabric için **ViaFabric**
gerekiyor — farklı bir jar, `mods/` dizinine konur. Bunu bilmeden paketlersek
çalışmayan bir şey gömmüş oluruz.

**Gereken dört parça:** yerel Fabric sağlayıcı yolu, ön-tohumlanmış
yapılandırma, gömülü JRE'nin `/data/java/index.json`'a kaydı, `mods/` içinde
ViaFabric jar'ı. Yük **MCOS-DATA ext4 bölümüne tohumlanmalı**, 122 MB'lık
initramfs'e değil.

**Lisans uyarısı:** Minecraft sunucu jar'ı muhtemelen bir OS imajı içinde
yeniden dağıtılamaz. `faz2-arastirma.json` → `offline` anketinde bu açıkça
işaretlenmiş. Kullanıcıya söylenmeli.

### 7.3 Serveo → playit

Kullanıcı: *"serveo seceneğini kaldır onun yerine playit seceneği ekle. playiti
göm indir... ayarları kendisi yapmalı oto"*

Değiştirilecek yerler (`faz2-arastirma.json` → `playit` anketinde tam liste):
`internal/tunnel/manager.go` (tüm paket), `panel/view_misc.go`,
`panel/view_wizard.go`, `panel/view_dashboard.go`, `panel/view_detail.go`,
`panel/sidebar.go`, `panel/app_test.go`, `internal/ipc/*`, `internal/model/*`.

Yeni arayüzde bölüm adı **zaten `Tünel (playit)`** olarak yazıldı
(`internal/fbpanel/app.go`), ama arkasındaki mantık henüz yok.

**Dikkat:** playit'in kimlik doğrulama akışı (claim code / web arayüzü)
"tamamen otomatik" olmayı engelleyebilir. Araştırma bunu inceledi; sonucu
okuyup kullanıcıya gerçekçi olanı söyle.

### 7.4 Turbo modu gerçekten iş yapsın

Kullanıcı: *"turbo mod gercekten ise yarasın"* — yani şu an yapmadığını
düşünüyor. `faz2-arastirma.json` → `turbo` anketi neyin plasebo olduğunu
dosya:satır ile listeliyor; ayrıca çekirdek yapılandırmasında cgroup/CPU
sınırlama desteğinin olup olmadığını raporluyor. **Bu anketi okumadan
değişiklik yapma.**

Not: `internal/model/model.go`'da `Priority` (low/normal/high) tipi zaten var —
uygulanıp uygulanmadığı kontrol edilmeli.

### 7.5 Kalan bölümleri yeni arayüze taşı

Henüz taşınmayanlar: **Yazılım, USB Bellek, MCOS Paylaşım**.
Şu an dürüstçe "bu bölüm henüz taşınmadı, F12 ile eski panele dön" yazıyor.
`faz2-arastirma.json` → `screens` anketinde her birinin tam düzeni var.

### 7.6 OOBE (kurulum sihirbazı) mantık hataları — HENÜZ DÜZELTİLMEDİ

Kullanıcı: *"kurulum ekranında bazı sacmalıklar var mantık hatası onları da
düzelt"*. Araştırma somut liste çıkardı (`faz1-arastirma.json` → `oobe`):

| # | Hata | Önem |
|---|---|---|
| 1 | "KURULUM BAŞARILI" ekranında Enter'a basınca **kurulumu yeniden çalıştırıyor** (diski tekrar siliyor) | **kritik** |
| 2 | Başarılı kurulum 5/10. adımda yeniden başlatıyor → 6-10. adımlar ve `doSetupSave` **hiç çalışmıyor**, kullanıcının girdiği hiçbir şey kaydedilmiyor | **kritik** |
| 3 | "Geri" butonu 2-5. adımlarda gösteriliyor ama **hiçbir şey yapmıyor** | yüksek |
| 4 | PC adı zorunlu tutuluyor, özette gösteriliyor ama **hiçbir yere kaydedilmiyor** (`model.Config`'de alan yok) | yüksek |
| 5 | "Düğüm adı" alanına **'h', 'l' ve boşluk yazılamıyor** (vim tarzı tuş atamaları karakterleri yutuyor) | yüksek |
| 6 | Wi-Fi listesi 8 satır çiziyor ama imleç daha aşağı inebiliyor | orta |
| 7 | Hatalar sessizce yutuluyor (disk listesi RPC, UpdateConfig, Persist) | orta |

**Önerilen çözüm (araştırmanın da önerisi):** Sıra değişmeli — **önce kaydet,
sonra kur**. `stepInstall` en sona alınmalı (`stepConfirm`'den sonra), böylece
`mcos-install` gerçek bir `config.json`'ı MCOS-DATA'ya kopyalayabilir.

Planlanan ama yapılmayan: mantığı görüntüleme katmanından ayırıp
`internal/oobe` paketinde saf bir durum makinesi yapmak — böylece hem eski
Bubble Tea paneli hem yeni framebuffer arayüzü aynı doğru mantığı kullanır.

---

## 8. NASIL DERLENİR / DENENİR

```bash
# Go testleri
wsl.exe -d ubuntu -- bash -lc 'cd ~/mcos && go test ./cmd/... ./internal/... ./panel/...'

# Kabuk testleri (diske DOKUNMAZ)
wsl.exe -d ubuntu -- bash -lc 'cd ~/mcos && make test-boot'

# Arayüzü donanımsız görmek (ÇOK KULLANIŞLI)
wsl.exe -d ubuntu -- bash -lc 'cd ~/mcos && go build -o /tmp/p ./cmd/mcos-panel-fb && /tmp/p --screenshot /tmp/ekran.png --connect /yok.sock'

# Widget mockup'ları (PNG üretir)
wsl.exe -d ubuntu -- bash -lc 'cd ~/mcos && MCOS_UI_OUT=/tmp/ui go test ./internal/fbui/ -run TestMockup'

# Tam OS derlemesi (kullanıcı yapar)
make os && make iso
```

**Not:** `go build ./...` KULLANMA. ISO bir kez derlendikten sonra
`os/buildroot/output/build/*/gcc/testsuite` altında binlerce geçersiz `.go`
dosyası oluşuyor. Makefile'da `GO_PKGS := ./cmd/... ./internal/... ./panel/...`
tanımlı, onu kullan.

---

## 9. AÇILIŞ ZİNCİRİ — Yeni panel nasıl çalışıyor?

```
GRUB (gfxmode=1920x1080…, gfxpayload=keep)
  └─ çekirdek + initramfs (root= YOK, sistem initramfs'ten çalışır)
       └─ /etc/init.d/S99mcos
            └─ /usr/bin/mcos-launch
                 ├─ mcosd'u başlat, hazır olmasını bekle
                 ├─ panel_mode() → MCOS_PANEL değişkeni / /data/mcos/panel.conf
                 ├─ "legacy" değilse → mcos-panel-fb   ← YENİ
                 │    ├─ çıkış 0  → normal kapanış
                 │    ├─ çıkış 64 → kullanıcı F12'ye bastı → eski paneli aç
                 │    └─ başka    → hata → fbterm + eski panele düş
                 └─ değilse → fbterm + mcos-panel      ← ESKİ (yedek)
```

**Kurtarma menüsü** (panel kapanınca çıkar): [1] yeniden aç, [2] font cache,
[3] mcos-persist, [4] kabuk, **[5] arayüz değiştir** (yeni ↔ eski).

---

## 10. ÖNEMLİ TASARIM KARARLARI VE NEDENLERİ

Bunları değiştirmeden önce nedenlerini oku:

1. **Font yalnızca metin çizer.** Çerçeve/ikon/işaret vektördür. Sebep: gömülü
   fontta 11 UI glifi yok ve yedek fonta düşmek satır kaydırıyordu (§3.9).

2. **`StrokeRoundRect` tek rasterleştirmede iki kontur çizer.** Dört kenarı
   ayrı çizmek köşelerde kopukluk üretir.

3. **Ölçüler font hücresinden türetilir**, sabit piksel değil. 4K'da minik,
   800×600'de devasa olmasın diye.

4. **Olay güdümlü döngü.** Boştayken çizim yok — üzerinde Minecraft sunucusu
   çalışan makinede CPU yakmasın.

5. **`K_UNICODE`, `K_OFF` değil.** Türkçe klavye düzenini çekirdek uygulasın.

6. **Bölüm sırası ve tuş atamaları eski panelle aynı.** Kullanıcı açıkça
   *"düzen aynı kalacak"* dedi.

7. **Taşınmayan bölümler sahte ekran göstermiyor.** Dürüstçe durumu söyleyip
   eski panele yönlendiriyor.

8. **Uyku sunucuları DURDURMAZ.** Kullanıcının cümlesi belirsizdi ("arkada
   sunucular kapanacak ama ekran gidecek") ama sunucuları durdurmak uzaktaki
   oyuncuları atmak demek — yalnızca ekran kapatılıyor, bu menüde de yazılı.

9. **Kendiliğinden uyku kapalı** (`IdleSleep: 0`). Sunucu makinesinin ekranı
   beklenmedik şekilde kararırsa kullanıcı çöktüğünü sanar.

---

## 11. BİLİNEN RİSKLER / DOĞRULANMAMIŞ ŞEYLER

1. **Yeni panel gerçek donanımda hiç çalıştırılmadı.** Yalnızca ekran görüntüsü
   kipinde doğrulandı. Kullanıcı bir sonraki derlemede deneyecek. Konsol geri
   verme üç yoldan garantili ama sahada test edilmedi.

2. **`FBIOBLANK` muhtemelen işe yaramayacak** — efifb/simpledrm'de donanım
   karartma yok. Kod bunu tespit edip ekranı boyayarak taklit ediyor ve
   kullanıcıya Güç menüsünde durumu yazıyor.

3. **Çekirdekte gerçek GPU sürücüsü yok** (i915/amdgpu/nouveau derlenmiyor).
   Bu yüzden **çözünürlük çalışırken değiştirilemez** — ekran ayarları
   `grub.cfg`'yi yazıp yeniden başlatmak zorunda. Ekran ayarları ekranı henüz
   yapılmadı (tasarımı `internal/fbui/mockup_test.go` içinde var).

4. **Depoda çöp dosyalar var:** `BOOTX64.EFI`, `core.img`, `test.txt`,
   `test_lipgloss.go`, `check_legacy.py`, `mcos-panel-fb` (kök dizinde) ve
   bozuk adlı bir dosya (`" kopyalanıyor...dd if=..."`). Bunlar test kalıntısı;
   temizlenebilir ama önce kullanıcıya sor.

5. **`scripts/mkiso.sh` ölü kod.** `make iso` kendi grub.cfg'sini üretiyor.
   İkisi de artık `scripts/lib/display.sh`'ten okuyor ama bu kafa karıştırıcı;
   birleştirmek iyi olur.

---

## 12. İLK YAPMAN GEREKENLER

1. Bu dosyayı oku (yaptın).
2. `docs/arastirma/faz2-arastirma.json` içindeki sentez planını oku
   (`result.plan`) — sıradaki işlerin dosya dosya değişiklik listesi orada.
3. `make test-boot` ve `go test` çalıştır — her şeyin hâlâ yeşil olduğunu gör.
4. Kullanıcıya hangi işten başlamak istediğini sor. Önerilen sıra:
   **OOBE mantık hataları (§7.6)** → çabuk ve kritik, kullanıcının diskini
   silen bir hata var. Sonra **playit (§7.3)**, sonra **çevrimdışı sunucu
   (§7.2)**, en son **disk kökü (§7.1)** çünkü en riskli olan o.
5. **Hiçbir diske yazma.** Kullanıcı kendisi derleyip yazıyor.
