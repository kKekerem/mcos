//go:build !linux

package supervisor

// Limits describes the OS-level resource ceiling for one server process.
//
// Linux disinda kaynak sinirlama yok; gelistirme makinesinde derlensin diye
// ayni tur burada da tanimli.
type Limits struct {
	CPUPercent int
	MemoryMB   int
	IOWeight   int
}

// ApplyCgroup is a no-op outside Linux.
func ApplyCgroup(string, int, Limits) string { return "" }

// RemoveCgroup is a no-op outside Linux.
func RemoveCgroup(string) {}

// CgroupStatus reports no controllers outside Linux.
func CgroupStatus() (bool, []string) { return false, nil }
