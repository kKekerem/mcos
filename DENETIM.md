# MCOS — Sistem Denetimi ve Hata Notları

Bu dosya, kod tabanının tamamı okunarak çıkarılmış mantık/tasarım hatalarının
kaydıdır. Her madde dosya:satır referansı ve **kanıt** içerir; varsayım yoktur.

Çalışma sırası (kullanıcı tarafından belirlendi):

1. ✅ Sistemi anla
2. ✅ Hataları not et (bu dosya)
3. ✅ **USB kurulum kernel panic** — A1, A2, A3, A4, A9, A10 düzeltildi;
   `scripts/test-disksig.sh` + `scripts/test-install-logic.sh` ile doğrulandı
4. ✅ Not edilen hatalar — D1, D2, D3, D4, D5, D6, D7, A6, A8 düzeltildi;
   `internal/files` ve `internal/cluster` için sıfırdan test yazıldı
5. ✅ Sunucu başlatma hatası — C1, C2, C3, C4 düzeltildi;
   `internal/server/console_test.go` ile doğrulandı
6. ✅ Arayüz altyapısı sıfırdan kuruldu (B1–B6) — aşağıya bakın

### Faz 6 — arayüz altyapısı

Ölçümle ortaya çıkan **kök sebepler** (hepsi fbterm 1.7.0 kaynağından
doğrulandı):

| Bulgu | Kanıt | Sonuç |
|---|---|---|
| Birincil font kurulu değil | `scripts/check-fonts.sh`: `Noto Color Emoji → KURULU DEGIL` | fbterm tüm metni, glifi olmayan **emoji fontuyla** çizmeye çalışıyordu |
| `.fbtermrc` renkleri ölü | `src/fbconfig.cpp` yalnızca `color-foreground`/`color-background` okur | 16 satırlık palet **hiç uygulanmıyordu**; fbterm'in gömülü VGA paleti geçerliydi |
| Kenarlık rengi magenta | Tema `Border:"5"`, VGA yuva 5 = `#aa00aa` | kenarlıklar ve ikincil metin mor çiziliyordu |
| 256 renk desteklenmiyor | `vterm_action.cpp:522` `case 38:` → **altçizgi** olarak yorumlar | `termenv.ANSI` zorunlu; 256'ya geçmek arayüzü bozacaktı |
| Parlak renkler (90-97) işlenmiyor | SGR switch'inde `case 90...97` **yok** | 8-15 yuvalarına yalnızca **bold** ile erişilir (`fbshell.cpp:704`) |
| Faint her zaman yuva 8 | `fbshell.cpp:701` | yuva 8 okunabilir olmalı; `Faint` kullanımı terk edildi |
| fbterm terminfo hedefte yok | `terminfo/Makefile.am`: `tic fbterm` — **`-o` yok** | çapraz derlemede host'a yazılıyordu; `TERM=fbterm` girdisiz kalıyordu |

**Yapılanlar:**

- `panel/theme/tokens.go` — tüm ölçüler, simgeler ve durum etiketleri tek
  yerde. Dağınık `%-18s`/`%-14s`/`25`/`%-30s` değerleri tek ızgaraya indi.
- `panel/theme/palette.go` — 16 yuvalık palet, fbterm'e çalışma zamanında
  `ESC[3;i;r;g;b}` ile yüklenir. `Border ≠ Muted ≠ Text` artık yapısal olarak
  garanti (test ediyor).
- `panel/theme/terminal.go` — `SetColorProfile` artık tema kurucusunda değil,
  tek seferlik `SetupTerminal()` içinde ve **her çizimden önce**.
- `panel/components.go` — gerçek çerçeveli butonlar, `Chip` (tek satır),
  `Field` (çok satırlı kutuyu `JoinHorizontal` ile doğru hizalar).
- Emoji tamamen kaldırıldı: 25 çift genişlikli glif → tek kolonluk token.
  `make check-ui` bunu kalıcı olarak zorunlu kılıyor.
- Kenar çubuğundaki çifte kenarlık (`││`) kaldırıldı.
- `InnerWidth` dolguyu iki kez çıkarıyordu → kartlar artık genişliği tam
  dolduruyor.

**Not:** Eski stil adları (`Val`, `Accent`, `CardTitle`, `Key`, `Badge`…) yeni
semantik stillere bağlandı. 6 128 satırlık görünüm katmanı tek seferde yeniden
yazılmadı; bu sayede tüm ekranlar yeni paleti ve yuvarlak kenarları anında
aldı, riskli toplu değişiklik yapılmadan. Yeni kod semantik adları kullanmalı.

### Eklenen test kapsamı

| Paket | Önce | Sonra |
|---|---|---|
| `internal/files` | test yok | 6 test (yol kaçışı, symlink, ID doğrulama) |
| `internal/cluster` | test yok | 8 test (kimlik doğrulama, otomatik eşleştirme) |
| `internal/server` | 2 test | 5 test (Done tespiti, sohbet ayrımı, hata mesajı) |
| kabuk (boot) | test yok | `test-disksig.sh` + `test-install-logic.sh` |

> Kurulum/USB yazma işlemi **kullanıcıya aittir**. Bu depoda yalnızca kod ve
> konfigürasyon değiştirilir; hiçbir diske yazma yapılmaz.

---

## Bölüm A — Boot ve kurulum (kernel panic)

### A1. `root=LABEL=` çekirdek tarafından desteklenmez → garantili panic 🔴

**Kanıt zinciri:**

1. [mcos-install:60](os/buildroot/external/board/mcos/rootfs-overlay/usr/bin/mcos-install:60)
   PARTUUID'yi şöyle almaya çalışıyor:
   ```sh
   ROOT_PARTUUID=$(blkid -s PARTUUID -o value "$ROOT_PART" 2>/dev/null || true)
   ```
2. Hedefteki `blkid` **busybox**'tır:
   `output/target/sbin/blkid -> ../bin/busybox` (doğrulandı).
   Busybox `blkid` yalnızca `blkid [BLOCKDEV]...` biçimini bilir; `-s` ve `-o`
   seçenekleri **yoktur** → komut boş döner.
3. Dolayısıyla `ROOT_PARTUUID` **her zaman boş** ve
   [mcos-install:64](os/buildroot/external/board/mcos/rootfs-overlay/usr/bin/mcos-install:64)
   fallback'i devreye girer:
   ```sh
   ROOT_CMDLINE="root=LABEL=MCOS-ROOT ..."
   ```
4. Linux çekirdeğinin `name_to_dev_t()` fonksiyonu **`LABEL=` biçimini
   desteklemez**. Desteklenenler: `/dev/xxx`, `maj:min`, `PARTUUID=`,
   `PARTLABEL=`. `LABEL=` (dosya sistemi etiketi) yalnızca **initramfs/userspace**
   tarafından çözülebilen bir biçimdir.
5. Kurulu sistemde initramfs **yok** (bkz. A2) → çözecek kimse yok.

**Sonuç:** `ROOT_DEV` hiç atanmaz →
`VFS: Unable to mount root fs on unknown-block(0,0)` →
`Kernel panic - not syncing`. Kullanıcının bildirdiği hata tam olarak budur.

**Not:** Sürücüler eksik değil — `CONFIG_USB_STORAGE=y`, `CONFIG_SATA_AHCI=y`,
`CONFIG_EXT4_FS=y`, `CONFIG_BLK_DEV_SD=y`, `CONFIG_MSDOS_PARTITION=y` hepsi
[kernel.config](os/buildroot/external/board/mcos/kernel.config) içinde mevcut.
Sorun sürücü değil, **cmdline biçimi**.

### A2. Kurulu sisteme initrd hiç kopyalanmıyor 🔴

[post-build.sh:138](os/buildroot/external/board/mcos/post-build.sh:138) hedef
rootfs'ten initrd'yi **siliyor** (kendini özyinelemeli paketlememesi için —
doğru bir önlem):

```sh
rm -f "${TARGET_DIR}/boot/initrd.img" "${TARGET_DIR}/boot/rootfs.cpio.gz"
```

Ama [mcos-install:119](os/buildroot/external/board/mcos/rootfs-overlay/usr/bin/mcos-install:119)
initrd'yi şu yollarda arıyor:

| Aranan yol | Cihazda var mı? |
|---|---|
| `/boot/initrd.img` | ❌ post-build sildi |
| `/boot/rootfs.cpio.gz` | ❌ post-build sildi |
| `/mnt/cdrom/boot/initrd.img` | ❌ `/mnt/cdrom` hiçbir yerde mount edilmiyor |
| `/media/*/boot/initrd.img` | ❌ `/media` hiçbir yerde mount edilmiyor |
| `/dist/iso/boot/initrd.img` | ❌ bu bir **derleme host'u** yolu, cihazda anlamsız |

→ initrd **asla bulunamaz**, `grub.cfg`'deki `if [ -f /boot/initrd.img ]`
koşulu daima false olur.

**Ayrıca:** Devam eden değişiklikteki "initrd ekle" yaklaşımı **yanlış yönde**.
Buildroot'un cpio rootfs'i `/init` içerir
([fs/cpio/cpio.mk:20](os/buildroot/buildroot/fs/cpio/cpio.mk:20) —
`ROOTFS_CPIO_ADD_INIT`). Çekirdek initramfs içinde `/init` bulursa onu PID 1
olarak çalıştırır ve **`root=`'u tamamen yok sayar**. Yani initrd yüklenirse
kurulu sistem gerçek diski hiç mount etmez, yine RAM'den boot eder ve tüm
değişiklikler her kapanışta kaybolur. Kurulum anlamsızlaşır.

### A3. Çekirdek yanlış bölüme kopyalanıyor → 1. GRUB girdisi bozuk 🟠

[mcos-install:107](os/buildroot/external/board/mcos/rootfs-overlay/usr/bin/mcos-install:107)
FAT boot bölümünü `/mnt/target/boot`'a mount ediyor, sonra bzImage'ı oraya
kopyalıyor. Yani `bzImage` **MCOS-BOOT** (FAT) bölümünde; `MCOS-ROOT`'un
`/boot` dizini **boş kalıyor**.

Ama üretilen grub.cfg'nin 1. girdisi
([mcos-install:164](os/buildroot/external/board/mcos/rootfs-overlay/usr/bin/mcos-install:164)):

```
menuentry "MCOS (Minecraft Server OS)" {
    search --no-floppy --label --set=root MCOS-ROOT   # ← ROOT bölümü
    linux /boot/bzImage                               # ← ama kernel BOOT'ta
}
```

`set default=0` olduğu için önce bu girdi denenir → **"file not found"**.
Kullanıcı elle 2. girdiyi seçmek zorunda; o da A1 yüzünden panic ediyor.

### A4. defconfig'de var olmayan Buildroot seçenekleri 🟠

[mcos_defconfig:31-32](os/buildroot/external/configs/mcos_defconfig:31):

```
BR2_PACKAGE_UTIL_LINUX_SFDISK=y     # ← böyle bir seçenek YOK
BR2_PACKAGE_UTIL_LINUX_PARTX=y      # ← geçerli
```

Buildroot 2024.02'de `package/util-linux/Config.in` içinde `_SFDISK` ve
`_BLKID` diye ayrı seçenek yok (doğrulandı — 66 alt seçenek listelendi).
`sfdisk`, `blkid`, `findfs`, `lsblk`, `wipefs` hepsi
**`BR2_PACKAGE_UTIL_LINUX_BINARIES`** ("basic set") altında gelir.

Geçersiz satırlar `defconfig` işlenirken **sessizce yok sayılır**. Sonuç:
`mcos-install:136`'daki `sfdisk --activate` ve `mcos-persist:39`'daki
`sfdisk --append` cihazda **hiç çalışmıyor** (`|| true` ile susturulmuş).
Doğrulama: `output/target/sbin/` içinde `blkid` (busybox) ve `fdisk` var,
`sfdisk` **yok**.

### A5. Tüm sistem 268 MB'lık initramfs olarak RAM'de çalışıyor 🟠

Doğrulanan imaj boyutları (`output/images/`):

| Dosya | Boyut |
|---|---:|
| `rootfs.cpio` | **268 MB** |
| `rootfs.cpio.gz` | **122 MB** |
| `bzImage` | 10 MB |

README ~80 MB ISO iddia ediyor; gerçek initramfs tek başına 122 MB sıkıştırılmış.
`BR2_TARGET_ROOTFS_EXT2` **hiç etkin değil**, yani diske yazılabilecek gerçek
bir kök dosya sistemi imajı üretilmiyor. Bu yüzden kurulum `cp -a` ile canlı
sistemi kopyalayan kırılgan bir el işi haline gelmiş.

Şişkinliğin kaynağı `BR2_PACKAGE_LINUX_FIRMWARE` + 30 alt firmware seçeneği
([mcos_defconfig:44-75](os/buildroot/external/configs/mcos_defconfig:44)).

### A6. `genimage.cfg` ölü ve tutarsız 🟡

[genimage.cfg:16](os/buildroot/external/board/mcos/genimage.cfg:16)
`image = "rootfs.ext2"` diyor ama o imaj hiç üretilmiyor (A5). Ayrıca
genimage'ı çağıran bir post-image script'i de yok. Dosya tamamen ölü.

### A7. `mcos-persist` canlı USB'yi bulamaz 🟡

[mcos-persist:24](os/buildroot/external/board/mcos/rootfs-overlay/usr/bin/mcos-persist:24)
boot USB'yi `blkid -L MCOS-ROOT` ile arıyor. Ama canlı ISO'nun etiketi
[Makefile:173](Makefile:173)'te `-volid MCOS` olarak veriliyor — `MCOS-ROOT`
etiketi **yalnızca mcos-install çalıştıktan sonra** var olur. Yani "USB'yi
kalıcı yap" özelliği canlı sistemde hiç çalışmaz.

### A9. `mcos-install` post-build listesinde yok → çalıştırılamaz 🟠

[post-build.sh:22](os/buildroot/external/board/mcos/post-build.sh:22) yalnızca
`S99mcos`, `mcos-launch`, `mcos-persist` için `normalize` + `makeexec`
çalıştırıyordu. **`mcos-install` listede yoktu.**

Ölçüm: dört overlay betiğinin de git modu `100644` (çalıştırılabilir **değil**).
Buildroot overlay'i `rsync -a` ile kopyaladığı için modlar aynen taşınır.
Diğer üçü post-build tarafından `chmod 755` yapılırken `mcos-install` 644
kalıyor → [messages.go:189](panel/messages.go:189)
`exec.Command("mcos-install", device)` **permission denied** ile düşer.

(Mevcut `output/target` ağacında dosya 755 görünüyor çünkü eski bir derlemeden
kalma; temiz bir derlemede kırılır.)

**Düzeltildi:** `mcos-install` listeye eklendi ve dört betiğin git modu 755
yapıldı (iki katmanlı savunma).

### A10. Kurulum sonunda veri kaybı riski 🔴

Eski [mcos-install:188-190]:

```sh
umount /mnt/target/boot || true
umount /mnt/target || true
rm -rf /mnt/target || true      # ← unmount BAŞARISIZ olursa...
```

`umount` başarısız olursa (meşgul dosya tanıtıcısı, açık kabuk vb.)
`rm -rf /mnt/target` **hâlâ bağlı olan kök bölümünün içeriğini**, yani yeni
kurulan sistemin tamamını siler. `|| true` bunu tamamen sessizleştirir.

**Düzeltildi:** `rmdir` kullanılıyor — boş olmayan dizinde güvenli şekilde
başarısız olur, asla özyinelemeli silme yapmaz.

### A8. `fstab`'da yanlış dosya sistemi alanı 🟡

[fstab:3](os/buildroot/external/board/mcos/rootfs-overlay/etc/fstab:3):
```
tmpfs  /dev  devtmpfs  mode=0755,nosuid  0  0
```
1. alan `tmpfs`, 3. alan `devtmpfs`. Tip alanı kazandığı için çalışıyor ama
yanlış; `devtmpfs /dev devtmpfs` olmalı.

---

---

## Geri alınan bulgular (test tarafından çürütüldü)

Dürüstlük kaydı: aşağıdaki madde denetim sırasında hata olarak not edilmişti,
ancak yazılan test **iddiayı çürüttü**. Yanlış olduğu için geri alındı.

### ~~A-yanlış. `cp -a` hedef dizin varsa iç içe kopyalar~~ ❌ DOĞRU DEĞİL

İddia: `mcos-install` önce `/mnt/target/etc` dizinini `mkdir` ettiği için
sonraki `cp -a /etc /mnt/target/` çağrısı `/mnt/target/etc/etc` üretiyor ve
kurulu sistemde `/etc` boş kalıyordu.

Ölçüm ([scripts/test-install-logic.sh](scripts/test-install-logic.sh) bölüm 1):

| Biçim | Hedef alt dizin var | Sonuç |
|---|---|---|
| `cp -a SRC DEST/` | evet | **içerik birleşir** — iç içe olmaz ✅ |
| `cp -a SRC DEST/etc` | evet | `DEST/etc/etc` — iç içe olur |
| `cp -a SRC/. DEST/etc/` | evet | içerik birleşir ✅ |

`cp` yalnızca hedef **tam yol** olarak verildiğinde iç içe kopyalar. Eski kod
`DEST/` biçimini kullandığı için **doğruydu**. Yeni kod `SRC/. → DEST/`
biçimini niyeti açık olduğu için koruyor, ama bu bir hata düzeltmesi değil.

---

## Bölüm B — Arayüz altyapısı (Unicode + yuvarlak kenar)

### B1. fbterm'in birincil yazı tipi emoji fontu → tüm metin ve kenarlar bozuk 🔴

**Bu, "unicode karakterler ve yuvarlak kenarlar görünmüyor" şikâyetinin
doğrudan sebebi.**

[post-build.sh:105](os/buildroot/external/board/mcos/post-build.sh:105) üretilen
`.fbtermrc`:

```
font-names=Noto Color Emoji,Noto Emoji,FontAwesome,DejaVu Sans Mono,Liberation Mono,mono
```

fbterm'de `font-names` listesinin **ilk öğesi birincil fonttur**, geri kalanlar
yalnızca eksik glif için yedektir. Burada:

- **"Noto Color Emoji" sistemde kurulu değil.** Gönderilen dosya
  `NotoEmoji-Regular.ttf` = ailesi **"Noto Emoji"** (monokrom).
  ([nerd-font.mk](os/buildroot/external/package/nerd-font/nerd-font.mk))
- Dolayısıyla birincil font **"Noto Emoji"** olur.
- "Noto Emoji" bir **emoji-only** fonttur: Latin harfleri **yok**,
  box-drawing (`─│╭╮╰╯`) glifleri **yok**.

→ fbterm tüm metni ve kenarları glifi olmayan bir fontla çizmeye çalışır;
sonuç boş kareler / bozuk semboller.

**Üstelik**, gönderilen tek uygun font olan `FiraCodeNerdFont-Regular.ttf`
(Latin + box-drawing + ikonlar, monospace) **listede hiç yok**.

Aynı ters sıralama fontconfig'de de var —
[post-build.sh:85](os/buildroot/external/board/mcos/post-build.sh:85)
`monospace` ailesi için `Noto Color Emoji`'yi ilk tercih yapıyor.

### B2. "5 tema" aslında tek tema 🟠

[theme.go:31-68](panel/theme/theme.go:31) — 6 palet tanımlı ama hepsi
**Accent dışında birebir aynı**:

```go
Bg:"0", Surface:"0", SurfaceA:"0", Border:"5", Dim:"5",
Text:"7", Muted:"5", Green:"2", Yellow:"3", Red:"1", Blue:"6"
```

Sadece `Accent` değişiyor (6→5→3→2→1→3). Yani `anthracite-orange` ve
`amber-graphite` **tamamen aynı** (ikisi de Accent="3").

Daha kötüsü: **`Border` ve `Muted` ikisi de "5"** → kenarlar ile ikincil metin
aynı renkte, hiyerarşi yok. "Arayüz çok karmaşık" şikâyetinin bir sebebi bu.

### B3. "Buton" ve "pill" bileşenleri görsel olarak buton değil 🟠

[components.go:200](panel/components.go:200):

```go
func renderPill(bg, fg lipgloss.Color, label string, bold bool) string {
	color := fg
	if fg == "0" { color = bg }
	return lipgloss.NewStyle().Foreground(color).Bold(bold).Render(label)
}
```

`bg` parametresi **arka plan olarak hiç kullanılmıyor** — sadece renkli kalın
metin üretiliyor. `KeyCap` da bunu çağırdığı için tuş kapakları da düz metin.
Kullanıcının istediği "anlaşılır butonlar" için gerçek çerçeve/dolgu gerekli.

### B4. `RenderInputField` çok satırlı kutuyu tek satır gibi birleştiriyor 🟠

[components.go:138](panel/components.go:138):

```go
res := fmt.Sprintf("%s : %s", lbl, box)
```

`box` `RoundedBorder` ile sarılı, yani **3 satırlık** bir blok. `%s` ile
birleştirmek 1. satırı etiketin yanına, 2-3. satırları **0. kolona** koyar →
form hizalaması tamamen bozulur. `lipgloss.JoinHorizontal` kullanılmalı.

### B5. Merkezî ölçü/token yok — hizalama her yerde farklı 🟠

Dağınık sabit genişlikler:

| Yer | Değer |
|---|---|
| [components.go:123](panel/components.go:123) `RenderInputField` etiket | `%-18s` |
| [components.go:243](panel/components.go:243) `kv` | `%-14s` |
| [components.go:52](panel/components.go:52) `RenderHeader` bar | `25` |
| [view_detail.go](panel/view_detail.go) USB satırı | `%-30s` |
| [components.go:22](panel/components.go:22) `RenderCard` min | `30 / 28` |

Kullanıcının talebi bu maddeyi doğrudan hedefliyor: **her arayüz elemanı bir
kez tanımlanmalı, adı ve uzunluğu belli olmalı, kullanım yerlerinde o sabit
çağrılmalı.**

### B6. `lipgloss.SetColorProfile` tema kurulumunda çağrılıyor 🟡

[theme.go:143](panel/theme/theme.go:143) — `New()` her çağrıldığında global
renk profilini yeniden set ediyor. Global durum, tema kurucusunun içinde
olmamalı; program başlangıcında bir kez yapılmalı.

### B7. Aynı fonksiyon iki dosyada — biri temizlenmiş 🟢

`scrollList` hem `panel/ui.go`'da hem
[components.go:211](panel/components.go:211)'de vardı; devam eden değişiklik
`ui.go` kopyasını sildi — **doğru** düzeltme. `ui.go` artık yalnızca
`clampLines` + `sliceScroll` içeriyor.

---

## Bölüm C — Sunucu başlatma

### C1. Başlatma hatası kullanıcıya anlamsız geliyor 🔴

[manager.go:124](internal/server/manager.go:124) `failStart` hatayı sarıyor:

```go
return failStart(fmt.Errorf("java bind: %w", err))
return failStart(fmt.Errorf("load launch info: %w", err))
return failStart(fmt.Errorf("install: %w", err))
```

Kullanıcı `load launch info: open /data/servers/x/data/.mcos-launch.json: no
such file or directory` gibi bir ham Go hatası görüyor. Türkçe, eyleme
dönüştürülebilir mesaj yok. Ayrıca:

- [manager.go:138](internal/server/manager.go:138) `loadLaunch` başarısız
  olabiliyor **ama** `EnsureInstalled` hemen öncesinde başarılı dönmüş
  olmalıydı; `IsInstalled` de aynı dosyayı okuyor
  ([install.go:30](internal/server/install.go:30)). Yani bu dal yalnızca
  dosya yarış/bozulma durumunda tetiklenir ve hata mesajı sebebi anlatmıyor.

### C2. "Done (" görülmezse sunucu sonsuza kadar "BAŞLIYOR" kalır 🟠

[manager.go:288](internal/server/manager.go:288):

```go
case strings.Contains(line, "Done (") && strings.Contains(line, "For help"):
```

Yalnızca bu iki kalıp aynı satırda geçerse durum `Running` olur. Forge/NeoForge
ve bazı sürümler farklı satır basar → sunucu gerçekten çalışsa bile panel
sonsuza kadar "BAŞLIYOR" gösterir. Zaman aşımı/yedek tespit yok.

### C3. `Restart` içinde veri yarışı 🟠

[manager.go:233](internal/server/manager.go:233):

```go
if p := m.rt(srv.ID).proc; p != nil {
```

`r.proc` **`r.mu` kilidi olmadan** okunuyor. Kod tabanının her yerinde bu alan
kilit altında okunuyor (`manager.go:175`, `246-248`, `309-312`). Gerçek race.

### C4. Oyuncu sayımı log metnine bağlı ve şişebilir 🟡

[manager.go:293](internal/server/manager.go:293) `joined the game` /
`left the game` içeren **her** satırda sayaç değişiyor. Bir oyuncu bu ifadeyi
sohbete yazarsa sayaç bozulur. `players.List` gerçek kaynak olarak var
([players.go](internal/players/players.go)) ama bu sayaç ondan bağımsız.

---

## Bölüm D — Güvenlik ve mimari (ilk denetimden)

### D1. Cluster TCP sunucusu kimlik doğrulaması olmadan her arabirimde dinliyor 🔴

- `ClusterConfig.Enabled` ([config.go:29](internal/model/config.go:29))
  OOBE'de kullanıcıya soruluyor ama **kodda hiç okunmuyor** (grep: 0 sonuç).
- [daemon.go:183](internal/daemon/daemon.go:183) `d.cluster.Start()` koşulsuz.
- [cluster.go:122](internal/cluster/cluster.go:122) `:27890` wildcard bind.
- [cluster.go:352](internal/cluster/cluster.go:352) `handlePeerConn` —
  token / `Paired` / kaynak-IP kontrolü **yok**; `assignTask` doğrudan
  `Executor.Execute` çağırıyor.
- [cluster.go:309](internal/cluster/cluster.go:309) keşfedilen her düğüm
  `Paired: true` ile **otomatik** eşleştiriliyor → `cluster.pair` RPC'si ve
  README'deki eşleştirme güvenlik hikâyesi pratikte yok.

LAN'daki herkes tekrarlı `assignTask{kind:"backup"}` ile diski doldurabilir.

### D2. `files.*` RPC'leri serverID doğrulamıyor → veri kökünden çıkış 🔴

[handlers.go:566-610](internal/daemon/handlers.go:566) — dört handler da
`p.ServerID`'yi doğrudan geçiyor, `store.GetServer` kontrolü yok.
`Paths.ServerData(id)` = `Join(Root,"servers",id,"data")` olduğundan
`id="../../.."` kökten çıkar; [files.go:34](internal/files/files.go:34)
prefix kontrolünü *aynı kaçmış kök* ile yaptığı için kontrol geçer.

Doğru desen zaten kod tabanında var — yeni USB handler'ı
[handlers_catalog.go](internal/daemon/handlers_catalog.go) `GetServer` yapıyor.

Ek olarak [files.go:31](internal/files/files.go:31) `resolve` symlink çözmüyor
(`EvalSymlinks` yok).

### D3. USB mod yükleme özelliği hiç çalışmıyor 🔴

[usb.go:62](internal/files/usb.go:62) tarama sonunda mount'u kaldırıyor:

```go
_ = exec.Command("umount", "-l", mountPoint).Run()
_ = os.Remove(mountPoint)
```

Ama panele dönen `USBJarItem.Path` o mount noktasının içini gösteriyor.
Kurulumda [usb.go:118](internal/files/usb.go:118) `os.Stat(src)` başarısız →
`continue` → `copied` 0 kalır ve **hata dönmez** → kullanıcı
`"0 mod/eklenti kuruldu"` başarı mesajı görür.

### D4. `usb.go` platform sözleşmesini ihlal ediyor + yanlış cihaz seçiyor 🟠

- Dosya `_linux.go` değil, build tag yok; ama `mount`/`/proc/mounts`/`/dev`
  kullanıyor. [README.md:96](README.md:96) kuralına aykırı.
- [usb.go:80](internal/files/usb.go:80) son karakter `'1'..'9'` testi
  **`nvme0n1`'i bölüm sanıyor** → sistem NVMe diskinin tamamı mount edilmeye
  çalışılıyor. Doğru desen `nvme0n1p1`.
- `/sys/block/<dev>/removable` hiç okunmuyor → "USB" iddiası yanlış, iç SATA
  diskler de taranıyor.
- Dedup anahtarı `isim+boyut` → farklı USB'lerdeki aynı isimli jar'lar birleşir.

### D5. `ipc` katmanı `files`'a bağımlı — katman ihlali 🟠

[types.go:4](internal/ipc/types.go:4) `import "mcos/internal/files"`.
`ipc` tek sözleşme katmanı ve Rust panelin aynaladığı yer; `files` ise
`store`+`log`'a bağlı iş mantığı paketi. `USBJarItem` diğer paylaşılan tipler
gibi `internal/model`'e ait.

### D6. `go build ./...` ve `make vet` bozuk 🟠

`os/buildroot/output/build/*/gcc/testsuite/go.*` altında binlerce geçersiz
`.go` dosyası var (ör. `bug257.go` 20 069 satır). [Makefile:51](Makefile:51)
`vet` hedefi `./...` kullanıyor → ISO bir kez derlendikten sonra kırılır.
[Makefile:48](Makefile:48) `test` hedefi doğru kapsamı kullanıyor — tutarsız.
[README.md:253](README.md:253) de yanlış komutu öneriyor.

### D7. Otomatik yedek zamanlayıcısı restart'ta sıfırlanıyor 🟡

[daemon.go:202](internal/daemon/daemon.go:202) `last` map'i yalnızca bellekte;
[daemon.go:222](internal/daemon/daemon.go:222) `!seen` dalı bir aralık daha
bekletiyor. Sık yeniden başlayan cihazda otomatik yedek hiç alınmaz.

[daemon.go:233](internal/daemon/daemon.go:233) `pruneBackups` yedek *submit*
edildikten hemen sonra çağrılıyor — yedek henüz üretilmemiş, budama bir tur
gecikmeli.

### D8. Test kapsamı riskli paketlerde sıfır 🟡

Toplam 17 083 satır, testler 906 satır (**%5,3**). Testi olmayan paketler:
`cluster` (kimliksiz ağ protokolü), `supervisor` (süreç yaşam döngüsü),
`files` (yol doğrulama), `ipc`, `backup`, `tunnel`, `netcfg`, `catalog`.
Hata maliyetinin en yüksek olduğu paketler tam olarak bunlar.

---

## Özet tablo

| # | Şiddet | Alan | Konu |
|---|---|---|---|
| A1 | 🔴 | boot | `root=LABEL=` çekirdek desteklemiyor → panic |
| A2 | 🔴 | boot | initrd hiç kopyalanmıyor; initrd eklemek de yanlış çözüm |
| A3 | 🟠 | boot | kernel BOOT bölümünde ama grub ROOT'ta arıyor |
| A4 | 🟠 | boot | defconfig'de var olmayan seçenekler → `sfdisk` yok |
| A5 | 🟠 | boot | 268 MB initramfs, ext4 rootfs imajı hiç üretilmiyor |
| A6 | 🟡 | boot | `genimage.cfg` ölü |
| A7 | 🟡 | boot | `mcos-persist` canlı USB'yi bulamıyor |
| A8 | 🟡 | boot | `fstab` `/dev` satırı yanlış |
| B1 | 🔴 | UI | fbterm birincil fontu emoji fontu → tüm gliflar bozuk |
| B2 | 🟠 | UI | 6 tema aslında tek tema; `Border`==`Muted` |
| B3 | 🟠 | UI | `renderPill` arka plan kullanmıyor, buton buton değil |
| B4 | 🟠 | UI | `RenderInputField` çok satırlı kutuyu bozuyor |
| B5 | 🟠 | UI | merkezî ölçü/token yok |
| B6 | 🟡 | UI | `SetColorProfile` tema kurucusunda |
| C1 | 🔴 | sunucu | başlatma hatası ham Go metni |
| C2 | 🟠 | sunucu | `Done (` görülmezse sonsuza kadar "BAŞLIYOR" |
| C3 | 🟠 | sunucu | `Restart` içinde `r.proc` yarışı |
| C4 | 🟡 | sunucu | oyuncu sayacı sohbetle şişebilir |
| D1 | 🔴 | güvenlik | cluster kimliksiz + `Enabled` okunmuyor |
| D2 | 🔴 | güvenlik | `files.*` serverID doğrulamıyor |
| D3 | 🔴 | özellik | USB mod yükleme 0 dosya kopyalıyor |
| D4 | 🟠 | özellik | `usb.go` platform/cihaz seçimi hatalı |
| D5 | 🟠 | mimari | `ipc` → `files` katman ihlali |
| D6 | 🟠 | derleme | `go build ./...` / `make vet` bozuk |
| D7 | 🟡 | daemon | yedek zamanlayıcı restart'ta sıfırlanıyor |
| D8 | 🟡 | test | riskli paketlerde test yok |
