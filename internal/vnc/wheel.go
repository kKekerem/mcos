package vnc

// Tekerlek eşlemesi platformdan bağımsız tutuluyor ki sınaması her yerde
// koşsun; uinput'a yazan kısım uinput_linux.go'da.

// Evdev göreli eksen kodları (linux/input-event-codes.h). VNC tekerleği
// çekirdeğe bu eksenlerle iletilir: düğme 4/5 dikey, 6/7 yatay.
const (
	relHWheel = 0x06
	relWheel  = 0x08
)

// wheelStep is one wheel notch to report.
type wheelStep struct {
	axis uint16
	val  int32
}

// wheelSteps returns the wheel notches implied by a button-mask change: a
// notch is counted when bit 3..6 goes from released to pressed.
func wheelSteps(prev, cur uint8) []wheelStep {
	var out []wheelStep
	for _, w := range []struct {
		bit  uint8
		axis uint16
		val  int32
	}{
		{1 << 3, relWheel, +1},  // düğme 4: yukarı
		{1 << 4, relWheel, -1},  // düğme 5: aşağı
		{1 << 5, relHWheel, -1}, // düğme 6: sola
		{1 << 6, relHWheel, +1}, // düğme 7: sağa
	} {
		if prev&w.bit == 0 && cur&w.bit != 0 {
			out = append(out, wheelStep{w.axis, w.val})
		}
	}
	return out
}
