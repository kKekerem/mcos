package vnc

import "testing"

// RFB'de tekerlek adımı düğme 4-7'nin BASILMASIDIR. Ölçülen hata: bu
// düğmeler BTN_GEAR_DOWN/UP tuşlarına çevriliyordu ve panel onları hiç
// tanımıyordu — VNC'de tekerlek ölüydü. Artık REL_WHEEL/REL_HWHEEL.
func TestWheelStepsBasmadaSayilir(t *testing.T) {
	cases := []struct {
		ad        string
		prev, cur uint8
		want      []wheelStep
	}{
		{"yukarı bas", 0, 1 << 3, []wheelStep{{relWheel, +1}}},
		{"yukarı bırak (sayılmaz)", 1 << 3, 0, nil},
		{"aşağı bas", 0, 1 << 4, []wheelStep{{relWheel, -1}}},
		{"sol düğme basılıyken aşağı", 1, 1 | 1<<4, []wheelStep{{relWheel, -1}}},
		{"sola kaydır", 0, 1 << 5, []wheelStep{{relHWheel, -1}}},
		{"sağa kaydır", 0, 1 << 6, []wheelStep{{relHWheel, +1}}},
		{"yalnızca sol tık", 0, 1, nil},
		{"basılı kalan adım yeniden sayılmaz", 1 << 3, 1 << 3, nil},
	}
	for _, c := range cases {
		got := wheelSteps(c.prev, c.cur)
		if len(got) != len(c.want) {
			t.Errorf("%s: %v, beklenen %v", c.ad, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: %v, beklenen %v", c.ad, got, c.want)
			}
		}
	}
}
