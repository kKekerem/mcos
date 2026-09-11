//go:build !linux

package files

// DetectUSB always reports "no USB" off Linux: tespit /sys/block ve
// /proc/mounts okumasına dayanır. Geliştirici makinesinde USB bölümü
// sidebar'da görünmez, bu da doğru davranıştır.
func DetectUSB() USBInfo { return USBInfo{} }
