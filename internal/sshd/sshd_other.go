//go:build !linux

package sshd

// Bu dosya Linux dışı yapılar içindir.
//
// ── Neden var ───────────────────────────────────────────────────────────────
// SSH sunucusu yalnızca cihazın kendisinde anlamlı. Ama internal/model ve
// internal/daemon Windows'a da derleniyor (masaüstü düğüm uygulaması ve
// çapraz derleme denetimleri için), ve bu paket oradan da görünüyor.
//
// Buradaki uygulamalar sessizce BAŞARISIZ OLMAZ: açıkça "bu platformda yok"
// der. Sessiz bir başarı, kullanıcıya SSH'ı açtığını sanmasına yol açardı.

type process struct{}

// Available reports that no SSH server can run here.
func Available() bool { return false }

// Flavor names the implementation; none off-device.
func Flavor() string { return "" }

func (m *Manager) start(int, bool) error { return ErrUnavailable }

// Stop is a no-op: nothing was ever started.
func (m *Manager) Stop() {}

// passwordSupported: bu platformda /etc/shadow yok; Manager.SetPassword
// sessizce başarılı olmak yerine açık bir hata döner.
const passwordSupported = false

// dataVolatile: cihaz dışında kalıcılık sorusu anlamsız.
func dataVolatile(string) bool { return false }

func localAddresses() []string { return nil }
