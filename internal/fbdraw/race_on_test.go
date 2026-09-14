//go:build race

package fbdraw

// raceEnabled, testin yarış dedektörü altında koştuğunu söyler.
//
// NEDEN GEREKLİ: bu pakette iki kod yolunun SÜRESİNİ oranlayan bir test var
// (TestBlurLargeRegionIsFast). Yarış dedektörü her bellek erişimini araçlar,
// yani maliyeti erişim SAYISIYLA orantılıdır — küçülterek çalışan yol ile tam
// çözünürlükte çalışan yolun erişim profilleri farklı olduğu için oran
// sistematik olarak bozulur. Ölçüldü: normalde 4+ kat olan hızlanma -race
// altında 2.7 kata düşüyor ve test, hiçbir şey bozulmamışken kırmızı yanıyor.
//
// Yanlış alarm veren bir test, zamanla bakılmayan bir teste dönüşür. Bu yüzden
// ORAN yalnızca ölçümün anlamlı olduğu koşuda doğrulanıyor; doğruluk testleri
// (bulanıklık gerçekten yayıyor mu, parlaklık korunuyor mu) -race altında da
// koşmaya devam ediyor.
const raceEnabled = true
