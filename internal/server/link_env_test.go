package server

import (
	"net/url"
	"strings"
	"testing"

	"mcos/internal/model"
)

// MCOS_LINK_COORDINATOR, eklentinin taban URL'sidir: Java tarafı
// URI.create(taban + "/link/topology") çağırır. Şemasız bir değer
// ("127.0.0.1:27892") Java'da "Illegal character in scheme name" hatası
// verir ve mod koordinatöre HİÇ bağlanamaz — ortak dünya her sunucuda
// sessizce kapalı kalır. Go'nun url.Parse'ı aynı girdiyi aynı nedenle
// reddettiği için burada birebir sınanabiliyor.
func TestKoordinatorAdresiMutlakURL(t *testing.T) {
	var val string
	for _, e := range linkEnv(&model.Server{ID: "s1"}) {
		if strings.HasPrefix(e, "MCOS_LINK_COORDINATOR=") {
			val = strings.TrimPrefix(e, "MCOS_LINK_COORDINATOR=")
		}
	}
	if val == "" {
		t.Fatal("MCOS_LINK_COORDINATOR ortamda yok")
	}
	u, err := url.Parse(val + "/link/topology")
	if err != nil {
		t.Fatalf("eklentinin kuracağı URL çözülemiyor (%q): %v", val, err)
	}
	if u.Scheme != "http" || u.Host != "127.0.0.1:27892" {
		t.Fatalf("koordinatör adresi yanlış: şema=%q host=%q (değer %q)",
			u.Scheme, u.Host, val)
	}
}
