@echo off
rem ============================================================================
rem  MCOS Flash - Windows baslaticisi
rem ============================================================================
rem
rem  Ne yapar:
rem    1. Yonetici hakki yoksa kendini -argumanlariyla birlikte- yonetici
rem       olarak yeniden baslatir,
rem    2. mcos-flash.exe'yi bu klasorden calistirir,
rem    3. pencereyi acik tutar ki hata mesaji okunabilsin.
rem
rem  NEDEN YONETICI: Windows, bir diske ham yazmak icin yonetici hakki ister.
rem  Haksiz da degil - bu islem diskteki her seyi siler. Hak olmadan
rem  calistirildiginda "Erisim engellendi" hatasi alinir ve nedeni anlasilmaz;
rem  bu yuzden en bastan hakki istiyoruz.
rem
rem  NEDEN .bat, NEDEN .exe'ye CIFT TIKLAMIYORUZ: .exe'ye cift tiklamak yonetici
rem  hakki VERMEZ ve kullanici hatayi gormeden pencereyi kapanmis bulur. Bu
rem  dosya ikisini de cozer.
rem
rem  ---------------------------------------------------------------------------
rem  NEDEN BU DOSYADA TURKCE HARF (c-cedilli, yumusak g, noktasiz i, ...) YOK
rem  ve NEDEN SATIR SONLARI CRLF: ikisi de zorunluluk, tercih degil.
rem
rem  cmd.exe bir toplu is dosyasini okurken siradaki satirin yerini BAYT
rem  cinsinden saklar, ama satiri konsolun kod sayfasina gore KARAKTERE cevirip
rem  ayristirir. Konsol UTF-8 ise (chcp 65001 - Windows Terminal'de ve
rem  "Dunya capinda UTF-8 destegi" secenegi acikken artik sik rastlanir) buradaki
rem  her Turkce harf 2 bayt tutar, iki sayac birbirinden kayar ve cmd bir
rem  sonraki satirin ORTASINDAN okumaya baslar. Sonuc: yorum satirlarinin
rem  parcalari komut olarak calisir.
rem
rem  Olculdu (bu dosyanin onceki UTF-8 + LF hali, chcp 65001 acik konsolda):
rem      'Turkce' is not recognized as an internal or external command
rem      '_PAUSE' is not recognized as an internal or external command
rem  ...gibi onlarca satir dokuluyor; akis bozuluyor. Ayni dosya yalniz ASCII
rem  harflerle, ya da CRLF satir sonlariyla tertemiz calisiyor.
rem
rem  Ikisini birden uyguluyoruz: CRLF dogru olan, ASCII ise garanti olan.
rem  Depoda core.autocrlf acik oldugu icin satir sonlari bir gun LF'e
rem  normallesebilir; dosya salt ASCII kaldigi surece o gun de calisir.
rem  Yorumlara Turkce harf EKLEMEYIN - bu bir bicim tercihi degil, hata kaynagi.
rem  ---------------------------------------------------------------------------
rem
rem  NEDEN GECIKMELI GENISLETME KAPALI: "setlocal enabledelayedexpansion"
rem  acikken icinde "!" gecen bir arguman (or. C:\imaj!eski\mcos.img) sessizce
rem  kirpilirdi. Hata kodunu blok icinde okumamiz gereken yerde
rem  "if errorlevel 1" kullaniyoruz; bu sozdizimi %ERRORLEVEL% genisletmesine
rem  hic gerek duymaz, kodu calisma aninda okur.
rem ============================================================================

setlocal
cd /d "%~dp0"

rem  Kendi yolumuzu HEMEN, hicbir "shift" calismadan once saklayalim.
rem  Yakalanan gercek hata (bu dosyada, arguman dongusu eklenirken): "shift"
rem  yalnizca %1, %2, ... degil %0'i da kaydirir. Dongu bir arguman yuttuktan
rem  sonra %~f0 artik betigin yolu degil, kaydirilan argumanin kendisiydi;
rem  yukseltme cagrisi "...\scratchpad\--dry-run" dosyasini baslatmaya
rem  calisiyordu (olculdu). Asagida ayrica "shift /1" kullaniyoruz - /1, kaydirmayi
rem  1. argumandan baslatir ve %0'a hic dokunmaz. Ikisi birlikte hem bu tuzagi
rem  kapatir hem de sonradan duzenleyen icin gorunur kilar.
set "MCOS_SELF=%~f0"
set "MCOS_DIR=%~dp0"

rem -- Argumanlari topla -------------------------------------------------------
rem  Yakalanan gercek hata: bu betik kendini yukseltirken argumanlari
rem  DUSURUYORDU (Start-Process'e -ArgumentList hic verilmiyordu).
rem  "mcos-flash.bat --dry-run" yazan kullanici, UAC onayini verdikten sonra
rem  argumansiz bir mcos-flash.exe ile karsilasiyordu; argumansiz calistirma ise
rem  etkilesimli sihirbazin ta kendisidir (bkz. cmd/mcos-flash/main.go usage).
rem  Yani "hicbir sey yazma, sadece akisi sina" diyen kullanici, uyaran tek bir
rem  satir bile gormeden GERCEKTEN DISK SILEN akisin icinde buluyordu kendini.
rem  POSIX kardesi bunu dogru yapiyor: mcos-flash.sh icinde
rem  "exec sudo -- $EXE $@".
rem
rem  NEDEN %* DEGIL de argumanlar tek tek toplaniyor:
rem    1. "shift" komutu %* degerini DEGISTIRMEZ. Yukseltirken basa ekledigimiz
rem       --mcos-elevated nobetci bayragi %* icinde kalir, exe'ye sizar ve
rem       Go'nun flag paketi "flag provided but not defined" deyip cikar.
rem       Bayragi ancak elle ayiklayabiliriz.
rem    2. `set "VAR=%*"` satiri tirnak esligini BOZAR: satir basindaki tirnak
rem       bir tirnak acar, argumanin kendi tirnagi onu kapatir ve geri kalan
rem       metin tirnak DISINDA kalir. `--image "C:\a&b\x.img"` boyle bir
rem       satirda "&" karakterini komut ayracina cevirip satiri ikiye boler
rem       (denendi: "& was unexpected at this time" ve satirin kalani komut
rem       olarak calisir). "%~1" ile okumak her argumani kendi dengeli tirnak
rem       ciftine kapattigi icin bu tuzak olusmaz.
rem
rem  MCOS_ARGS    : exe'ye verilecek liste; her arguman ayri ayri tirnaklanir.
rem  MCOS_ARGS_PS : yukseltme komut satirina konacak liste; tirnaksiz, cunku
rem                 PowerShell cagrisinin kendisi zaten tirnak icinde.
rem  MCOS_UNSAFE  : en az bir arguman yukseltmeden sag gecirilemiyor.
set MCOS_ARGS=
set MCOS_ARGS_PS=
set MCOS_UNSAFE=
set MCOS_RELAUNCHED=

:collect_args
rem  "ARGUMAN KALMADI" ile "BOS ARGUMAN" ayri seylerdir; ayri denetim isterler.
rem  Yakalanan gercek hata: tek basina duran `if "%~1"=="" goto args_done`
rem  satiri, komut satirindaki BOS bir argumani ("") "liste bitti" ile ayni
rem  sayiyordu - cunku %~1 cevreleyen tirnaklari soyar ve geriye bos dize
rem  kalir. Boylece
rem      mcos-flash.bat --image "" --dry-run
rem  cagrisinda dongu ilk bos argumanda duruyor, ARKASINDAKI --dry-run
rem  sessizce dusuyordu. Sonuc yine argumansiz bir exe: yani etkilesimli,
rem  GERCEKTEN DISK YAZAN oturum. 16. bulgunun ta kendisi, baska bir kapidan.
rem  Ayirt edici %1: argumanin tirnaklari soyulmamis halidir. Arguman hic
rem  yoksa bos gelir; bos arguman varsa iki tirnaktan ibaret ("") bir metin
rem  gelir. Bu satira ancak %~1 bos oldugunda ulasilir, oradaki icerik de
rem  yalnizca tirnaklardan ibaret olabilir; bu yuzden tirnaksiz %1 kullanmak
rem  burada "&" tuzagini acmaz.
if not "%~1"=="" goto have_arg
if "%1"=="" goto args_done

:have_arg
rem  Nobetci bayragini yalnizca bu betik ekler; exe'ye gecmesin diye listeye
rem  alinmadan yutuluyor.
rem  NEDEN OLUMLU TEST + AYRI ETIKET: onceki `if /i not ... goto keep_arg`
rem  bicimi, karsilastirma satiri ayristirilamadiginda (argumanin ICINDE cift
rem  tirnak varsa cmd "sozdizimi hatali" deyip bir SONRAKI satirdan devam
rem  eder) akisi dogrudan `set MCOS_RELAUNCHED=1` satirina dusuruyordu:
rem  arguman listeden dusuyor, ustelik betik kendini "zaten yukseltilmis"
rem  saniyor ve 17. bulgunun nobetcisini kendi elleriyle bosa cikariyordu.
rem  Simdi ayni dusme, argumani normal isleyen yola cikar; arguman yutulmak
rem  yerine "tasinamaz" damgasi yer ve yukseltme reddedilir. Kusurlu durumda
rem  susmak degil, durmak.
if /i "%~1"=="--mcos-elevated" goto eat_sentinel
goto keep_arg

:eat_sentinel
set MCOS_RELAUNCHED=1
goto next_arg

:keep_arg
rem  Guvenli karakter suzgeci. delims= listesindeki karakterler AYRAC sayilir;
rem  arguman tamamen bu listeden olusuyorsa geriye tek bir belirtec kalmaz ve
rem  dongu govdesi hic calismaz. Govde calisiyorsa argumanda, yukseltme komut
rem  satirindan sag salim geciremeyecegimiz bir karakter var demektir:
rem    "      -> cmd'nin -Command belirtecini erkenden kapatir,
rem    '      -> PowerShell'in tek tirnakli dizesini erkenden kapatir,
rem    bosluk -> -ArgumentList icinde tirnaksiz durdugu icin arguman ikiye
rem              bolunur ("C:\imaj klasoru\x.img" iki ayri arguman olurdu),
rem    & | ^ ( ) %% ! -> cmd'nin ayristiricisina yem olur,
rem    ASCII disi (or. C:\Imajlar) -> yukselen kopya BASKA bir kod sayfasiyla
rem              acildigi icin yolun bozulma ihtimali var; tahmin etmektense
rem              reddedip kullaniciya ne yapacagini soyluyoruz.
rem  Basa konan "x": for /f, ";" ile baslayan satiri varsayilan olarak yorum
rem  sayip atlar ve bos satiri hic islemez; sabit bir harf eklemek bu iki kacagi
rem  da kapatir ("x" listede oldugu icin sonucu degistirmez).
set MCOS_ARG_UNSAFE=
set MCOS_ARG_SEEN=
rem  BOS ARGUMAN yukseltmeden sag gecemez: -ArgumentList tek bir DIZEDIR ve
rem  argumanlar orada yalnizca BOSLUKLA ayrilir; bos bir arguman o dizede
rem  hicbir iz birakmaz. Sessizce atlasaydik yukselen kopyada arguman SIRASI
rem  kayardi - "--image" ile eslesen, kullanicinin verdigi bos deger yerine
rem  bir SONRAKI bayrak olurdu. Tasiyamiyorsak reddediyoruz.
if "%~1"=="" set MCOS_ARG_UNSAFE=1
rem  Asil suzgecin CALISTIGINI ayrica kanitliyoruz. O suzgec "temiz" sonucunu
rem  govdesinin HIC calismamasiyla bildirir - ama satirin kendisi
rem  ayristirilamayip hatayla dustugunde de govde calismaz. Iki durum disaridan
rem  birbirinin ayni gorunur ve ikincisi yanlislikla "guvenli" diye okunurdu.
rem  Bu probun tek isi farki acmak: delims bos oldugu icin daima bir belirtec
rem  vardir, yani satir calisabildiyse MCOS_ARG_SEEN mutlaka kurulur.
rem  Kurulmadiysa FOR ayristirilamamistir (or. argumanin icinde cift tirnak
rem  vardir) ve asil suzgecin sessizligi kanit sayilamaz. Supheli durumda
rem  yukseltmeyi reddetmek, sessizce yanlis arguman gondermekten iyidir -
rem  karsi tarafta disk siliniyor.
for /f "tokens=1 delims=" %%U in ("x%~1") do set MCOS_ARG_SEEN=1
if not defined MCOS_ARG_SEEN set MCOS_ARG_UNSAFE=1
for /f "delims=abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.:\/=" %%U in ("x%~1") do set MCOS_ARG_UNSAFE=1
if defined MCOS_ARG_UNSAFE set MCOS_UNSAFE=1
rem  Tirnakli "set" bicimi: %~1 cevresindeki tirnaklardan arindirilmis oldugu
rem  icin satirdaki tirnaklar dengede kalir ve icindeki "&" komut ayracina
rem  donusemez. Deger yalnizca guvenli argumanlar icin birikir.
if not defined MCOS_ARG_UNSAFE set "MCOS_ARGS_PS=%MCOS_ARGS_PS% %~1"
rem  Tirnaksiz "set" bicimi + elle tirnak: her arguman kendi tirnak ciftiyle
rem  sarilir, boylece "C:\imaj klasoru\mcos.img" gibi BOSLUKLU yollar exe'ye tek
rem  parca ulasir; tirnaklari exe'nin calisma zamani soyar.
set MCOS_ARGS=%MCOS_ARGS% "%~1"

:next_arg
rem  "/1": kaydirma 1. argumandan baslasin; %0 (betigin kendi yolu) bozulmasin.
shift /1
goto collect_args

:args_done

rem -- Yonetici denetimi -------------------------------------------------------
rem  Yakalanan gercek hata: denetim "net session" ile yapiliyordu. O komut
rem  LanmanServer ("Server") hizmetine baglidir; hizmet durdurulmus ya da devre
rem  disi birakilmissa (Home kurulumlari, dosya paylasimi kapatilmis makineler)
rem  TAM YONETICI HAKKIYLA bile sifirdan farkli doner. Sonuc: betik kendini
rem  yukseltiyor, yukselen kopya ayni denetimde yine kaliyor, bir kopya daha
rem  aciyor... Kullanicinin gordugu sey, tek bir hata mesaji bile olmadan
rem  ekranda birbirini kovalayan sonsuz konsol penceresiydi; mcos-flash.exe hic
rem  calismiyordu.
rem
rem  Yerine "fsutil dirty query": hicbir hizmete bagli degil ve yalnizca birimin
rem  "kirli" bitini SORGULAR, hicbir sey yazmaz. Yonetici belirteci yoksa
rem  "Error 5" ile duser. Ciktisi yerellestirilmistir ("Erisim engellendi"), bu
rem  yuzden metne degil sadece cikis koduna bakiyoruz - Turkce Windows'ta da
rem  ayni calissin diye.
rem  Tam yolla cagiriyoruz: cmd komutu once icinde bulundugumuz klasorde arar ve
rem  burasi indirilenler klasoru olabilir; oraya "fsutil.bat" birakan biri aksi
rem  halde yukseltilmis haklarla calisirdi.
set MCOS_ADMIN=
"%SystemRoot%\System32\fsutil.exe" dirty query "%SystemDrive%" >nul 2>&1
if not errorlevel 1 set MCOS_ADMIN=1

if defined MCOS_ADMIN goto run

rem  Nobetci: buraya zaten yukseltilmis bir kopya olarak geldiysek yukseltmeyi
rem  BIR DAHA denemiyoruz. UAC onayi verilmemis olsaydi bu kopya hic
rem  calismazdi; demek ki denetim yanlis negatif verdi. Uyarip devam etmek hem
rem  sonsuz pencere zincirinden hem de sessizce pes etmekten iyidir: hak
rem  gercekten yoksa exe zaten "Erisim engellendi" deyip duracak.
if defined MCOS_RELAUNCHED (
    echo.
    echo   UYARI: yonetici denetimi sonuc vermedi, ama bu kopya zaten
    echo   yukseltilmis olarak baslatildi. Devam ediliyor.
    echo.
    goto run
)

rem  Tasinamayan arguman varsa YUKSELTMIYORUZ. Sessizce dusurmek, kullanicinin
rem  "--dry-run" istegini canli bir yazma oturumuna cevirir; bu satirlarin
rem  varlik sebebi tam olarak budur.
rem  NEDEN BLOK DEGIL de duz satirlar: parantezli bir blok tek seferde
rem  ayristirilir ve icindeki degiskenler o anda genisler; argumanda bir ")"
rem  varsa blogu erkenden kapatirdi. Duz satirlarda echo'nun gordugu ")"
rem  sadece metindir.
if not defined MCOS_UNSAFE goto elevate
echo.
echo   Bu argumanlari yonetici penceresine guvenle tasiyamiyorum.
echo   Yonetici komut istemi acin ve sunu yapistirin:
echo.
rem  Yakalanan gercek hata: bu satir %* basiyordu. %*, kullanicinin yazdigi
rem  metni HAM haliyle tasir. Tirnaksiz bir "&" - komut isteminde
rem  "--image C:\a^&b\x.img" yazildiginda argumana duz "&" olarak ulasir - bu
rem  satiri tam ortasindan ikiye bolerdi: kullanici yapistiracagi komutu
rem  goremez, ustelik satirin kalani KOMUT olarak calisirdi. Yani satir, tam da
rem  kendisini tetikleyen karakterlerde patliyordu; oysa tek isi o karakterleri
rem  bildirmekti. %MCOS_ARGS% icinde ise her arguman kendi DENGELI tirnak
rem  ciftindedir; tirnak icindeki "&" cmd icin duz metindir ve basilan satir
rem  oldugu gibi yonetici istemine yapistirilabilir.
rem  Basta ikinci bir bosluk yok: MCOS_ARGS zaten bosluk ile baslar.
echo       "%MCOS_SELF%"%MCOS_ARGS%
echo.
pause
exit /b 1

:elevate
echo.
echo   MCOS Flash yonetici hakki istiyor.
echo   Onay penceresi acilacak...
echo.
rem  PowerShell ile kendini yukselterek yeniden baslat. -Verb RunAs, Windows'un
rem  kendi UAC onayini gosterir; parola sorulmaz.
rem
rem  TIRNAKLAR NEDEN BOYLE:
rem   * Dis tirnaklar ("): cmd'nin tum ifadeyi -Command'a TEK belirtec olarak
rem     vermesi icin. "C:\Program Files\..." gibi bosluklu bir yol da boylece
rem     PowerShell'e tek parca ulasir.
rem   * Ic tirnaklar ('): PowerShell'in tek tirnagi. Icinde $ ve ters tirnak
rem     yorumlanmaz, dolayisiyla yoldaki bir "$" bozulmaz. Yolda tek tirnak
rem     varsa (C:\Users\D'Arcy) dizeyi erken kapatirdi; PowerShell'de kacis
rem     yontemi tirnagi ikiye katlamaktir, MCOS_SELF'i asagida bunun icin
rem     isliyoruz.
rem   * set'in tirnakli bicimi: yol icinde "&" ya da "(" gecen bir dizin
rem     (C:\Program Files (x86)\...) tirnaksiz bir set satirini bolerdi.
rem   * -ArgumentList'e HER ZAMAN en az nobetci bayragi gidiyor; boylece
rem     PowerShell'in reddettigi "-ArgumentList ''" durumu hic olusmuyor ve
rem     argumanli/argumansiz tek bir kod yolu kaliyor.
set "MCOS_SELF_PS=%MCOS_SELF:'=''%"
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
    "Start-Process -FilePath '%MCOS_SELF_PS%' -ArgumentList '--mcos-elevated%MCOS_ARGS_PS%' -Verb RunAs" 2>nul
if errorlevel 1 (
    echo   Yukseltme basarisiz. Bu dosyaya SAG TIKLAYIP
    echo   "Yonetici olarak calistir" secin.
    pause
    exit /b 1
)
rem  Is artik ayri, yukseltilmis bir pencerede; onun cikis kodunu buradan
rem  goremeyiz. Betigi bir betikten/CI'dan cagiriyorsaniz zaten yukseltilmis bir
rem  kabuktan cagirin: buradan donen 0 "kurulum bitti" demek degil, "devretme
rem  basarili" demektir.
exit /b 0

:run
rem -- Ikiliyi bul -------------------------------------------------------------
set "EXE=%MCOS_DIR%mcos-flash.exe"
if not exist "%EXE%" (
    echo.
    echo   mcos-flash.exe bu klasorde bulunamadi:
    echo     %MCOS_DIR%
    echo.
    echo   Derlemek icin:  make flash-windows
    echo.
    pause
    exit /b 1
)

rem  Konsolu bu betik acik tuttugu icin program kendi beklemesini yapmasin.
set MCOS_FLASH_NO_PAUSE=1

rem  UTF-8 kod sayfasi: exe'nin bastigi Turkce karakterler bozuk gorunmesin.
rem  (Yukaridaki mesajlar bilerek ASCII: chcp'den once calistiklari icin konsol
rem  hala makinenin kendi kod sayfasinda ve Turkce harfler bozuk cikardi.)
chcp 65001 >nul 2>&1

"%EXE%" %MCOS_ARGS%
set RC=%errorlevel%

echo.
if %RC% neq 0 (
    echo   Islem basarisiz oldu ^(cikis kodu %RC%^).
) else (
    echo   Islem tamamlandi.
)
echo.
pause
exit /b %RC%
