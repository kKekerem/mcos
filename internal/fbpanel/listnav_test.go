package fbpanel

import "testing"

// ════════════════════════════════════════════════════════════════════════════
// LİSTE PENCERESİNDE GEZİNME BİLGİ SATIRLARINI ATLAR
// ════════════════════════════════════════════════════════════════════════════
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
//
// Uzaktan kontrol, ekran paylaşımı ve SSH pencereleri önce ON DÖRT satırlık
// bir açıklama, sonra eylemleri gösteriyor. İmleç 0. satırda başlıyordu: yani
// "Adres : 10.0.2.15" gibi SEÇİLEMEYEN bir metnin üstünde, vurgulu ve
// seçilebilir görünerek. Enter hiçbir şey yapmıyordu ve ilk eyleme inmek on
// dört tuş vuruşu gerektiriyordu.

func navItems() []ListItem {
	return []ListItem{
		{Label: "Adres : 10.0.2.15", Disabled: true},
		{Label: "Port  : 5900", Disabled: true},
		{Label: "", Disabled: true},
		{Label: "Yalnızca izlemeye al", Value: "viewonly"},
		{Label: "Parolayı yenile", Value: "rotate"},
		{Label: "Ekran paylaşımını kapat", Value: "disable"},
	}
}

func TestListCursorStartsOnFirstSelectable(t *testing.T) {
	m := NewListModal("Ekran Paylaşımı", "", navItems(), nil)
	if m.cursor != 3 {
		t.Fatalf("imleç %d. satırda başladı; ilk seçilebilir satır 3 — "+
			"kullanıcı seçilemeyen bir satırı vurgulu görüyor", m.cursor)
	}
}

func TestListDownSkipsInfoRows(t *testing.T) {
	a, _ := newTestApp(t)
	m := NewListModal("Ekran Paylaşımı", "", navItems(), nil)

	m.Key(a, "down")
	if m.cursor != 4 {
		t.Errorf("aşağı sonrası imleç %d; 4 olmalı", m.cursor)
	}
	m.Key(a, "down")
	if m.cursor != 5 {
		t.Errorf("aşağı sonrası imleç %d; 5 olmalı", m.cursor)
	}
	// Başa sararken bilgi satırlarına DÜŞMEMELİ.
	m.Key(a, "down")
	if m.cursor != 3 {
		t.Errorf("sarma sonrası imleç %d; 3 olmalı (bilgi satırı değil)",
			m.cursor)
	}
}

func TestListUpSkipsInfoRows(t *testing.T) {
	a, _ := newTestApp(t)
	m := NewListModal("Ekran Paylaşımı", "", navItems(), nil)

	m.Key(a, "up") // 3 -> 5 (başa sarıp son eyleme)
	if m.cursor != 5 {
		t.Errorf("yukarı sonrası imleç %d; 5 olmalı", m.cursor)
	}
	m.Key(a, "up")
	if m.cursor != 4 {
		t.Errorf("yukarı sonrası imleç %d; 4 olmalı", m.cursor)
	}
	m.Key(a, "up")
	if m.cursor != 3 {
		t.Errorf("yukarı sonrası imleç %d; 3 olmalı", m.cursor)
	}
}

// "Şu an uygulanan" satır varsa imleç oraya gider: bu davranış korunmalı.
func TestListCursorPrefersCurrentRow(t *testing.T) {
	items := []ListItem{
		{Label: "başlık", Disabled: true},
		{Label: "Grafit Teal"},
		{Label: "Kehribar", Current: true},
	}
	m := NewListModal("Tema", "", items, nil)
	if m.cursor != 2 {
		t.Errorf("imleç %d; uygulanan satır olan 2 olmalı", m.cursor)
	}
}

// Hepsi bilgi satırıysa gezinme SONSUZ DÖNGÜYE girmemeli.
func TestListAllInfoRowsDoesNotHang(t *testing.T) {
	a, _ := newTestApp(t)
	m := NewListModal("Bilgi", "", []ListItem{
		{Label: "bir", Disabled: true},
		{Label: "iki", Disabled: true},
	}, nil)
	m.Key(a, "down")
	m.Key(a, "up")
	if m.cursor != 0 {
		t.Errorf("imleç %d; seçilebilir satır yokken yerinde kalmalı", m.cursor)
	}
}
