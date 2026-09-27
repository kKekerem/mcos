package vnc

import "crypto/des"

// ════════════════════════════════════════════════════════════════════════════
// VNC KİMLİK DOĞRULAMA (RFB güvenlik türü 2)
// ════════════════════════════════════════════════════════════════════════════
//
// Sunucu 16 baytlık rastgele bir meydan okuma gönderir; istemci onu parolayla
// DES-ECB ile şifreleyip geri yollar. Sunucu aynı işlemi yapıp karşılaştırır.
//
// ── İKİ TUHAFLIK ve neden onlara UYMAK ZORUNDAYIZ ───────────────────────────
//
//  1. PAROLA 8 BAYTA KIRPILIR. RFB'nin kendi tanımı bu. Daha uzun bir parola
//     sessizce kısalır — bu yüzden MCOS 8 karakterlik bir parola ÜRETİYOR ve
//     kullanıcıdan uzun bir parola istemiyor (sahte güvenlik olurdu).
//
//  2. ANAHTARIN HER BAYTI TERS ÇEVRİLİR. VNC'nin özgün uygulaması DES
//     anahtarını bit sırası ters yükleyen bir donanıma göre yazılmıştı ve o
//     hata protokolün parçası oldu. Bu ters çevirme yapılmazsa RealVNC dahil
//     HİÇBİR istemci bağlanamaz.
//
// ── Bu yeterince güvenli mi? ────────────────────────────────────────────────
//
// HAYIR — ve MCOS bunu gizlemiyor. VNC kimlik doğrulaması 8 karakterlik bir
// parolayı tek DES bloğuyla korur; ağı dinleyen biri kaba kuvvetle kırabilir.
// Telefon köprüsünün aksine trafik de ŞİFRELİ DEĞİLDİR (RFB düz metindir).
//
// Bu yüzden ekran paylaşımı:
//   - varsayılan KAPALI,
//   - yalnızca yerel ağ için önerilir (panel bunu ekranda yazar),
//   - internete açılacaksa SSH tüneli önerilir (Ayarlar ekranındaki not).
//
// Alternatif olan "None" güvenliği hiç sunulmuyor; parolasız bir VNC portu
// ağdaki herkese klavye ve ekran verirdi.

// vncEncrypt returns the DES-ECB encryption of challenge under password.
func vncEncrypt(challenge []byte, password string) []byte {
	key := vncKey(password)
	block, err := des.NewCipher(key)
	if err != nil {
		// des.NewCipher yalnızca anahtar 8 bayt değilse hata verir ve
		// vncKey her zaman 8 bayt döndürür. Yine de sessizce yanlış bir
		// sonuç döndürmek yerine BOŞ dönüyoruz: boş yanıt asla eşleşmez,
		// yani hata "herkesi içeri al" değil "kimseyi içeri alma" olur.
		return nil
	}
	out := make([]byte, len(challenge))
	for i := 0; i+8 <= len(challenge); i += 8 {
		block.Encrypt(out[i:i+8], challenge[i:i+8])
	}
	return out
}

// vncKey builds the 8-byte DES key: password padded with NULs, every byte
// bit-reversed (see the note above).
func vncKey(password string) []byte {
	key := make([]byte, 8)
	copy(key, password) // 8 bayttan uzunu kırpılır, kısası NUL ile dolar
	for i, b := range key {
		key[i] = reverseBits(b)
	}
	return key
}

// reverseBits flips the bit order of one byte (0b10000000 -> 0b00000001).
func reverseBits(b byte) byte {
	var r byte
	for i := 0; i < 8; i++ {
		r <<= 1
		r |= b & 1
		b >>= 1
	}
	return r
}

// newDES is a thin wrapper so tests can build the same cipher without
// importing crypto/des themselves.
func newDES(key []byte) (interface {
	Encrypt(dst, src []byte)
}, error) {
	return des.NewCipher(key)
}

// Encrypt exposes the VNC challenge encryption for tooling (tools/vncshot).
//
// İstemci tarafında İKİNCİ bir uygulama yazmak, ikisinin sessizce ayrışması
// demekti — ve o ayrışma yalnızca gerçek bir istemci bağlanmaya çalıştığında
// görünürdü. Tek uygulama, iki taraf.
func Encrypt(challenge []byte, password string) []byte {
	return vncEncrypt(challenge, password)
}
