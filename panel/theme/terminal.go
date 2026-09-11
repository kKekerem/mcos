package theme

import (
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// SetupTerminal performs the ONE-TIME global terminal configuration and
// installs the theme's palette.
//
// Program açılışında bir kez çağrılır (cmd/mcos-panel). Eskiden
// lipgloss.SetColorProfile çağrısı theme.New() içindeydi; yani her tema
// nesnesi oluşturulduğunda global durum yeniden yazılıyordu. Global yan etki
// kurucunun içinde olmamalı.
//
// Yaptığı iki şey:
//
//  1. Renk profilini termenv.ANSI'ye sabitler. Bu ZORUNLU: fbterm 256 renk
//     SGR'sini (ESC[38;5;Nm) desteklemez — "case 38" dizisini altçizgi olarak
//     yorumlar (src/lib/vterm_action.cpp:522). Otomatik algılamaya bırakılsa
//     TERM=fbterm için terminfo girdisi de bulunmadığından profil Ascii'ye
//     düşer ve tüm renkler kaybolur.
//
//  2. Temanın 16 renklik paletini fbterm'e yükler. .fbtermrc içindeki
//     "color-N=" satırları fbterm tarafından okunmaz (src/fbconfig.cpp yalnızca
//     color-foreground / color-background tanır), bu yüzden paleti çalışma
//     zamanında Linux konsol escape dizisiyle yüklemek TEK yoldur. Aksi halde
//     fbterm'in gömülü VGA paleti kullanılır ve örneğin kenarlık yuvası
//     #aa00aa magenta çıkar.
//
// fbterm dışındaki terminallerde palet dizileri sessizce yok sayılır.
func SetupTerminal(w io.Writer, themeName string) {
	lipgloss.SetColorProfile(termenv.ANSI)
	ApplyTheme(w, themeName)
}

// ApplyTheme re-installs just the palette, for use when the user switches
// theme at runtime. Renk profili yeniden ayarlanmaz.
//
// Palet komutu yalnızca fbterm'de anlamlıdır ve başka terminallerde ekrana
// çöp basardı, bu yüzden TERM kontrolü burada yapılır — çağıranların bunu
// tekrar etmesi gerekmez.
func ApplyTheme(w io.Writer, themeName string) {
	if w == nil || os.Getenv("TERM") != "fbterm" {
		return
	}
	if !Valid(themeName) {
		themeName = defaultTheme
	}
	_ = InstallPalette(w, themeName)
}
