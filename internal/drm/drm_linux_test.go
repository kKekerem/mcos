//go:build linux

package drm

import (
	"testing"
	"unsafe"
)

// ════════════════════════════════════════════════════════════════════════════
// ÇEKİRDEK YAPILARIYLA BİREBİR UYUM
// ════════════════════════════════════════════════════════════════════════════
//
// ── Neden bu test hayati ────────────────────────────────────────────────────
//
// DRM ioctl NUMARASI, yapının BOYUTUNU içerir:
//
//	_IOC(dir,type,nr,size) = (dir<<30) | (size<<16) | (type<<8) | nr
//
// Yani bir alan eklenir/çıkarılır ya da hizalama kayarsa ioctl numarası
// DEĞİŞİR ve çekirdek isteği tanımaz. Daha kötüsü: boyut doğru ama alan
// SIRASI yanlışsa çekirdek yanlış yere yazar ve bu, kullanıcı alanı
// belleğinin bozulması demektir.
//
// Boyutlar include/uapi/drm/drm_mode.h'den hesaplandı ve çalışan sistemde
// doğrulandı (MCOS içinde, QEMU/bochs-drm):
//
//	cardRes=64  getConn=80  modeInfo=68
//	-> crtc=1 baglayici=1, 21 mod, 1280x800@75Hz tercih edilen

func TestYapiBoyutlariCekirdekleUyuyor(t *testing.T) {
	for _, tc := range []struct {
		ad    string
		got   uintptr
		bekle uintptr
	}{
		// 4 x __u64 (32) + 8 x __u32 (32) = 64
		{"drm_mode_card_res", unsafe.Sizeof(cardRes{}), 64},
		// 4 x __u64 (32) + 12 x __u32 (48) = 80
		{"drm_mode_get_connector", unsafe.Sizeof(getConn{}), 80},
		// __u32 (4) + 10 x __u16 (20) + 3 x __u32 (12) + char[32] = 68
		{"drm_mode_modeinfo", unsafe.Sizeof(ModeInfo{}), 68},
	} {
		if tc.got != tc.bekle {
			t.Errorf("%s boyutu %d; çekirdekte %d — ioctl numarası YANLIŞ "+
				"çıkar ve çekirdek belleği bozulabilir", tc.ad, tc.got, tc.bekle)
		}
	}
}

// ioctl numarası el ile hesaplanan değerle uyuşmalı.
func TestIoctlNumarasi(t *testing.T) {
	// DRM_IOCTL_MODE_GETRESOURCES = DRM_IOWR(0xA0, struct drm_mode_card_res)
	// = (3<<30) | (64<<16) | ('d'<<8) | 0xA0
	bekle := uintptr(3)<<30 | 64<<16 | 'd'<<8 | 0xA0
	if got := iowr(nrGetResources, 64); got != bekle {
		t.Errorf("GETRESOURCES ioctl %#x; %#x bekleniyordu", got, bekle)
	}
}

// ── Mod seçimi ──────────────────────────────────────────────────────────────

func mod(w, h, hz int, tercih bool) Mode {
	// MilliHz DE doldurulmali: sıralama tam değeri kullanıyor (59,94 ile
	// 60,00'i ayırt edebilmek için). Yalnızca Refresh vermek, testin
	// sıralamayı sıfırlarla yapmasına yol açardı.
	return Mode{Width: w, Height: h, Refresh: hz, MilliHz: hz * 1000,
		Preferred: tercih}
}

// ── Seçim politikası: ÖNCE ALAN ────────────────────────────────────────────
//
// Kullanıcı "maks çözünürlükte maks yenileme hızında açılsın" dedi. Bu iki
// alternatifi de eler:
//
//   - Tazelemeyi öne almak 640x480@75'i 1920x1080@60'ın üstüne çıkarırdı.
//   - Sürücünün TERCİH ettiğini öne almak da yetmiyor: ölçüldü ki QEMU'da
//     tercih edilen 1280x800 iken listede 2560x1080 vardı.
//
// Bu yüzden tercih bayrağı yalnızca EŞİTLİK BOZUCU.
func TestEnBuyukCozunurlukKazanir(t *testing.T) {
	ms := []Mode{
		mod(1920, 1080, 60, false),
		mod(1280, 800, 75, true), // tercih edilen ama KÜÇÜK
		mod(2560, 1080, 50, false),
	}
	sirala(ms)
	b, ok := Best(ms)
	if !ok || b.Width != 2560 {
		t.Errorf("en iyi mod %v; en büyük alan (2560x1080) olmalı", b)
	}
}

// Alan EŞİTSE tercih edilen kazanmalı: aynı boyutta iki mod arasında
// sürücünün doğal saydığı doğru seçimdir.
func TestAlanEsitseTercihKazanir(t *testing.T) {
	ms := []Mode{
		mod(1920, 1080, 60, false),
		mod(1920, 1080, 60, true),
	}
	sirala(ms)
	if b, _ := Best(ms); !b.Preferred {
		t.Error("alan ve tazeleme eşitken tercih edilen mod seçilmedi")
	}
}

// Aynı çözünürlükte EN YÜKSEK tazeleme seçilmeli.
func TestAyniCozunurlukteEnHizli(t *testing.T) {
	ms := []Mode{
		mod(1920, 1080, 50, false),
		mod(1920, 1080, 60, false),
		mod(1280, 800, 75, false),
	}
	sirala(ms)
	b, _ := Best(ms)
	if b.Width != 1920 || b.Refresh != 60 {
		t.Errorf("en iyi mod %v; 1920x1080@60 olmalı", b)
	}
}

// "Tazeleme hızı maks kaç destekliyorsa o olmalı" isteğinin karşılığı.
func TestEnYuksekTazeleme(t *testing.T) {
	ms := []Mode{
		mod(1920, 1080, 50, false),
		mod(1920, 1080, 60, false),
		mod(1920, 1080, 75, false),
		mod(1280, 800, 144, false),
	}
	m, ok := HighestRefresh(ms, 1920, 1080)
	if !ok || m.Refresh != 75 {
		t.Errorf("1920x1080'de en yüksek tazeleme %v; 75 Hz bekleniyordu", m)
	}
	if _, ok := HighestRefresh(ms, 3840, 2160); ok {
		t.Error("olmayan çözünürlük için mod döndü")
	}
}

// vrefresh alanı boşsa piksel saatinden hesaplanmalı: bazı sürücüler onu
// doldurmuyor ve o zaman listede "0 Hz" görünürdü.
func TestVrefreshHesaplaniyor(t *testing.T) {
	// 1920x1080@60: clock 148500 kHz, htotal 2200, vtotal 1125
	// 148500000 / (2200*1125) = 60
	m := cevir(ModeInfo{
		Clock: 148500, Hdisplay: 1920, Vdisplay: 1080,
		Htotal: 2200, Vtotal: 1125, Vrefresh: 0,
	})
	if m.Refresh != 60 {
		t.Errorf("hesaplanan tazeleme %d Hz; 60 bekleniyordu", m.Refresh)
	}
}

// ── Neden vrefresh alanına GÜVENİLMİYOR ────────────────────────────────────
//
// Çekirdek o alanı dolduruyor ama TAM SAYI Hz'e YUVARLIYOR
// (drm_mode_convert_to_umode -> drm_mode_vrefresh). 59,94 ile 60,00 ayırt
// edilemez hâle geliyor ve aynı çözünürlükte iki modu sıralarken bu fark
// önemli. Bu yüzden değer HER ZAMAN piksel saatinden hesaplanıyor.
func TestVrefreshDaimaHesaplaniyor(t *testing.T) {
	// 1920x1080@59,94: clock 148352, htotal 2200, vtotal 1125
	m := cevir(ModeInfo{
		Clock: 148352, Hdisplay: 1920, Vdisplay: 1080,
		Htotal: 2200, Vtotal: 1125,
		Vrefresh: 60, // çekirdeğin YUVARLADIĞI değer
	})
	if m.MilliHz < 59_900 || m.MilliHz > 59_990 {
		t.Errorf("mHz %d; ~59.940 bekleniyordu (yuvarlanmış 60 değil)", m.MilliHz)
	}
	if m.Refresh != 59 {
		t.Errorf("Hz %d; tam sayıya inince 59 olmalı", m.Refresh)
	}
}

// Geçmeli (interlace) modda tazeleme İKİ KATI sayılmalı: yarım kare iki kez
// taranıyor. Çekirdeğin formülü de böyle.
func TestGecmeliModTazelemesi(t *testing.T) {
	duz := refreshMilliHz(ModeInfo{Clock: 74250, Htotal: 2200, Vtotal: 1125})
	gec := refreshMilliHz(ModeInfo{Clock: 74250, Htotal: 2200, Vtotal: 1125,
		Flags: flagInterlace})
	if gec != duz*2 {
		t.Errorf("geçmeli %d, düz %d; iki katı bekleniyordu", gec, duz)
	}
}

// Sıfıra bölme olmamalı: bozuk bir mod tablosu paneli çökertmemeli.
func TestBozukModCokertmiyor(t *testing.T) {
	m := cevir(ModeInfo{Clock: 100, Htotal: 0, Vtotal: 0})
	if m.Refresh != 0 {
		t.Errorf("bozuk modda tazeleme %d; 0 bekleniyordu", m.Refresh)
	}
}

func TestBosListeGuvenli(t *testing.T) {
	if _, ok := Best(nil); ok {
		t.Error("boş listeden mod döndü")
	}
	if _, ok := HighestRefresh(nil, 1920, 1080); ok {
		t.Error("boş listeden mod döndü")
	}
}

// ── Yeni yapılar: sayfa çevirme, DIRTYFB, ADDFB2, encoder, sürüm ───────────
//
// Boyutlar include/uapi/drm/drm.h ve drm_mode.h'den (linux-6.6.32) okundu.
func TestYeniYapiBoyutlari(t *testing.T) {
	for _, tc := range []struct {
		ad    string
		got   uintptr
		bekle uintptr
	}{
		// 4 x __u32 + __u64
		{"drm_mode_crtc_page_flip", unsafe.Sizeof(pageFlipCmd{}), 24},
		// 4 x __u32 + __u64
		{"drm_mode_fb_dirty_cmd", unsafe.Sizeof(dirtyCmd{}), 24},
		// 4 x __u16
		{"drm_clip_rect", unsafe.Sizeof(clipRect{}), 8},
		// 5 x __u32 + 3 x __u32[4] (68) + 4 bayt hizalama + __u64[4] (32)
		{"drm_mode_fb_cmd2", unsafe.Sizeof(fbCmd2{}), 104},
		// 7 x __u32
		{"drm_mode_fb_cmd", unsafe.Sizeof(fbCmd{}), 28},
		// 5 x __u32
		{"drm_mode_get_encoder", unsafe.Sizeof(getEncoder{}), 20},
		// __u64 + 7 x __u32 + drm_mode_modeinfo (68)
		{"drm_mode_crtc", unsafe.Sizeof(modeCrtc{}), 104},
		// 3 x int + hizalama + 3 x (size_t + işaretçi)
		{"drm_version", unsafe.Sizeof(drmVersion{}), 64},
		// 6 x __u32 + __u64
		{"drm_mode_create_dumb", unsafe.Sizeof(createDumb{}), 32},
		// 2 x __u32 + __u64
		{"drm_mode_map_dumb", unsafe.Sizeof(mapDumb{}), 16},
		{"drm_get_cap", unsafe.Sizeof(getCap{}), 16},
	} {
		if tc.got != tc.bekle {
			t.Errorf("%s boyutu %d; çekirdekte %d", tc.ad, tc.got, tc.bekle)
		}
	}
}

// Çekirdek başlığındaki tanımlarla aynı sayılar.
func TestYeniIoctlNumaralari(t *testing.T) {
	for _, tc := range []struct {
		ad    string
		got   uintptr
		bekle uintptr
	}{
		{"PAGE_FLIP", iowr(nrPageFlip, 24), 0xC01864B0},
		{"DIRTYFB", iowr(nrDirtyFB, 24), 0xC01864B1},
		{"ADDFB2", iowr(nrAddFB2, 104), 0xC06864B8},
		{"GETENCODER", iowr(nrGetEncoder, 20), 0xC01464A6},
		{"VERSION", iowr(nrVersion, 64), 0xC0406400},
		{"SET_MASTER", io(nrSetMaster), 0x641E},
		{"DROP_MASTER", io(nrDropMaster), 0x641F},
	} {
		if tc.got != tc.bekle {
			t.Errorf("%s = %#x; %#x bekleniyordu", tc.ad, tc.got, tc.bekle)
		}
	}
}

func TestFourcc(t *testing.T) {
	// drm_fourcc.h: fourcc_code('X','R','2','4') = 0x34325258
	if fourccXRGB8888 != 0x34325258 {
		t.Errorf("XRGB8888 = %#x", fourccXRGB8888)
	}
	if fourccXBGR8888 != 0x34324258 {
		t.Errorf("XBGR8888 = %#x", fourccXBGR8888)
	}
}
