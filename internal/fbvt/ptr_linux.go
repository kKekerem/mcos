//go:build linux

package fbvt

import "unsafe"

// ptrOf returns an unsafe pointer to v.
//
// Ayri bir dosyada: unsafe kullanimi tek bir yerde toplansin ve gerisi
// denetlenebilir kalsin.
func ptrOf(v *int32) unsafe.Pointer { return unsafe.Pointer(v) }
