//go:build !linux

package sound

// Linux dışında ses seviyesi denetimi yok: MCOS yalnızca Linux'ta çalışıyor,
// bu dosya geliştirme makinesinde derlemenin sürmesi için var.

func Volume() int             { return 0 }
func SetVolume(v int) int     { return 0 }
func VolumeUp() int           { return 0 }
func VolumeDown() int         { return 0 }
func ToggleMute() (int, bool) { return 0, false }
func ReadVolume() int         { return 0 }
