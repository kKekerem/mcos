//go:build race

package fbpanel

// raceEnabled, testin yarış dedektörü altında koştuğunu söyler.
//
// NEDEN GEREKLİ: kapanış animasyonu ZAMANA bağlı çalışır (16 ms'de bir kare,
// toplam 1,64 sn). Yarış dedektörü her bellek erişimini araçladığı için tek
// bir karenin çizimi kat kat uzuyor ve 1,64 saniyeye sığan kare sayısı
// düşüyor. Ölçüldü: normal koşuda ~100 kare, -race altında ~15.
//
// Bunun sonucu, "ara kare yakalandı mı" denetimini kırılgan yapıyor: siyaha
// geçiş evresi (son 620 ms) yalnızca birkaç kare çizilebiliyor ve o karelerin
// hepsi eşiklerin dışında kalabiliyor. Test hiçbir şey bozulmamışken kırmızı
// yanıyordu.
//
// Yanlış alarm veren bir test, zamanla bakılmayan bir teste dönüşür. Bu yüzden
// ARA KARE denetimi yalnızca ölçümün anlamlı olduğu koşuda yapılıyor;
// animasyonun GERÇEKTEN oynadığı (kare sayısı) ve SİYAHLA bittiği -race
// altında da doğrulanmaya devam ediyor.
//
// Aynı desen internal/fbdraw'da da var (race_on_test.go) ve aynı gerekçeyle.
const raceEnabled = true
