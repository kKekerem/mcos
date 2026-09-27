//go:build !linux

package vnc

import "errors"

// Geliştirme makinesinde uinput YOKTUR: MCOS'un girdi enjeksiyonu doğrudan
// Linux çekirdeğine konuşuyor. VNC sunucusu yine derlenip çalışır, yalnızca
// İZLEME kipinde (girdi iletilmez).
func NewInjector(w, h int) (Injector, error) {
	return nil, errors.New("uinput yalnızca Linux'ta")
}
