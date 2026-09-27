//go:build linux

package fbinput

import "testing"

// Ses aç/kıs basılı tutulunca tekrarlamalı; sessiz yalnızca basışta (tekrarı
// her 30 ms'de bir aç/kapa yapardı). Bırakma hiçbir şey yapmamalı.
func TestMedyaTusuKurallari(t *testing.T) {
	for _, tc := range []struct {
		code uint16
		val  int32
		ad   string
		ok   bool
	}{
		{keyVolumeUp, 1, "volumeup", true},
		{keyVolumeUp, 2, "volumeup", true},
		{keyVolumeUp, 0, "volumeup", false},
		{keyVolumeDown, 1, "volumedown", true},
		{keyMute, 1, "mute", true},
		{keyMute, 2, "mute", false},
		{30 /* KEY_A */, 1, "", false},
	} {
		ad, ok := mediaKey(tc.code, tc.val)
		if ok != tc.ok || (ok && ad != tc.ad) {
			t.Errorf("kod %d değer %d: (%q,%v), (%q,%v) bekleniyordu", tc.code, tc.val, ad, ok, tc.ad, tc.ok)
		}
	}
}
