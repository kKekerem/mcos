package main

import (
	"os"
	"path/filepath"
)

// anchorRunDir, durum/hazır/kare dosyalarını ÇALIŞMA DİZİNİNE bağlar.
//
// ════════════════════════════════════════════════════════════════════════════
// DÜZELTİLEN GERÇEK HATA: DİSKE KURULUMDAN SONRA AÇILIŞ EKRANI "KÖR" KALIYORDU
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı (VirtualBox, diske kurduktan sonra): "açılış ekranı 'sistem
// başlatılıyor'da takılı kaldı, sonra panel birden geldi".
//
// Açılış ekranı initramfs'in /init'inden başlıyor. Diskten açılışta /init
// kökü diske devrediyor:
//
//	mount -o move /dev "$NEWROOT/dev"
//	exec switch_root "$NEWROOT" /sbin/init
//
// switch_root YALNIZCA init'in kökünü değiştirir. Arka planda çalışan bu
// sürecin kökü ESKİ initramfs olarak kalır; orada /dev artık boş bir dizindir
// (devtmpfs yeni köke taşındı). Yani "/dev/.mcos/stage" ve
// "/dev/.mcos/ready" yolları bu süreç için bir daha HİÇ çözülmüyordu:
//
//   - aşama metni ilk satırda ("Sistem başlatılıyor…", %5) donuyordu,
//   - "hazırım" bayrağı hiç görülmüyordu; mcos-launch 5 sn bekleyip SIGTERM
//     gönderiyordu, süreç konsolu metin kipine döndürüp KAREYİ KAYDETMEDEN
//     çıkıyordu ve panel yakınlaşma geçişi olmadan, birden beliriyordu.
//
// Canlı (USB) açılışta switch_root yok; bu yüzden hata yalnızca diske
// kurulumdan sonra görülüyordu.
//
// ── Çözüm ───────────────────────────────────────────────────────────────────
//
// Açılışta dizine chdir yapılır ve dosyalara GÖRELİ adla bakılır. Çalışma
// dizini bir yol değil, dizinin kendisine tutulan bir referanstır: devtmpfs
// başka bir yere taşınsa da (mount --move) aynı dizini göstermeye devam eder.
// mcos-stage'in yazdığı, mcos-launch'ın oluşturduğu ve panelin okuduğu
// dosyaların hepsi aynı devtmpfs dizininde olduğu için artık her iki açılış
// yolunda da görülüyorlar.
//
// Yalnızca üç yol da AYNI dizindeyse uygulanır (varsayılan budur); aksi hâlde
// yollar olduğu gibi kalır — eski davranış, hiçbir şey bozulmaz.
func anchorRunDir(until, stage, save string) (string, string, string) {
	dir := ""
	for _, p := range []string{until, stage, save} {
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			return until, stage, save
		}
		d := filepath.Dir(p)
		if dir == "" {
			dir = d
		} else if d != dir {
			return until, stage, save
		}
	}
	if dir == "" {
		return until, stage, save
	}
	if err := os.Chdir(dir); err != nil {
		return until, stage, save
	}
	rel := func(p string) string {
		if p == "" {
			return ""
		}
		return filepath.Base(p)
	}
	return rel(until), rel(stage), rel(save)
}
