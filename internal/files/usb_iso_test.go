package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindISOs(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"mcos.iso", "ISO/yeni.ISO", "a/b/derin.iso", ".gizli.iso", "not.txt"} {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := findISOs(root)
	for i := range got {
		got[i] = filepath.ToSlash(got[i])
	}
	// Kök + bir klasör; derindeki ve gizli dosya listelenmez.
	if s := strings.Join(got, ","); s != "ISO/yeni.ISO,mcos.iso" {
		t.Errorf("findISOs = %s", s)
	}
}
