//go:build !linux && !windows

package flash

import (
	"errors"
	"runtime"
)

// Enumerate is unavailable on this platform.
//
// macOS'ta bu, IOKit sorgusu veya `diskutil list -plist` ayrıştırması ister;
// BSD'lerde geoms taranır. İkisi de doğru yapılmadığında SİSTEM DİSKİNİ
// gizleyemez — ve sistem diskini gizleyemeyen bir flaşlama aracı tehlikelidir.
//
// Bu yüzden sessizce boş liste döndürmüyoruz: kullanıcı neden hiçbir aygıt
// görmediğini bilmeli.
func Enumerate(bool) ([]Device, error) {
	return nil, errors.New(
		"aygıt listeleme " + runtime.GOOS + " üzerinde desteklenmiyor " +
			"(Linux ve Windows destekleniyor)")
}
