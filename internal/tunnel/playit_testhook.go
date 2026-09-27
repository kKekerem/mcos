package tunnel

// SetPlayitBinsForTest points the agent at fake playitd/playit-cli binaries
// and returns a function that restores the real paths.
//
// YALNIZCA SINAMALAR İÇİN: daemon paketindeki uçtan uca sınama (ajan başlar →
// eşitleyici tüneli açar → adres playit.status'a düşer) gerçek bir hesap ve
// /usr/bin/playitd olmadan koşabilsin diye. İmajda kimse çağırmaz.
func SetPlayitBinsForTest(daemonBin, cliBin string) (restore func()) {
	oldD, oldC := playitDaemonBin, playitCLIBin
	playitDaemonBin, playitCLIBin = daemonBin, cliBin
	return func() { playitDaemonBin, playitCLIBin = oldD, oldC }
}
