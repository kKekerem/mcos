package main

import "syscall"

// raiseFileLimit, açık dosya sınırını izin verilen en yükseğe çeker.
//
// NEDEN: Minecraft sunucusunda her oyuncu bir soket, her bölge dosyası bir
// tanımlayıcıdır. Varsayılan yumuşak sınır 1024'tür ve sunucu süreçleri
// daemon'dan DEVRALIR: birkaç yüz oyuncu ve eklentiyle sunucu yeni
// bağlantıları "Too many open files" ile reddetmeye başlardı. Sert sınır
// da düşükse (busybox init 4096 bırakabilir) önce sert sınır, o olmazsa
// yalnızca yumuşak sınır yükseltilir.
func raiseFileLimit() uint64 {
	var r syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &r); err != nil {
		return 0
	}
	const hedef = 1 << 20
	if r.Max < hedef {
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &syscall.Rlimit{Cur: hedef, Max: hedef}); err == nil {
			return hedef
		}
	}
	r.Cur = r.Max
	_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &r)
	_ = syscall.Getrlimit(syscall.RLIMIT_NOFILE, &r)
	return r.Cur
}
