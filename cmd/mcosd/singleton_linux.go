//go:build linux

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// tryLock takes a non-blocking exclusive advisory lock.
//
// flock BİLEREK seçildi (fcntl yerine): flock kilidi AÇIK DOSYA TANIMINA
// bağlıdır ve süreç ölünce çekirdek onu bırakır. Yani çöken bir daemon,
// bir daha açılmayan bir kilit dosyası bırakmaz — "makine açılmıyor" sınıfı
// bir arıza doğurmaz.
func tryLock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
