MCOS Düğüm — Windows
====================

Bu program, bu bilgisayarı MCOS'a "ikinci PC" olarak ekler. MCOS kurmanız
gerekmez: eşleşince MCOS buraya sunucuyu kurar, dünyanın bir bölgesini verir,
mod/eklentileri kopyalar, eşitler ve başlatır.

KURULUM
  1. mcos-node.exe'ye çift tıklayın.
  2. Bir kez yönetici izni sorulur: güvenlik duvarında yalnızca YEREL AĞA
     izin verilir. "Evet" deyin.
  3. MCOS'ta: MCOS Paylaşım -> Ağı tara -> bu PC'yi seçip Eşleştir.
     Bu pencerede 6 haneli bir KOD çıkar. MCOS ekranındaki kodla aynıysa
     burada "Kabul et", MCOS'ta "Kodlar aynı, onayla". Anahtar girmeniz
     GEREKMEZ; otomatik gelir.
  Hepsi bu. Düğüm arka planda (penceresiz) çalışır ve oturum açıldığında
  kendiliğinden başlar. Pencereyi kapatmak düğümü durdurmaz.

MCOS BU PC'Yİ BULAMAZSA
  MCOS'ta "IP adresi gir…" -> düğüm penceresindeki adres.
  Eski bir MCOS sürümü kodla eşleştiremezse: pencerede "Gelişmiş: eşleştirme
  anahtarını elle gir" (MCOS: MCOS Paylaşım -> Eşleştirme anahtarını göster).

KOMUTLAR (Komut İstemi'nde, bu klasörde)
  mcos-node.exe                  durumu gösterir
  mcos-node.exe --kabul KOD      MCOS'tan gelen eşleştirme isteğini kabul eder
  mcos-node.exe --key ANAHTAR    (gelişmiş) anahtarı elle verir
  mcos-node.exe --durdur         düğümü durdurur (sunucu dünyayı kaydeder)
  mcos-node.exe --kur            kurulumu / güvenlik duvarı iznini yeniden yapar
  mcos-node.exe --kaldir         otomatik başlatmayı ve güvenlik duvarı
                                 kurallarını kaldırır; dünyalar silinmez

Program kendini %LOCALAPPDATA%\MCOS-Node\program\ altına kopyalar.
Günlük ve dünyalar: %LOCALAPPDATA%\MCOS-Node\
mods\link klasörü ortak dünya eklentileridir (her Minecraft sürümü için ayrı
jar + index-*.tsv); mcos-node.exe'nin yanında durmalı.
