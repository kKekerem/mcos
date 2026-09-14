@echo off
rem ============================================================================
rem  MCOS Dugum - Windows baslatici
rem ============================================================================
rem
rem  Ne yapar:
rem    Bu bilgisayari MCOS'a "ikinci PC" olarak ekleyen programi calistirir.
rem    Cift tiklamak yeterlidir.
rem
rem  NEDEN YONETICI HAKKI ISTEMIYOR: mcos-flash'in aksine bu program hicbir
rem  diske ham yazma yapmaz. Yalnizca kendi klasorune yazar ve bir TCP portu
rem  dinler. Gereksiz yere yonetici istemek, kullanicinin "evet"e refleksle
rem  basmasini ogretir; istemedigimiz tam olarak budur.
rem
rem  NEDEN chcp 65001: Turkce karakterler (ç, ğ, ı, ö, ş, ü) ve durum
rem  ekranindaki cizgi karakterleri, kod sayfasi 65001 (UTF-8) olmadan
rem  bozuk gorunur.
rem ============================================================================

setlocal

rem Kod sayfasini UTF-8 yap; ciktisini yut, kullaniciyi ilgilendirmiyor.
chcp 65001 >nul 2>&1

rem Betigin bulundugu klasor. %~dp0 sondaki ters bolu ile gelir.
set "HERE=%~dp0"
set "EXE=%HERE%mcos-node.exe"

if not exist "%EXE%" (
    echo.
    echo   mcos-node.exe bu klasorde bulunamadi:
    echo     %HERE%
    echo.
    echo   Derlemek icin:  make node-windows
    echo.
    pause
    exit /b 1
)

rem %* : kullanicinin verdigi TUM argumanlari oldugu gibi aktar.
rem
rem NEDEN ONEMLI: argumanlari aktarmayan bir baslatici, "--key ..." gibi
rem secenekleri sessizce yutar ve kullanici neden ise yaramadigini anlayamaz.
"%EXE%" %*

rem Pencereyi acik tut: program bir hatayla cikarsa kullanici mesaji gormeli.
rem Normal cikista (Ctrl+C) da mesaji okuyacak zaman taniyoruz.
echo.
pause

endlocal
