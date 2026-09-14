package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Yapılandırma dosyasının YERİ, MCOS'ta kullanıcının gördüğü en somut
// davranışlardan birini belirler: yanlış yerdeyse makine adı, Wi-Fi, saat
// dilimi, SSH ayarları ve setupComplete bayrağı her açılışta sıfırlanır ve
// kurulum sihirbazı baştan çalışır. Bu yüzden seçim mantığı test ediliyor.

func TestConfigLivesUnderDataRootByDefault(t *testing.T) {
	dir := t.TempDir()
	got := defaultConfigPath(dir)
	want := filepath.Join(dir, "config.json")
	if got != want {
		t.Fatalf("yeni kurulumda yapılandırma kalıcı dizinde olmalı:\n got %q\nwant %q", got, want)
	}
}

func TestExistingDataRootConfigWins(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(`{"setupComplete":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := defaultConfigPath(dir); got != p {
		t.Fatalf("var olan kalıcı dosya seçilmeliydi: got %q want %q", got, p)
	}
}

// Kurulu bir sistemde /etc GERÇEKTEN kalıcıdır ve orada kullanıcının eski
// ayarları olabilir. data-root'ta dosya yokken onu görmezden gelmek, sessizce
// fabrika ayarlarına dönmek demektir — bu testin koruduğu şey budur.
func TestLegacyEtcConfigIsAdoptedWhenDataRootIsEmpty(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eski yol yalnızca linux'ta geçerli")
	}
	if _, err := os.Stat(legacyConfigPath); err != nil {
		t.Skipf("bu makinede %s yok; devralma yolu denenemiyor", legacyConfigPath)
	}
	dir := t.TempDir() // içi boş: data-root'ta config.json YOK
	if got := defaultConfigPath(dir); got != legacyConfigPath {
		t.Fatalf("eski yapılandırma devralınmalıydı: got %q want %q", got, legacyConfigPath)
	}
}

// Hiçbir koşulda /etc yolu, data-root'taki dosyanın önüne geçmemeli: bu ters
// dönerse kalıcı ayarlar RAM'deki bir dosyanın gölgesinde kalır.
func TestDataRootAlwaysBeatsLegacy(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := defaultConfigPath(dir); got == legacyConfigPath {
		t.Fatal("data-root'ta dosya varken eski /etc yolu seçildi")
	}
}
