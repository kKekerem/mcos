//go:build !linux

package sound

// Geliştirme makinesinde (Windows/macOS) ses YOKTUR.
//
// MCOS'un ses yolu doğrudan Linux ses aygıtlarına konuşuyor; geliştiricinin
// makinesinde hoparlör çalmak ne isteniyor ne de taşınabilir. Panel yine de
// derlenip çalışmalı, bu yüzden arka uç bulunamaz ve Play() sessizce yutulur.
func probe() backend { return nil }
