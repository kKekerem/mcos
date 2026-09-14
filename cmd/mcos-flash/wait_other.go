//go:build !windows

package main

// waitOnWindows is a no-op outside Windows.
//
// Linux/macOS'ta program bir terminalden çalıştırılır ve çıkış satırı
// ekranda kalır; bekletmek yalnızca can sıkardı.
func waitOnWindows() {}
