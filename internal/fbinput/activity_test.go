package fbinput

import "testing"

// TestInputEventSize — struct input_event boyutu amd64'te 24 bayt olmali.
//
// Yanlis boyut, olay akisini KAYDIRIR: her okumada tur/kod alanlari yanlis
// ofsetten okunur ve fare hareketi ya hic gorulmez ya da rastgele
// yorumlanir. Bu sabit sessizce yanlis olabilecegi icin yaziyla sabitlendi.
func TestInputEventSize(t *testing.T) {
	// timeval(16) + type(2) + code(2) + value(4)
	const want = 16 + 2 + 2 + 4
	if inputEventSize != want {
		t.Errorf("inputEventSize = %d, %d olmali", inputEventSize, want)
	}
}

// TestWatchActivityNeverFails — aygit yoksa bile panel calismali.
func TestWatchActivityNeverFails(t *testing.T) {
	a := WatchActivity()
	if a == nil {
		t.Fatal("WatchActivity nil dondu")
	}
	defer a.Close()
	a.Touch()
	if a.Idle() > 0 && a.Devices() == 0 {
		// Aygit yoksa Touch yine de sayaci sifirlamali.
		t.Logf("aygit yok, bosta: %v", a.Idle())
	}
	if err := a.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	// Iki kez kapatmak panik yapmamali.
	_ = a.Close()
}
