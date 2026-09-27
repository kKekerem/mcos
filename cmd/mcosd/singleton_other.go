//go:build !linux

package main

import "os"

// Geliştirme makinesinde kilit YOK: orada tek bir daemon elle çalıştırılıyor
// ve flock'un taşınabilir karşılığı platforma göre değişiyor. Hedef sistem
// (Linux) korumalı; geliştirme makinesinde ikinci bir örnek yalnızca
// geliştiricinin kendi sorunudur.
func tryLock(*os.File) error { return nil }
