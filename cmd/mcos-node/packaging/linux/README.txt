MCOS Düğüm — Linux
==================

Bu program, bu bilgisayarı MCOS'a "ikinci PC" olarak ekler. MCOS kurmanız
gerekmez: eşleşince MCOS buraya sunucuyu kurar, dünyanın bir bölgesini verir,
mod/eklentileri kopyalar, eşitler ve başlatır.

KURULUM (kök hakkı gerekmez)
  sh install.sh
  -> oturum açıldığında kendiliğinden başlayan bir systemd kullanıcı servisi kurar.

EŞLEŞTİRME (anahtar girmeniz gerekmez)
  MCOS'ta: MCOS Paylaşım -> Ağı tara -> bu PC'yi seçip Eşleştir.
  Burada "mcos-node" komutu 6 haneli bir KOD gösterir. MCOS ekranındaki
  kodla aynıysa K yazıp Enter (ya da: mcos-node --kabul KOD), MCOS'ta
  "Kodlar aynı, onayla". Anahtar otomatik gelir ve kaydedilir.
  Bulamazsa: MCOS'ta "IP adresi gir…" -> mcos-node komutunun gösterdiği adres.

KOMUTLAR
  mcos-node                    durumu gösterir (servis çalışıyorsa izler)
  mcos-node --kabul KOD        MCOS'tan gelen eşleştirme isteğini kabul eder
  mcos-node --reddet KOD       reddeder
  mcos-node --key ANAHTAR      (gelişmiş) anahtarı elle verir (servis yeniden başlar)
  mcos-node --durdur           düğümü durdurur (sunucu dünyayı kaydeder)
  sh install.sh --kaldir       kaldırır; dünyalar ~/.local/share/mcos-node'da kalır

Günlük: ~/.local/share/mcos-node/mcos-node.log
Portlar: TCP 2222 (eşleştirme), 25565-25600 (Minecraft), 27893 (oyuncu
aktarımı); UDP 27891 (ağda bulunma). Güvenlik duvarı varsa yerel ağa açın.
Oturum açmadan çalışsın: loginctl enable-linger $USER
